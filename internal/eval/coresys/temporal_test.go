package coresys

import (
	"fmt"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// The temporal suite names each delivered record that carries a superseded
// value, with its type and why the read path let it through (decision 0120).
func TestTemporalSuiteLogsStaleRecords(t *testing.T) {
	ds := tiny()
	s, err := loaded(bg, factoryEnv(t), ds, suiteVariant())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var old string
	if err := s.e.Store.DB().QueryRow(`SELECT id FROM events WHERE content LIKE '%Analista Financeira%' ORDER BY id LIMIT 1`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	var lines []string
	Logf = func(format string, a ...any) { lines = append(lines, fmt.Sprintf(format, a...)) }
	t.Cleanup(func() { Logf = func(string, ...any) {} })
	rs := make([]eval.QuestionResult, len(ds.Evaluated()))
	rs[0] = eval.QuestionResult{QuestionID: "q1", Forbidden: true, Delivered: []string{old}}
	m, err := TemporalSuite(bg, eval.SuiteInput{System: s, Dataset: ds, Results: rs})
	if err != nil {
		t.Fatal(err)
	}
	if m["stale_answers"] != 1 || len(lines) != 1 || !strings.Contains(lines[0], "event "+old) || !strings.Contains(lines[0], "q1") {
		t.Fatalf("measured %v, lines %q", m, lines)
	}
	if strings.Contains(lines[0], "Analista") {
		t.Fatalf("diagnostics must not carry content: %q", lines[0])
	}
}
