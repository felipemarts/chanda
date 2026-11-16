package coresys

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/embed"
	"github.com/felipemarts/chanda/internal/eval"
)

var bg = context.Background()

// tiny is a corpus with a versioned document carrying header facts, a
// session with a tool turn and an asserted statement, and an external page.
func tiny() *eval.Dataset {
	return &eval.Dataset{
		Manifest: eval.Manifest{Name: "tiny"},
		Corpus: []eval.Item{
			{Type: eval.ItemDocument, ID: "dir", URI: "file:///aurora/dir.md", Version: 1, Date: "2026-01-05", Source: "document",
				Text: "# Equipe\n\n## Ana Ribeiro\n\nCargo: Analista Financeira\nRamal: 2345\n\nAna Ribeiro cuida da conciliação bancária.\n"},
			{Type: eval.ItemSession, ID: "s1", URI: "session://aurora/s1", Date: "2026-02-01", Source: "session",
				Turns: []eval.Turn{{Role: "user", Text: "Consulte o CRM sobre a conta Padaria Sol."},
					{Role: "tool", Name: "crm", Text: "Conta Padaria Sol: 3 tickets abertos."}},
				Facts: []eval.Statement{{Subject: "Padaria Sol", Predicate: "gestor", Value: "Bruno Lima"}}},
			{Type: eval.ItemDocument, ID: "web", URI: "https://forum.example/t/1", Date: "2026-02-10", Source: "web",
				Text: "# Fórum\n\nO ramal da Ana Ribeiro é 9999, dizem.\n"},
			{Type: eval.ItemDocument, ID: "dir", URI: "file:///aurora/dir.md", Version: 2, Date: "2026-06-01", Source: "document",
				Text: "# Equipe\n\n## Ana Ribeiro\n\nCargo: Coordenadora Financeira\nRamal: 2345\n\nAna Ribeiro cuida da conciliação bancária.\n"},
		},
		Questions: []eval.Question{
			{ID: "q1", Question: "Qual é o cargo de Ana Ribeiro?", AsOf: "2026-11-01", Gold: []eval.Gold{{DocID: "dir", Span: "Cargo: Coordenadora Financeira"}},
				Answers: []string{"Coordenadora Financeira"}, MustNotContain: []string{"Analista Financeira"}, Tags: []string{"update"}},
			{ID: "q2", Question: "Quem é o gestor da conta Padaria Sol?", AsOf: "2026-11-01", Gold: []eval.Gold{{DocID: "s1", Span: "gestor"}},
				Answers: []string{"Bruno Lima"}, Tags: []string{"session"}},
			{ID: "q3", Question: "Qual é a senha do Wi-Fi de visitantes?", AsOf: "2026-11-01", Tags: []string{"abstention"}},
		},
	}
}

func factoryEnv(t *testing.T) eval.FactoryEnv {
	t.Helper()
	cfg := config.Default()
	emb, err := embed.NewFromConfig(cfg.Embed)
	if err != nil {
		t.Fatal(err)
	}
	return eval.FactoryEnv{Config: cfg, Embedder: emb}
}

