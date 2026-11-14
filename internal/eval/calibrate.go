package eval

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
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
// one embedding model, chosen on the training split only (decision 0124).
type Calibration struct {
	System          string  `json:"system"`
	ModelID         string  `json:"model_id"`
	Answerable      int     `json:"answerable"`        // training questions with gold
	Judged          int     `json:"judged"`            // ... with a gold record among the judged candidates
	Unanswerable    int     `json:"unanswerable"`      // training questions without gold
	MinTermCoverage float64 `json:"min_term_coverage"` // chosen floor (> 1: coverage never suffices)
	MinCosine       float64 `json:"min_cosine"`        // chosen floor (> 1: cosine never suffices)
	Kept            int     `json:"kept"`              // judged answerable questions with a gold record passing
	Filled          int     `json:"filled"`            // unanswerable questions with a candidate passing
	Cost            int     `json:"cost"`              // (Judged - Kept) + Filled, the criterion
	EmptyAnswerable int     `json:"empty_answerable"`  // answerable questions with nothing passing
	NoisePassing    int     `json:"noise_passing"`     // non-gold candidates of answerable questions passing
	NoiseTotal      int     `json:"noise_total"`       // non-gold candidates judged
	// By tag of the training split, the same counts (paraphrase, plain).
	ByTag map[string]TagCalibration `json:"by_tag"`
	// Signals summarises, per population, what the floors are chosen from.
	Signals []SignalSummary `json:"signals"`
}

// TagCalibration counts kept gold for one group of answerable questions.
type TagCalibration struct {
	Questions int `json:"questions"`
	Judged    int `json:"judged"`
	Kept      int `json:"kept"`
}

// SignalSummary describes one population of the training split: for
// answerable questions the best gold coverage and cosine, for unanswerable
// ones the best coverage and cosine of any candidate (what must stay below
// the floors for the result to be empty).
type SignalSummary struct {
	Population string    `json:"population"`
	N          int       `json:"n"`
	Coverage   []float64 `json:"coverage_p10_p50_p90"`
	Cosine     []float64 `json:"cosine_p10_p50_p90"`
}

// CalibrateEvidence chooses the evidence floors of the context cut
// (read.min_term_coverage, read.min_cosine) for the system's embedding
// model, from the training split only (ds.Training(), never the evaluated
// or gate questions). The training split holds plainly worded, paraphrased
// and unanswerable questions (decision 0123), so the criterion weighs both
// errors of the cut (decision 0124):
//
//   - an answerable question whose gold record is among the judged
//     candidates is lost when no gold record passes the floors;
//   - an unanswerable question is filled when any candidate passes.
//
// Each error costs one, on the natural mix of the split (which mirrors the
// evaluated mix). The floors minimise lost + filled over every pair of
// candidate floors: coverage at each value observed (or off), cosine at the
// midpoint between consecutive observed values (or off). Ties go to fewer
// filled unanswerable questions (principle 8), then to fewer non-gold
// candidates of answerable questions passing (the same decisions with less
// filler: principle 8 again), then to the lower floors. A candidate passes
// when coverage or cosine reaches its floor (decision 0020): either signal
// suffices on its own.
func CalibrateEvidence(ctx context.Context, p EvidenceProber, ds *Dataset, budget int) (Calibration, error) {
	cal := Calibration{System: p.Name(), ByTag: map[string]TagCalibration{}}
	if d, ok := p.(Describer); ok {
		cal.ModelID = d.Info().EmbeddingModel
	}
	var qs []calQuestion
	for _, q := range ds.Training() {
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
		x := calQuestion{answerable: q.Answerable(), tag: "plain", cands: cs, gold: make([]bool, len(cs))}
		if q.HasTag(TagParaphrase) {
			x.tag = TagParaphrase
		}
		for i, c := range cs {
			for _, g := range q.Gold {
				if Hits(c.Retrieved, g) {
					x.gold[i] = true
				}
			}
		}
		qs = append(qs, x)
	}
	return chooseFloors(cal, qs)
}

// calQuestion is one training question with the candidates its read judged.
type calQuestion struct {
	answerable bool
	tag        string
	cands      []EvidenceCandidate
	gold       []bool
}

// floorOff disables a signal: coverage and cosine never exceed 1.
const floorOff = 1.01

