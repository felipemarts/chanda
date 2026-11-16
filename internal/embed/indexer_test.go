package embed

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

func newIndexer(t *testing.T, e *env.Env, dim int) (*Indexer, *ExactIndex) {
	t.Helper()
	h, err := NewHash(dim)
	if err != nil {
		t.Fatal(err)
	}
	x := NewExactIndex(e)
	return NewIndexer(e, h, x), x
}

func queryHash(t *testing.T, ix *Indexer, text string) []float32 {
	t.Helper()
	v, err := ix.emb.Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatal(err)
	}
	return v[0]
}

func seedPageItem(t testing.TB, e *env.Env, ns, page, text string) string {
	t.Helper()
	id := e.IDs.For(core.RecPageItem)
	_, err := e.Store.DB().Exec(`INSERT INTO page_items(id, namespace, page_id, ordinal, text, status, trust_zone, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, ns, page, 1, text, "active", "user", core.FormatTime(e.Clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// BASE.md write step 7 and the flush tool: queued text becomes searchable
// once flush returns, for every embeddable record type.
func TestFlushDrainsQueueAndIndexesEveryRecordType(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	ix, x := newIndexer(t, e, 256)
	src := seedSource(t, e, "ns")
	ev := seedEvent(t, e, "ns", src, "O preço do plano empresarial subiu para 120 reais")
	ft := seedFact(t, e, "ns", "Maria é gerente de vendas")
	pg := seedPage(t, e, "ns", "Deploy", "Como fazer deploy do cluster kubernetes", "active")
	pi := seedPageItem(t, e, "ns", pg, "Receita de bolo de cenoura")
	for _, r := range []RecordRef{{core.RecEvent, ev}, {core.RecFact, ft}, {core.RecPage, pg}, {core.RecPageItem, pi}} {
		enqueue(t, e, "ns", r.Type, r.ID)
	}
	if n, _ := ix.Pending(ctx, "ns"); n != 4 {
		t.Fatalf("pending %d", n)
	}
	if err := ix.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Pending(ctx, ""); n != 0 {
		t.Fatalf("queue not empty after flush: %d", n)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM embeddings WHERE model_id = ? AND dim = 256`, ix.ModelID()); n != 4 {
		t.Fatalf("embeddings %d", n)
	}
	cases := []struct {
		query string
		top   string
	}{
		{"qual o preço do plano?", ev},
		{"quem é a gerente de vendas", ft},
		{"deploy kubernetes", pg},
		{"bolo de cenoura", pi},
	}
	for _, c := range cases {
		hs := mustSearch(t, x, "ns", ix.ModelID(), queryHash(t, ix, c.query), 4)
		if len(hs) == 0 || hs[0].RecordID != c.top {
			t.Fatalf("query %q: top %v, want %s", c.query, hs, c.top)
		}
	}
	// Embeddings get no provenance edges (keyed by record).
	if n := countRows(t, e, `SELECT COUNT(*) FROM provenance`); n != 0 {
		t.Fatalf("provenance edges added: %d", n)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, ActEmbedIndex); n != 1 {
		t.Fatalf("expected one audit entry for the batch, got %d", n)
	}
	verifyAudit(t, e)
}

// The queue is drained in (enqueued_at, record_type, record_id) order, one
// transaction per batch of cfg.Embed.BatchSize.
func TestQueueDrainOrderAndBatchSize(t *testing.T) {
	e, _ := env.NewTest(t)
	e.Cfg.Embed.BatchSize = 2
	ix, _ := newIndexer(t, e, 16)
	src := seedSource(t, e, "ns")
	a := seedEvent(t, e, "ns", src, "a")
	b := seedEvent(t, e, "ns", src, "b")
	f := seedFact(t, e, "ns", "f")
	ins := func(rt core.RecordType, id, at string) {
		if _, err := e.Store.DB().Exec(`INSERT INTO embed_queue VALUES (?,?,?,?)`, "ns", string(rt), id, at); err != nil {
			t.Fatal(err)
		}
	}
	ins(core.RecEvent, b, "2026-01-01T00:00:00.000000000Z")
	ins(core.RecFact, f, "2026-01-01T00:00:00.000000000Z")
	ins(core.RecEvent, a, "2026-01-02T00:00:00.000000000Z")
	picked, done, err := ix.batch(context.Background())
	if err != nil || picked != 2 || done != 2 {
		t.Fatalf("batch: %d %d %v", picked, done, err)
	}
	var left []string
	rows, _ := e.Store.DB().Query(`SELECT record_id FROM embed_queue ORDER BY record_id`)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		left = append(left, s)
	}
	rows.Close()
	if !reflect.DeepEqual(left, []string{a}) {
		t.Fatalf("left in queue %v, want [%s] (event b and fact f were first)", left, a)
	}
	if n, err := ix.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("RunOnce %d %v", n, err)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, ActEmbedIndex); n != 2 {
		t.Fatalf("one audit entry per batch, got %d", n)
	}
}