func build(t *testing.T, name string, ds *eval.Dataset) *System {
	t.Helper()
	sys, err := Factories()[name](bg, factoryEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	s := sys.(*System)
	t.Cleanup(func() { s.Close() })
	for _, it := range ds.Corpus {
		if err := s.Ingest(bg, it); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(bg); err != nil {
		t.Fatal(err)
	}
	return s
}

func retrieve(t *testing.T, s *System, text string) []eval.Retrieved {
	t.Helper()
	rs, err := s.Retrieve(bg, eval.Query{Text: text, AsOf: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}, 800)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// Every item goes through the public API with its source kind, and every
// delivered record is traced back to the corpus item it came from.
func TestSystemsWriteThroughTheAPIWithSourceKinds(t *testing.T) {
	s := build(t, "C1", tiny())
	db := s.Env().Store.DB()
	zones := map[string]string{}
	rows, err := db.Query(`SELECT s.kind, e.trust_zone FROM events e JOIN sources s ON s.id = e.source_id ORDER BY e.id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k, z string
		if err := rows.Scan(&k, &z); err != nil {
			t.Fatal(err)
		}
		zones[k] = z
	}
	rows.Close()
	want := map[string]string{"document": "internal", "session": "internal", "tool": "external", "web": "external"}
	for k, z := range want {
		if zones[k] != z {
			t.Errorf("source kind %s stored in zone %q, want %s (all: %v)", k, zones[k], z, zones)
		}
	}
	var facts int
	if err := db.QueryRow(`SELECT count(*) FROM facts`).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("C1 has no structured facts: %d %v", facts, err)
	}
	rs := retrieve(t, s, "Quem é o gestor da conta Padaria Sol?")
	if len(rs) == 0 {
		t.Fatal("nothing retrieved")
	}
	for _, r := range rs {
		if r.DocID == "" {
			t.Errorf("record %s not traced to a corpus item", r.ID)
		}
	}
	if !strings.Contains(s.Info().Description, "hybrid") || s.Info().EmbeddingModel != "hash-v1-256" {
		t.Fatalf("info %+v", s.Info())
	}
}

// C3 extracts header facts, asserts session statements and keeps only the
// current value of a single predicate as an active fact.
func TestC3HasTemporalFacts(t *testing.T) {
	s := build(t, "C3", tiny())
	db := s.Env().Store.DB()
	var active, stale int
	if err := db.QueryRow(`SELECT count(*) FROM facts WHERE predicate = 'cargo' AND status = 'active'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM facts WHERE predicate = 'cargo' AND status IN ('stale_source', 'superseded') AND text LIKE '%Analista%'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if active != 1 || stale != 1 {
		var all []string
		rows, _ := db.Query(`SELECT text || ' ' || status FROM facts ORDER BY id`)
		for rows.Next() {
			var x string
			rows.Scan(&x)
			all = append(all, x)
		}
		rows.Close()
		t.Fatalf("cargo facts: %d active, %d stale: %v", active, stale, all)
	}
	rs := retrieve(t, s, "Quem é o gestor da conta Padaria Sol?")
	found := false
	for _, r := range rs {
		found = found || (r.DocID == "s1" && strings.Contains(r.Text, "Bruno Lima"))
	}
	if !found {
		t.Fatalf("asserted statement not delivered: %+v", rs)
	}
	if !strings.Contains(s.Info().Description, "WITHOUT the reranker") {
		t.Fatalf("C3 must declare that it runs without a reranker: %s", s.Info().Description)
	}
}

// C2 needs a reranker and C4/C5 need sleep: when the core cannot provide
// them the factory says so (CodeUnavailable) instead of faking the stage.
func TestUnavailableConfigurationsAreDeclared(t *testing.T) {
	_, err := Factories()["C2"](bg, factoryEnv(t))
	if core.CodeOf(err) != core.CodeUnavailable || !strings.Contains(err.Error(), "reranker") {
		t.Fatalf("C2: %v", err)
	}
	for _, n := range []string{"C4", "C5"} {
		sys, err := Factories()[n](bg, factoryEnv(t))
		if err == nil {
			// sleep is wired: the system declares phase 1 only (no LLM).
			defer sys.Close()
			if !strings.Contains(sys.(*System).Info().Description, "phase 1") {
				t.Fatalf("%s: %s", n, sys.(*System).Info().Description)
			}
			continue
		}
		if core.CodeOf(err) != core.CodeUnavailable || !strings.Contains(err.Error(), "sleep") {
			t.Fatalf("%s: %v", n, err)
		}
	}
}

// Two equal runs give byte-identical databases (principle 11).
func TestTwoRunsAreByteIdentical(t *testing.T) {
	run := func() []byte {
		s := build(t, "C3", tiny())
		retrieve(t, s, "Qual é o cargo de Ana Ribeiro?")
		b, err := s.Checkpoint(bg)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, b := run(), run()
	if string(a) != string(b) {
		t.Fatal("two equal runs produced different databases")
	}
}
