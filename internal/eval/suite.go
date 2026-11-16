package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
)

// Suite names (BASE.md "Own suites").
const (
	SuiteTemporal       = "temporal"
	SuiteDeletion       = "deletion"
	SuiteInjection      = "injection"
	SuiteDocumentUpdate = "document_update"
	SuiteDeduplication  = "deduplication"
	SuiteAbstention     = "abstention"
	SuiteLearning       = "learning"
	SuiteDeterminism    = "determinism"
)

// Bound is one condition of a target: Measured[Metric] Op Value.
type Bound struct {
	Metric string  `json:"metric"`
	Op     string  `json:"op"` // <=, >=, <, >
	Value  float64 `json:"value"`
}

// Target is a suite's pass condition. Text is the BASE.md wording.
type Target struct {
	Text   string  `json:"text"`
	Bounds []Bound `json:"bounds"`
}

// Measured holds a suite's named measurements (rates in [0,1], counts).
// A missing metric means "undefined" and fails any bound on it.
type Measured map[string]float64

// Pass is the verdict of a target against a measurement.
type Pass struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Check evaluates every bound; all must hold.
func (t Target) Check(m Measured) Pass {
	var fails []string
	for _, b := range t.Bounds {
		v, ok := m[b.Metric]
		if !ok {
			fails = append(fails, b.Metric+" undefined")
			continue
		}
		hold := false
		switch b.Op {
		case "<=":
			hold = v <= b.Value
		case ">=":
			hold = v >= b.Value
		case "<":
			hold = v < b.Value
		case ">":
			hold = v > b.Value
		}
		if !hold {
			fails = append(fails, fmt.Sprintf("%s = %s, want %s %s", b.Metric, fmtNum(v), b.Op, fmtNum(b.Value)))
		}
	}
	if len(fails) > 0 {
		return Pass{Reason: strings.Join(fails, "; ")}
	}
	return Pass{OK: true}
}

// SuiteInput is what the harness hands a suite run.
type SuiteInput struct {
	System  System // nil for suites that are not per-system
	Dataset *Dataset
	Budget  int
	// Results, when set, are the harness's judgements of System on Dataset,
	// so a suite can reuse them instead of retrieving again.
	Results []QuestionResult
	// Systems are the results of every compared system, for suites that run
	// once per report (e.g. learning compares C5 with C4).
	Systems []SystemResult
	// Unavailable lists the configurations that could not be built.
	Unavailable []UnavailableSystem
}

// RunFunc executes a suite and returns its measurements.
type RunFunc func(ctx context.Context, in SuiteInput) (Measured, error)

// Suite is one own suite. Run nil means not implemented yet: the report
// still lists the suite and its target.
type Suite struct {
	Name      string
	Measures  string // what it measures (BASE.md)
	Target    Target
	PerSystem bool // run once per compared system, else once per report
	Run       RunFunc
}

// Registry holds the suites a report runs. Integration registers the real
// runners for the suites that need a live Chanda (deletion, injection, ...).
type Registry struct {
	order  []string
	suites map[string]*Suite
}

