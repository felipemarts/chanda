package forget_test

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/forget"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/prov/provtest"
)

// keysCorpus adds learned keys (decision 0145) to the forget corpus, each
// with its edges, its vector and the FTS row its trigger writes:
//
//	key_rec  -> evt_06, from ret_90 (a retrieval citing nothing)
//	key_ret  -> evt_02, from ret_01 (cites evt_03, of the merger plan)
//	key_ent  -> evt_02, from ret_90, text naming Zyntharix Holdings
//	key_srch -> evt_02, from ret_90, text with the search scope's terms
//	key_keep -> evt_02, from ret_90, unrelated text
func keysCorpus(t *testing.T) *env.Env {
	t.Helper()
	e, _ := corpus(t)
	s := provtest.New(t, e, ns)
	s.Exec(`INSERT INTO retrievals(id,namespace,mode,query_text,params,candidates,delivered,ts) VALUES('ret_90',?,'context','lunch plans','{}','[]','[]',?)`, ns, provtest.T)
	for _, k := range []struct{ id, rec, ret, text string }{
		{"key_rec", "evt_06", "ret_90", "where can we eat on friday"},
		{"key_ret", "evt_02", "ret_01", "how do I enroll in payroll"},
		{"key_ent", "evt_02", "ret_90", "did Zyntharix Holdings sign the badge policy"},
		{"key_srch", "evt_02", "ret_90", "vegetarian cafeteria badges"},
		{"key_keep", "evt_02", "ret_90", "which laptop do new hires get"},
	} {
		s.Exec(`INSERT INTO learned_keys(id,namespace,record_type,record_id,retrieval_id,text,created_at) VALUES(?,?,'event',?,?,?,?)`,
			k.id, ns, k.rec, k.ret, k.text, provtest.T)
		s.Edge("key", k.id, "retrieval", k.ret, core.RelDerivedFrom)
		s.Edge("key", k.id, "event", k.rec, core.RelPointsTo)
		s.Embedding("key", k.id, k.text)
	}
	return e
}

// Keys are removed with their record, with their retrieval, and when an
// entity or search scope matches their text; they are counted in the
// report and the tombstone, and nothing of them is left (rows, FTS rows,
// vectors, edges). The other keys stay.
func TestForgetRemovesLearnedKeys(t *testing.T) {
	cases := []struct {
		name  string
		scope forget.Scope
		keys  []string
	}{
		{"record", forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_06"}, []string{"key_rec", "key_srch"}},
		{"retrieval", forget.Scope{Kind: forget.ScopeDocument, Ref: "doc_02"}, []string{"key_ret"}},
		{"entity", forget.Scope{Kind: forget.ScopeEntity, Ref: "Zyntharix Holdings"}, []string{"key_ent", "key_ret"}},
		{"search", forget.Scope{Kind: forget.ScopeSearch, Query: "cafeteria vegetarian"}, []string{"key_rec", "key_srch"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := keysCorpus(t)
			ctx := context.Background()
			dry, err := forget.Forget(ctx, e, ns, c.scope, true, "admin")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dry.Removed.Keys, c.keys) {
				t.Fatalf("dry run keys %v, want %v", dry.Removed.Keys, c.keys)
			}
			rep, err := forget.Forget(ctx, e, ns, c.scope, false, "admin")
			if err != nil {
				t.Fatalf("forget: %v (%+v)", err, rep.Verification)
			}
			if !reflect.DeepEqual(rep.Removed.Keys, c.keys) || rep.Counts["keys"] != len(c.keys) {
				t.Fatalf("removed keys %v counts %v", rep.Removed.Keys, rep.Counts)
			}
			var counts string
			if err := e.Store.DB().QueryRow(`SELECT removed_counts FROM tombstones WHERE id = ?`, rep.TombstoneID).Scan(&counts); err != nil {
				t.Fatal(err)
			}
			if want := `"keys":` + strconv.Itoa(len(c.keys)); !strings.Contains(counts, want) {
				t.Fatalf("tombstone counts %s lack %s", counts, want)
			}
			for _, k := range c.keys {
				for _, q := range []string{
					`SELECT count(*) FROM learned_keys WHERE id = ?`,
					`SELECT count(*) FROM learned_keys_fts WHERE record_id = ?`,
					`SELECT count(*) FROM embeddings WHERE record_id = ?`,
					`SELECT count(*) FROM provenance WHERE derived_id = ?`,
				} {
					if n := count(t, e, q, k); n != 0 {
						t.Errorf("%s: %s left %d rows", k, q, n)
					}
				}
			}
			if n := count(t, e, `SELECT count(*) FROM learned_keys WHERE id = 'key_keep'`); n != 1 {
				t.Fatal("an unrelated key was removed")
			}
			if !rep.Verification.OK {
				t.Fatalf("verification %+v", rep.Verification)
			}
			if g, err := prov.Check(ctx, e.Store.DB(), ns); err != nil || !g.OK {
				t.Fatalf("provenance after forget: %+v %v", g.Violations, err)
			}
		})
	}
}