// Records that no longer exist or are retired are dropped from the queue
// without a vector; vectors a retired record already has are kept (only
// forget deletes).
func TestIndexerSkipsMissingAndRetiredRecords(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	ix, x := newIndexer(t, e, 16)
	src := seedSource(t, e, "ns")
	kept := seedEvent(t, e, "ns", src, "indexado antes de aposentar")
	enqueue(t, e, "ns", core.RecEvent, kept)
	if err := ix.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	retire := func(id string) {
		if _, err := e.Store.DB().Exec(`UPDATE events SET retired_at = ? WHERE id = ?`, core.FormatTime(e.Clock.Now()), id); err != nil {
			t.Fatal(err)
		}
	}
	retire(kept)
	retired := seedEvent(t, e, "ns", src, "aposentado antes de indexar")
	retire(retired)
	enqueue(t, e, "ns", core.RecEvent, retired)
	enqueue(t, e, "ns", core.RecEvent, "evt_forgotten")
	enqueue(t, e, "ns", core.RecPage, seedPage(t, e, "ns", "t", "b", "retired"))
	enqueue(t, e, "ns", core.RecordType("unknown"), "x_1")
	other := seedEvent(t, e, "other", seedSource(t, e, "other"), "outro namespace")
	enqueue(t, e, "ns", core.RecEvent, other) // queue row in the wrong namespace
	n, err := ix.RunOnce(ctx)
	if err != nil || n != 5 {
		t.Fatalf("RunOnce %d %v", n, err)
	}
	if n, _ := ix.Pending(ctx, ""); n != 0 {
		t.Fatalf("pending %d", n)
	}
	if got := countRows(t, e, `SELECT COUNT(*) FROM embeddings`); got != 1 {
		t.Fatalf("embeddings %d, want only the vector indexed before retirement", got)
	}
	if hs := mustSearch(t, x, "ns", ix.ModelID(), queryHash(t, ix, "indexado"), 5); len(hs) != 1 || hs[0].RecordID != kept {
		t.Fatalf("retired record's vector must be kept: %v", hs)
	}
	var details string
	e.Store.DB().QueryRow(`SELECT details FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, ActEmbedIndex).Scan(&details)
	if details != `{"indexed":0,"model_id":"hash-v1-16","skipped":5}` {
		t.Fatalf("audit details %s", details)
	}
}

// editingEmbedder edits a page while the batch is outside its transaction,
// to prove the batch does not store a vector of stale text.
type editingEmbedder struct {
	*HashEmbedder
	e     *env.Env
	page  string
	calls int
	fail  bool
}

func (m *editingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.fail {
		return nil, errors.New("model crashed")
	}
	if m.calls == 1 {
		if _, err := m.e.Store.DB().Exec(`UPDATE pages SET body = 'corpo novo' WHERE id = ?`, m.page); err != nil {
			return nil, err
		}
	}
	return m.HashEmbedder.Embed(ctx, texts)
}

func TestIndexerDoesNotStoreVectorOfStaleText(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	h, _ := NewHash(16)
	pg := seedPage(t, e, "ns", "titulo", "corpo velho", "active")
	m := &editingEmbedder{HashEmbedder: h, e: e, page: pg}
	x := NewExactIndex(e)
	ix := NewIndexer(e, m, x)
	enqueue(t, e, "ns", core.RecPage, pg)
	if n, err := ix.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("first run must leave the edited page queued: %d %v", n, err)
	}
	if err := ix.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var blob []byte
	if err := e.Store.DB().QueryRow(`SELECT vector FROM embeddings WHERE record_id = ?`, pg).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	want, _ := h.Embed(ctx, []string{"titulo\n\ncorpo novo"})
	if !bytes.Equal(blob, Encode(want[0])) {
		t.Fatal("stored vector is not the embedding of the current text")
	}
}

func TestIndexerEmbedErrorKeepsQueue(t *testing.T) {
	e, _ := env.NewTest(t)
	h, _ := NewHash(16)
	src := seedSource(t, e, "ns")
	ev := seedEvent(t, e, "ns", src, "texto")
	enqueue(t, e, "ns", core.RecEvent, ev)
	ix := NewIndexer(e, &editingEmbedder{HashEmbedder: h, e: e, fail: true}, NewExactIndex(e))
	if err := ix.Flush(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if n, _ := ix.Pending(context.Background(), "ns"); n != 1 {
		t.Fatalf("queue row lost on failure: pending %d", n)
	}
}

func TestBackgroundLoopDrainsOnNotify(t *testing.T) {
	e, _ := env.NewTest(t)
	e.Cfg.Embed.PollIntervalMS = 60_000 // only Notify can wake it in time
	ix, _ := newIndexer(t, e, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := ix.Start(ctx)
	defer func() { cancel(); <-done }()
	src := seedSource(t, e, "ns")
	enqueue(t, e, "ns", core.RecEvent, seedEvent(t, e, "ns", src, "acordar"))
	ix.Notify()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n, err := ix.Pending(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background loop did not drain the queue")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ix.LastError() != nil {
		t.Fatal(ix.LastError())
	}
}

// Model migration end to end: a new model backfills next to the old one,
// and the old one is dropped only once the new one is complete.
func TestModelMigrationBackfillAndComplete(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	oldIx, x := newIndexer(t, e, 16)
	src := seedSource(t, e, "ns")
	for _, s := range []string{"um", "dois", "três"} {
		enqueue(t, e, "ns", core.RecEvent, seedEvent(t, e, "ns", src, s))
	}
	if err := oldIx.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	h32, _ := NewHash(32)
	newIx := NewIndexer(e, h32, x)
	if _, err := newIx.CompleteMigration(ctx, "ns", oldIx.ModelID()); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("drop before backfill must be refused: %v", err)
	}
	if _, err := newIx.CompleteMigration(ctx, "ns", newIx.ModelID()); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("dropping the current model must be refused: %v", err)
	}
	n, err := newIx.Backfill(ctx, "ns")
	if err != nil || n != 3 {
		t.Fatalf("backfill %d %v", n, err)
	}
	if n, _ := newIx.Backfill(ctx, "ns"); n != 0 {
		t.Fatalf("second backfill enqueued %d", n)
	}
	if err := newIx.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	ms, _ := x.Models(ctx, "ns")
	if !reflect.DeepEqual(ms, []ModelCount{{"hash-v1-16", 3}, {"hash-v1-32", 3}}) {
		t.Fatalf("both models must coexist: %v", ms)
	}
	dropped, err := newIx.CompleteMigration(ctx, "ns", oldIx.ModelID())
	if err != nil || dropped != 3 {
		t.Fatalf("complete: %d %v", dropped, err)
	}
	ms, _ = x.Models(ctx, "ns")
	if !reflect.DeepEqual(ms, []ModelCount{{"hash-v1-32", 3}}) {
		t.Fatalf("models after drop %v", ms)
	}
	verifyAudit(t, e)
}

// Principle 11: the same queue drained twice from the same state gives
// byte-identical databases.
func TestIndexerDeterministicDatabase(t *testing.T) {
	run := func(dir string) []byte {
		path := filepath.Join(dir, "chanda.db")
		e, _ := env.NewTestAt(t, path, config.Default())
		e.Cfg.Embed.BatchSize = 3
		ix, x := newIndexer(t, e, 64)
		src := seedSource(t, e, "ns")
		for _, s := range []string{"alfa", "beta", "gama", "delta", "épsilon", "zeta", "eta"} {
			enqueue(t, e, "ns", core.RecEvent, seedEvent(t, e, "ns", src, s))
		}
		enqueue(t, e, "ns", core.RecFact, seedFact(t, e, "ns", "fato teta"))
		if err := ix.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		hs := mustSearch(t, x, "ns", ix.ModelID(), queryHash(t, ix, "gama"), 3)
		if err := x.Remove(context.Background(), "ns", hs[0].RecordType, hs[0].RecordID); err != nil {
			t.Fatal(err)
		}
		if err := e.Store.Checkpoint(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.Store.Close()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(run(t.TempDir()), run(t.TempDir())) {
		t.Fatal("databases differ")
	}
}
