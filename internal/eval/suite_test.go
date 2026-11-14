package eval

import (
	"context"
	"strings"
	"testing"
)

func TestTargetCheck(t *testing.T) {
	tg := Target{Bounds: []Bound{{"a", "<=", 0.05}, {"b", ">", 0}}}
	cases := []struct {
		m    Measured
		ok   bool
		want string
	}{
		{Measured{"a": 0.05, "b": 0.1}, true, ""},
		{Measured{"a": 0.06, "b": 0.1}, false, "a = 0.06, want <= 0.05"},
		{Measured{"a": 0, "b": 0}, false, "b = 0, want > 0"},
		{Measured{"a": 0}, false, "b undefined"},
	}
	for _, c := range cases {
		p := tg.Check(c.m)
		if p.OK != c.ok || p.Reason != c.want {
			t.Errorf("%v: got %+v", c.m, p)
		}
	}
}

// BASE.md "Own suites": eight suites with their initial targets.
func TestRegistryHasEveryBaseSuite(t *testing.T) {
	r := NewRegistry()
	want := map[string]string{
		SuiteTemporal: "Up to 1%", SuiteDeletion: "100%", SuiteInjection: "0%", SuiteDocumentUpdate: "0%",
		SuiteDeduplication: "0% and up to 5%", SuiteAbstention: "95% precision",
		SuiteLearning: "Positive, with no regression at the gate", SuiteDeterminism: "100%",
	}
	got := r.Suites()
	if len(got) != len(want) {
		t.Fatalf("%d suites", len(got))
	}
	for _, s := range got {
		if want[s.Name] != s.Target.Text || len(s.Target.Bounds) == 0 {
			t.Errorf("%s: target %+v", s.Name, s.Target)
		}
		generic := s.Name == SuiteAbstention || s.Name == SuiteTemporal || s.Name == SuiteLearning
		if (s.Run != nil) != generic {
			t.Errorf("%s: unexpected runner state", s.Name)
		}
	}
	if err := r.Register("nope", false, nil); err == nil {
		t.Error("registering an unknown suite must fail")
	}
	if err := r.Add(Suite{Name: SuiteTemporal}); err == nil {
		t.Error("duplicate suite accepted")
	}
	ran := false
	if err := r.Register(SuiteDeletion, false, func(context.Context, SuiteInput) (Measured, error) {
		ran = true
		return Measured{"forget_pass_rate": 1}, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Suites() {
		if s.Name == SuiteDeletion {
			if res := runSuite(context.Background(), s, SuiteInput{}); res.Status != "pass" || !ran {
				t.Fatalf("%+v", res)
			}
		}
	}
	if res := runSuite(context.Background(), r.Suites()[2], SuiteInput{}); res.Status != "not_implemented" {
		t.Fatalf("%+v", res)
	}
}

// oracle answers exactly the answerable questions of validDataset and
// abstains on the rest.
type oracle struct{ NoMemory }

func (oracle) Name() string { return "oracle" }
func (oracle) Retrieve(_ context.Context, q Query, _ int) ([]Retrieved, error) {
	if q.Text == "Quem é o CEO?" {
		return nil, nil
	}
	return []Retrieved{{ID: "x", DocID: "d1", Text: "O plano A custa R$ 12."}}, nil
}

// BASE.md principle 8 and the abstention suite (target 95% precision).
func TestAbstentionSuiteAgainstAnySystem(t *testing.T) {
	ds := validDataset()
	ctx := context.Background()
	m, err := AbstentionSuite(ctx, SuiteInput{System: oracle{}, Dataset: ds, Budget: 800})
	if err != nil {
		t.Fatal(err)
	}
	if m["precision"] != 1 || m["recall"] != 1 || m["unanswerable"] != 1 {
		t.Fatalf("oracle: %v", m)
	}
	m, _ = AbstentionSuite(ctx, SuiteInput{System: NoMemory{}, Dataset: ds, Budget: 800})
	if m["precision"] != 0.25 || m["recall"] != 1 {
		t.Fatalf("B0: %v", m)
	}
	s := NewRegistry().Suites()[5]
	if s.Name != SuiteAbstention || s.Target.Check(m).OK {
		t.Fatal("B0 must fail the abstention target")
	}
}

// BASE.md temporal suite: answers with an already superseded value, up to 1%.
func TestTemporalSuiteCountsSupersededValues(t *testing.T) {
	ds := validDataset()
	ctx := context.Background()
	stale := staleSystem{}
	m, err := TemporalSuite(ctx, SuiteInput{System: stale, Dataset: ds, Budget: 800})
	if err != nil {
		t.Fatal(err)
	}
	// Only q1 (update, with a superseded value) counts; the stale system shows R$ 10.
	if m["questions"] != 1 || m["stale_answer_rate"] != 1 || m["current_answer_rate"] != 0 {
		t.Fatalf("stale system: %v", m)
	}
	m, _ = TemporalSuite(ctx, SuiteInput{System: oracle{}, Dataset: ds, Budget: 800})
	if m["stale_answer_rate"] != 0 || m["current_answer_rate"] != 1 {
		t.Fatalf("oracle: %v", m)
	}
	if s := NewRegistry().Suites()[0]; s.Name != SuiteTemporal || !s.Target.Check(m).OK || !s.PerSystem {
		t.Fatal("the oracle must pass the temporal target")
	}
}

type staleSystem struct{ NoMemory }

func (staleSystem) Name() string { return "stale" }
func (staleSystem) Retrieve(context.Context, Query, int) ([]Retrieved, error) {
	return []Retrieved{{ID: "x", DocID: "d1", Text: "O plano A custa R$ 10."}}, nil
}

// BASE.md learning suite: gain of C5 over C4, positive, with no regression
// at the gate; not available when C4 or C5 did not run.
func TestLearningSuiteComparesC5WithC4(t *testing.T) {
	ds := validDataset()
	ctx := context.Background()
	_, err := LearningSuite(ctx, SuiteInput{Dataset: ds, Unavailable: []UnavailableSystem{{Name: "C4", Reason: "no sleep"}}})
	if err == nil || !strings.Contains(err.Error(), "C4: no sleep") {
		t.Fatalf("missing systems: %v", err)
	}
	res := runSuite(ctx, Suite{Name: SuiteLearning, Run: LearningSuite}, SuiteInput{Dataset: ds})
	if res.Status != "not_available" {
		t.Fatalf("%+v", res)
	}
	judge := func(sys System) SystemResult {
		rs, err := judgeAll(ctx, sys, ds, 800, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return SystemResult{Name: sys.Name(), Questions: rs, Overall: Aggregate(rs)}
	}
	c4, c5 := judge(NoMemory{}), judge(oracle{})
	c4.Name, c5.Name = "C4", "C5"
	m, err := LearningSuite(ctx, SuiteInput{Dataset: ds, Systems: []SystemResult{c4, c5}})
	if err != nil {
		t.Fatal(err)
	}
	if m["gain_c5_over_c4"] <= 0 || m["gate_regression"] >= 0 {
		t.Fatalf("%v", m)
	}
	same := c5
	same.Name = "C4"
	m, _ = LearningSuite(ctx, SuiteInput{Dataset: ds, Systems: []SystemResult{same, c5}})
	if m["gain_c5_over_c4"] != 0 || m["gate_regression"] != 0 {
		t.Fatalf("same results: %v", m)
	}
}
