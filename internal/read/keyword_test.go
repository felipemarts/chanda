package read

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

func TestQueryTermsStripStopwordsAndFold(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Qual é o preço do plano Ouro?", []string{"preco", "plano", "ouro"}},
		{"What is the price of the gold plan", []string{"price", "gold", "plan"}},
		{"ação AÇÃO acao", []string{"acao"}},
		{"o que é isso", nil},
		{"version 2 of x", []string{"version", "2"}},
	}
	for _, c := range cases {
		if got := queryTerms(c.in, 32); !reflect.DeepEqual(got, c.want) {
			t.Errorf("queryTerms(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := queryTerms("alpha beta gamma delta", 2); len(got) != 2 {
		t.Errorf("max terms not applied: %v", got)
	}
}

func TestMatchExprQuotesEveryToken(t *testing.T) {
	got := matchExpr(queryTerms(`foo* OR "bar" NEAR(baz) col:qux`, 32))
	want := `"foo" OR "bar" OR "near" OR "baz" OR "col" OR "qux"`
	if got != want {
		t.Fatalf("matchExpr = %s, want %s", got, want)
	}
}

// BASE.md read step 2: user text must never cause an FTS syntax error.
func TestKeywordSearchNeverFailsOnFTSSyntax(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_1", NS: "ns", Source: "src_doc_ns", Content: "near the column and the star"})
	r := New(f.e, Deps{})
	nasty := []string{
		`"`, `""`, `"unterminated`, `NEAR(`, `NEAR(a b, 3)`, `a* OR`, `*`, `col:x`, `content:star`,
		`^foo`, `(((`, `)`, `-x`, `AND`, `OR OR`, `NOT`, `{a b}:c`, `a + b`, `'; DROP TABLE events; --`,
		"\x00\x01", "🚀🔥", `\"`, `a:b:c*^"`, "", "   ",
	}
	for _, q := range nasty {
		if _, err := r.keywordSearch(f.ctx, "ns", queryTerms(q, 32), keywordFilter{}); err != nil {
			t.Errorf("query %q: %v", q, err)
		}
	}
	got, err := r.keywordSearch(f.ctx, "ns", queryTerms(`content:star*`, 32), keywordFilter{})
	if err != nil || len(got[core.RecEvent]) != 1 {
		t.Fatalf("star query: %v %v", got, err)
	}
}

// BASE.md read step 2: FTS5 BM25, top K per record type, filtered by namespace in the query.
func TestKeywordSearchBM25TopKPerTypeAndNamespace(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.basic("other")
	f.event(ev{ID: "evt_a", NS: "ns", Source: "src_doc_ns", Content: "kiwi kiwi kiwi orchard"})
	f.event(ev{ID: "evt_b", NS: "ns", Source: "src_doc_ns", Content: "kiwi and many other words about the orchard and weather"})
	f.event(ev{ID: "evt_c", NS: "ns", Source: "src_doc_ns", Content: "kiwi once"})
	f.event(ev{ID: "evt_o", NS: "other", Source: "src_doc_other", Content: "kiwi kiwi kiwi kiwi"})
	f.fact(fc{ID: "fct_a", NS: "ns", Text: "kiwi season starts in may"})
	f.page(pg{ID: "pg_a", NS: "ns", Title: "Kiwi", Body: "about fruit"})
	f.e.Cfg.Read.KeywordTopK = 2
	r := New(f.e, Deps{})
	got, err := r.keywordSearch(f.ctx, "ns", []string{"kiwi"}, keywordFilter{})
	if err != nil {
		t.Fatal(err)
	}
	evs := got[core.RecEvent]
	if len(evs) != 2 || evs[0].id != "evt_a" || evs[0].rank != 1 || evs[1].rank != 2 {
		t.Fatalf("events: %+v", evs)
	}
	if evs[0].score <= evs[1].score {
		t.Fatalf("scores must be -bm25, descending: %+v", evs)
	}
	for _, h := range evs {
		if h.id == "evt_o" {
			t.Fatal("other namespace leaked")
		}
	}
	if len(got[core.RecFact]) != 1 || len(got[core.RecPage]) != 1 {
		t.Fatalf("per-type lists: %+v", got)
	}
	only, _ := r.keywordSearch(f.ctx, "ns", []string{"kiwi"}, keywordFilter{types: map[core.RecordType]bool{core.RecFact: true}})
	if len(only) != 1 || len(only[core.RecFact]) != 1 {
		t.Fatalf("type filter: %+v", only)
	}
}

func TestCoverageAndTokenEstimate(t *testing.T) {
	terms := []string{"preco", "plano", "ouro"}
	if c := coverage(terms, "O preço do plano Prata", 4); c < 0.66 || c > 0.67 {
		t.Errorf("coverage = %v", c)
	}
	long := queryTerms("deploy the payments service to staging after the database migration finishes tonight", 32)
	if c := coverage(long, "database migration notes", 4); c != 0.5 {
		t.Errorf("capped coverage = %v (terms %v)", c, long)
	}
	if estimateTokens("abcde") != 2 || estimateTokens("") != 0 || estimateTokens("ação") != 1 {
		t.Error("estimateTokens must be ceil(runes/4)")
	}
}
