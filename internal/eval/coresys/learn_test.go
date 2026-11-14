package coresys

import (
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// C5 approves only the gate split as its gate set, turns the training split
// into outcomes derived from the gold labels, and states what the sleep run
// did with them; evaluated questions are never seen.
func TestC5TrainsOnTheTrainingSplitWithTheGateSplit(t *testing.T) {
	ds := tiny()
	train := []eval.Question{{ID: "t1", Question: "Qual o cargo atual de Ana Ribeiro?", AsOf: "2026-11-01",
		Gold: []eval.Gold{{DocID: "dir", Span: "Cargo: Coordenadora Financeira"}}, Answers: []string{"Coordenadora Financeira"}, Split: eval.SplitTrain}}
	gate := []eval.Question{{ID: "g1", Question: "Quem gere a conta Padaria Sol?", AsOf: "2026-11-01",
		Gold: []eval.Gold{{DocID: "s1", Span: "gestor"}}, Answers: []string{"Bruno Lima"}, Split: eval.SplitGate}}
	s := build(t, "C5", ds)
	if err := s.Train(bg, train, gate); err != nil {
		t.Fatal(err)
	}
	db := s.Env().Store.DB()
	var approved, outcomes int
	if err := db.QueryRow(`SELECT count(*) FROM gate_questions WHERE status = 'approved'`).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM outcomes`).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if approved != len(gate) || outcomes != TrainPasses*len(train) {
		t.Fatalf("%d approved gate questions, %d outcomes", approved, outcomes)
	}
	var q string
	if err := db.QueryRow(`SELECT question FROM gate_questions WHERE status = 'approved'`).Scan(&q); err != nil || q != gate[0].Question {
		t.Fatalf("gate question %q %v", q, err)
	}
	if d := s.Info().Description; !strings.Contains(d, "tasks") || !strings.Contains(d, "gate questions") {
		t.Fatalf("C5 must state the outcome of learning: %s", d)
	}
	// C4 does not learn.
	c4 := build(t, "C4", ds)
	if err := c4.Train(bg, train, gate); err != nil {
		t.Fatal(err)
	}
	if err := c4.Env().Store.DB().QueryRow(`SELECT count(*) FROM outcomes`).Scan(&outcomes); err != nil || outcomes != 0 {
		t.Fatalf("C4 recorded %d outcomes", outcomes)
	}
}