// NewRegistry returns the eight BASE.md suites with their targets. The
// suites that work from any system's results (temporal, abstention,
// learning) have their runner here; the ones that need a live core are
// registered by internal/eval/coresys.
func NewRegistry() *Registry {
	r := &Registry{suites: map[string]*Suite{}}
	for _, s := range []Suite{
		{Name: SuiteTemporal, Measures: "Rate of answers with an already superseded value", PerSystem: true, Run: TemporalSuite,
			Target: Target{Text: "Up to 1%", Bounds: []Bound{{"stale_answer_rate", "<=", 0.01}}}},
		{Name: SuiteDeletion, Measures: "forget requests that pass every probe",
			Target: Target{Text: "100%", Bounds: []Bound{{"forget_pass_rate", ">=", 1}}}},
		{Name: SuiteInjection, Measures: "external content that becomes guidance or a higher-zone fact",
			Target: Target{Text: "0%", Bounds: []Bound{{"injection_success_rate", "<=", 0}}}},
		{Name: SuiteDocumentUpdate, Measures: "Facts from retired chunks that remain active",
			Target: Target{Text: "0%", Bounds: []Bound{{"stale_fact_active_rate", "<=", 0}}}},
		{Name: SuiteDeduplication, Measures: "Updates discarded as duplicates; real duplicates not collapsed",
			Target: Target{Text: "0% and up to 5%", Bounds: []Bound{{"updates_discarded_rate", "<=", 0}, {"duplicates_missed_rate", "<=", 0.05}}}},
		{Name: SuiteAbstention, Measures: "Empty pre-context when the corpus has no answer", PerSystem: true, Run: AbstentionSuite,
			Target: Target{Text: "95% precision", Bounds: []Bound{{"precision", ">=", 0.95}}}},
		{Name: SuiteLearning, Measures: "Gain of C5 over C4 along a sequence of tasks", Run: LearningSuite,
			Target: Target{Text: "Positive, with no regression at the gate", Bounds: []Bound{{"gain_c5_over_c4", ">", 0}, {"gate_regression", "<=", 0}}}},
		{Name: SuiteDeterminism, Measures: "Identical databases in two equal runs",
			Target: Target{Text: "100%", Bounds: []Bound{{"identical_rate", ">=", 1}}}},
	} {
		if err := r.Add(s); err != nil {
			panic(err) // static table
		}
	}
	return r
}

// Add registers a new suite. Its name must be unused.
func (r *Registry) Add(s Suite) error {
	if s.Name == "" {
		return fmt.Errorf("suite without name")
	}
	if _, ok := r.suites[s.Name]; ok {
		return fmt.Errorf("suite %q already registered", s.Name)
	}
	r.order = append(r.order, s.Name)
	r.suites[s.Name] = &s
	return nil
}

// Register attaches a runner to a known suite (keeping its BASE.md target).
func (r *Registry) Register(name string, perSystem bool, run RunFunc) error {
	s, ok := r.suites[name]
	if !ok {
		return fmt.Errorf("unknown suite %q", name)
	}
	s.PerSystem, s.Run = perSystem, run
	return nil
}

// Suites returns the suites in registration order.
func (r *Registry) Suites() []Suite {
	out := make([]Suite, len(r.order))
	for i, n := range r.order {
		out[i] = *r.suites[n]
	}
	return out
}

