package read

import (
	"database/sql"
	"slices"
	"testing"

	"strings"
)

func (f *fx) count(q string) int {
	f.t.Helper()
	var n int
	if err := f.e.Store.DB().QueryRowContext(f.ctx, q).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// Decision 0061: the gate evaluator is read-only and runs on the gate
// transaction. Score logs no retrieval, writes no audit row, draws no id,
// and inside a transaction gives the keyword-only read path's answer.
func TestScoreIsReadOnlyAndRunsOnTheGivenQueryer(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_1", NS: "ns", Source: "src_doc_ns", Content: "The deploy pipeline runs every night at two."})
	f.event(ev{ID: "evt_2", NS: "ns", Source: "src_doc_ns", Content: "The pipeline owner is the platform team."})
	f.event(ev{ID: "evt_3", NS: "ns", Source: "src_doc_ns", Content: "Lunch is served at noon."})
	keyword := New(f.e, Deps{})
	full := New(f.e, Deps{Embedder: plainEmb{}, VectorIndex: &fakeIndex{}, Utility: fakeUtility{}, Learned: fakeLearned{}})

	want, err := keyword.Context(f.ctx, ContextInput{Namespace: "ns", Query: "deploy pipeline"})
	if err != nil {
		t.Fatal(err)
	}
	logged, audited := f.count(`SELECT count(*) FROM retrievals`), f.count(`SELECT count(*) FROM audit_log`)

	var got ScoreResult
	err = f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		got, err = full.Score(f.ctx, ScoreInput{Namespace: "ns", Query: "deploy pipeline"}, ScoreOptions{Q: tx})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(got.Items), ids(want.Items)) || len(got.Items) == 0 || !strings.Contains(got.Text, scoreRetrievalID) {
		t.Fatalf("score in tx %v, keyword context %v", ids(got.Items), ids(want.Items))
	}
	if f.count(`SELECT count(*) FROM retrievals`) != logged || f.count(`SELECT count(*) FROM audit_log`) != audited {
		t.Fatal("Score must not log a retrieval or write an audit row")
	}
	// Outside a transaction it is the full read path (vectors and utility).
	s, err := full.Score(f.ctx, ScoreInput{Namespace: "ns", Query: "deploy pipeline", Mode: ModeSearch}, ScoreOptions{})
	if err != nil || len(s.Items) == 0 || s.Text != "" {
		t.Fatalf("search score %+v %v", s, err)
	}
	if f.count(`SELECT count(*) FROM retrievals`) != logged {
		t.Fatal("Score must not log a retrieval")
	}
}

// Decision 0070: λ weights the evaluated model's term only when given; a
// fixed λ of 0 is the baseline.
func TestScoreFixedLambdaAndTerm(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_a", NS: "ns", Source: "src_doc_ns", Content: "alpha beta gamma delta"})
	f.event(ev{ID: "evt_b", NS: "ns", Source: "src_doc_ns", Content: "alpha beta something else entirely different"})
	r := New(f.e, Deps{})
	in := ScoreInput{Namespace: "ns", Query: "alpha beta gamma", Mode: ModeSearch}
	zero, one := 0.0, 0.3
	base, err := r.Score(f.ctx, in, ScoreOptions{Lambda: &zero, Term: fakeTerm{"evt_b": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(base.Items), []string{"evt_a", "evt_b"}) {
		t.Fatalf("baseline order %v", ids(base.Items))
	}
	learned, err := r.Score(f.ctx, in, ScoreOptions{Lambda: &one, Term: fakeTerm{"evt_b": 1, "evt_a": 0}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(learned.Items), []string{"evt_b", "evt_a"}) {
		t.Fatalf("a strong term at λ = 0.3 must reorder: %v", ids(learned.Items))
	}
}
