package sleep

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeLearning struct{ calls int }

func (fl *fakeLearning) Learn(ctx context.Context, ns string) (LearnResult, error) {
	fl.calls++
	return LearnResult{Trained: "mdl_1", Evaluated: "mdl_1", Passed: true, Promoted: true, Baseline: 50, Learned: 60, Lambda: 0.05}, nil
}

// BASE.md open question "who writes the gate set" (decision 0092): phase 1
// proposes questions from current trusted facts; nothing is approved by
// sleep, nothing is proposed twice, untrusted facts never feed the gate.
func TestPhase1ProposesGateQuestions(t *testing.T) {
	f := newFixture(t)
	f.e.Cfg.Sleep.GateProposals = 2
	f.event("evt_u", "user", "Atlas owner is Marina")
	f.event("evt_i", "internal", "Atlas status is green")
	f.event("evt_x", "external", "Atlas price is 10")
	f.entity("ent_a", "Project Atlas", "")
	f.fact("fct_1", "ent_a", "owner", "Marina Duarte", "active", "user", true, "evt_u")
	f.fact("fct_2", "ent_a", "status", "green", "active", "internal", true, "evt_i")
	f.fact("fct_3", "ent_a", "current_price", "10", "active", "external", true, "evt_x")
	f.fact("fct_4", "ent_a", "manager", "Rui", "superseded", "user", true, "evt_u")
	f.fact("fct_5", "ent_a", "email", "a@b.c", "active", "internal", true, "evt_i")
	f.advance(time.Minute)
	fl := &fakeLearning{}
	s := New(f.e, Deps{Learning: fl})

	res, err := s.Run(f.ctx, RunInput{Namespace: ns, Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Counts.GateProposed; len(got) != 2 {
		t.Fatalf("proposed %v, want 2 (the per-run cap)", got)
	}
	if n := f.count(`SELECT count(*) FROM gate_questions WHERE status='proposed'`); n != 2 {
		t.Fatalf("%d proposed rows", n)
	}
	if q := f.str(`SELECT question FROM gate_questions ORDER BY rowid LIMIT 1`); q != "What is Project Atlas owner?" {
		t.Fatalf("first question %q (user zone first)", q)
	}
	var exp GateExpected
	if err := json.Unmarshal([]byte(f.str(`SELECT expected FROM gate_questions ORDER BY rowid LIMIT 1`)), &exp); err != nil {
		t.Fatal(err)
	}
	if len(exp.RecordIDs) != 1 || exp.RecordIDs[0] != "fct_1" || exp.Answers[0] != "Marina Duarte" || exp.AsOf == "" {
		t.Fatalf("expected %+v", exp)
	}
	if fl.calls != 1 || res.Counts.Learning.Trained != "mdl_1" || !contains(res.Report, "Ranker: trained mdl_1") {
		t.Fatalf("learning not run or not reported: %d %+v", fl.calls, res.Counts.Learning)
	}

	f.advance(25 * time.Hour)
	res, err = s.Run(f.ctx, RunInput{Namespace: ns, Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Counts.GateProposed) != 1 {
		t.Fatalf("second run proposed %v, want only the remaining trusted fact", res.Counts.GateProposed)
	}
	if n := f.count(`SELECT count(*) FROM gate_questions WHERE question LIKE '%price%' OR question LIKE '%manager%'`); n != 0 {
		t.Fatal("external or superseded facts must not feed the gate")
	}
	if n := f.count(`SELECT count(*) FROM gate_questions WHERE status <> 'proposed'`); n != 0 {
		t.Fatal("sleep must never approve gate questions")
	}
	if f.auditCount(ActGatePropose) != 3 {
		t.Fatalf("%d gate_propose audits", f.auditCount(ActGatePropose))
	}
	f.verifyAudit()
}
