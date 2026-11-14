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
