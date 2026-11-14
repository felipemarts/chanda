package forget_test

import (
	"context"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/forget"
	"github.com/felipemarts/chanda-mcp/internal/prov"
)

func probe(v *forget.Verification, name string) forget.Probe {
	for _, p := range v.Probes {
		if p.Name == name {
			return p
		}
	}
	return forget.Probe{}
}

// The probes must detect residue, not pass vacuously.
func TestProbesDetectResidue(t *testing.T) {
	cases := []struct {
		name   string
		plant  func(t *testing.T, ctx context.Context, exec func(string, ...any))
		failed []string
	}{
		{"fts entry left behind aborts the transaction", func(t *testing.T, ctx context.Context, exec func(string, ...any)) {
			exec(`DROP TRIGGER events_fts_ad`)
		}, nil},
		{"content copied into an unknown table is found in the file and by id", func(t *testing.T, ctx context.Context, exec func(string, ...any)) {
			exec(`CREATE TABLE scratch(note TEXT)`)
			exec(`INSERT INTO scratch VALUES (?)`, "copy of evt_07: "+contents["evt_07"])
		}, []string{forget.ProbeBytes, forget.ProbeReferences}},
		{"a vector left behind is found by vector search", func(t *testing.T, ctx context.Context, exec func(string, ...any)) {
			exec(`CREATE TRIGGER keep_vec AFTER DELETE ON embeddings WHEN OLD.record_id = 'evt_07' BEGIN
				INSERT INTO embeddings VALUES (OLD.namespace, OLD.record_type, OLD.record_id, OLD.model_id, OLD.dim, OLD.vector); END`)
		}, []string{forget.ProbeVector, forget.ProbeReferences}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := corpus(t)
			ctx := context.Background()
			c.plant(t, ctx, func(q string, args ...any) {
				if _, err := e.Store.DB().Exec(q, args...); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			})
			rep, err := forget.Forget(ctx, e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_07"}, false, "admin")
			if c.failed == nil { // caught inside the transaction: rolled back
				if core.CodeOf(err) != core.CodeIntegrity || count(t, e, `SELECT count(*) FROM events WHERE id = 'evt_07'`) != 1 {
					t.Fatalf("expected a rolled-back integrity error, got %v", err)
				}
				return
			}
			if core.CodeOf(err) != core.CodeIntegrity || rep.Verification == nil || rep.Verification.OK {
				t.Fatalf("expected an integrity failure, got err=%v verification=%+v", err, rep.Verification)
			}
			for _, name := range c.failed {
				if p := probe(rep.Verification, name); p.Pass {
					t.Errorf("probe %s passed: %+v", name, p)
				}
			}
			for _, p := range rep.Verification.Probes {
				for _, f := range p.Failures {
					if strings.Contains(f, "5511987654321") {
						t.Errorf("failure message holds content: %s", f)
					}
				}
			}
		})
	}
}

// Architect note (facts area): supersedes goes new -> old and is not a
// derivation. Forgetting the old fact's origin removes the old fact only; the
// successor survives and its superseded_by link to a removed fact is cleared.
func TestForgetOldFactOriginKeepsSuccessor(t *testing.T) {
	e, _ := corpus(t)
	ctx := context.Background()
	rep, err := forget.Forget(ctx, e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_10"}, false, "admin")
	if err != nil {
		t.Fatalf("%v %+v", err, rep.Verification)
	}
	if len(rep.Removed.Facts) != 1 || rep.Removed.Facts[0] != "fct_06" {
		t.Fatalf("removed facts = %v", rep.Removed.Facts)
	}
	if n := count(t, e, `SELECT count(*) FROM facts WHERE id = 'fct_03' AND status = 'active'`); n != 1 {
		t.Fatal("successor fact was removed")
	}
	if n := count(t, e, `SELECT count(*) FROM provenance WHERE relation = 'supersedes'`); n != 0 {
		t.Fatal("supersedes edge to the removed fact survived")
	}
	// The entity is still mentioned by evt_01 and fct_03: shared vocabulary stays.
	if len(rep.Removed.Entities) != 0 {
		t.Fatalf("live entity removed: %v", rep.Removed.Entities)
	}
}

// Forgetting a derived record forgets the events it came from (otherwise the
// next sleep would rebuild it from the raw log).
func TestForgetDerivedRecordForgetsItsOrigins(t *testing.T) {
	e, _ := corpus(t)
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: "fct_05"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Removed.Events) != 1 || rep.Removed.Events[0] != "evt_07" {
		t.Fatalf("events = %v", rep.Removed.Events)
	}
}

