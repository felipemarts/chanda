package eval

import (
	"context"
	"math"
	"testing"

	"github.com/felipemarts/chanda/internal/env"
)

// BASE.md "Gate set": each namespace has a fixed, approved question set used
// by the sleep gate and by ranking promotion. Only approved rows of the
// namespace are read, in a total order.
func TestLoadGateQuestionsReadsApprovedOnly(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	for _, r := range []struct{ id, ns, q, exp, status string }{
		{"gq_2", "ns", "Quanto custa o plano A?", `{"record_ids":["evt_a"],"answers":["10"]}`, "approved"},
		{"gq_1", "ns", "Quem é o CEO?", `{}`, "approved"},
		{"gq_3", "ns", "proposta", `{}`, "proposed"},
		{"gq_4", "ns", "aposentada", `{}`, "retired"},
		{"gq_5", "other", "outro namespace", `{}`, "approved"},
	} {
		if _, err := e.Store.DB().ExecContext(ctx,
			`INSERT INTO gate_questions(id, namespace, question, expected, status, created_at) VALUES (?,?,?,?,?,?)`,
			r.id, r.ns, r.q, r.exp, r.status, "2026-01-01T00:00:00.000000000Z"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadGateQuestions(ctx, e.Store.DB(), "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "gq_1" || got[1].ID != "gq_2" || got[1].Expected.RecordIDs[0] != "evt_a" || got[1].Expected.Answers[0] != "10" {
		t.Fatalf("got %+v", got)
	}
	if _, err := e.Store.DB().ExecContext(ctx,
		`INSERT INTO gate_questions(id, namespace, question, expected, status, created_at) VALUES ('gq_9','ns','x','{"ids":1}','approved','t')`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGateQuestions(ctx, e.Store.DB(), "ns"); err == nil {
		t.Fatal("malformed expected JSON accepted")
	}
}

type scripted map[string][]Retrieved

func (s scripted) Name() string                       { return "scripted" }
func (s scripted) Ingest(context.Context, Item) error { return nil }
func (s scripted) Close() error                       { return nil }
func (s scripted) Retrieve(_ context.Context, q Query, _ int) ([]Retrieved, error) {
	return s[q.Text], nil
}

// Hand-computed gate score:
//
//	g1: ids [evt_a], answers ["10"]; result [evt_x "nada", evt_a "custa 10"]
//	    nDCG@10 = (1/log2 3) / 1 = 0.6309298, answer hit = 1 -> 0.8154649
//	g2: abstention; result empty -> 1
//	g3: answers ["Maria"]; result "João" -> 0
//	score = 100 * (0.8154649 + 1 + 0) / 3 = 60.5154959
func TestGateScoreHandComputed(t *testing.T) {
	sys := scripted{
		"g1": {{ID: "evt_x", Text: "nada"}, {ID: "evt_a", Text: "custa 10"}},
		"g3": {{ID: "evt_j", Text: "João"}},
	}
	qs := []GateQuestion{
		{ID: "1", Question: "g1", Expected: GateExpected{RecordIDs: []string{"evt_a"}, Answers: []string{"10"}}},
		{ID: "2", Question: "g2"},
		{ID: "3", Question: "g3", Expected: GateExpected{Answers: []string{"Maria"}}},
	}
	got, err := GateScore(context.Background(), sys, qs)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-60.5154959) > 1e-6 {
		t.Fatalf("score %v", got)
	}
	if _, err := GateScore(context.Background(), sys, nil); err == nil {
		t.Fatal("empty gate set must fail")
	}
	// Budget enforcement: with a zero budget nothing fits; only the abstention question scores.
	got, _ = GateScoreBudget(context.Background(), sys, qs, 0)
	if math.Abs(got-100.0/3) > 1e-9 {
		t.Fatalf("budget-limited score %v", got)
	}
}
