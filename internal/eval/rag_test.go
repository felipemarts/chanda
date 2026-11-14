package eval

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestLocalEmbedderIsDeterministicAndNormalized(t *testing.T) {
	e := NewLocalEmbedder(64)
	ctx := context.Background()
	a, _ := e.Embed(ctx, []string{"Plano Essencial custa", "", "plano essencial custa"})
	b, _ := e.Embed(ctx, []string{"Plano Essencial custa"})
	if !reflect.DeepEqual(a[0], b[0]) {
		t.Fatal("same text, different vectors")
	}
	if d := dot(a[0], a[0]); d < 0.9999 || d > 1.0001 {
		t.Fatalf("norm^2 = %v", d)
	}
	if dot(a[1], a[1]) != 0 {
		t.Fatal("empty text must embed to the zero vector")
	}
	if d := dot(a[0], a[2]); d < 0.9999 {
		t.Fatalf("case/accents must not matter, cos = %v", d)
	}
	if e.ModelID() != "eval-localhash-v1-64" || e.Dim() != 64 {
		t.Fatal(e.ModelID())
	}
}

func TestPlainRAGRanksBySimilarity(t *testing.T) {
	ctx := context.Background()
	p := NewPlainRAG(NewLocalEmbedder(256), 50, 2000)
	ingestAll(t, p, tinyCorpus...)
	got, err := p.Retrieve(ctx, Query{Text: "antecedência das férias"}, 800)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].DocID != "ferias" || got[0].Score <= got[1].Score {
		t.Fatalf("got %v", ids(got))
	}
	if got, _ := p.Retrieve(ctx, Query{Text: "férias"}, 5); len(got) != 0 {
		t.Fatalf("budget 5 tokens should fit nothing, got %v", ids(got))
	}
	if p.Info().EmbeddingModel != "eval-localhash-v1-256" {
		t.Fatal(p.Info())
	}
}

func TestFullContextTruncatesToMostRecent(t *testing.T) {
	ctx := context.Background()
	f := NewFullContext(2000)
	ingestAll(t, f, tinyCorpus...)
	all, _ := f.Retrieve(ctx, Query{}, 800)
	if g := ids(all); !reflect.DeepEqual(g, []string{"precos@v1#0", "precos@v1#1", "ferias@v1#0", "precos@v2#0"}) {
		t.Fatalf("full: %v", g)
	}
	last := CountTokens(all[3].Text)
	cut, _ := f.Retrieve(ctx, Query{}, last)
	if len(cut) != 1 || cut[0].ID != "precos@v2#0" {
		t.Fatalf("truncated: %v", ids(cut))
	}
	if !strings.Contains(f.Info().Description, "truncated in 1 of 2 queries") {
		t.Fatal(f.Info().Description)
	}
}