func TestEntityScopeByAlias(t *testing.T) {
	e, _ := corpus(t)
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "ORTENSIA, Valquiria"}, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"evt_04", "evt_08"}; strings.Join(rep.Removed.Events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", rep.Removed.Events)
	}
}

func TestScopeErrors(t *testing.T) {
	cases := []struct {
		name  string
		scope forget.Scope
		code  core.Code
	}{
		{"unknown kind", forget.Scope{Kind: "everything"}, core.CodeInvalidArgument},
		{"missing record", forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_99"}, core.CodeNotFound},
		{"not an id", forget.Scope{Kind: forget.ScopeRecord, Ref: "hello"}, core.CodeInvalidArgument},
		{"record of another namespace", forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_91"}, core.CodeNotFound},
		{"missing document", forget.Scope{Kind: forget.ScopeDocument, Ref: "file:///nope"}, core.CodeNotFound},
		{"missing source", forget.Scope{Kind: forget.ScopeSource, Ref: "nope"}, core.CodeNotFound},
		{"missing entity", forget.Scope{Kind: forget.ScopeEntity, Ref: "Nobody"}, core.CodeNotFound},
		{"open date range", forget.Scope{Kind: forget.ScopeDateRange, From: "2026-01-01"}, core.CodeInvalidArgument},
		{"reversed date range", forget.Scope{Kind: forget.ScopeDateRange, From: "2026-05-01", To: "2026-01-01"}, core.CodeInvalidArgument},
		{"empty search", forget.Scope{Kind: forget.ScopeSearch, Query: " ;; "}, core.CodeInvalidArgument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := corpus(t)
			if _, err := forget.Forget(context.Background(), e, ns, c.scope, false, "admin"); core.CodeOf(err) != c.code {
				t.Fatalf("err = %v, want %s", err, c.code)
			}
		})
	}
}

// FTS5 syntax in a search scope is quoted, never interpreted.
func TestSearchScopeIsSafe(t *testing.T) {
	e, _ := corpus(t)
	ctx := context.Background()
	for _, q := range []string{`cafeteria OR "`, `content:* NEAR(`, `cafeteria AND NOT vegetarian`} {
		if _, err := forget.Forget(ctx, e, ns, forget.Scope{Kind: forget.ScopeSearch, Query: q}, true, "admin"); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	// "AND NOT" are terms here, so nothing matches all of them.
	rep, _ := forget.Forget(ctx, e, ns, forget.Scope{Kind: forget.ScopeSearch, Query: "cafeteria AND NOT vegetarian"}, true, "admin")
	if len(rep.Removed.Events) != 0 {
		t.Fatalf("operators were interpreted: %v", rep.Removed.Events)
	}
}

func TestEmptyMatchChangesNothing(t *testing.T) {
	e, _ := corpus(t)
	before := count(t, e, `SELECT count(*) FROM audit_log`)
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeDateRange, From: "2020-01-01", To: "2020-02-01"}, false, "admin")
	if err != nil || rep.TombstoneID != "" || count(t, e, `SELECT count(*) FROM audit_log`) != before {
		t.Fatalf("empty forget mutated: %+v %v", rep, err)
	}
}

// BASE.md open question, decided in 0050: the tombstone keeps content hashes
// only when forget.tombstone_keeps_hash is set.
func TestTombstoneKeepsHashOnlyWhenConfigured(t *testing.T) {
	e, _ := corpus(t)
	e.Cfg.Forget.TombstoneKeepsHash = true
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_07"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var hashes string
	e.Store.DB().QueryRow(`SELECT content_hashes FROM tombstones WHERE id = ?`, rep.TombstoneID).Scan(&hashes)
	if !strings.Contains(hashes, `"`) || strings.Contains(hashes, "5511987654321") {
		t.Fatalf("content_hashes = %q", hashes)
	}
	if r, _ := prov.Verify(context.Background(), e); !r.OK {
		t.Fatalf("verify: %+v", r)
	}
}
