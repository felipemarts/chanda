package read

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
)

// BASE.md MCP "get": one record by id, with its origins; expand_neighbors.
func TestGet(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	corpus(f, "other")
	// A third chunk in version 2 of the document; version 2 keeps backup and the new chunk.
	f.event(ev{ID: "evt_new_ns", NS: "ns", Source: "src_doc_ns", DocID: "doc_ops_ns", DocVersion: 2, Ordinal: 1, Content: "Restore drills run quarterly."})
	f.exec(`UPDATE documents SET current_version = 2 WHERE id = 'doc_ops_ns'`)
	f.chunk("ns", "doc_ops_ns", 2, 0, "evt_backup_ns")
	f.chunk("ns", "doc_ops_ns", 2, 1, "evt_new_ns")
	f.event(ev{ID: "evt_t1", NS: "ns", Source: "src_user_ns", Kind: "turn", Zone: "user", SessionID: "s1", Ordinal: 1, Content: "hi"})
	f.event(ev{ID: "evt_t2", NS: "ns", Source: "src_user_ns", Kind: "turn", Zone: "user", SessionID: "s1", Ordinal: 2, Content: "there"})
	f.event(ev{ID: "evt_t3", NS: "ns", Source: "src_user_ns", Kind: "turn", Zone: "user", SessionID: "s1", Ordinal: 3, Content: "bye"})
	r := New(f.e, Deps{})

	g, err := r.Get(f.ctx, "ns", "fct_backup_ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if g.Record.Value != "02:00" || g.Record.Type != core.RecFact || !reflect.DeepEqual(g.Origin.EventIDs, []string{"evt_backup_ns"}) ||
		len(g.OriginEvents) != 1 || g.OriginEvents[0].DocID != "doc_ops_ns" || len(g.Edges) != 1 ||
		!reflect.DeepEqual(g.Origin.DocURIs, []string{"file:///ops-ns.md"}) {
		t.Fatalf("fact: %+v", g)
	}
	g, _ = r.Get(f.ctx, "ns", "pg_backup_ns", false)
	if g.Record.Title == "" || !reflect.DeepEqual(g.Origin.EventIDs, []string{"evt_backup_ns"}) {
		t.Fatalf("page: %+v", g)
	}

	// Neighbours in the current document version (2): backup is ordinal 0, new is 1.
	g, _ = r.Get(f.ctx, "ns", "evt_backup_ns", true)
	if g.Previous != nil || g.Next == nil || g.Next.ID != "evt_new_ns" {
		t.Fatalf("neighbours v2: prev=%+v next=%+v", g.Previous, g.Next)
	}
	// The deploy chunk only exists in version 1: neighbours come from version 1.
	g, _ = r.Get(f.ctx, "ns", "evt_deploy_ns", true)
	if g.Previous == nil || g.Previous.ID != "evt_backup_ns" || g.Next != nil {
		t.Fatalf("neighbours v1: prev=%+v next=%+v", g.Previous, g.Next)
	}
	g, _ = r.Get(f.ctx, "ns", "evt_t2", true)
	if g.Previous == nil || g.Previous.ID != "evt_t1" || g.Next == nil || g.Next.ID != "evt_t3" {
		t.Fatalf("session neighbours: %+v %+v", g.Previous, g.Next)
	}
	if g, _ := r.Get(f.ctx, "ns", "evt_t2", false); g.Previous != nil || g.Next != nil {
		t.Fatal("neighbours only on request")
	}

	if _, err := r.Get(f.ctx, "ns", "evt_backup_other", false); core.CodeOf(err) != core.CodeNotFound {
		t.Fatalf("other namespace must be not_found: %v", err)
	}
	if _, err := r.Get(f.ctx, "ns", "bogus", false); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := r.Get(f.ctx, "ns", "evt_missing", false); core.CodeOf(err) != core.CodeNotFound {
		t.Fatalf("missing: %v", err)
	}
}

// BASE.md MCP "explain": scores at each stage for a query and a record.
func TestExplain(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	f.event(ev{ID: "evt_retired", NS: "ns", Source: "src_doc_ns", Content: "old database backup policy", Retired: t0})
	r := New(f.e, Deps{})
	res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "database backup policy"})
	if err != nil {
		t.Fatal(err)
	}
	ex, err := r.Explain(f.ctx, "ns", res.RetrievalID, res.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ex.Found || !ex.Candidate.Delivered() || ex.Candidate.RRF == nil || ex.Candidate.Final == nil || ex.Mode != ModeContext || ex.Query != "database backup policy" {
		t.Fatalf("explain: %+v", ex)
	}
	// The retired event was filtered in the keyword query itself, so it never became a candidate.
	ex, _ = r.Explain(f.ctx, "ns", res.RetrievalID, "evt_retired")
	if ex.Found {
		t.Fatalf("retired event must not be a candidate: %+v", ex.Candidate)
	}
	ex, _ = r.Explain(f.ctx, "ns", res.RetrievalID, "evt_deploy_ns")
	if ex.Found {
		t.Fatalf("non-matching record: %+v", ex)
	}
	if _, err := r.Explain(f.ctx, "other", res.RetrievalID, "x"); core.CodeOf(err) != core.CodeNotFound {
		t.Fatalf("explain across namespaces: %v", err)
	}
}

// BASE.md MCP "stats": counts, size, last sleep, ranking state.
func TestStats(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	f.event(ev{ID: "evt_r", NS: "ns", Source: "src_doc_ns", Content: "x", Retired: t0})
	f.fact(fc{ID: "fct_q", NS: "ns", Text: "x", Status: "quarantined"})
	f.exec(`INSERT INTO sleep_runs(id,namespace,started_at,ended_at,phases,budget,status) VALUES
		('run_1','ns','2026-01-01T00:00:00.000000000Z','2026-01-01T00:01:00.000000000Z','[1]','{}','completed'),
		('run_2','ns','2026-01-02T00:00:00.000000000Z',NULL,'[1]','{}','running')`)
	f.exec(`INSERT INTO rank_state(namespace,enabled,lambda,outcome_n,updated_at) VALUES ('ns',1,0.1,7,?)`, t0)
	f.exec(`INSERT INTO embed_queue(namespace,record_type,record_id,enqueued_at) VALUES ('ns','event','evt_kiwi_ns',?)`, t0)
	r := New(f.e, Deps{})
	st, err := r.Stats(f.ctx, "ns")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"events": 4, "events.live": 3, "events.retired": 1, "facts": 2, "facts.active": 1, "facts.quarantined": 1,
		"pages": 1, "pages.active": 1, "sources": 3, "documents": 1, "provenance": 2}
	for k, v := range want {
		if st.Counts[k] != v {
			t.Errorf("count %s = %d, want %d", k, st.Counts[k], v)
		}
	}
	if st.DBSizeBytes <= 0 || st.LastSleep == nil || st.LastSleep.ID != "run_2" || st.Ranking == nil || st.Ranking.Lambda != 0.1 ||
		!st.Ranking.Enabled || st.EmbedQueue != 1 {
		t.Fatalf("stats: %+v %+v %+v", st, st.LastSleep, st.Ranking)
	}
	empty, _ := r.Stats(f.ctx, "nobody")
	if empty.Counts["events"] != 0 || empty.LastSleep != nil || empty.Ranking != nil {
		t.Fatalf("empty namespace: %+v", empty)
	}
}
