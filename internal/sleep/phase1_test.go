package sleep

import (
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
)

// BASE.md: "Without a provider, Chanda works in full, only without free-text
// extraction and without pages." Phase 2 requested without a provider runs
// phase 1 only and says so.
func TestWithoutProviderPhase1OnlyWithNotice(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "user", "hello there")
	f.advance(time.Hour)
	fl := &fakeFlush{}
	integ := &fakeIntegrity{}
	s := New(f.e, Deps{Integrity: integ, Embeddings: fl, Utility: fl})
	res, err := s.Run(f.ctx, RunInput{Phases: []int{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusCompleted || !hasNotice(res, "no LLM provider") {
		t.Fatalf("status=%s notices=%v", res.Status, res.Notices)
	}
	if fl.embed < 1 || fl.utility != 1 || integ.calls != 2 {
		t.Fatalf("phase 1 steps: embed=%d utility=%d integrity=%d", fl.embed, fl.utility, integ.calls)
	}
	if f.str(`SELECT status FROM sleep_runs WHERE id=?`, res.RunID) != StatusCompleted ||
		!contains(f.str(`SELECT report FROM sleep_runs WHERE id=?`, res.RunID), "# Sleep run "+res.RunID) {
		t.Fatal("run row must hold status and report")
	}
	if f.auditCount(audit.ActSleepStart) != 1 || f.auditCount(audit.ActSleepEnd) != 1 {
		t.Fatal("sleep start and end must be audited")
	}
	if f.str(`SELECT snapshot FROM sleep_runs WHERE id=?`, res.RunID) != "" {
		t.Fatal("no snapshot without phase-2 changes")
	}
	f.verifyAudit()
}

func hasNotice(r *Result, sub string) bool {
	for _, n := range r.Notices {
		if contains(n, sub) {
			return true
		}
	}
	return false
}

// BASE.md Phase 1: "Proposes entity merges by name and vector similarity.
// The proposal stays pending; it is not applied on its own."
func TestPhase1ProposesMergesNeverApplies(t *testing.T) {
	f := newFixture(t)
	f.entity("ent_01", "Acme Corporation", "org", "acme corp")
	f.entity("ent_02", "ACME Corp.", "org")          // alias "acme corp" exists → canonical name matches it
	f.entity("ent_03", "Globex", "org")              // unrelated
	f.entity("ent_04", "Acme Corporation", "person") // same name, different type: never proposed
	f.entity("ent_05", "Initech", "")
	f.entity("ent_06", "Initrode", "")
	f.advance(time.Hour)
	vec := fakeVectors{"ent_05": {1, 0, 0}, "ent_06": {0.99, 0.05, 0}, "ent_03": {0, 1, 0}}
	s := New(f.e, Deps{EntityVectors: vec})
	res, err := s.Run(f.ctx, RunInput{Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Counts.MergeProposals) != 2 {
		t.Fatalf("proposals = %v", res.Counts.MergeProposals)
	}
	cases := []struct{ a, b, reason string }{
		{"ent_01", "ent_02", "name_similarity"},
		{"ent_05", "ent_06", "vector_similarity"},
	}
	for _, c := range cases {
		if got := f.str(`SELECT reason FROM entity_merge_proposals WHERE entity_a=? AND entity_b=? AND status='pending'`, c.a, c.b); got != c.reason {
			t.Errorf("%s/%s: reason %q, want %q", c.a, c.b, got, c.reason)
		}
	}
	if n := f.count(`SELECT count(*) FROM entities WHERE merged_into IS NOT NULL`); n != 0 {
		t.Fatal("phase 1 must never apply a merge")
	}
	if f.auditCount(ActMergePropose) != 2 {
		t.Fatal("each proposal is audited")
	}
	f.advance(time.Hour)
	res, err = s.Run(f.ctx, RunInput{Phases: []int{1}})
	if err != nil || len(res.Counts.MergeProposals) != 0 || f.count(`SELECT count(*) FROM entity_merge_proposals`) != 2 {
		t.Fatalf("second run must not propose again: %v %v", res.Counts.MergeProposals, err)
	}
	f.verifyAudit()
}

// Decision 0125: vector merge proposals use the threshold of the configured
// embedding model (sleep.merge_similarities); name similarity keeps
// sleep.merge_similarity whatever the model.
func TestPhase1VectorMergeThresholdPerModel(t *testing.T) {
	f := newFixture(t)
	f.e.Cfg.Embed.ModelID = "e5-small@hugot0.8.6+simd"
	f.e.Cfg.Sleep.MergeSimilarities = map[string]float64{"e5-small": 0.999}
	f.entity("ent_01", "Acme Corporation", "org", "acme corp")
	f.entity("ent_02", "ACME Corp.", "org")
	f.entity("ent_05", "Initech", "")
	f.entity("ent_06", "Initrode", "")
	f.entity("ent_07", "Umbrella", "")
	f.entity("ent_08", "Hooli", "")
	f.advance(time.Hour)
	// cosine(05, 06) ≈ 0.9987 is under the e5 threshold; (07, 08) is 1.
	vec := fakeVectors{"ent_05": {1, 0, 0}, "ent_06": {0.99, 0.05, 0}, "ent_07": {0, 0, 1}, "ent_08": {0, 0, 1}}
	res, err := New(f.e, Deps{EntityVectors: vec}).Run(f.ctx, RunInput{Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Counts.MergeProposals) != 2 {
		t.Fatalf("proposals = %v", res.Counts.MergeProposals)
	}
	for _, c := range []struct{ a, b, reason string }{
		{"ent_01", "ent_02", "name_similarity"},
		{"ent_07", "ent_08", "vector_similarity"},
	} {
		if got := f.str(`SELECT reason FROM entity_merge_proposals WHERE entity_a=? AND entity_b=?`, c.a, c.b); got != c.reason {
			t.Errorf("%s/%s: reason %q, want %q", c.a, c.b, got, c.reason)
		}
	}
	f.verifyAudit()
}

// BASE.md Phase 1: "Marks as stale_source the facts whose origin was retired."
func TestPhase1MarksStaleSourceWhenOriginRetired(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "chunk one")
	f.event("evt_2", "internal", "chunk two")
	f.event("evt_3", "internal", "chunk three")
	f.entity("ent_1", "Ana", "person")
	cases := []struct {
		fact, status string
		origins      []string
		want         string
	}{
		{"fct_1", "active", []string{"evt_1"}, "stale_source"},      // only origin retired
		{"fct_2", "active", []string{"evt_1", "evt_2"}, "active"},   // one live origin left
		{"fct_3", "quarantined", []string{"evt_1"}, "stale_source"}, // quarantined too
		{"fct_4", "superseded", []string{"evt_1"}, "superseded"},    // history untouched
		{"fct_5", "active", []string{"evt_3"}, "active"},            // live
	}
	for _, c := range cases {
		f.fact(c.fact, "ent_1", "skill", c.fact, c.status, "internal", false, c.origins...)
	}
	f.exec(`UPDATE events SET retired_at=? WHERE id='evt_1'`, f.now())
	f.advance(time.Hour)
	res, err := New(f.e, Deps{}).Run(f.ctx, RunInput{Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := f.str(`SELECT status FROM facts WHERE id=?`, c.fact); got != c.want {
			t.Errorf("%s: status %s, want %s", c.fact, got, c.want)
		}
	}
	if len(res.Counts.StaleFacts) != 2 || f.auditCount(audit.ActStaleSource) != 2 {
		t.Fatalf("stale = %v", res.Counts.StaleFacts)
	}
}

// BASE.md Forgetting step 3: "A page with any forgotten origin leaves reads
// immediately and is rebuilt in the next sleep, without the removed content."
func TestPagesNeedingRebuildAreRebuiltFromRemainingItems(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "kept")
	f.exec(`INSERT INTO pages(id,namespace,kind,title,body,version,status,trust_zone,created_at,updated_at)
		VALUES('pg_1',?,'lesson','t','old body with forgotten text',3,'needs_rebuild','internal',?,?)`, ns, f.now(), f.now())
	f.exec(`INSERT INTO page_items(id,namespace,page_id,ordinal,text,status,trust_zone,created_at) VALUES
		('pgi_1',?,'pg_1',1,'kept item','active','internal',?),
		('pgi_2',?,'pg_1',2,'forgotten item','active','internal',?)`, ns, f.now(), ns, f.now())
	f.edge("page_item", "pgi_1", "event", "evt_1", "cites")
	f.edge("page_item", "pgi_2", "event", "evt_gone", "cites") // its event was forgotten
	f.advance(time.Hour)
	res, err := New(f.e, Deps{}).Run(f.ctx, RunInput{Phases: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Counts.PagesRebuilt) != 1 {
		t.Fatalf("rebuilt = %v", res.Counts.PagesRebuilt)
	}
	if b := f.str(`SELECT body FROM pages WHERE id='pg_1'`); b != "kept item" {
		t.Fatalf("body = %q", b)
	}
	if st := f.str(`SELECT status FROM pages WHERE id='pg_1'`); st != "active" {
		t.Fatalf("status = %s", st)
	}
	if st := f.str(`SELECT status FROM page_items WHERE id='pgi_2'`); st != "retired" {
		t.Fatalf("orphan item status = %s", st)
	}
	if n := f.count(`SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'forgotten'`); n != 0 {
		t.Fatal("removed content must leave the page index")
	}
	if n := f.count(`SELECT count(*) FROM provenance WHERE derived_type='page' AND derived_id='pg_1' AND source_id='evt_1' AND relation='cites'`); n != 1 {
		t.Fatal("rebuilt page must cite the events of its remaining items")
	}
}
