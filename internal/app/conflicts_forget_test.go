package app

import (
	"strings"
	"testing"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// Decision 0148: forgetting either record of a pair removes its queue row
// and any supersession that names it, counted in the report and the
// tombstone; the forget verification (no reference to a removed id
// anywhere) passes.
func TestConflictsForgetCascade(t *testing.T) {
	for _, side := range []string{"old", "new"} {
		w := conflictScenario(t, nil)
		a := w.a
		target := w.porto
		if side == "new" {
			target = w.lisbon
		}
		pairs := count(t, a, `SELECT count(*) FROM conflict_candidates WHERE older_id = ? OR newer_id = ?`, target, target)
		if pairs == 0 || count(t, a, `SELECT count(*) FROM supersessions`) != 1 {
			t.Fatalf("%s: setup: %d pairs", side, pairs)
		}
		f := must(a.Forget(cli, chanda.ForgetInput{Scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: target}}))
		if !f.Verification.Passed || f.Removed.ConflictPairs != int64(pairs) || f.Removed.Supersessions != 1 {
			t.Fatalf("%s: forget %+v", side, f)
		}
		if n := count(t, a, `SELECT count(*) FROM conflict_candidates WHERE older_id = ? OR newer_id = ?`, target, target); n != 0 {
			t.Fatalf("%s: %d pairs left", side, n)
		}
		if n := count(t, a, `SELECT count(*) FROM supersessions`); n != 0 {
			t.Fatalf("%s: %d supersessions left", side, n)
		}
		counts := str(t, a, `SELECT removed_counts FROM tombstones WHERE id = ?`, f.TombstoneID)
		if !strings.Contains(counts, `"supersessions":1`) || !strings.Contains(counts, `"conflict_candidates":`) {
			t.Fatalf("%s: tombstone counts %s", side, counts)
		}
		verifyOK(t, a)
	}
}

// A forget that touches no pair reports no conflict counts (the outputs of
// existing forgets are unchanged).
func TestConflictsForgetOtherRecord(t *testing.T) {
	w := conflictScenario(t, nil)
	other := remember(t, w.a, "The cafeteria menu changes on Fridays.", "")
	f := must(w.a.Forget(cli, chanda.ForgetInput{Scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: other}}))
	if f.Removed.ConflictPairs != 0 || f.Removed.Supersessions != 0 || !f.Verification.Passed {
		t.Fatalf("forget %+v", f)
	}
	if counts := str(t, w.a, `SELECT removed_counts FROM tombstones WHERE id = ?`, f.TombstoneID); strings.Contains(counts, "supersessions") ||
		strings.Contains(counts, "conflict") {
		t.Fatalf("tombstone counts %s", counts)
	}
	if n := count(t, w.a, `SELECT count(*) FROM supersessions`); n != 1 {
		t.Fatalf("%d supersessions", n)
	}
}
