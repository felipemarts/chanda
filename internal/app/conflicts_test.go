package app

import (
	"database/sql"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/conflicts"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// conflictsOn turns decision 0148 on with a floor the hash embedder's
// cosines between related sentences clear.
func conflictsOn(c *config.Config) {
	c.Sleep.Conflicts.Enabled = true
	c.Sleep.Conflicts.MinCosine = 0.3
}

func remember(t *testing.T, a *App, content, validFrom string) string {
	t.Helper()
	return must(a.Remember(agent, chanda.RememberInput{Content: content, ValidFrom: validFrom})).ID
}

// Decision 0148, write path: an update of a fact stated in free text forms
// one pending pair (older, newer by effective time); a duplicate, an
// unrelated record and a same-signature record do not; nothing else
// changes; off, the queue stays empty.
func TestConflictDetectionOnWrite(t *testing.T) {
	for _, on := range []bool{false, true} {
		mut := func(c *config.Config) {}
		if on {
			mut = conflictsOn
		}
		a, _ := newApp(t, mut)
		porto := remember(t, a, "The office of Acme is in Porto.", "2024-01-01")
		lisbon := remember(t, a, "The office of Acme is in Lisbon now.", "2024-06-01")
		remember(t, a, "The cafeteria menu changes on Fridays.", "2024-07-01")
		remember(t, a, "The office of Acme is in Lisbon now!", "2024-08-01") // near-duplicate of lisbon
		n := count(t, a, `SELECT count(*) FROM conflict_candidates`)
		if !on {
			if n != 0 {
				t.Fatalf("off: %d queued pairs", n)
			}
			continue
		}
		var older, newer, differs, shared, status string
		var cos float64
		if err := a.e.Store.DB().QueryRow(`SELECT older_id, newer_id, cosine, differs, shared, status FROM conflict_candidates
			WHERE older_id = ? AND newer_id = ?`, porto, lisbon).Scan(&older, &newer, &cos, &differs, &shared, &status); err != nil {
			t.Fatalf("the Porto/Lisbon pair is missing: %v", err)
		}
		if differs != conflicts.CompEntities || status != conflicts.StatusPending || shared != `["acme","office"]` || cos < 0.3 {
			t.Fatalf("pair: %s %s %v %s", differs, status, cos, shared)
		}
		if k := count(t, a, `SELECT count(*) FROM conflict_candidates WHERE older_id = ? OR newer_id = ?`, lisbon, lisbon); k > 2 {
			t.Fatalf("%d pairs name the Lisbon record", k)
		}
		// Stored once: the same pair queued again changes nothing.
		err := a.e.Store.Tx(bg, func(tx *sql.Tx) error {
			k, err := conflicts.Enqueue(bg, tx, a.e.IDs, "ns", "2024-01-01T00:00:00.000000000Z",
				[]conflicts.Pair{{OlderID: porto, NewerID: lisbon, Cosine: 0.5, Differs: "entities", Shared: []string{"acme"}}})
			if k != 0 {
				t.Errorf("re-queued %d", k)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		verifyOK(t, a)
	}
}

// max_per_record bounds the pairs of one new record.
func TestConflictDetectionCap(t *testing.T) {
	a, _ := newApp(t, func(c *config.Config) { conflictsOn(c); c.Sleep.Conflicts.MaxPerRecord = 1 })
	remember(t, a, "The office of Acme is in Porto.", "2024-01-01")
	remember(t, a, "The office of Acme is in Braga.", "2024-02-01")
	last := remember(t, a, "The office of Acme is in Lisbon now.", "2024-06-01")
	if n := count(t, a, `SELECT count(*) FROM conflict_candidates WHERE newer_id = ? OR older_id = ?`, last, last); n != 1 {
		t.Fatalf("max_per_record 1: the new record has %d pairs", n)
	}
}
