package app

import (
	"testing"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Decision 0062: an accepted merge sets entities.merged_into and moves the
// aliases; facts keep their old subject id. Timeline, as_of, current reads,
// reconciliation and get must follow the merge (decision 0093).
func TestReadersFollowAcceptedMerges(t *testing.T) {
	a, _ := newApp(t, nil)
	job := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Marina Duarte", Predicate: "job_title", Value: "Engineer", ValidFrom: "2025-01-01"}))
	loc := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "M. Duarte", Predicate: "location", Value: "Recife", ValidFrom: "2025-01-01"}))
	survivor := str(t, a, `SELECT subject FROM facts WHERE id = ?`, job.ID)
	merged := str(t, a, `SELECT subject FROM facts WHERE id = ?`, loc.ID)
	if survivor == merged {
		t.Fatal("fixture: two entities expected")
	}
	// The merge as sleep applies it (phase 2 verdict supersede on a proposal).
	db := a.e.Store.DB()
	if _, err := db.Exec(`UPDATE entities SET merged_into = ? WHERE id = ?`, survivor, merged); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE entity_aliases SET entity_id = ? WHERE entity_id = ?`, survivor, merged); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Marina Duarte", "m. duarte", merged} {
		tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: name}))
		if tl.Subject != survivor || tl.SubjectName != "Marina Duarte" || len(tl.Entries) != 2 {
			t.Fatalf("timeline(%q) %+v", name, tl)
		}
	}
	tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Marina Duarte", Predicate: "location", AsOf: "2025-06-01"}))
	if len(tl.Entries) != 1 || tl.Entries[0].Value != "Recife" {
		t.Fatalf("as_of %+v", tl.Entries)
	}
	cur, err := a.facts.Current(bg, "ns", "Marina Duarte", "")
	if err != nil || len(cur) != 2 {
		t.Fatalf("current %+v %v", cur, err)
	}

	// Reconciliation sees the merged entity's facts: a new location
	// supersedes the old one although it was written on the old id.
	next := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Marina Duarte", Predicate: "location", Value: "Lisboa", ValidFrom: "2026-01-01"}))
	if next.Action != chanda.ActionSuperseded || len(next.Related) != 1 || next.Related[0] != loc.ID {
		t.Fatalf("assert after merge %+v", next.WriteResult)
	}
	tl = must(a.Timeline(cli, chanda.TimelineInput{Subject: "Marina Duarte", Predicate: "location"}))
	if len(tl.Entries) != 2 || tl.Entries[0].Status != "superseded" || tl.Entries[1].Value != "Lisboa" {
		t.Fatalf("location timeline %+v", tl.Entries)
	}

	g := must(a.Get(cli, chanda.GetInput{ID: loc.ID}))
	if g.Fact == nil || g.Fact.Subject != survivor || g.Fact.SubjectName != "Marina Duarte" {
		t.Fatalf("get follows the merge: %+v", g.Fact)
	}
	s := must(a.Search(cli, chanda.SearchInput{Query: "Recife location", IncludeHistory: true}))
	found := false
	for _, it := range s.Items {
		if it.ID == loc.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("search %+v", s.Items)
	}
	verifyOK(t, a)
}