func chooseFloors(cal Calibration, qs []calQuestion) (Calibration, error) {
	covSet, cosSet := map[float64]bool{floorOff: true}, map[float64]bool{}
	for _, q := range qs {
		for _, c := range q.cands {
			covSet[c.Coverage] = true
			if c.HasCosine {
				cosSet[c.Cosine] = true
			}
		}
	}
	covs := sortedFloats(covSet)
	cosObs := sortedFloats(cosSet)
	coss := []float64{floorOff}
	if len(cosObs) > 0 {
		coss = append(coss, cosObs[0])
		for i := 1; i < len(cosObs); i++ {
			coss = append(coss, (cosObs[i-1]+cosObs[i])/2)
		}
	}
	sort.Float64s(coss)
	if len(qs) == 0 {
		return cal, fmt.Errorf("calibration: the training split is empty")
	}
	// scores[i][j]: (cost, filled) at covs[i], coss[j].
	type score struct{ cost, filled int }
	less := func(a, b score) bool { return a.cost < b.cost || (a.cost == b.cost && a.filled < b.filled) }
	scores := make([][]score, len(covs))
	best := score{cost: math.MaxInt}
	for i, c := range covs {
		// Per question at this coverage floor: does lexical evidence alone
		// decide it, and otherwise which cosine would.
		type qc struct {
			answerable, judged, lex bool
			cos                     float64 // best cosine of gold (answerable) or of any candidate
		}
		pre := make([]qc, len(qs))
		for k, q := range qs {
			x := qc{answerable: q.answerable, cos: math.Inf(-1)}
			for n, cd := range q.cands {
				if q.answerable && !q.gold[n] {
					continue
				}
				x.judged = true
				if cd.Coverage >= c {
					x.lex = true
				}
				if cd.HasCosine {
					x.cos = math.Max(x.cos, cd.Cosine)
				}
			}
			pre[k] = x
		}
		scores[i] = make([]score, len(coss))
		for j, s := range coss {
			var sc score
			for _, x := range pre {
				pass := x.lex || x.cos >= s
				switch {
				case x.answerable && x.judged && !pass:
					sc.cost++
				case !x.answerable && x.judged && pass:
					sc.cost++
					sc.filled++
				}
			}
			scores[i][j] = sc
			if less(sc, best) {
				best = sc
			}
		}
	}
	// Among the optimal pairs, the one that lets the fewest non-gold
	// candidates of answerable questions through (the same decisions on the
	// training questions, less filler in the context); then the lower
	// coverage floor, then the lower cosine floor.
	bc, bs, bn := 0.0, 0.0, math.MaxInt
	for i, c := range covs {
		lex := 0
		var rest []float64 // cosines of the noise below the coverage floor
		for _, q := range qs {
			if !q.answerable {
				continue
			}
			for n, cd := range q.cands {
				switch {
				case q.gold[n]:
				case cd.Coverage >= c:
					lex++
				case cd.HasCosine:
					rest = append(rest, cd.Cosine)
				}
			}
		}
		sort.Float64s(rest)
		for j, s := range coss {
			if scores[i][j] != best {
				continue
			}
			noise := lex + len(rest) - sort.SearchFloat64s(rest, s)
			if noise < bn {
				bc, bs, bn = c, s, noise
			}
		}
	}
	if bs > 1 {
		bs = floorOff
	}
	// Floors are published with four decimals, unless rounding would cross
	// an observed value (and so change a decision).
	bs = roundFloor(bs, cosObs)
	if bc < floorOff {
		// Coverage values are fractions k/m with m <= coverage_term_cap: four
		// decimals rounded down keep every one that passed.
		bc = math.Floor(bc*1e4) / 1e4
	}
	r := evaluateFloors(qs, bc, bs)
	cal.MinTermCoverage, cal.MinCosine = bc, bs
	cal.Answerable, cal.Judged, cal.Unanswerable = r.Answerable, r.Judged, r.Unanswerable
	cal.Kept, cal.Filled, cal.EmptyAnswerable = r.Kept, r.Filled, r.EmptyAnswerable
	cal.NoisePassing, cal.NoiseTotal = r.NoisePassing, r.NoiseTotal
	cal.Cost = (r.Judged - r.Kept) + r.Filled
	cal.ByTag = r.ByTag
	cal.Signals = summarise(qs)
	return cal, nil
}

// roundFloor rounds v to four decimals unless an observed value would pass
// one of v and its rounding and not the other, in which case v is kept.
func roundFloor(v float64, observed []float64) float64 {
	if v >= floorOff {
		return v
	}
	r := math.Round(v*1e4) / 1e4
	lo, hi := math.Min(v, r), math.Max(v, r)
	for _, o := range observed {
		if o >= lo && o < hi {
			return v
		}
	}
	return r
}

