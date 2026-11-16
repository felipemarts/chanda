package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Decision 0148, the conflicts tool: list shows pairs and supersessions,
// revert ends a supersession (reads see the old record again), reset
// deletes the queue and keeps supersessions; each mutating action is
// audited; stats writes nothing.
func TestConflictsToolListRevertReset(t *testing.T) {
	w := conflictScenario(t, nil)
	a := w.a
	l := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsList}))
	if !l.Enabled || l.Judged != 2 || l.Pending < 1 || l.Active != 1 || l.Reverted != 0 || len(l.Supersessions) != 1 ||
		l.Verdicts["supersedes"] != 1 || l.Reasons["uncertain:not_later"] != 1 || int64(len(l.Pairs)) != l.Pending+l.Judged || l.AuditID != 0 {
		t.Fatalf("list %+v", l)
	}
	sup := l.Supersessions[0]
	if sup.OldID != w.porto || sup.NewID != w.lisbon || sup.Status != "active" || sup.PairID == "" || sup.RunID == "" {
		t.Fatalf("supersession %+v", sup)
	}
	var applied *chanda.ConflictPair
	for i := range l.Pairs {
		if l.Pairs[i].ID == sup.PairID {
			applied = &l.Pairs[i]
		}
	}
	if applied == nil || applied.Supersession != sup.ID || applied.Reason != "applied" || applied.ModelVerdict != "supersedes" ||
		applied.ModelCurrent != "B" || applied.Confidence == nil || *applied.Confidence != 0.9 || applied.Differs != "entities" {
		t.Fatalf("applied pair %+v", applied)
	}

	const q = "Where is the office of Acme?"
	if find(must(a.Context(agent, chanda.ContextInput{Query: q})).Items, w.porto) != nil {
		t.Fatal("before revert the old record is hidden")
	}
	rv := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsRevert, ID: sup.ID}))
	if rv.RevertedID != sup.ID || rv.Active != 0 || rv.Reverted != 1 || rv.AuditID == 0 {
		t.Fatalf("revert %+v", rv)
	}
	if find(must(a.Context(agent, chanda.ContextInput{Query: q})).Items, w.porto) == nil {
		t.Fatal("after revert the old record is back")
	}
	if _, err := a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsRevert, ID: sup.ID}); err == nil {
		t.Fatal("reverting twice must fail")
	}
	if _, err := a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsRevert, ID: "sup_nope"}); err == nil {
		t.Fatal("an unknown supersession must fail")
	}
	if _, err := a.Conflicts(cli, chanda.ConflictsInput{Action: "frob"}); err == nil {
		t.Fatal("an unknown action must fail")
	}

	rs := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsReset}))
	if rs.Removed != l.Pending+l.Judged || rs.Pending != 0 || rs.Judged != 0 || rs.Reverted != 1 || rs.AuditID == 0 {
		t.Fatalf("reset %+v", rs)
	}
	after := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsList}))
	if len(after.Pairs) != 0 || len(after.Supersessions) != 1 || after.Supersessions[0].PairID != "" || after.Supersessions[0].Status != "reverted" {
		t.Fatalf("after reset %+v", after)
	}
	for _, act := range []string{audit.ActConflictsRevert, audit.ActConflictsReset} {
		if n := count(t, a, `SELECT count(*) FROM audit_log WHERE action = ?`, act); n != 1 {
			t.Fatalf("%s audited %d times", act, n)
		}
	}
	// Scan with write refills the queue; the reverted pair is never queued again.
	sc := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, Write: true}))
	if sc.Scan == nil || sc.Scan.Queued != sc.Scan.CandidatePairs-sc.Scan.AlreadySuperseded || sc.Scan.AlreadySuperseded != 1 || sc.AuditID == 0 ||
		sc.Pending != int64(sc.Scan.Queued) {
		t.Fatalf("scan write %+v %+v", sc, sc.Scan)
	}
	verifyOK(t, a)
}

