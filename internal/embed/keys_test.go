package embed

import (
	"context"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// roleSpy is a RoleEmbedder whose query role gives a different vector, so
// a test can tell which role embedded a text.
type roleSpy struct {
	*HashEmbedder
	passages, queries []string
}

func (r *roleSpy) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	r.passages = append(r.passages, texts...)
	return r.HashEmbedder.Embed(ctx, texts)
}

func (r *roleSpy) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	r.queries = append(r.queries, texts...)
	pre := make([]string, len(texts))
	for i, t := range texts {
		pre[i] = "query " + t
	}
	return r.HashEmbedder.Embed(ctx, pre)
}

func seedKey(t testing.TB, e *env.Env, ns, recordID, text string) string {
	t.Helper()
	db := e.Store.DB()
	ret := e.IDs.New(core.PrefixRetrieval)
	now := core.FormatTime(e.Clock.Now())
	if _, err := db.Exec(`INSERT INTO retrievals(id, namespace, mode, query_text, params, candidates, delivered, ts)
		VALUES (?,?,?,?,?,?,?,?)`, ret, ns, "context", text, "{}", "[]", "[]", now); err != nil {
		t.Fatal(err)
	}
	id := e.IDs.New(core.PrefixKey)
	if _, err := db.Exec(`INSERT INTO learned_keys(id, namespace, record_type, record_id, retrieval_id, text, created_at)
		VALUES (?,?,?,?,?,?,?)`, id, ns, "event", recordID, ret, text, now); err != nil {
		t.Fatal(err)
	}
	return id
}

// Decision 0145: a learned key is embedded with the query role, every
// other record with the passage role, and core.RecordTypes leaves keys out.
func TestKeysAreEmbeddedWithTheQueryRole(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	h, _ := NewHash(64)
	spy := &roleSpy{HashEmbedder: h}
	x := NewExactIndex(e)
	ix := NewIndexer(e, spy, x)
	src := seedSource(t, e, "ns")
	ev := seedEvent(t, e, "ns", src, "O preço do plano empresarial subiu")
	key := seedKey(t, e, "ns", ev, "quanto custa o plano")
	enqueue(t, e, "ns", core.RecEvent, ev)
	enqueue(t, e, "ns", core.RecKey, key)
	if err := ix.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(spy.passages) != 1 || spy.passages[0] != "O preço do plano empresarial subiu" {
		t.Fatalf("passages %q", spy.passages)
	}
	if len(spy.queries) != 1 || spy.queries[0] != "quanto custa o plano" {
		t.Fatalf("queries %q", spy.queries)
	}
	want, _ := spy.EmbedQuery(ctx, []string{"quanto custa o plano"})
	hs := mustSearch(t, x, "ns", spy.ModelID(), want[0], 1)
	if len(hs) != 1 || hs[0].RecordID != key || hs[0].RecordType != core.RecKey || !approx(hs[0].Score, 1) {
		t.Fatalf("key vector is not the query-role vector: %+v", hs)
	}
	for _, rt := range core.RecordTypes() {
		if rt == core.RecKey {
			t.Fatal("keys are not memory records")
		}
	}
	hs, err := x.Search(ctx, "ns", spy.ModelID(), want[0], 5, core.RecordTypes())
	if err != nil || len(hs) != 1 || hs[0].RecordID != ev {
		t.Fatalf("passage search must skip keys: %+v %v", hs, err)
	}
	if refs, err := x.MissingFor(ctx, "ns", "other-model"); err != nil || len(refs) != 2 {
		t.Fatalf("keys count for a model migration: %+v %v", refs, err)
	}
}
