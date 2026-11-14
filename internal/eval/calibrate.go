package eval

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
)

// EvidenceCandidate is one candidate of a read that reached the relevance
// cut (it passed the rule filter), with the evidence features the cut tests
// (decision 0020): lexical coverage and, for vector hits, the cosine.
type EvidenceCandidate struct {
	Retrieved
	Coverage  float64
	Cosine    float64
	HasCosine bool
}

// EvidenceProber is implemented by a system whose read path cuts by
// evidence (C1..C5): Evidence runs one context query and returns every
// candidate the cut judged, whatever the cut decided.
type EvidenceProber interface {
	System
	Evidence(ctx context.Context, q Query, budgetTokens int) ([]EvidenceCandidate, error)
}

// Calibration is the result of CalibrateEvidence: the evidence floors for
// one embedding model, chosen on the training split only.
type Calibration struct {
	System          string  `json:"system"`
	ModelID         string  `json:"model_id"`
	Questions       int     `json:"questions"`         // training questions with gold
	Reachable       int     `json:"reachable"`         // ... with a gold record among the judged candidates
	Retention       float64 `json:"retention"`         // target share of Reachable that keeps a gold record
	MinTermCoverage float64 `json:"min_term_coverage"` // chosen floor
	MinCosine       float64 `json:"min_cosine"`        // chosen floor (> 1: the semantic side never suffices)
	Kept            int     `json:"kept"`              // training questions that keep a gold record at the floors
	NoisePassing    int     `json:"noise_passing"`     // non-gold candidates that pass at the floors
	NoiseTotal      int     `json:"noise_total"`       // non-gold candidates judged
}

// CalibrateRetention is the share of the training questions whose gold
// evidence was retrieved that must keep it through the cut: the BASE.md
// abstention target (95%) read from the other side.
const CalibrateRetention = 0.95

// CalibrateEvidence chooses the evidence floors of the context cut
// (read.min_term_coverage, read.min_cosine) for the system's embedding
// model, from the training split only (ds.Training(), never the evaluated
// questions). Every training question is answerable, so the procedure is
// one-class: among the floor pairs that keep a gold record for at least
// retention × the questions whose gold record was retrieved at all, it
// picks the one that lets the fewest non-gold candidates through (the
// candidates an unanswerable question would deliver); ties go to the
// higher coverage floor, then the higher cosine floor. Candidate floors are
// the coverage and cosine values observed on the training candidates, plus
// one above every cosine (semantic evidence off).
func CalibrateEvidence(ctx context.Context, p EvidenceProber, ds *Dataset, budget int, retention float64) (Calibration, error) {
	type qc struct {
		cands []EvidenceCandidate
		gold  []bool
	}
	var qs []qc
	covSet, cosSet := map[float64]bool{}, map[float64]bool{}
	cal := Calibration{System: p.Name(), Retention: retention}
	if d, ok := p.(Describer); ok {
		cal.ModelID = d.Info().EmbeddingModel
	}
	for _, q := range ds.Training() {
		if !q.Answerable() {
			continue
		}
		query := Query{ID: q.ID, Text: q.Question}
		if q.AsOf != "" {
			t, err := ItemTime(q.AsOf)
			if err != nil {
				return cal, err
			}
			query.AsOf = t
		}
		cs, err := p.Evidence(ctx, query, budget)
		if err != nil {
			return cal, fmt.Errorf("evidence %s: %w", q.ID, err)
		}
		x := qc{cands: cs, gold: make([]bool, len(cs))}
		for i, c := range cs {
			for _, g := range q.Gold {
				if Hits(c.Retrieved, g) {
					x.gold[i] = true
				}
			}
			covSet[c.Coverage] = true
			if c.HasCosine {
				cosSet[c.Cosine] = true
			}
		}
		qs = append(qs, x)
		cal.Questions++
	}
	floats := func(m map[float64]bool) []float64 {
		out := make([]float64, 0, len(m)+1)
		for v := range m {
			out = append(out, v)
		}
		sort.Float64s(out)
		return out
	}
	covs, coss := floats(covSet), floats(cosSet)
	off := 1.0
	if len(coss) > 0 {
		off = math.Max(1, coss[len(coss)-1]) + 0.01
	}
	coss = append(coss, off)
	if len(covs) == 0 {
		covs = []float64{0}
	}
	covs = append(covs, 1.01)
	pass := func(c EvidenceCandidate, a, b float64) bool {
		return c.Coverage >= a || (c.HasCosine && c.Cosine >= b)
	}
	for _, x := range qs {
		if slicesAny(x.gold) {
			cal.Reachable++
		}
		for i := range x.cands {
			if !x.gold[i] {
				cal.NoiseTotal++
			}
		}
	}
	need := int(math.Ceil(retention * float64(cal.Reachable)))
	best := false
	for _, a := range covs {
		for _, b := range coss {
			kept, noise := 0, 0
			for _, x := range qs {
				hit := false
				for i, c := range x.cands {
					if !pass(c, a, b) {
						continue
					}
					if x.gold[i] {
						hit = true
					} else {
						noise++
					}
				}
				if hit {
					kept++
				}
			}
			if kept < need {
				continue
			}
			better := !best || noise < cal.NoisePassing ||
				(noise == cal.NoisePassing && (a > cal.MinTermCoverage || (a == cal.MinTermCoverage && b > cal.MinCosine)))
			if better {
				best = true
				cal.MinTermCoverage, cal.MinCosine, cal.Kept, cal.NoisePassing = a, b, kept, noise
			}
		}
	}
	if !best {
		return cal, fmt.Errorf("calibration: no floor keeps %d of %d training questions", need, cal.Reachable)
	}
	return cal, nil
}

func slicesAny(bs []bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

// WriteCalibration prints c as the read configuration it recommends.
func WriteCalibration(w io.Writer, c Calibration) {
	fmt.Fprintf(w, "calibration on the training split (%d questions, %d with a gold record among the judged candidates), system %s, model %s:\n",
		c.Questions, c.Reachable, c.System, c.ModelID)
	fmt.Fprintf(w, "  kept a gold record: %d (target %.0f%% of %d)\n", c.Kept, 100*c.Retention, c.Reachable)
	fmt.Fprintf(w, "  non-gold candidates passing: %d of %d\n", c.NoisePassing, c.NoiseTotal)
	fmt.Fprintf(w, "read:\n  evidence_floors:\n    %q: {min_term_coverage: %s, min_cosine: %s}\n", c.ModelID, fmtNum(c.MinTermCoverage), fmtNum(c.MinCosine))
}
