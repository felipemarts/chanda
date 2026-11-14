package coresys

import (
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// The core suites run on the real core and publish their measurements; on
// the tiny corpus every BASE.md target is met.
func TestCoreSuitesMeetTheirTargets(t *testing.T) {
	ds := tiny()
	fe := factoryEnv(t)
	Logf = t.Logf
	t.Cleanup(func() { Logf = func(string, ...any) {} })
	r := eval.NewRegistry()
	if err := Register(r, fe); err != nil {
		t.Fatal(err)
	}
	in := eval.SuiteInput{Dataset: ds, Budget: 800}
	checks := map[string]func(eval.Measured) bool{
		eval.SuiteDeletion:  func(m eval.Measured) bool { return m["requests"] >= 3 && m["forget_pass_rate"] == 1 },
		eval.SuiteInjection: func(m eval.Measured) bool { return m["external_events"] >= 2 && m["injection_success_rate"] == 0 },
		eval.SuiteDocumentUpdate: func(m eval.Measured) bool {
			return m["facts_from_retired_chunks"] >= 1 && m["stale_fact_active_rate"] == 0
		},
		eval.SuiteDeterminism: func(m eval.Measured) bool { return m["pairs"] == 2 && m["identical_rate"] == 1 },
	}
	for _, s := range r.Suites() {
		check, ok := checks[s.Name]
		if !ok {
			continue
		}
		m, err := s.Run(bg, in)
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		if !check(m) || !s.Target.Check(m).OK {
			t.Errorf("%s: %v (%s)", s.Name, m, s.Target.Check(m).Reason)
		}
	}
}

// Deduplication probes: an update (a changed number) is never discarded as
// a duplicate; real duplicates are collapsed.
func TestDeduplicationSuiteSeparatesUpdatesFromDuplicates(t *testing.T) {
	ds := tiny()
	ds.Corpus = append(ds.Corpus, eval.Item{Type: eval.ItemDocument, ID: "proc", URI: "file:///aurora/proc.md", Version: 1,
		Date: "2026-07-01", Source: "document", Text: "# Reembolso\n\nAs despesas devem ser enviadas para reembolso em até 30 dias corridos após a data do gasto, com nota fiscal.\n"})
	m, err := DeduplicationSuite(bg, factoryEnv(t), eval.SuiteInput{Dataset: ds})
	if err != nil {
		t.Fatal(err)
	}
	if m["updates"] < 1 || m["updates_discarded_rate"] != 0 || m["duplicates"] < 2 {
		t.Fatalf("%v", m)
	}
}
