package rank

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

func TestShadowModeLogsButDoesNotChangeOrder(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 0 })
	w := newWorld()
	if _, ok, err := r.Learned().Score(bg, "ns", Candidate{ID: "evt_u00"}); ok || err != nil {
		t.Fatalf("no model yet: ok=%v err=%v", ok, err)
	}
	w.play(t, r, e, fc, "ns", 0, 80)
	if _, err := r.Train(bg, "ns", "", "sleep"); err != nil {
		t.Fatal(err)
	}
	cs := w.query(1000)
	fc.Step = 0 // freeze "now" so both calls see the same time since last use
	shadow, err := r.Learned().ScoreAll(bg, "ns", cs)
	if err != nil || len(shadow) != len(cs) {
		t.Fatalf("shadow scores %v %v", shadow, err)
	}
	one, ok, err := r.Learned().Score(bg, "ns", cs[0])
	if !ok || err != nil || one != shadow[cs[0].ID] {
		t.Fatalf("single score %v %v %v", one, ok, err)
	}
	// The useful record is preferred by the shadow model...
	for _, c := range cs {
		if !w.isUseful(c.ID) && shadow[c.ID] >= shadow[w.useful[1000%4]] {
			t.Fatalf("shadow model does not prefer the useful record: %v", shadow)
		}
	}
	// ...but until promotion λ = 0, the term is neutral and the order is relevance.
	lambda, _ := r.Lambda(bg, "ns")
	terms, _ := r.Terms(bg, "ns", cs)
	if lambda != 0 {
		t.Fatalf("lambda %v before promotion", lambda)
	}
	var items []Scored
	for _, c := range cs {
		if terms[c.ID] != Neutral {
			t.Fatal("term must be neutral without a promoted model")
		}
		items = append(items, Scored{ID: c.ID, Relevance: c.Relevance(), Term: terms[c.ID]})
	}
	if !reflect.DeepEqual(Order(items, lambda), c4Order(items)) {
		t.Fatal("shadow mode changed the order")
	}
}

func TestModelWithOtherFeaturesIsRejected(t *testing.T) {
	r, e, _ := newTest(t, nil)
	e.Store.DB().ExecContext(bg, `INSERT INTO ranker_models(id, namespace, kind, status, weights, feature_names, n_examples, created_at)
		VALUES ('mdl_x','ns','logreg','shadow','{"mean":[0],"std":[1],"w":[1]}','["bm25"]',1,'x')`)
	if _, _, err := r.Learned().Score(bg, "ns", Candidate{ID: "evt_1"}); core.CodeOf(err) != core.CodeIntegrity {
		t.Fatalf("want integrity error, got %v", err)
	}
}
