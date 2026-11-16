package read

import (
	"reflect"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/core"
)

// BASE.md read step 4 ("Filter by rule") and the modes table row
// "History and superseded facts".
func TestRuleFilter(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.basic("other")
	f.event(ev{ID: "evt_ok", NS: "ns", Source: "src_doc_ns", Content: "x"})
	f.event(ev{ID: "evt_retired", NS: "ns", Source: "src_doc_ns", Content: "x", Retired: t0})
	f.event(ev{ID: "evt_instr", NS: "ns", Source: "src_web_ns", Kind: "web", Zone: "external", Content: "x", Flags: "instruction_like"})
	f.event(ev{ID: "evt_flagged", NS: "ns", Source: "src_doc_ns", Content: "x"})
	f.flag("ns", "event", "evt_flagged")
	f.event(ev{ID: "evt_other", NS: "other", Source: "src_doc_other", Content: "x"})
	f.fact(fc{ID: "fct_active", NS: "ns", Text: "x", ValidFrom: "2026-01-01T00:00:00Z"})
	f.fact(fc{ID: "fct_future", NS: "ns", Text: "x", ValidFrom: "2026-06-01T00:00:00Z"})
	f.fact(fc{ID: "fct_closed", NS: "ns", Text: "x", ValidFrom: "2025-01-01T00:00:00Z", ValidTo: "2026-02-01T00:00:00Z"})
	f.fact(fc{ID: "fct_superseded", NS: "ns", Text: "x", Status: "superseded", ValidFrom: "2025-01-01T00:00:00Z", ValidTo: "2026-01-01T00:00:00Z"})
	for _, st := range []string{"quarantined", "rejected", "flagged", "stale_source"} {
		f.fact(fc{ID: "fct_" + st, NS: "ns", Text: "x", Status: st})
	}
	f.page(pg{ID: "pg_active", NS: "ns", Title: "t", Body: "b"})
	f.page(pg{ID: "pg_staged", NS: "ns", Title: "t", Body: "b", Status: "staged"})
	f.page(pg{ID: "pg_rebuild", NS: "ns", Title: "t", Body: "b", Status: "needs_rebuild"})
	f.page(pg{ID: "pg_ext", NS: "ns", Title: "t", Body: "b", Zone: "external"})

	r := New(f.e, Deps{})
	recs, err := r.loadRecords(f.ctx, "ns", map[core.RecordType][]string{
		core.RecEvent: {"evt_ok", "evt_retired", "evt_instr", "evt_flagged", "evt_other"},
		core.RecFact:  {"fct_active", "fct_future", "fct_closed", "fct_superseded", "fct_quarantined", "fct_rejected", "fct_flagged", "fct_stale_source"},
		core.RecPage:  {"pg_active", "pg_staged", "pg_rebuild", "pg_ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	jan15 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	search := filterOpts{ns: "ns", asOf: march, mode: ModeSearch}
	history := filterOpts{ns: "ns", asOf: march, mode: ModeSearch, includeHistory: true}
	ctxHistory := filterOpts{ns: "ns", asOf: march, mode: ModeContext, includeHistory: true}
	context := filterOpts{ns: "ns", asOf: march, mode: ModeContext}
	cases := []struct {
		name string
		id   string
		o    filterOpts
		want string
	}{
		{"event without retired_at stays", "evt_ok", search, ""},
		{"retired event dropped", "evt_retired", search, dropRetired},
		{"instruction_like returned in search", "evt_instr", search, ""},
		{"instruction_like never in context", "evt_instr", context, dropInstruction},
		{"flagged record (flags table) dropped", "evt_flagged", search, dropFlagged},
		{"other namespace never loaded", "evt_other", search, dropNotFound},
		{"active fact valid at as_of stays", "fct_active", search, ""},
		{"fact not yet valid at as_of dropped", "fct_future", search, dropNotValid},
		{"fact closed before as_of dropped", "fct_closed", search, dropNotValid},
		{"fact valid at earlier as_of stays", "fct_closed", filterOpts{ns: "ns", asOf: jan15, mode: ModeSearch}, ""},
		{"superseded dropped by default", "fct_superseded", search, dropStatus},
		{"superseded returned with include_history in search", "fct_superseded", history, ""},
		{"include_history ignored in context", "fct_superseded", ctxHistory, dropStatus},
		{"explicit as_of selects the superseded fact valid then", "fct_superseded", filterOpts{ns: "ns", asOf: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), asOfExplicit: true, mode: ModeContext}, ""},
		{"explicit as_of outside its window still drops it", "fct_superseded", filterOpts{ns: "ns", asOf: march, asOfExplicit: true, mode: ModeContext}, dropNotValid},
		{"quarantined never", "fct_quarantined", history, dropStatus},
		{"rejected never", "fct_rejected", history, dropStatus},
		{"flagged status never", "fct_flagged", history, dropStatus},
		{"stale_source never", "fct_stale_source", history, dropStatus},
		{"active page stays", "pg_active", search, ""},
		{"staged page dropped", "pg_staged", search, dropStatus},
		{"needs_rebuild page dropped", "pg_rebuild", search, dropStatus},
		{"type filter", "pg_active", filterOpts{ns: "ns", asOf: march, mode: ModeSearch, types: map[core.RecordType]bool{core.RecFact: true}}, dropType},
		{"zone filter", "pg_ext", filterOpts{ns: "ns", asOf: march, mode: ModeSearch, zones: map[core.Zone]bool{core.ZoneInternal: true}}, dropZone},
		{"zone filter keeps matching zone", "pg_ext", filterOpts{ns: "ns", asOf: march, mode: ModeSearch, zones: map[core.Zone]bool{core.ZoneExternal: true}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ruleFilter(recs[c.id], c.o); got != c.want {
				t.Fatalf("ruleFilter(%s) = %q, want %q", c.id, got, c.want)
			}
		})
	}
	// Defence: a record whose namespace differs is dropped even if loaded.
	forged := *recs["evt_ok"]
	forged.ns = "other"
	if got := ruleFilter(&forged, search); got != dropNamespace {
		t.Fatalf("forged namespace: %q", got)
	}
}

// BASE.md read step 4: near-duplicates are collapsed into the most recent record.
func TestNearDuplicatesCollapseIntoMostRecent(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_old", NS: "ns", Source: "src_doc_ns", Content: "x", RecordedAt: "2026-01-01T00:00:00.000000000Z"})
	f.event(ev{ID: "evt_mid", NS: "ns", Source: "src_doc_ns", Content: "x", RecordedAt: "2026-01-02T00:00:00.000000000Z"})
	f.event(ev{ID: "evt_new", NS: "ns", Source: "src_doc_ns", Content: "x", RecordedAt: "2026-01-03T00:00:00.000000000Z"})
	f.event(ev{ID: "evt_newest_retired", NS: "ns", Source: "src_doc_ns", Content: "x", RecordedAt: "2026-01-04T00:00:00.000000000Z", Retired: t0})
	f.event(ev{ID: "evt_alone", NS: "ns", Source: "src_doc_ns", Content: "x"})
	// mid dup of old, new dup of mid (transitive), retired newest dup of new.
	f.prov("ns", "event", "evt_mid", "event", "evt_old", core.RelDuplicateOf)
	f.prov("ns", "event", "evt_new", "event", "evt_mid", core.RelDuplicateOf)
	f.prov("ns", "event", "evt_newest_retired", "event", "evt_new", core.RelDuplicateOf)
	r := New(f.e, Deps{})
	o := filterOpts{ns: "ns", asOf: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), mode: ModeSearch}

	// Candidates old and alone: old's group is represented by evt_new (not a candidate).
	recs, _ := r.loadRecords(f.ctx, "ns", map[core.RecordType][]string{core.RecEvent: {"evt_old", "evt_alone"}})
	rep, extra, err := r.collapseDuplicates(f.ctx, recs, o)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"evt_old": "evt_new", "evt_alone": "evt_alone"}
	if !reflect.DeepEqual(rep, want) {
		t.Fatalf("rep = %v, want %v", rep, want)
	}
	if len(extra) != 1 || extra["evt_new"] == nil {
		t.Fatalf("extra = %v", extra)
	}
}

