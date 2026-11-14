package lme

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScoreBatteryMetrics(t *testing.T) {
	ranked := []string{"s1", "e1", "s2", "s3", "s4", "s5", "e2", "s6"}
	m := Score(ranked, []string{"e1", "e2", "e3"}, []string{"e1", "e2"})
	if !near(m.Recall[5], 1.0/3) || !near(m.Recall[10], 2.0/3) || !near(m.MRR, 0.5) {
		t.Fatalf("%+v", m)
	}
	// Official: any@5 = 1, all@5 = 0 (e2 at rank 7), all@10 = 1.
	if m.RecallAny[5] != 1 || m.RecallAll[5] != 0 || m.RecallAll[10] != 1 {
		t.Fatalf("%+v", m)
	}
	// ndcg_any@5: rel = [0,1,0,0,0]; DCG = 1/log2(2) = 1; ideal [1,1] = 1 + 1 = 2.
	if !near(m.NDCGAny[5], 0.5) {
		t.Fatalf("ndcg@5 = %v", m.NDCGAny[5])
	}
	// ndcg_any@10: DCG = 1 + 1/log2(6) (rank 7, 0-based 6 -> log2(7))... per
	// eval_utils.dcg: rel[0] + sum_{i>=1} rel[i]/log2(i+1), i 0-based.
	want := (1/math.Log2(2) + 1/math.Log2(7)) / 2
	if !near(m.NDCGAny[10], want) {
		t.Fatalf("ndcg@10 = %v want %v", m.NDCGAny[10], want)
	}
	if z := Score(nil, []string{"e1"}, []string{"e1"}); z.MRR != 0 || z.Recall[5] != 0 || z.NDCGAny[10] != 0 {
		t.Fatalf("empty ranking %+v", z)
	}
	// First-position hit: dcg uses rel[0] undiscounted.
	if f := Score([]string{"e1"}, []string{"e1"}, []string{"e1"}); f.NDCGAny[5] != 1 || f.MRR != 1 {
		t.Fatalf("%+v", f)
	}
}

func TestAggregateSkipsAbstentionAndNoTarget(t *testing.T) {
	hit := Score([]string{"e"}, []string{"e"}, []string{"e"})
	miss := Score([]string{"x"}, []string{"e"}, nil)
	a := Aggregate([]QResult{
		{Type: "t1", HasOfficial: true, M: hit},
		{Type: "t1", HasOfficial: false, M: miss},
		{Type: "t2", Abstention: true, M: hit},
	})
	all := a["all"]
	if all.N != 2 || all.NOfficial != 1 || !near(all.Recall["@5"], 0.5) || all.RecallAny["@5"] != 1 || !near(all.MRR, 0.5) {
		t.Fatalf("all %+v", all)
	}
	if a[Abstention].N != 1 || a["t2"] != nil {
		t.Fatalf("abstention %+v t2 %+v", a[Abstention], a["t2"])
	}
	if g := Groups(a); len(g) != 3 || g[0] != "all" || g[2] != Abstention {
		t.Fatalf("groups %v", g)
	}
}

func TestRankSessions(t *testing.T) {
	got := RankSessions([][]string{{"a"}, {}, {"b", "a"}, {"c"}, {"b"}})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatal(got)
	}
}

func TestDistPercentiles(t *testing.T) {
	d := NewDist([]float64{5, 1, 4, 2, 3, 6, 7, 8, 9, 10})
	if d.Median != 5 || d.P95 != 10 || d.Min != 1 || d.Max != 10 || d.Mean != 5.5 {
		t.Fatalf("%+v", d)
	}
}
