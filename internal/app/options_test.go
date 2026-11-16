package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/embed"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/pkg/chanda"
)

func newAppWith(t *testing.T, o Options, mut func(*config.Config)) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Namespace = "ns"
	if mut != nil {
		mut(cfg)
	}
	e, _ := env.NewTestAt(t, filepath.Join(t.TempDir(), "chanda.db"), cfg)
	a, err := NewWith(e, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

type namedEmbedder struct{ core.Embedder }

func (namedEmbedder) ModelID() string { return "injected" }

// BASE.md "Evaluation": the configurations C1..C5 are subsets of the
// system, selected with app.Options.
func TestOptionsSelectConfigurations(t *testing.T) {
	header := func(c *config.Config) { c.Write.Extractors = []config.ExtractorConfig{{Name: "hdr", Kind: "header"}} }
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plano.md"), []byte("# Plano Pro\n\nOwner: Ana\n\nThe plan includes support by e-mail.\n"), 0o644)

	t.Run("DisableFacts", func(t *testing.T) {
		a := newAppWith(t, Options{DisableFacts: true}, header)
		must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
		if n := count(t, a, `SELECT count(*) FROM facts`); n != 0 {
			t.Fatalf("%d facts extracted with facts disabled", n)
		}
		_, err := a.AssertFact(cli, chanda.AssertFactInput{Subject: "Plano Pro", Predicate: "owner", Value: "Rui"})
		if core.CodeOf(err) != core.CodeUnavailable {
			t.Fatalf("assert_fact with facts disabled: %v", err)
		}
	})
	t.Run("DisableVectors", func(t *testing.T) {
		a := newAppWith(t, Options{DisableVectors: true}, nil)
		must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
		must(a.Flush(cli, chanda.FlushInput{}))
		s := must(a.Search(cli, chanda.SearchInput{Query: "support e-mail"}))
		if len(s.Items) == 0 {
			t.Fatal("keyword search found nothing")
		}
		if p := str(t, a, `SELECT params FROM retrievals WHERE id = ?`, s.RetrievalID); !strings.Contains(p, `"lists":1`) {
			t.Fatalf("vectors used with DisableVectors: %s", p)
		}
	})
	t.Run("Embedder", func(t *testing.T) {
		h, err := embed.NewFromConfig(config.Default().Embed)
		if err != nil {
			t.Fatal(err)
		}
		a := newAppWith(t, Options{Embedder: namedEmbedder{h}}, nil)
		if st := must(a.Stats(cli, chanda.StatsInput{})); st.EmbedModel != "injected" {
			t.Fatalf("embedder not injected: %q", st.EmbedModel)
		}
	})
	t.Run("Full", func(t *testing.T) {
		a := newAppWith(t, Options{}, header)
		must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
		if n := count(t, a, `SELECT count(*) FROM facts`); n != 1 {
			t.Fatalf("%d facts, want 1", n)
		}
		if a.Options().DisableFacts {
			t.Fatal("zero options must be the full system")
		}
	})
}