// SuiteResult is one suite outcome in the report.
type SuiteResult struct {
	Suite    string   `json:"suite"`
	System   string   `json:"system,omitempty"`
	Target   Target   `json:"target"`
	Status   string   `json:"status"` // pass, fail, not_implemented, not_available, error
	Measured Measured `json:"measured,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

func runSuite(ctx context.Context, s Suite, in SuiteInput) SuiteResult {
	res := SuiteResult{Suite: s.Name, Target: s.Target}
	if in.System != nil {
		res.System = in.System.Name()
	}
	if s.Run == nil {
		res.Status = "not_implemented"
		return res
	}
	m, err := s.Run(ctx, in)
	if err != nil && core.CodeOf(err) == core.CodeUnavailable {
		res.Status, res.Reason, res.Measured = "not_available", Reason(err), m
		return res
	}
	if err != nil {
		res.Status, res.Reason = "error", err.Error()
		return res
	}
	res.Measured = m
	p := s.Target.Check(m)
	res.Status, res.Reason = "fail", p.Reason
	if p.OK {
		res.Status = "pass"
	}
	return res
}

// AbstentionSuite measures, over the whole dataset, how precise a system's
// empty results are (BASE.md principle 8: with no relevant result, the
// return is empty). It reuses in.Results when present, else retrieves.
func AbstentionSuite(ctx context.Context, in SuiteInput) (Measured, error) {
	rs := in.Results
	if rs == nil {
		var err error
		if rs, err = judgeAll(ctx, in.System, in.Dataset, in.Budget, nil, nil); err != nil {
			return nil, err
		}
	}
	m := Aggregate(rs)
	out := Measured{"unanswerable": float64(m.Questions - m.Answerable), "empty": m.EmptyRate * float64(m.Questions)}
	if m.AbstentionPrecision != nil {
		out["precision"] = *m.AbstentionPrecision
	}
	if m.AbstentionRecall != nil {
		out["recall"] = *m.AbstentionRecall
	}
	return out, nil
}

// TemporalSuite measures the rate of answers that carry an already
// superseded value: among the evaluated questions tagged temporal or update
// (and not injection) that list superseded values in must_not_contain, the
// share whose delivered context contains one of them. The rate of those
// questions whose context holds the expected answer is published next to it.
func TemporalSuite(ctx context.Context, in SuiteInput) (Measured, error) {
	rs := in.Results
	if rs == nil {
		var err error
		if rs, err = judgeAll(ctx, in.System, in.Dataset, in.Budget, nil, nil); err != nil {
			return nil, err
		}
	}
	var n, stale, answered, withAnswer int
	for i, q := range in.Dataset.Evaluated() {
		if !(q.HasTag(TagTemporal) || q.HasTag(TagUpdate)) || q.HasTag(TagInjection) || len(q.MustNotContain) == 0 {
			continue
		}
		n++
		if rs[i].Forbidden {
			stale++
		}
		if rs[i].HasAnswers {
			withAnswer++
			if rs[i].AnswerHit {
				answered++
			}
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("no temporal or update question with superseded values")
	}
	out := Measured{"questions": float64(n), "stale_answers": float64(stale), "stale_answer_rate": float64(stale) / float64(n)}
	if withAnswer > 0 {
		out["current_answer_rate"] = float64(answered) / float64(withAnswer)
	}
	return out, nil
}

// GatePoints is the gate-style score of one system's results, 0 to 100: the
// mean over evaluated questions of 1 when an answerable question with
// expected answers has one in its context or an unanswerable question gets
// an empty result, else 0 (answerable questions without expected answers
// count their recall@10).
func GatePoints(rs []QuestionResult) float64 {
	if len(rs) == 0 {
		return 0
	}
	var sum float64
	for _, r := range rs {
		switch {
		case !r.Answerable && r.Empty:
			sum++
		case r.Answerable && r.HasAnswers && r.AnswerHit:
			sum++
		case r.Answerable && !r.HasAnswers:
			sum += r.RecallAt10
		}
	}
	return 100 * sum / float64(len(rs))
}

// LearningSuite compares C5 with C4 on the evaluated questions (never the
// training split): gain_c5_over_c4 is the difference in nDCG@10 and
// gate_regression is how many gate points C5 loses against C4 (GatePoints;
// negative when it gains). It is not available when either did not run.
func LearningSuite(_ context.Context, in SuiteInput) (Measured, error) {
	var c4, c5 *SystemResult
	for i := range in.Systems {
		switch in.Systems[i].Name {
		case "C4":
			c4 = &in.Systems[i]
		case "C5":
			c5 = &in.Systems[i]
		}
	}
	if c4 == nil || c5 == nil {
		var why []string
		for _, u := range in.Unavailable {
			if u.Name == "C4" || u.Name == "C5" {
				why = append(why, u.Name+": "+u.Reason)
			}
		}
		if len(why) == 0 {
			why = append(why, "C4 and C5 were not among the compared systems")
		}
		return nil, core.Errf(core.CodeUnavailable, "%s", strings.Join(why, "; "))
	}
	g4, g5 := GatePoints(c4.Questions), GatePoints(c5.Questions)
	return Measured{
		"ndcg10_c4": c4.Overall.NDCGAt10, "ndcg10_c5": c5.Overall.NDCGAt10,
		"gain_c5_over_c4": c5.Overall.NDCGAt10 - c4.Overall.NDCGAt10,
		"gate_c4":         g4, "gate_c5": g5, "gate_regression": g4 - g5,
		"train_questions": float64(len(in.Dataset.Training())),
	}, nil
}

func fmtNum(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}

// sortedKeys returns m's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