// Scan counts what write-time detection queued, the guard's refusals by
// reason and the pairs by component; details lists them; min_cosine
// overrides the floor; without write nothing changes; the same database
// gives the same scan.
func TestConflictsScan(t *testing.T) {
	a, _ := newApp(t, conflictsOn)
	porto := remember(t, a, "The office of Acme is in Porto.", "2024-01-01")
	lisbon := remember(t, a, "The office of Acme is in Lisbon now.", "2024-06-01")
	remember(t, a, "The Zenith plan costs 10 dollars.", "2024-01-01")
	remember(t, a, "The Zenith plan costs 12 dollars.", "2024-06-01")
	remember(t, a, "The cafeteria menu changes on Fridays.", "")
	queued := count(t, a, `SELECT count(*) FROM conflict_candidates`)
	audits := count(t, a, `SELECT count(*) FROM audit_log`)

	sc := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, Details: true})).Scan
	if sc.EventsScanned != 5 || sc.CandidatePairs != queued || sc.Queued != 0 || sc.MinCosine != 0.3 || len(sc.Candidates) != queued {
		t.Fatalf("scan %+v (queued on write: %d)", sc, queued)
	}
	if sc.PairsByComponent["entities"] < 1 || sc.PairsByComponent["numbers"] < 1 || len(sc.PairsByComponent) != 4 || len(sc.RefusalsByReason) != 4 {
		t.Fatalf("by component %v, refusals %v", sc.PairsByComponent, sc.RefusalsByReason)
	}
	found := false
	for _, c := range sc.Candidates {
		found = found || (c.OlderID == porto && c.NewerID == lisbon && c.Differs == "entities")
	}
	if !found {
		t.Fatalf("candidates %+v", sc.Candidates)
	}
	if n := count(t, a, `SELECT count(*) FROM audit_log`); n != audits {
		t.Fatal("a scan without write is not audited and writes nothing")
	}
	again := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, Details: true})).Scan
	if !reflect.DeepEqual(sc, again) {
		t.Fatalf("scan is not deterministic:\n%+v\n%+v", sc, again)
	}
	hi := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, MinCosine: 0.999})).Scan
	if hi.CandidatePairs != 0 || hi.MinCosine != 0.999 || hi.Candidates != nil || hi.EventsScanned != 5 {
		t.Fatalf("min_cosine override %+v", hi)
	}
	if _, err := a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, MinCosine: 1}); err == nil {
		t.Fatal("min_cosine 1 must fail")
	}
	// JSON field names are part of the contract.
	b, _ := json.Marshal(sc)
	var m map[string]any
	json.Unmarshal(b, &m)
	for _, k := range []string{"min_cosine", "events_scanned", "neighbour_pairs", "candidate_pairs", "queued", "already_queued",
		"already_superseded", "near_dup_refusals_by_reason", "pairs_by_component", "candidates", "near_dup_refusals"} {
		if _, ok := m[k]; !ok {
			t.Errorf("scan JSON misses %q: %s", k, b)
		}
	}
	// Off: write-time detection queued nothing, but scan still measures.
	off, _ := newApp(t, nil)
	remember(t, off, "The office of Acme is in Porto.", "2024-01-01")
	remember(t, off, "The office of Acme is in Lisbon now.", "2024-06-01")
	o := must(off.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, MinCosine: 0.3}))
	if o.Enabled || o.Scan.CandidatePairs != 1 || len(o.Notices) == 0 || o.Pending != 0 {
		t.Fatalf("off: %+v %+v", o, o.Scan)
	}
}

// The guard's refusals: a pair above the near-duplicate threshold whose
// numbers differ is counted (and listed) under numbers_differ.
func TestConflictsScanRefusals(t *testing.T) {
	a, _ := newApp(t, func(c *config.Config) { conflictsOn(c); c.Write.NearDupThresholds = nil; c.Write.NearDupThreshold = 0.3 })
	x := remember(t, a, "The Zenith plan costs 10 dollars per month.", "2024-01-01")
	y := remember(t, a, "The Zenith plan costs 12 dollars per month.", "2024-06-01")
	sc := must(a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsScan, Details: true})).Scan
	if sc.RefusalsByReason["numbers_differ"] != 1 || len(sc.NearDupRefusals) != 1 ||
		sc.NearDupRefusals[0] != (chanda.ScanRefusal{OlderID: x, NewerID: y, Cosine: sc.NearDupRefusals[0].Cosine, Reason: "numbers_differ"}) {
		t.Fatalf("refusals %+v %+v", sc.RefusalsByReason, sc.NearDupRefusals)
	}
}

// Determinism: two runs of the same scenario give the same pairs,
// verdicts and supersessions (ids included).
func TestConflictsDeterminism(t *testing.T) {
	strip := func(o chanda.ConflictsOutput) chanda.ConflictsOutput { o.Meta = chanda.Meta{}; return o }
	x := strip(must(conflictScenario(t, nil).a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsList})))
	y := strip(must(conflictScenario(t, nil).a.Conflicts(cli, chanda.ConflictsInput{Action: chanda.ConflictsList})))
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("two runs differ:\n%+v\n%+v", x, y)
	}
}
