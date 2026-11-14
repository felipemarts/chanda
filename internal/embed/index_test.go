package embed

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

const testModel = "test-model-8"

func newIndex(t *testing.T) (*env.Env, *ExactIndex) {
	t.Helper()
	e, _ := env.NewTest(t)
	return e, NewExactIndex(e)
}

func mustUpsert(t *testing.T, x *ExactIndex, ns string, rt core.RecordType, id, model string, v []float32) {
	t.Helper()
	if err := x.Upsert(context.Background(), ns, rt, id, model, v); err != nil {
		t.Fatal(err)
	}
}

func mustSearch(t *testing.T, x *ExactIndex, ns, model string, q []float32, k int, types ...core.RecordType) []core.VectorHit {
	t.Helper()
	hs, err := x.Search(context.Background(), ns, model, q, k, types)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

// core.VectorIndex contract: exact, deterministic, ties broken by id ascending.
func TestSearchOrderScoreDescThenIDAsc(t *testing.T) {
	_, x := newIndex(t)
	q := axis(8, 0)
	// Inserted out of id order; evt_b and evt_c tie, evt_a/evt_d tie lower.
	mustUpsert(t, x, "ns", core.RecEvent, "evt_d", testModel, mix(axis(8, 0), axis(8, 1), 1, 1))
	mustUpsert(t, x, "ns", core.RecEvent, "evt_c", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecEvent, "evt_a", testModel, mix(axis(8, 0), axis(8, 1), 1, 1))
	mustUpsert(t, x, "ns", core.RecEvent, "evt_b", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecEvent, "evt_e", testModel, axis(8, 2))
	hs := mustSearch(t, x, "ns", testModel, q, 10)
	if got, want := ids(hs), []string{"evt_b", "evt_c", "evt_a", "evt_d", "evt_e"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if !approx(hs[0].Score, 1) || !approx(hs[2].Score, 0.70710677) || !approx(hs[4].Score, 0) {
		t.Fatalf("scores %+v", hs)
	}
	if got := ids(mustSearch(t, x, "ns", testModel, q, 3)); !reflect.DeepEqual(got, []string{"evt_b", "evt_c", "evt_a"}) {
		t.Fatalf("top-3 %v", got)
	}
	// Unnormalized query is normalized; zero query returns nothing.
	if got := mustSearch(t, x, "ns", testModel, []float32{5, 0, 0, 0, 0, 0, 0, 0}, 1); !approx(got[0].Score, 1) {
		t.Fatalf("unnormalized query score %v", got[0].Score)
	}
	if got := mustSearch(t, x, "ns", testModel, make([]float32, 8), 5); len(got) != 0 {
		t.Fatalf("zero query returned %v", got)
	}
}

// The cache must stay coherent: after Upsert and Remove, Search sees the
// change without a reload, and the stored rows match.
func TestIndexCoherenceAfterUpsertAndRemove(t *testing.T) {
	e, x := newIndex(t)
	ctx := context.Background()
	q := axis(8, 0)
	mustUpsert(t, x, "ns", core.RecEvent, "evt_1", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecFact, "fct_1", testModel, axis(8, 1))
	if err := x.Warm(ctx, "ns", testModel); err != nil { // loads the cache
		t.Fatal(err)
	}
	key := shardKey{"ns", testModel}
	cached := x.shards[key]
	if cached == nil || len(cached.ids) != 2 {
		t.Fatal("Warm did not load the shard")
	}

	steps := []struct {
		name string
		do   func()
		want []string
	}{
		{"insert new record", func() { mustUpsert(t, x, "ns", core.RecPage, "pg_1", testModel, mix(axis(8, 0), axis(8, 1), 3, 1)) }, []string{"evt_1", "pg_1", "fct_1"}},
		{"update existing vector", func() { mustUpsert(t, x, "ns", core.RecEvent, "evt_1", testModel, axis(8, 2)) }, []string{"pg_1", "evt_1", "fct_1"}},
		{"remove middle record", func() {
			if err := x.Remove(ctx, "ns", core.RecPage, "pg_1"); err != nil {
				t.Fatal(err)
			}
		}, []string{"evt_1", "fct_1"}},
		{"remove unknown record is a no-op", func() {
			if err := x.Remove(ctx, "ns", core.RecPage, "pg_none"); err != nil {
				t.Fatal(err)
			}
		}, []string{"evt_1", "fct_1"}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			s.do()
			if x.shards[key] != cached {
				t.Fatal("cache was reloaded instead of updated in place")
			}
			got := ids(mustSearch(t, x, "ns", testModel, q, 5))
			if !reflect.DeepEqual(got, s.want) {
				t.Fatalf("got %v want %v", got, s.want)
			}
			// The cache equals a fresh load from SQLite.
			fresh := NewExactIndex(e)
			if g2 := ids(mustSearch(t, fresh, "ns", testModel, q, 5)); !reflect.DeepEqual(g2, got) {
				t.Fatalf("cache %v differs from fresh load %v", got, g2)
			}
		})
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action IN (?,?)`, ActEmbedIndex, ActEmbedRemove); n != 5 {
		t.Fatalf("audit entries = %d, want 5 (4 upserts, 1 remove; no-op not audited)", n)
	}
	verifyAudit(t, e)
}

// Forget deletes embeddings with plain SQL in its own package; the index must
// never return a vector that is no longer in the table.
func TestIndexSeesOutOfBandDeletes(t *testing.T) {
	e, x := newIndex(t)
	mustUpsert(t, x, "ns", core.RecEvent, "evt_1", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecEvent, "evt_2", testModel, axis(8, 0))
	if got := mustSearch(t, x, "ns", testModel, axis(8, 0), 5); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if _, err := e.Store.DB().Exec(`DELETE FROM embeddings WHERE record_id = 'evt_1'`); err != nil {
		t.Fatal(err)
	}
	if got := ids(mustSearch(t, x, "ns", testModel, axis(8, 0), 5)); !reflect.DeepEqual(got, []string{"evt_2"}) {
		t.Fatalf("stale cache returned %v", got)
	}
}

func TestSearchFilterByRecordType(t *testing.T) {
	_, x := newIndex(t)
	mustUpsert(t, x, "ns", core.RecEvent, "evt_1", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecFact, "fct_1", testModel, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecPage, "pg_1", testModel, axis(8, 0))
	cases := []struct {
		name  string
		types []core.RecordType
		want  []string
	}{
		{"no filter", nil, []string{"evt_1", "fct_1", "pg_1"}},
		{"facts only", []core.RecordType{core.RecFact}, []string{"fct_1"}},
		{"events and pages", []core.RecordType{core.RecPage, core.RecEvent}, []string{"evt_1", "pg_1"}},
		{"type without vectors", []core.RecordType{core.RecPageItem}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ids(mustSearch(t, x, "ns", testModel, axis(8, 0), 10, c.types...))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestIndexDimensionAndArgumentChecks(t *testing.T) {
	_, x := newIndex(t)
	ctx := context.Background()
	mustUpsert(t, x, "ns", core.RecEvent, "evt_1", testModel, axis(8, 0))
	cases := []struct {
		name string
		err  error
		code core.Code
	}{
		{"upsert with other dimension", x.Upsert(ctx, "ns", core.RecEvent, "evt_2", testModel, axis(4, 0)), core.CodeInvalidArgument},
		{"upsert empty vector", x.Upsert(ctx, "ns", core.RecEvent, "evt_2", testModel, nil), core.CodeInvalidArgument},
		{"upsert without model", x.Upsert(ctx, "ns", core.RecEvent, "evt_2", "", axis(8, 0)), core.CodeInvalidArgument},
		{"same record from another namespace", x.Upsert(ctx, "other", core.RecEvent, "evt_1", testModel, axis(8, 0)), core.CodeConflict},
		{"query with other dimension", func() error { _, err := x.Search(ctx, "ns", testModel, axis(4, 0), 5, nil); return err }(), core.CodeInvalidArgument},
		{"search without namespace", func() error { _, err := x.Search(ctx, "", testModel, axis(8, 0), 5, nil); return err }(), core.CodeInvalidArgument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if core.CodeOf(c.err) != c.code {
				t.Fatalf("err = %v, want %s", c.err, c.code)
			}
		})
	}
	// A different dimension is fine under another model id.
	mustUpsert(t, x, "ns", core.RecEvent, "evt_1", "other-model-4", axis(4, 0))
}

// BASE.md: "The namespace filter is applied in the query".
func TestSearchIsolatesNamespaces(t *testing.T) {
	_, x := newIndex(t)
	mustUpsert(t, x, "a", core.RecEvent, "evt_a", testModel, axis(8, 0))
	mustUpsert(t, x, "b", core.RecEvent, "evt_b", testModel, axis(8, 0))
	if got := ids(mustSearch(t, x, "a", testModel, axis(8, 0), 5)); !reflect.DeepEqual(got, []string{"evt_a"}) {
		t.Fatalf("namespace a got %v", got)
	}
	if got := ids(mustSearch(t, x, "c", testModel, axis(8, 0), 5)); len(got) != 0 {
		t.Fatalf("empty namespace got %v", got)
	}
}

// BASE.md integrity rule: "embeddings is keyed by model_id. Changing the
// embedding model creates new vectors without deleting the old ones until
// the migration finishes."
func TestModelMigrationKeepsOldVectorsUntilDropped(t *testing.T) {
	e, x := newIndex(t)
	ctx := context.Background()
	src := seedSource(t, e, "ns")
	e1 := seedEvent(t, e, "ns", src, "um")
	e2 := seedEvent(t, e, "ns", src, "dois")
	f1 := seedFact(t, e, "ns", "fato")
	const oldM, newM = "old-8", "new-4"
	for _, id := range []string{e1, e2} {
		mustUpsert(t, x, "ns", core.RecEvent, id, oldM, axis(8, 0))
	}
	mustUpsert(t, x, "ns", core.RecFact, f1, oldM, axis(8, 0))
	mustUpsert(t, x, "ns", core.RecEvent, e1, newM, axis(4, 1))

	if got := ids(mustSearch(t, x, "ns", newM, axis(4, 1), 10)); !reflect.DeepEqual(got, []string{e1}) {
		t.Fatalf("search must use only the requested model: %v", got)
	}
	if got := mustSearch(t, x, "ns", oldM, axis(8, 0), 10); len(got) != 3 {
		t.Fatalf("old vectors must remain during migration: %v", got)
	}
	missing, err := x.MissingFor(ctx, "ns", newM)
	if err != nil {
		t.Fatal(err)
	}
	if want := []RecordRef{{core.RecEvent, e2}, {core.RecFact, f1}}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing %v want %v", missing, want)
	}
	mustUpsert(t, x, "ns", core.RecEvent, e2, newM, axis(4, 1))
	mustUpsert(t, x, "ns", core.RecFact, f1, newM, axis(4, 1))
	if missing, _ = x.MissingFor(ctx, "ns", newM); len(missing) != 0 {
		t.Fatalf("still missing %v", missing)
	}
	n, err := x.DropModel(ctx, "ns", oldM)
	if err != nil || n != 3 {
		t.Fatalf("drop: %d %v", n, err)
	}
	if got := mustSearch(t, x, "ns", oldM, axis(8, 0), 10); len(got) != 0 {
		t.Fatalf("dropped model still searchable: %v", got)
	}
	ms, _ := x.Models(ctx, "ns")
	if !reflect.DeepEqual(ms, []ModelCount{{newM, 3}}) {
		t.Fatalf("models %v", ms)
	}
	if countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ? AND target = ?`, ActEmbedDrop, oldM) != 1 {
		t.Fatal("drop not audited")
	}
	verifyAudit(t, e)
}

func TestMissingForSkipsRetiredRecords(t *testing.T) {
	e, x := newIndex(t)
	src := seedSource(t, e, "ns")
	live := seedEvent(t, e, "ns", src, "vivo")
	retired := seedEvent(t, e, "ns", src, "aposentado")
	if _, err := e.Store.DB().Exec(`UPDATE events SET retired_at = ? WHERE id = ?`, core.FormatTime(e.Clock.Now()), retired); err != nil {
		t.Fatal(err)
	}
	seedPage(t, e, "ns", "t", "b", "retired")
	pg := seedPage(t, e, "ns", "t", "b", "active")
	seedEvent(t, e, "other", seedSource(t, e, "other"), "outro namespace")
	missing, err := x.MissingFor(context.Background(), "ns", testModel)
	if err != nil {
		t.Fatal(err)
	}
	if want := []RecordRef{{core.RecEvent, live}, {core.RecPage, pg}}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing %v want %v", missing, want)
	}
}

// Principle 11 for the parallel scan: the result does not depend on the
// number of workers, even with many exact ties.
func TestParallelScanIsDeterministic(t *testing.T) {
	e, _ := env.NewTest(t)
	const n, dim = 20000, 16
	sh := newShard(1, dim)
	r := splitmix(7)
	for i := 0; i < n; i++ {
		v := r.unit(dim)
		if i%3 == 0 { // force ties: many records share a few vectors
			v = axis(dim, i%dim)
		}
		sh.set(core.RecEvent, fmt.Sprintf("evt_%06d", (i*7919)%n), v)
	}
	x := NewExactIndex(e)
	x.shards[shardKey{"ns", testModel}] = sh
	q := mix(axis(dim, 3), r.unit(dim), 1, 0.01)
	var ref []hit
	for _, w := range []int{1, 2, 3, 8, 16} {
		got := sh.scan(context.Background(), q, 50, nil, w)
		if ref == nil {
			ref = got
			continue
		}
		if !reflect.DeepEqual(got, ref) {
			t.Fatalf("workers=%d differs from workers=1", w)
		}
	}
	for i := 1; i < len(ref); i++ {
		if sh.better(ref[i], ref[i-1]) {
			t.Fatalf("not totally ordered at %d", i)
		}
	}
}
