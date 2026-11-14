package lme

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/cli"
)

// runChandaForTest is "lme chanda" inside the test binary (see TestMain).
func runChandaForTest() int {
	RegisterProvider()
	return cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
}

func testRunOptions(t *testing.T) *RunOptions {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LME_TEST_CHANDA", "1") // inherited by the server process only
	emb := EmbedOptions{Kind: "hash"}
	return &RunOptions{
		Dataset: tiny, Conds: []string{CondBM25, CondRAG, CondChanda}, BM25Unit: UnitSession, Embed: emb,
		Chanda: ChandaOptions{Exe: exe, Embed: emb, Budget: 2000}, Budget: 2000, SearchLimit: 100, Workers: 2,
		WorkDir: t.TempDir(),
	}
}

// End to end over a real chanda server (this test binary in server mode,
// MCP over stdio): phase 1 on the fixture, then the D context.
func TestPhase1AndContextDOverMCP(t *testing.T) {
	if testing.Short() {
		t.Skip("starts chanda servers")
	}
	o := testRunOptions(t)
	emb, err := OpenEmbedder(t.Context(), o.Embed)
	if err != nil {
		t.Fatal(err)
	}
	qs, err := Phase1(t.Context(), o, emb)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 3 || qs[0].QuestionID != "q1" || qs[2].QuestionID != "q3_abs" {
		t.Fatalf("results %v", qs)
	}
	c := qs[0].Conds[CondChanda]
	if c.NRanked == 0 || c.Ranked[0] != "answer_a1" || c.Metrics.Recall[5] != 1 || c.Metrics.MRR != 1 {
		t.Fatalf("chanda q1 %+v", c)
	}
	if c.Extra["turns"] != 6 || c.Extra["created"] != 6 || c.DiskBytes == 0 || c.DBBytes == 0 {
		t.Fatalf("chanda q1 ingest %+v", c)
	}
	for _, cond := range []string{CondBM25, CondRAG} {
		if r := qs[0].Conds[cond]; r.Ranked[0] != "answer_a1" {
			t.Fatalf("%s q1 %+v", cond, r)
		}
	}
	rep := Summarize(qs, o.Conds)
	if a := rep[CondChanda].Groups["all"]; a.N != 2 || a.NOfficial != 1 || rep[CondChanda].Groups[Abstention].N != 1 {
		t.Fatalf("summary %+v", a)
	}
	if md := Phase1Markdown(&Phase1Report{Meta: map[string]any{"x": 1}, Conditions: rep}, o.Conds); !strings.Contains(md, "| chanda | 2 |") {
		t.Fatalf("markdown:\n%s", md)
	}
	// Per-question databases are deleted.
	if left, _ := os.ReadDir(o.WorkDir + "/q1/chanda"); len(left) != 0 {
		t.Fatalf("work dir not cleaned: %v", left)
	}

	o.IDs = []string{"q1"}
	lines, err := Contexts(t.Context(), o, emb, CondD)
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if !strings.Contains(l.Context, "Business Administration") || l.ContextTokens != Tokens(l.Context) || l.ContextTokens > 2000 {
		t.Fatalf("D context %+v", l)
	}
	if strings.Contains(l.Context, "answer_") || strings.Contains(l.Context, "sharegpt") {
		t.Fatalf("dataset session id leaked into the context:\n%s", l.Context)
	}
	if len(l.RetrievedSessionIDs) == 0 || l.RetrievedSessionIDs[0] != "answer_a1" || l.EvidenceRecall == nil || *l.EvidenceRecall != 1 {
		t.Fatalf("D sessions %+v", l)
	}
	lb, err := Contexts(t.Context(), o, emb, CondB)
	if err != nil || !strings.Contains(lb[0].Context, "Session Date: 2023/05/22 (Mon) 09:23") {
		t.Fatalf("B %v %+v", err, lb)
	}
	o.Chanda.LLMURL = ""
	if _, err := Contexts(t.Context(), o, emb, CondE); err == nil || !strings.Contains(err.Error(), "LLM endpoint") {
		t.Fatalf("E without endpoint: %v", err)
	}
}
