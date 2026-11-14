package read

import (
	"slices"
	"testing"
	"time"
)

// Decision 0093 (BASE.md "Temporal correctness"): in context mode a raw
// excerpt whose extracted fact was later replaced, rejected or flagged is
// not delivered, so the old value never reaches the agent through it. An
// explicit as_of inside the old fact's validity keeps it; a value stated as
// history keeps it; search mode keeps everything.
func TestContextDropsExcerptsOfReplacedFacts(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	const t1, t2, t3 = "2025-01-01T00:00:00.000000000Z", "2025-02-01T00:00:00.000000000Z", "2025-03-01T00:00:00.000000000Z"
	f.event(ev{ID: "evt_old", NS: "ns", Source: "src_doc_ns", Content: "Atlas project owner: Marina Duarte.", RecordedAt: t1})
	f.event(ev{ID: "evt_new", NS: "ns", Source: "src_doc_ns", Content: "Atlas project owner: Bruno Lima.", RecordedAt: t2})
	f.event(ev{ID: "evt_hist", NS: "ns", Source: "src_doc_ns", Content: "Atlas project owner in 2025 was Rui Alves.", RecordedAt: t3})
	f.event(ev{ID: "evt_wrong", NS: "ns", Source: "src_doc_ns", Content: "Atlas project owner: Zeca.", RecordedAt: t3})
	f.event(ev{ID: "evt_plain", NS: "ns", Source: "src_doc_ns", Content: "Atlas project owner meetings happen on Mondays.", RecordedAt: t1})
	f.fact(fc{ID: "fct_new", NS: "ns", Predicate: "owner", Value: "Bruno Lima", Text: "Atlas owner: Bruno Lima", ValidFrom: t2, RecordedAt: t2})
	f.fact(fc{ID: "fct_old", NS: "ns", Predicate: "owner", Value: "Marina Duarte", Text: "Atlas owner: Marina Duarte",
		Status: "superseded", ValidFrom: t1, ValidTo: t2, RecordedAt: t1})
	f.fact(fc{ID: "fct_hist", NS: "ns", Predicate: "owner", Value: "Rui Alves", Text: "Atlas owner: Rui Alves",
		Status: "superseded", ValidFrom: "2024-01-01T00:00:00.000000000Z", ValidTo: t1, RecordedAt: t3})
	f.fact(fc{ID: "fct_wrong", NS: "ns", Predicate: "owner", Value: "Zeca", Text: "Atlas owner: Zeca", Status: "rejected", RecordedAt: t3})
	f.exec(`UPDATE facts SET superseded_by = 'fct_new' WHERE id = 'fct_old'`)
	f.exec(`UPDATE facts SET superseded_by = 'fct_old' WHERE id = 'fct_hist'`)
	for _, e := range [][2]string{{"fct_old", "evt_old"}, {"fct_new", "evt_new"}, {"fct_hist", "evt_hist"}, {"fct_wrong", "evt_wrong"}} {
		f.prov("ns", "fact", e[0], "event", e[1], "derived_from")
	}
	r := New(f.e, Deps{})
	ctxIDs := func(asOf time.Time) []string {
		res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "Atlas project owner", AsOf: asOf, BudgetTokens: 1500})
		if err != nil {
			t.Fatal(err)
		}
		return ids(res.Items)
	}
	now := ctxIDs(time.Time{})
	for _, id := range []string{"evt_old", "evt_wrong"} {
		if slices.Contains(now, id) {
			t.Errorf("%s delivered in context: %v", id, now)
		}
	}
	for _, id := range []string{"evt_new", "evt_hist", "evt_plain", "fct_new"} {
		if !slices.Contains(now, id) {
			t.Errorf("%s missing from context: %v", id, now)
		}
	}
	then := ctxIDs(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	if !slices.Contains(then, "evt_old") {
		t.Errorf("as_of inside the old validity must keep the excerpt: %v", then)
	}
	s, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "Atlas project owner"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(s.Items), "evt_old") {
		t.Errorf("search keeps every live event: %v", ids(s.Items))
	}
}
