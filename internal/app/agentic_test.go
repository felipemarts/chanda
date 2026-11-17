package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Decision 0151: with an LLM configured, context and search search the query
// followed by the LLM's terms; read.agentic_search: false, no LLM or a failed
// call leave the query as given.
func TestAgenticSearch(t *testing.T) {
	const memory = "A reunião de orçamento ficou para quinta-feira às 10h."
	const query = "quando é o encontro financeiro?"
	terms := func(s string) *llm.Fake {
		return &llm.Fake{Handler: func(core.LLMRequest) llm.FakeResponse {
			return llm.FakeResponse{JSON: `{"terms":"` + s + `"}`}
		}}
	}
	off := func(c *config.Config) { f := false; c.Read.AgenticSearch = &f }
	setup := func(t *testing.T, o Options, mut func(*config.Config)) *App {
		o.DisableVectors = true // keyword only: the hash embedder's n-grams would blur the case
		a := newAppWith(t, o, mut)
		must(a.Remember(cli, chanda.RememberInput{Content: memory}))
		must(a.Flush(cli, chanda.FlushInput{}))
		return a
	}
	found := func(items []chanda.Item) bool {
		return slices.ContainsFunc(items, func(it chanda.Item) bool { return strings.Contains(it.Text, "orçamento") })
	}

	t.Run("default is on", func(t *testing.T) {
		if !config.Default().Read.Agentic() {
			t.Fatal("read.agentic_search must default to on")
		}
	})
	t.Run("terms reach the read path", func(t *testing.T) {
		f := terms("reunião orçamento quinta-feira")
		a := setup(t, Options{LLM: f}, nil)
		s := must(a.Search(agent, chanda.SearchInput{Query: query}))
		if !found(s.Items) || !slices.Contains(s.Notices, noticeAgentic) {
			t.Fatalf("expanded search: items %+v notices %v", s.Items, s.Notices)
		}
		calls := f.Calls()
		if len(calls) != 1 || calls[0].Prompt != query || calls[0].System != agenticSystem {
			t.Fatalf("llm calls: %+v", calls)
		}
		logged := str(t, a, `SELECT query_text FROM retrievals WHERE id = ?`, s.RetrievalID)
		if logged != query+"\nreunião orçamento quinta-feira" {
			t.Fatalf("logged query %q: the caller's words first, then the terms", logged)
		}
		c := must(a.Context(agent, chanda.ContextInput{Query: query}))
		if !strings.Contains(c.Text, "orçamento") || !slices.Contains(c.Notices, noticeAgentic) {
			t.Fatalf("expanded context: %q notices %v", c.Text, c.Notices)
		}
	})
	t.Run("off", func(t *testing.T) {
		f := terms("reunião orçamento quinta-feira")
		a := setup(t, Options{LLM: f}, off)
		s := must(a.Search(agent, chanda.SearchInput{Query: query}))
		if len(f.Calls()) != 0 || slices.Contains(s.Notices, noticeAgentic) {
			t.Fatalf("agentic search off still called the LLM: %d calls", len(f.Calls()))
		}
		if found(s.Items) {
			t.Fatal("the plain query shares no term with the memory; the case must need the expansion")
		}
		if got := str(t, a, `SELECT query_text FROM retrievals WHERE id = ?`, s.RetrievalID); got != query {
			t.Fatalf("logged query %q", got)
		}
	})
	t.Run("no LLM", func(t *testing.T) {
		a := setup(t, Options{}, nil)
		s := must(a.Search(agent, chanda.SearchInput{Query: query}))
		if slices.Contains(s.Notices, noticeAgentic) || slices.Contains(s.Notices, noticeAgenticFailed) {
			t.Fatalf("notices without an LLM: %v", s.Notices)
		}
	})
	t.Run("failure falls back", func(t *testing.T) {
		f := &llm.Fake{Script: []llm.FakeResponse{{Err: core.Errf(core.CodeUnavailable, "down")}, {JSON: `{"answer":"x"}`}}}
		a := setup(t, Options{LLM: f}, nil)
		for range 2 { // a transport error, then an answer off the schema
			s := must(a.Search(agent, chanda.SearchInput{Query: query}))
			if !slices.Contains(s.Notices, noticeAgenticFailed) {
				t.Fatalf("notices %v", s.Notices)
			}
			if got := str(t, a, `SELECT query_text FROM retrievals WHERE id = ?`, s.RetrievalID); got != query {
				t.Fatalf("logged query %q after a failed call", got)
			}
		}
	})
	t.Run("empty terms", func(t *testing.T) {
		a := setup(t, Options{LLM: terms("  ")}, nil)
		s := must(a.Search(agent, chanda.SearchInput{Query: query}))
		if slices.Contains(s.Notices, noticeAgentic) {
			t.Fatalf("empty terms reported as an expansion: %v", s.Notices)
		}
	})
}
