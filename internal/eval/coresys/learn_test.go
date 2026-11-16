package coresys

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/eval"
	"github.com/felipemarts/chanda/internal/sleep"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// The gate key cites the records that are evidence for the gold units
// (decision 0110). With answers alone the gate scored answer-in-context,
// which reordering inside the context cannot change, so the learned and the
// baseline order always tied and no model could be promoted.
func TestC5GateKeyCitesEvidenceRecords(t *testing.T) {
	ds := tiny()
	s := build(t, "C5", ds)
	train := []eval.Question{{ID: "t1", Question: "Qual o cargo atual de Ana Ribeiro?", AsOf: "2026-11-01",
		Gold: []eval.Gold{{DocID: "dir", Span: "Cargo: Coordenadora Financeira"}}, Answers: []string{"Coordenadora Financeira"}, Split: eval.SplitTrain}}
	gate := []eval.Question{
		{ID: "g1", Question: "Qual é o cargo de Ana Ribeiro?", AsOf: "2026-11-01",
			Gold: []eval.Gold{{DocID: "dir", Span: "Cargo: Coordenadora Financeira"}}, Answers: []string{"Coordenadora Financeira"}, Split: eval.SplitGate},
		{ID: "g2", Question: "Qual é a senha do Wi-Fi de visitantes?", AsOf: "2026-11-01", Split: eval.SplitGate},
	}
	if err := s.Train(bg, train, gate); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Env().Store.DB().Query(`SELECT question, expected FROM gate_questions WHERE status = 'approved' ORDER BY question`)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]sleep.GateExpected{}
	for rows.Next() {
		var q, exp string
		var e sleep.GateExpected
		if err := rows.Scan(&q, &exp); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(exp), &e); err != nil {
			t.Fatal(err)
		}
		keys[q] = e
	}
	rows.Close()
	cited := keys[gate[0].Question].RecordIDs
	if len(cited) == 0 {
		t.Fatalf("gate key without record ids: %+v", keys[gate[0].Question])
	}
	for _, id := range cited {
		g, err := s.API().Get(op(bg), chanda.GetInput{Namespace: Namespace, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if r := (eval.Retrieved{ID: id, DocID: s.docOf(g.Item), Text: g.Item.Text}); !eval.Hits(r, gate[0].Gold[0]) {
			t.Errorf("%s is not evidence: %+v", id, r)
		}
	}
	// The old version's chunk ("Analista Financeira") is not evidence.
	for _, id := range cited {
		g, _ := s.API().Get(op(bg), chanda.GetInput{Namespace: Namespace, ID: id})
		if strings.Contains(g.Item.Text, "Analista") {
			t.Errorf("%s cites the superseded value", id)
		}
	}
	if ids := keys[gate[1].Question].RecordIDs; len(ids) != 0 {
		t.Fatalf("abstention question cites %v", ids)
	}
}

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
