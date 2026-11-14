package hugot

import (
	"context"
	"fmt"
	"testing"
)

// withRecycleEvery sets recycleEvery for one test.
func withRecycleEvery(t *testing.T, n int) {
	t.Helper()
	old := recycleEvery
	recycleEvery = n
	t.Cleanup(func() { recycleEvery = old })
}

// A session that is reloaded every few calls (decision 0136) gives the same
// bits as one that never is.
func TestEmbedderRecycleIsBitIdentical(t *testing.T) {
	dir := modelDir(t, "intfloat_multilingual-e5-small")
	ctx := context.Background()
	texts := make([]string, 9)
	for i := range texts {
		texts[i] = fmt.Sprintf("Lembrete %d: a reunião com a Ana mudou para quinta às %dh.", i, 9+i)
	}
	ref, err := sharedEmbedder(t, "intfloat_multilingual-e5-small").Embed(ctx, texts)
	if err != nil {
		t.Fatal(err)
	}
	withRecycleEvery(t, 3)
	e, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	first := e.session
	got, err := e.Embed(ctx, texts)
	if err != nil {
		t.Fatal(err)
	}
	if e.session == first {
		t.Fatal("the session was not reloaded")
	}
	q, err := e.EmbedQuery(ctx, texts[:4])
	if err != nil {
		t.Fatal(err)
	}
	refQ, err := sharedEmbedder(t, "intfloat_multilingual-e5-small").EmbedQuery(ctx, texts[:4])
	if err != nil {
		t.Fatal(err)
	}
	for i := range texts {
		if !bitsEqual(got[i], ref[i]) {
			t.Errorf("passage %d differs after reloads", i)
		}
	}
	for i := range q {
		if !bitsEqual(q[i], refQ[i]) {
			t.Errorf("query %d differs after reloads", i)
		}
	}
}

func TestRerankerRecycleIsBitIdentical(t *testing.T) {
	dir := modelDir(t, "cross-encoder_mmarco-mMiniLMv2-L12-H384-v1")
	ctx := context.Background()
	docs := []string{"o voo sai às 10h", "a consulta é na terça", "comprar pão", "o voo foi remarcado", "reunião às 9h"}
	plain, err := OpenReranker(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := plain.Score(ctx, "que horas sai o voo?", docs)
	plain.Close()
	if err != nil {
		t.Fatal(err)
	}
	withRecycleEvery(t, 2)
	r, err := OpenReranker(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	first := r.session
	got, err := r.Score(ctx, "que horas sai o voo?", docs)
	if err != nil {
		t.Fatal(err)
	}
	if r.session == first {
		t.Fatal("the session was not reloaded")
	}
	for i := range docs {
		if got[i] != ref[i] {
			t.Errorf("doc %d: %v after reloads, %v without", i, got[i], ref[i])
		}
	}
}

// The reload is triggered by a caller's context, which ends with the call;
// the new session must keep working afterwards (regression: the session
// inherited the caller's context and every later call failed with
// "context canceled").
func TestRecycleSurvivesTheCallersContext(t *testing.T) {
	dir := modelDir(t, "intfloat_multilingual-e5-small")
	// 3: after a reload (warm-up + the triggering call) the next call runs on
	// the session the reload created, before any further reload.
	withRecycleEvery(t, 3)
	e, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := e.Embed(ctx, []string{fmt.Sprintf("texto %d", i)}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		cancel()
	}
	rdir := modelDir(t, "cross-encoder_mmarco-mMiniLMv2-L12-H384-v1")
	r, err := OpenReranker(context.Background(), rdir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := r.Score(ctx, "voo", []string{fmt.Sprintf("o voo %d", i)}); err != nil {
			t.Fatalf("rerank call %d: %v", i, err)
		}
		cancel()
	}
}
