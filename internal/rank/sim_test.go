package rank

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

// gateEvaluator scores held-out queries (never logged, so never trained on)
// by the reciprocal rank of the useful record, in points (0..100).
func gateEvaluator(t *testing.T, r *Ranker, w world, ns string, queries []int) Evaluator {
	return func(useLearned bool, lambda float64) (float64, error) {
		total := 0.0
		for _, q := range queries {
			order, err := orderFor(r, w, ns, q, useLearned, lambda)
			if err != nil {
				return 0, err
			}
			for i, id := range order {
				if w.isUseful(id) {
					total += 1 / float64(i+1)
					break
				}
			}
		}
		return 100 * total / float64(len(queries)), nil
	}
}

func orderFor(r *Ranker, w world, ns string, q int, useLearned bool, lambda float64) ([]string, error) {
	cs := w.query(q)
	terms := map[string]float64{}
	if useLearned {
		var err error
		if terms, err = r.Learned().ScoreAll(bg, ns, cs); err != nil {
			return nil, err
		}
	} else {
		lambda = 0
	}
	items := make([]Scored, len(cs))
	for i, c := range cs {
		items[i] = Scored{ID: c.ID, Relevance: c.Relevance(), Term: terms[c.ID]}
	}
	return Order(items, lambda), nil
}

// Learning suite (BASE.md "Own suites"): along a sequence of tasks, records
// that are consistently useful gain utility, a ranker trained on the log beats
// relevance on held-out data, promotion needs the margin, and turning
// learning off restores the baseline order exactly.
func TestLearningSuiteSimulation(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 0 })
	w := newWorld()
	w.play(t, r, e, fc, "ns", 0, 160)

	// Stage 1: useful records rise above all others.
	all := append(append([]string{}, w.useful...), w.useless...)
	scores, err := r.Scores(bg, "ns", all)
	if err != nil {
		t.Fatal(err)
	}
	minUseful, maxUseless := 1.0, 0.0
	for _, id := range w.useful {
		s, ok := scores[id]
		if !ok {
			t.Fatalf("useful %s below min exposures", id)
		}
		minUseful = min(minUseful, s)
	}
	for _, id := range w.useless {
		if s, ok := scores[id]; ok {
			maxUseless = max(maxUseless, s)
		}
	}
	if minUseful <= maxUseless {
		t.Fatalf("utility: min useful %v <= max useless %v", minUseful, maxUseless)
	}

	// Stage 2: both rankers beat relevance on the held-out retrievals.
	for _, kind := range []string{KindMLP, KindLogReg} { // logreg last: it is the one evaluated below
		tr, err := r.Train(bg, "ns", kind, "sleep")
		if err != nil {
			t.Fatal(err)
		}
		if tr.EvalScore <= tr.BaselineScore {
			t.Fatalf("%s: held-out AUC %v does not beat baseline %v", kind, tr.EvalScore, tr.BaselineScore)
		}
	}

	var gate []int
	for q := 5000; q < 5040; q++ {
		gate = append(gate, q)
	}
	eval := gateEvaluator(t, r, w, "ns", gate)
	base, _ := eval(false, 0)
	learned, _ := eval(true, r.env.Cfg.Ranking.LambdaStep)
	if learned <= base {
		t.Fatalf("gate: learned %v does not beat baseline %v", learned, base)
	}
	t.Logf("gate MRR: baseline %.2f, learned at λ=%.2f %.2f", base, r.env.Cfg.Ranking.LambdaStep, learned)

	// Promotion only with the margin.
	r.env.Cfg.Ranking.PromotionMargin = learned - base + 1
	res, err := r.Promote(bg, "ns", "eval", eval)
	if err != nil || res.Passed {
		t.Fatalf("promoted without the margin: %+v %v", res, err)
	}
	if l, _ := r.Lambda(bg, "ns"); l != 0 {
		t.Fatalf("lambda %v", l)
	}
	r.env.Cfg.Ranking.PromotionMargin = 1
	res, err = r.Promote(bg, "ns", "eval", eval)
	if err != nil || !res.Promoted {
		t.Fatalf("not promoted with the margin met: %+v %v", res, err)
	}
	lambda, _ := r.Lambda(bg, "ns")
	if lambda != r.env.Cfg.Ranking.LambdaStep {
		t.Fatalf("lambda after promotion %v", lambda)
	}

	// Live: Terms uses the promoted model, so C5 order differs from C4 somewhere.
	differs := false
	for _, q := range gate {
		cs := w.query(q)
		terms, err := r.Terms(bg, "ns", cs)
		if err != nil {
			t.Fatal(err)
		}
		items := make([]Scored, len(cs))
		for i, c := range cs {
			items[i] = Scored{ID: c.ID, Relevance: c.Relevance(), Term: terms[c.ID]}
		}
		if !reflect.DeepEqual(Order(items, lambda), c4Order(items)) {
			differs = true
		}
	}
	if !differs {
		t.Fatal("promoted ranking never changed the order")
	}

	// Learning off: exactly the baseline order on every gate query.
	if _, err := r.SetEnabled(bg, "ns", false, "admin"); err != nil {
		t.Fatal(err)
	}
	lambda, _ = r.Lambda(bg, "ns")
	for _, q := range gate {
		cs := w.query(q)
		terms, _ := r.Terms(bg, "ns", cs)
		items := make([]Scored, len(cs))
		for i, c := range cs {
			items[i] = Scored{ID: c.ID, Relevance: c.Relevance(), Term: terms[c.ID]}
		}
		if !reflect.DeepEqual(Order(items, lambda), c4Order(items)) {
			t.Fatalf("query %d: order with learning off differs from baseline", q)
		}
	}
	if after, _ := eval(false, 0); after != base {
		t.Fatalf("baseline changed: %v vs %v", after, base)
	}
}
