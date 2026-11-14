package read

import (
	"math"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mk(id string, kw, vec int) *cand {
	return &cand{rec: &record{id: id, typ: core.RecEvent}, kwRank: kw, vecRank: vec}
}

// BASE.md read step 5: Reciprocal Rank Fusion (k = 60) over the two lists.
func TestRRFFusionK60(t *testing.T) {
	both := mk("evt_both", 1, 1)
	kwOnly := mk("evt_kw", 2, 0)
	vecOnly := mk("evt_vec", 0, 3)
	fuse([]*cand{both, kwOnly, vecOnly}, 60, 2)
	if !near(both.rrf, 2.0/61) || !near(both.relevance, 1) {
		t.Errorf("both: rrf=%v rel=%v", both.rrf, both.relevance)
	}
	if !near(kwOnly.rrf, 1.0/62) || !near(kwOnly.relevance, (1.0/62)/(2.0/61)) {
		t.Errorf("kw: rrf=%v rel=%v", kwOnly.rrf, kwOnly.relevance)
	}
	if !near(vecOnly.rrf, 1.0/63) {
		t.Errorf("vec: rrf=%v", vecOnly.rrf)
	}
	solo := mk("evt_solo", 1, 0)
	fuse([]*cand{solo}, 60, 1)
	if !near(solo.relevance, 1) {
		t.Errorf("keyword-only top item must normalize to 1: %v", solo.relevance)
	}
}

// BASE.md read step 7: final = (1 − λ)·relevance + λ·utility; λ capped by lambda_max.
func TestUtilityAdjustment(t *testing.T) {
	f := newFx(t)
	a, b := mk("evt_a", 1, 0), mk("evt_b", 2, 0)
	fuse([]*cand{a, b}, 60, 1)
	r := New(f.e, Deps{Utility: fakeUtility{lambda: 0.9, by: map[string]float64{"evt_b": 1}}})
	l, err := r.lambda(f.ctx, "ns")
	if err != nil || l != f.e.Cfg.Ranking.LambdaMax {
		t.Fatalf("lambda must be capped at %v: %v %v", f.e.Cfg.Ranking.LambdaMax, l, err)
	}
	if err := r.adjust(f.ctx, "ns", []*cand{a, b}, l); err != nil {
		t.Fatal(err)
	}
	if !near(a.final, 0.7*a.relevance+0.3*NeutralUtility) || a.hasUtil {
		t.Errorf("no utility → neutral 0.5: %+v", a)
	}
	if !near(b.final, 0.7*b.relevance+0.3) {
		t.Errorf("b final = %v", b.final)
	}
	f.e.Cfg.Ranking.Enabled = false
	if l, _ := r.lambda(f.ctx, "ns"); l != 0 {
		t.Errorf("learning off → λ = 0, got %v", l)
	}
	if l, _ := New(f.e, Deps{}).lambda(f.ctx, "ns"); l != 0 {
		t.Errorf("nil Utility → λ = 0, got %v", l)
	}
}

// BASE.md read step 6: the reranker scores the top N; the step is optional by configuration.
func TestRerankTopN(t *testing.T) {
	f := newFx(t)
	a, b, c := mk("evt_a", 1, 0), mk("evt_b", 2, 0), mk("evt_c", 3, 0)
	a.rec.text, b.rec.text, c.rec.text = "A", "B", "C"
	cs := []*cand{a, b, c}
	fuse(cs, 60, 1)
	rr := fakeRerank{by: map[string]float32{"A": -5, "B": 5, "C": 9}}
	r := New(f.e, Deps{Reranker: rr})
	if ran, _ := r.rerankStep(f.ctx, "q", cs); ran {
		t.Fatal("rerank_enabled=false must skip the step")
	}
	f.e.Cfg.Read.RerankEnabled, f.e.Cfg.Read.RerankTopN = true, 2
	ran, err := r.rerankStep(f.ctx, "q", cs)
	if err != nil || !ran {
		t.Fatal(ran, err)
	}
	if !a.hasRerank || !b.hasRerank || c.hasRerank {
		t.Fatalf("only top 2 reranked: %v %v %v", a.hasRerank, b.hasRerank, c.hasRerank)
	}
	if !(b.relevance > a.relevance && a.relevance > 0 && c.relevance < b.relevance) {
		t.Fatalf("relevances a=%v b=%v c=%v", a.relevance, b.relevance, c.relevance)
	}
	for _, x := range cs {
		if x.relevance < 0 || x.relevance > 1 {
			t.Fatalf("relevance out of [0,1]: %v", x.relevance)
		}
	}
}