// fmtFloor prints a floor with four decimals when that is exact.
func fmtFloor(v float64) string {
	if v == round4(v) {
		return fmtNum(v)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// evaluateFloors counts the decisions of the cut at floors (c, s).
func evaluateFloors(qs []calQuestion, c, s float64) Calibration {
	r := Calibration{ByTag: map[string]TagCalibration{}}
	for _, q := range qs {
		passAny, goldJudged, goldPass := false, false, false
		for i, x := range q.cands {
			pass := x.Coverage >= c || (x.HasCosine && x.Cosine >= s)
			passAny = passAny || pass
			if q.gold[i] {
				goldJudged = true
				goldPass = goldPass || pass
			} else if q.answerable {
				r.NoiseTotal++
				if pass {
					r.NoisePassing++
				}
			}
		}
		if !q.answerable {
			r.Unanswerable++
			if passAny {
				r.Filled++
			}
			continue
		}
		r.Answerable++
		t := r.ByTag[q.tag]
		t.Questions++
		if !passAny {
			r.EmptyAnswerable++
		}
		if goldJudged {
			r.Judged++
			t.Judged++
			if goldPass {
				r.Kept++
				t.Kept++
			}
		}
		r.ByTag[q.tag] = t
	}
	return r
}

// summarise reports, per population, the 10th, 50th and 90th percentiles of
// the best coverage and best cosine: of the gold records for answerable
// questions (those with a gold record judged), of any candidate for
// unanswerable ones.
func summarise(qs []calQuestion) []SignalSummary {
	type acc struct{ cov, cos []float64 }
	pops := map[string]*acc{}
	for _, q := range qs {
		name := "unanswerable"
		if q.answerable {
			name = q.tag
		}
		bestCov, bestCos, any, anyCos := 0.0, -1.0, false, false
		for i, x := range q.cands {
			if q.answerable && !q.gold[i] {
				continue
			}
			any = true
			bestCov = math.Max(bestCov, x.Coverage)
			if x.HasCosine {
				anyCos = true
				bestCos = math.Max(bestCos, x.Cosine)
			}
		}
		if !any {
			continue
		}
		a := pops[name]
		if a == nil {
			a = &acc{}
			pops[name] = a
		}
		a.cov = append(a.cov, bestCov)
		if anyCos {
			a.cos = append(a.cos, bestCos)
		}
	}
	var out []SignalSummary
	for _, name := range []string{"plain", TagParaphrase, "unanswerable"} {
		a := pops[name]
		if a == nil {
			continue
		}
		out = append(out, SignalSummary{Population: name, N: len(a.cov), Coverage: pcts(a.cov), Cosine: pcts(a.cos)})
	}
	return out
}

func pcts(vs []float64) []float64 {
	if len(vs) == 0 {
		return nil
	}
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	return []float64{round4(Percentile(s, 10)), round4(Percentile(s, 50)), round4(Percentile(s, 90))}
}

func sortedFloats(m map[float64]bool) []float64 {
	out := make([]float64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Float64s(out)
	return out
}

// WriteCalibration prints c as the read configuration it recommends.
func WriteCalibration(w io.Writer, c Calibration) {
	fmt.Fprintf(w, "evidence floors from the training split, system %s, model %s (decision 0124):\n", c.System, c.ModelID)
	fmt.Fprintf(w, "  %d answerable training questions (%d with a gold record judged), %d unanswerable\n", c.Answerable, c.Judged, c.Unanswerable)
	fmt.Fprintf(w, "  at the floors: gold kept %d of %d, unanswerable filled %d of %d, cost %d; answerable with nothing passing %d\n",
		c.Kept, c.Judged, c.Filled, c.Unanswerable, c.Cost, c.EmptyAnswerable)
	var tags []string
	for t := range c.ByTag {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	for _, t := range tags {
		b := c.ByTag[t]
		fmt.Fprintf(w, "    %-10s %3d questions, %3d with gold judged, %3d kept\n", t, b.Questions, b.Judged, b.Kept)
	}
	fmt.Fprintf(w, "  non-gold candidates passing: %d of %d\n", c.NoisePassing, c.NoiseTotal)
	fmt.Fprintf(w, "  best evidence (p10 / p50 / p90): gold record for answerable questions, any candidate for unanswerable ones\n")
	for _, s := range c.Signals {
		fmt.Fprintf(w, "    %-12s n %3d  coverage %s  cosine %s\n", s.Population, s.N, joinNums(s.Coverage), joinNums(s.Cosine))
	}
	fmt.Fprintf(w, "read:\n  evidence_floors:\n    %q: {min_term_coverage: %s, min_cosine: %s}\n", c.ModelID, fmtFloor(c.MinTermCoverage), fmtFloor(c.MinCosine))
}

func joinNums(vs []float64) string {
	if len(vs) == 0 {
		return "-"
	}
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmtNum(v)
	}
	return strings.Join(parts, " / ")
}
