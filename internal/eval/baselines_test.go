package eval

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// BASE.md write step 6: "split by structure (headings and paragraphs, no overlap)".
func TestChunkItemSplitsByStructure(t *testing.T) {
	doc := Item{Type: ItemDocument, ID: "d", URI: "u", Version: 2, Text: "# T\n\nIntro line one\nline two.\n\n## A\n\nPara A.\n\n### A1\n\nPara A1.\n\n## B\n\nPara B.\n"}
	got := ChunkItem(doc, 2000)
	want := []Chunk{
		{ID: "d@v2#0", DocID: "d", URI: "u", Version: 2, Ordinal: 0, HeadingPath: "T", Text: "Intro line one\nline two."},
		{ID: "d@v2#1", DocID: "d", URI: "u", Version: 2, Ordinal: 1, HeadingPath: "T > A", Text: "Para A."},
		{ID: "d@v2#2", DocID: "d", URI: "u", Version: 2, Ordinal: 2, HeadingPath: "T > A > A1", Text: "Para A1."},
		{ID: "d@v2#3", DocID: "d", URI: "u", Version: 2, Ordinal: 3, HeadingPath: "T > B", Text: "Para B."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	long := Item{Type: ItemDocument, ID: "l", Text: "Primeira frase aqui. Segunda frase aqui. Terceira."}
	var texts []string
	for _, c := range ChunkItem(long, 25) {
		texts = append(texts, c.Text)
	}
	if strings.Join(texts, "|") != "Primeira frase aqui.|Segunda frase aqui.|Terceira." {
		t.Fatalf("long paragraph split: %q", texts)
	}
	for _, c := range ChunkItem(Item{Type: ItemDocument, ID: "h", Text: strings.Repeat("x", 30)}, 10) {
		if len(c.Text) != 10 {
			t.Fatalf("hard split produced %q", c.Text)
		}
	}
	sess := Item{Type: ItemSession, ID: "s", Turns: []Turn{{Role: "user", Text: "oi"}, {Role: "assistant", Text: "olá"}}}
	if c := ChunkItem(sess, 2000); len(c) != 2 || c[1].Text != "olá" || c[1].ID != "s@v1#1" {
		t.Fatalf("session chunks: %+v", c)
	}
}

// BASE.md write step 4 applied to the raw-history baselines: an unchanged
// paragraph of a new version is not indexed twice.
func TestRawLogSkipsUnchangedChunks(t *testing.T) {
	l := newRawLog(2000)
	v1 := Item{Type: ItemDocument, ID: "d", Version: 1, Text: "A.\n\nB."}
	v2 := Item{Type: ItemDocument, ID: "d", Version: 2, Text: "A.\n\nC."}
	other := Item{Type: ItemDocument, ID: "e", Version: 1, Text: "A."}
	if n := len(l.add(v1)); n != 2 {
		t.Fatalf("v1 added %d", n)
	}
	if got := l.add(v2); len(got) != 1 || got[0].Text != "C." {
		t.Fatalf("v2 added %+v", got)
	}
	if n := len(l.add(other)); n != 1 {
		t.Fatalf("same text in another item must be kept, added %d", n)
	}
}

func TestFillRespectsBudget(t *testing.T) {
	cs := []Chunk{{ID: "a", Text: strings.Repeat("x", 40)}, {ID: "b", Text: strings.Repeat("x", 80)}, {ID: "c", Text: strings.Repeat("x", 20)}}
	got := fill(cs, nil, 16) // 10 + 20 + 5 tokens
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" || got[1].Tokens != 5 {
		t.Fatalf("got %+v", got)
	}
}

func ingestAll(t *testing.T, s System, items ...Item) {
	t.Helper()
	for _, it := range items {
		if err := s.Ingest(context.Background(), it); err != nil {
			t.Fatal(err)
		}
	}
}

func ids(rs []Retrieved) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

var tinyCorpus = []Item{
	{Type: ItemDocument, ID: "precos", Version: 1, Text: "# Preços\n\nO Plano Essencial custa R$ 10 por mês.\n\nO Plano Profissional custa R$ 20 por mês."},
	{Type: ItemDocument, ID: "ferias", Version: 1, Text: "# Férias\n\nAs férias devem ser pedidas com 30 dias de antecedência."},
	{Type: ItemDocument, ID: "precos", Version: 2, Text: "# Preços\n\nO Plano Essencial custa R$ 12 por mês.\n\nO Plano Profissional custa R$ 20 por mês."},
}

func TestNoMemoryReturnsNothing(t *testing.T) {
	var s System = NoMemory{}
	ingestAll(t, s, tinyCorpus...)
	if got, _ := s.Retrieve(context.Background(), Query{Text: "plano essencial"}, 800); len(got) != 0 {
		t.Fatalf("B0 returned %v", got)
	}
}

func TestBM25RanksByKeywordAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	b, err := NewBM25(ctx, 50, 2000)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ingestAll(t, b, tinyCorpus...)
	got, err := b.Retrieve(ctx, Query{Text: "Quanto custa o plano essencial?"}, 800)
	if err != nil {
		t.Fatal(err)
	}
	// Both versions of the Essencial paragraph rank above Profissional; the
	// raw history never retires the old price (that is B1's known weakness).
	if g := ids(got); len(g) != 3 || !reflect.DeepEqual(g[2], "precos@v1#1") ||
		!strings.Contains(got[0].Text+got[1].Text, "R$ 10") || !strings.Contains(got[0].Text+got[1].Text, "R$ 12") {
		t.Fatalf("got %v", g)
	}
	// Accents and case do not matter (remove_diacritics 2).
	got, _ = b.Retrieve(ctx, Query{Text: "FERIAS antecedencia"}, 800)
	if len(got) != 1 || got[0].DocID != "ferias" {
		t.Fatalf("diacritics: %v", ids(got))
	}
	// Only stopwords, or no match: empty.
	for _, q := range []string{"o que é isso?", "estacionamento"} {
		if got, _ := b.Retrieve(ctx, Query{Text: q}, 800); len(got) != 0 {
			t.Fatalf("%q returned %v", q, ids(got))
		}
	}
	if n, err := b.SizeBytes(ctx); err != nil || n <= 0 {
		t.Fatalf("size %d %v", n, err)
	}
}
