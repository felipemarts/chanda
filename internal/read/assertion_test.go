package read

import "testing"

// The raw fact_assertion event written by assert_fact is never a read
// candidate: its fact is the readable record, so a superseded value cannot
// come back through the event (BASE.md temporal suite).
func TestFactAssertionEventsAreNotRead(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_A", NS: "ns", Source: "src_user_ns", Kind: "fact_assertion", Content: "Ana job_title: CTO", Zone: "user"})
	f.fact(fc{ID: "fct_A", NS: "ns", Predicate: "job_title", Value: "CEO", Text: "Ana job_title: CEO", Zone: "user"})
	f.prov("ns", "fact", "fct_A", "event", "evt_A", "derived_from")
	r := New(f.e, Deps{})
	for _, mode := range []Mode{ModeSearch, ModeContext} {
		var items []Item
		if mode == ModeSearch {
			res, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "Ana job_title", Actor: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			items = res.Items
			if res.AuditID == 0 {
				t.Fatal("search must report the audit id of its retrieval")
			}
		} else {
			res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "Ana job_title", Actor: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			items = res.Items
		}
		for _, it := range items {
			if it.ID == "evt_A" {
				t.Fatalf("%s returned the fact_assertion event", mode)
			}
		}
	}
	var actor string
	if err := f.e.Store.DB().QueryRow(`SELECT actor FROM audit_log WHERE action = 'retrieve' ORDER BY id LIMIT 1`).Scan(&actor); err != nil || actor != "alice" {
		t.Fatalf("retrieve audited as %q (%v), want the caller", actor, err)
	}
	if g, err := r.Get(f.ctx, "ns", "evt_A", false); err != nil || g.Record.ID != "evt_A" {
		t.Fatalf("get must still show the event: %v", err)
	}
}
