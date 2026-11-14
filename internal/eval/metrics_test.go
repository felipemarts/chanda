package eval

import (
	"math"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// Hand-computed example (BASE.md "Metrics": recall@k, MRR and nDCG over the
// gold chunks). Units: U1 = d1 "alpha"; U2 = d2 "beta" or d3 "beta".
//
//	rank 1 d9 "zzz"        miss
//	rank 2 d2 "has beta"   first hit of U2
//	rank 3 d1 "no"         right doc, span missing (doc-level hit of U1 only)
//	rank 4 d3 "beta again" U2 again (alternative; no new unit)
//	rank 5 d1 "Alpha here" first hit of U1
//
// recall@1 = 0, recall@3 = 1/2, recall@5 = 1, MRR = 1/2,
// nDCG@10 = (1/log2 3 + 1/log2 6) / (1 + 1/log2 3) = 1.0177826 / 1.6309298 = 0.6240510,
// doc recall@1 = 0, doc recall@3 = 1.
func TestJudgeHandComputed(t *testing.T) {
	q := Question{ID: "q", Gold: []Gold{{DocID: "d1", Span: "alpha"}, {DocID: "d2", Span: "beta", Unit: "b"}, {DocID: "d3", Span: "beta", Unit: "b"}},
		Answers: []string{"ALPHA"}, MustNotContain: []string{"zzz"}}
	rs := []Retrieved{
		{ID: "1", DocID: "d9", Text: "zzz", Tokens: 1},
		{ID: "2", DocID: "d2", Text: "has beta", Tokens: 2},
		{ID: "3", DocID: "d1", Text: "no", Tokens: 1},
		{ID: "4", DocID: "d3", Text: "beta again", Tokens: 3},
		{ID: "5", DocID: "d1", Text: "Alpha here", Tokens: 3},
	}
	r := Judge(q, rs)
	if r.GoldRanks[0] != 5 || r.GoldRanks[1] != 2 {
		t.Fatalf("gold ranks %v", r.GoldRanks)
	}
	checks := []struct {
		name      string
		got, want float64
	}{
		{"recall@1", recallAt(r.GoldRanks, 1), 0},
		{"recall@3", recallAt(r.GoldRanks, 3), 0.5},
		{"recall@5", r.RecallAt5, 1},
		{"recall@10", r.RecallAt10, 1},
		{"mrr", r.MRR, 0.5},
		{"ndcg@10", r.NDCGAt10, 0.6240510},
		{"doc recall@10", r.DocRecallAt10, 1},
	}
	for _, c := range checks {
		if !near(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if !r.AnswerHit || !r.Forbidden || r.Tokens != 10 || r.Empty {
		t.Fatalf("answer=%v forbidden=%v tokens=%d empty=%v", r.AnswerHit, r.Forbidden, r.Tokens, r.Empty)
	}
	// nDCG@2 with only U2 hit at rank 2: (1/log2 3) / (1 + 1/log2 3) = 0.6309298/1.6309298 = 0.3868528.
	if got := ndcgAt([]int{5, 2}, 2); !near(got, 0.3868528) {
		t.Errorf("ndcg@2 = %v", got)
	}
	// One excerpt covering both units at rank 1 is clamped to 1.
	if got := ndcgAt([]int{1, 1}, 10); got != 1 {
		t.Errorf("clamped ndcg = %v", got)
	}
	// No hit at all.
	if r := Judge(q, []Retrieved{{DocID: "x", Text: "nada"}}); r.MRR != 0 || r.RecallAt10 != 0 || r.NDCGAt10 != 0 {
		t.Fatalf("miss: %+v", r)
	}
}

// BASE.md principle 8 / abstention suite: precision = empty results that were
// truly unanswerable; recall = unanswerable questions answered with empty.
func TestAggregateAbstentionHandComputed(t *testing.T) {
	ans := Question{ID: "a", Gold: []Gold{{DocID: "d"}}, Answers: []string{"x"}}
	una := Question{ID: "u", Tags: []string{TagAbstention}}
	some := []Retrieved{{ID: "1", DocID: "d", Text: "x", Tokens: 4}}
	rs := []QuestionResult{
		Judge(ans, nil),  // answerable, empty: false abstention
		Judge(ans, some), // answerable, answered
		Judge(una, nil),  // unanswerable, empty: correct abstention
		Judge(una, some), // unanswerable, answered: missed abstention
	}
	m := Aggregate(rs)
	if *m.AbstentionPrecision != 0.5 || *m.AbstentionRecall != 0.5 || m.EmptyRate != 0.5 {
		t.Fatalf("precision %v recall %v empty %v", *m.AbstentionPrecision, *m.AbstentionRecall, m.EmptyRate)
	}
	if m.Answerable != 2 || m.RecallAt10 != 0.5 || m.MRR != 0.5 || *m.AnswerInContext != 0.5 {
		t.Fatalf("%+v", m)
	}
	if m.TokensMean != 2 || m.TokensP50 != 0 || m.TokensP95 != 4 || m.ForbiddenRate != nil {
		t.Fatalf("tokens mean %v p50 %d p95 %d forbidden %v", m.TokensMean, m.TokensP50, m.TokensP95, m.ForbiddenRate)
	}
	never := Aggregate([]QuestionResult{Judge(una, some)})
	if never.AbstentionPrecision != nil || *never.AbstentionRecall != 0 {
		t.Fatal("a system that never abstains has undefined precision")
	}
}

func TestPercentileNearestRank(t *testing.T) {
	xs := []int{5, 1, 4, 2, 3}
	cases := []struct {
		p    float64
		want int
	}{{50, 3}, {95, 5}, {20, 1}, {21, 2}, {100, 5}}
	for _, c := range cases {
		if got := Percentile(xs, c.p); got != c.want {
			t.Errorf("p%v = %d, want %d", c.p, got, c.want)
		}
	}
	if Percentile([]int{10, 20}, 50) != 10 || Percentile([]int(nil), 50) != 0 {
		t.Fatal("edge cases")
	}
	if xs[0] != 5 {
		t.Fatal("Percentile must not reorder its input")
	}
}

func TestEnforceBudgetRecountsTokens(t *testing.T) {
	rs := []Retrieved{{ID: "a", Text: strings.Repeat("x", 40), Tokens: 1}, {ID: "b", Text: strings.Repeat("x", 40)}, {ID: "c", Text: "xxxx"}}
	got, dropped := EnforceBudget(rs, 12)
	if len(got) != 2 || got[0].Tokens != 10 || got[1].ID != "c" || dropped != 1 {
		t.Fatalf("got %+v dropped %d", got, dropped)
	}
}