func TestOriginEventsWalksProvenance(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_1", NS: "ns", Source: "src_doc_ns", Content: "x"})
	f.event(ev{ID: "evt_2", NS: "ns", Source: "src_doc_ns", Content: "x"})
	f.event(ev{ID: "evt_3", NS: "ns", Source: "src_doc_ns", Content: "x"})
	f.fact(fc{ID: "fct_1", NS: "ns", Text: "x"})
	f.fact(fc{ID: "fct_old", NS: "ns", Text: "x"})
	f.prov("ns", "fact", "fct_1", "event", "evt_1", core.RelDerivedFrom)
	f.prov("ns", "fact", "fct_1", "fact", "fct_old", core.RelSupersedes) // not an origin
	f.prov("ns", "fact", "fct_old", "event", "evt_3", core.RelDerivedFrom)
	f.page(pg{ID: "pg_1", NS: "ns", Title: "t", Body: "b"})
	f.exec(`INSERT INTO page_items(id,namespace,page_id,ordinal,text,status,trust_zone,created_at) VALUES ('pgi_1','ns','pg_1',1,'b','active','internal',?)`, t0)
	f.prov("ns", "page_item", "pgi_1", "event", "evt_2", core.RelCites)
	f.prov("ns", "page", "pg_1", "fact", "fct_1", core.RelCites)
	r := New(f.e, Deps{})
	got, _, err := r.originEvents(f.ctx, "ns", core.RecFact, "fct_1")
	if err != nil || !reflect.DeepEqual(got, []string{"evt_1"}) {
		t.Fatalf("fact origins = %v %v", got, err)
	}
	got, edges, err := r.originEvents(f.ctx, "ns", core.RecPage, "pg_1")
	if err != nil || !reflect.DeepEqual(got, []string{"evt_1", "evt_2"}) || len(edges) != 4 {
		t.Fatalf("page origins = %v %v %v", got, edges, err)
	}
}
