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
	Retention       float64 `json:"retention"`         // share of the questions each floor must keep
	CoverageSample  int     `json:"coverage_sample"`   // questions with a gold record among the judged candidates
	CosineSample    int     `json:"cosine_sample"`     // ... among the vector hits
	MinTermCoverage float64 `json:"min_term_coverage"` // chosen floor
	MinCosine       float64 `json:"min_cosine"`        // chosen floor (> 1: no gold record was a vector hit)
	Kept            int     `json:"kept"`              // questions that keep a gold record at both floors
	NoisePassing    int     `json:"noise_passing"`     // non-gold candidates that pass at the floors
	NoiseTotal      int     `json:"noise_total"`       // non-gold candidates judged
}

// CalibrateRetention is the share of the training questions whose gold
// evidence must pass each floor: the BASE.md abstention target (95%) read
// from the answerable side.
const CalibrateRetention = 0.95

// CalibrateEvidence chooses the evidence floors of the context cut
// (read.min_term_coverage, read.min_cosine) for the system's embedding
// model, from the training split only (ds.Training(), never the evaluated
// questions). Every training question is answerable, so the procedure is
// one-class and per signal: for each training question it takes the best
// coverage and the best cosine of its gold records among the candidates
// the cut judged, and sets each floor at the largest value that the best
// gold record of at least retention of those questions reaches (the 5th
// percentile for 95%). Each signal thus admits what it typically gives
// real evidence, on its own scale. A joint criterion (fewest non-gold
// candidates at a joint retention) was rejected: the training split holds
// only plainly worded questions, on which lexical coverage alone suffices,
// so it would always switch the semantic signal off, a bias of the split
// rather than a property of the model. The joint retention and the non-gold
// candidates passing are reported for information.
func CalibrateEvidence(ctx context.Context, p EvidenceProber, ds *Dataset, budget int, retention float64) (Calibration, error) {
	type qc struct {
		cands []EvidenceCandidate
		gold  []bool
	}
	var qs []qc
	cal := Calibration{System: p.Name(), Retention: retention}
	if d, ok := p.(Describer); ok {
		cal.ModelID = d.Info().EmbeddingModel
	}
	var bestCov, bestCos []float64
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
		cov, cos, hasGold, hasCos := 0.0, 0.0, false, false
		for i, c := range cs {
			for _, g := range q.Gold {
				if Hits(c.Retrieved, g) {
					x.gold[i] = true
				}
			}
			if !x.gold[i] {
				cal.NoiseTotal++
				continue
			}
			if !hasGold || c.Coverage > cov {
				cov, hasGold = c.Coverage, true
			}
			if c.HasCosine && (!hasCos || c.Cosine > cos) {
				cos, hasCos = c.Cosine, true
			}
		}
		if hasGold {
			bestCov = append(bestCov, cov)
		}
		if hasCos {
			bestCos = append(bestCos, cos)
		}
		qs = append(qs, x)
		cal.Questions++
	}
	cal.CoverageSample, cal.CosineSample = len(bestCov), len(bestCos)
	if len(bestCov) == 0 {
		return cal, fmt.Errorf("calibration: no training question has a gold record among the judged candidates")
	}
	cal.MinTermCoverage = quantileFloor(bestCov, retention)
	cal.MinCosine = 1.01 // no gold vector hit: the semantic side never suffices
	if len(bestCos) > 0 {
		cal.MinCosine = quantileFloor(bestCos, retention)
	}
	for _, x := range qs {
		hit := false
		for i, c := range x.cands {
			if c.Coverage >= cal.MinTermCoverage || (c.HasCosine && c.Cosine >= cal.MinCosine) {
				if x.gold[i] {
					hit = true
				} else {
					cal.NoisePassing++
				}
			}
		}
		if hit {
			cal.Kept++
		}
	}
	return cal, nil
}

// quantileFloor is the largest v in vs such that at least share of vs are
// ≥ v.
func quantileFloor(vs []float64, share float64) float64 {
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	need := int(math.Ceil(share * float64(len(s))))
	if need < 1 {
		need = 1
	}
	return s[len(s)-need]
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
	fmt.Fprintf(w, "calibration on the training split (%d questions), system %s, model %s, %.0f%% of the gold records per signal:\n",
		c.Questions, c.System, c.ModelID, 100*c.Retention)
	fmt.Fprintf(w, "  coverage floor from %d questions, cosine floor from %d (questions with a gold record among the judged candidates / vector hits)\n",
		c.CoverageSample, c.CosineSample)
	fmt.Fprintf(w, "  at both floors: %d questions keep a gold record; %d of %d non-gold candidates pass\n", c.Kept, c.NoisePassing, c.NoiseTotal)
	fmt.Fprintf(w, "read:\n  evidence_floors:\n    %q: {min_term_coverage: %s, min_cosine: %s}\n", c.ModelID, fmtNum(c.MinTermCoverage), fmtNum(c.MinCosine))
}
