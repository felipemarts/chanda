package lme

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceSummaryAndPairs(t *testing.T) {
	f := func(x float64) *float64 { return &x }
	a := []*EvidenceRow{
		{QuestionID: "q1", QuestionType: "ssu", Condition: "D", EvidenceTurns: 1, EvidenceTurnsInContext: 1, EvidenceTurnRecall: f(1),
			ContextItems: []ContextItem{{Role: "user", Tokens: 10}, {Role: "assistant", Tokens: 30, Truncated: true}}},
		{QuestionID: "q2", QuestionType: "ms", Condition: "D", EvidenceTurns: 2, EvidenceTurnsInContext: 1, EvidenceTurnRecall: f(0.5),
			ContextItems: []ContextItem{{Role: "user", Tokens: 20}}},
		{QuestionID: "q3_abs", QuestionType: "ms", Condition: "D"}, // not scored
	}
	s := SummarizeEvidence("a.jsonl", a)
	if s.Condition != "D" || s.Questions != 3 || len(s.Groups) != 3 || s.Groups[2].Group != "all" {
		t.Fatalf("summary %+v", s)
	}
	all := s.Groups[2]
	if all.N != 2 || all.MeanRecall != 0.75 || all.FullRecall != 0.5 || all.MeanItems != 1.5 || all.TokensPerItem != 20 ||
		all.AssistantShare != 0.5 || all.TruncatedShare != 1.0/3 {
		t.Fatalf("all %+v", all)
	}
	b := []*EvidenceRow{
		{QuestionID: "q1", EvidenceTurns: 1, EvidenceTurnsInContext: 0, EvidenceTurnRecall: f(0)},
		{QuestionID: "q2", EvidenceTurns: 2, EvidenceTurnsInContext: 2, EvidenceTurnRecall: f(1)},
		{QuestionID: "q9", EvidenceTurns: 1, EvidenceTurnsInContext: 1, EvidenceTurnRecall: f(1)},
	}
	p := PairEvidence("a", a, "b", b)
	if p.N != 2 || len(p.OnlyA) != 1 || p.OnlyA[0] != "q1" || len(p.OnlyB) != 1 || p.OnlyB[0] != "q2" || p.OnlyInB != 1 ||
		p.HigherA != 1 || p.HigherB != 1 || p.SignTestP != 1 {
		t.Fatalf("pair %+v", p)
	}
	if md := EvidenceMarkdown([]*EvidenceFile{s}, []*EvidencePair{p}); !strings.Contains(md, "| all | 2 | 75.0 | 50.0 |") || !strings.Contains(md, "A full, B not: q1") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestReadEvidenceRows(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.jsonl")
	line := `{"question_id":"q1","question_type":"ssu","condition":"B","evidence_turns":1,"evidence_turns_in_context":1,"evidence_turn_recall":1,"context_items":[{"session":0,"turn":0,"role":"user","tokens":5}]}`
	if err := os.WriteFile(good, []byte(line+"\n\n"+strings.Replace(line, `"q1"`, `"q2"`, 1)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadEvidenceRows(good)
	if err != nil || len(rows) != 2 || !rows[0].Scored() || rows[1].QuestionID != "q2" || rows[0].ContextItems[0].Tokens != 5 {
		t.Fatalf("%v %+v", err, rows)
	}
	old := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(old, []byte(`{"question_id":"q1","condition":"D"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidenceRows(old); err == nil || !strings.Contains(err.Error(), "evidence_turns") {
		t.Fatalf("old file: %v", err)
	}
}

func TestSignTestP(t *testing.T) {
	cases := []struct {
		k, m int
		want float64
	}{{0, 0, 1}, {1, 1, 1}, {5, 0, 0.0625}, {0, 6, 0.03125}, {8, 2, 0.109375}}
	for _, c := range cases {
		if got := SignTestP(c.k, c.m); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("SignTestP(%d, %d) = %v, want %v", c.k, c.m, got, c.want)
		}
	}
}
