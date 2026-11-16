package lme

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

func studyTestOptions(t *testing.T, sets ...string) *StudyOptions {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LME_TEST_CHANDA", "1") // inherited by the server process only
	var ovs []Override
	for _, s := range sets {
		ov, err := ParseOverride(s)
		if err != nil {
			t.Fatal(err)
		}
		ovs = append(ovs, ov)
	}
	return &StudyOptions{Budget: 2000, Log: t.Logf,
		Chanda: ChandaOptions{Exe: exe, Embed: EmbedOptions{Kind: "hash"}, Budget: 2000, Set: ovs}}
}

func withSets(o *StudyOptions, t *testing.T, sets ...string) *StudyOptions {
	c := *o
	c.Chanda.Set = append([]Override(nil), o.Chanda.Set...)
	for _, s := range sets {
		ov, err := ParseOverride(s)
		if err != nil {
			t.Fatal(err)
		}
		c.Chanda.Set = append(c.Chanda.Set, ov)
	}
	return &c
}

var reFichaLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \| (user|assistant|mixed|-) \| evt_[0-9A-Z]+(,evt_[0-9A-Z]+)* \| .+`)

// The study steps end to end on the synthetic LoCoMo fixture, with the hash
// embedder and no LLM: ingest, ask the use half, record outcomes, copy,
// sleep phase 1 only (S0) and the guided extractive digest (S1), leak
// check, ask the test half on both, plain RAG.
func TestStudyEndToEndLocomo(t *testing.T) {
	if testing.Short() {
		t.Skip("starts chanda servers")
	}
	ctx := t.Context()
	// burst_seconds 0 only to spare the test the 11 s wait before each sleep.
	o := studyTestOptions(t, "read.context_derived_max_share=0.3", "sleep.burst_seconds=0")
	emb, err := OpenEmbedder(ctx, o.Chanda.Embed)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	convs, err := LoadLocomo(locomoTiny)
	if err != nil {
		t.Fatal(err)
	}
	conv := convs[0]
	sum, _, _ := FileSHA256(locomoTiny)
	abs, _ := filepath.Abs(locomoTiny)
	root := t.TempDir()
	base := filepath.Join(root, "base")
	m := &StudyMeta{Embedder: "hash", EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Dataset: abs, DatasetSHA256: sum}
	if err := StudyIngestLocomo(ctx, o, base, conv, m); err != nil {
		t.Fatal(err)
	}
	if len(m.Turns) != 10 || m.Turns[3].Key != "D1:4" || m.Turns[3].EventID == "" || m.Turns[0].SessionID != "conv-t1-s1" {
		t.Fatalf("turn map %+v", m.Turns)
	}
	if err := StudyIngestLocomo(ctx, o, base, conv, m); err == nil {
		t.Fatal("second ingest into the same directory: want an error")
	}
	sp, err := ReadLocomoSplit("testdata/split-tiny.json")
	if err != nil {
		t.Fatal(err)
	}
	use, _ := LocomoStudyQuestions(conv, sp, HalfUse)
	test, _ := LocomoStudyQuestions(conv, sp, HalfTest)

	ask, err := StudyAsk(ctx, o, base, "base", use)
	if err != nil {
		t.Fatal(err)
	}
	if len(ask) != 3 || ask[0].RetrievalID == "" || ask[0].Gold != "Pixel" || ask[0].SpeakerA != "Ana" || ask[0].ConfigHash == "" {
		t.Fatalf("ask %+v", ask[0])
	}
	l0 := ask[0]
	if !strings.Contains(l0.Context, "Pixel") || l0.ContextTokens != Tokens(l0.Context) || len(l0.Items) == 0 || l0.SleepItems != 0 {
		t.Fatalf("q0 block %+v", l0)
	}
	if !l0.Evidence.Scored || l0.Evidence.Turns[0].Key != "D1:2" || !l0.Evidence.Turns[0].Reached || l0.Evidence.Turns[0].By != MatchByID || !l0.Evidence.Full {
		t.Fatalf("q0 evidence %+v", l0.Evidence)
	}
	if strings.Contains(l0.Context, "observation") || strings.Contains(l0.Context, "NEVER READ") {
		t.Fatal("dataset summaries reached the database")
	}
	answers := map[string]StudyAnswer{
		"conv-t1-q0": {QuestionID: "conv-t1-q0", Answer: "Pixel", Verdict: json.RawMessage(`true`)},
		"conv-t1-q1": {QuestionID: "conv-t1-q1", Answer: "Not mentioned", Verdict: json.RawMessage(`"no"`)},
		"conv-t1-q3": {QuestionID: "conv-t1-q3", Answer: "a bowl from pottery class", Verdict: json.RawMessage(`"CORRECT"`)},
	}
	recs, err := StudyOutcome(ctx, o, base, ask, answers, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].Value != 1 || len(recs[0].UsedIDs) == 0 || recs[1].Value != -1 || len(recs[1].UsedIDs) != 0 || recs[2].Value != 1 {
		t.Fatalf("outcomes %+v", recs)
	}
	// A second run resumes: the recorded outcomes are kept, none is added.
	again, err := StudyOutcome(ctx, o, base, ask, answers, false)
	if err != nil || len(again) != 3 || again[0].OutcomeID != recs[0].OutcomeID || again[1].Value != -1 {
		t.Fatalf("second outcome: %+v %v", again, err)
	}
	flipped := map[string]StudyAnswer{}
	for k, v := range answers {
		flipped[k] = v
	}
	for _, l := range ask[:1] {
		a := flipped[l.QuestionID]
		if recs[0].Value > 0 {
			a.Verdict = json.RawMessage("false")
		} else {
			a.Verdict = json.RawMessage("true")
		}
		flipped[l.QuestionID] = a
	}
	if _, err := StudyOutcome(ctx, o, base, ask, flipped, false); err == nil || !strings.Contains(err.Error(), "already has outcome") {
		t.Fatalf("a different value must be refused: %v", err)
	}
	// Gold answers and judge text never reach the database: q1's gold is
	// in no turn, so it must not be anywhere in the file.
	for _, n := range []string{"chanda.db", "chanda.db-wal"} {
		b, _ := os.ReadFile(filepath.Join(base, n))
		if bytes.Contains(b, []byte("The Monday before 8 May 2023")) || bytes.Contains(b, []byte("CORRECT")) {
			t.Fatalf("%s holds a gold answer or judge text", n)
		}
	}

	s0, s1 := filepath.Join(root, "S0"), filepath.Join(root, "S1")
	if err := StudyCopy(base, s0); err != nil {
		t.Fatal(err)
	}
	if err := StudyCopy(base, s1); err != nil {
		t.Fatal(err)
	}
	if err := StudyCopy(base, s1); err == nil {
		t.Fatal("copy onto a non-empty directory: want an error")
	}
	r0, err := StudySleep(ctx, o, s0, "S0", []int{1}, chanda.SleepBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if r0.Run == nil || r0.Run.Status != "completed" || len(r0.Fichas) != 0 || r0.StatsAfter.Outcomes != 3 || !strings.Contains(r0.Run.Report, "## Phase 1") {
		t.Fatalf("S0 sleep %+v", r0)
	}

	guided := withSets(o, t, "sleep.mode=guided", "sleep.guided.digest=extractive")
	r1, err := StudySleep(ctx, guided, s1, "S1", []int{1, 2}, chanda.SleepBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Phase2Skipped != "" || r1.Run.Status != "completed" {
		t.Fatalf("S1 sleep: status %s, skipped %q\n%s", r1.Run.Status, r1.Phase2Skipped, r1.Run.Report)
	}
	var counts struct {
		Guided *struct {
			Digest         string         `json:"digest"`
			GroupsDigested int            `json:"groups_digested"`
			LinesWritten   int            `json:"lines_written"`
			LinesDiscarded map[string]int `json:"lines_discarded"`
			Fichas         []string       `json:"fichas"`
		} `json:"guided"`
	}
	if err := json.Unmarshal(r1.Counts, &counts); err != nil || counts.Guided == nil {
		t.Fatalf("S1 counts %s: %v", r1.Counts, err)
	}
	g := counts.Guided
	if g.Digest != "extractive" || g.GroupsDigested == 0 || g.LinesWritten == 0 || len(g.Fichas) == 0 || len(r1.Fichas) != len(g.Fichas) {
		t.Fatalf("S1 guided counts %+v, %d fichas read", g, len(r1.Fichas))
	}
	var cost chanda.SleepRunOutput
	_ = json.Unmarshal(r1.Output, &cost)
	if cost.Cost.LLMCalls != 0 {
		t.Fatalf("extractive digest made %d LLM calls", cost.Cost.LLMCalls)
	}
	for _, f := range r1.Fichas {
		if f.Kind != "summary" || !strings.HasPrefix(f.Title, "Digest") || len(f.Items) == 0 {
			t.Fatalf("ficha %+v", f)
		}
		for _, it := range f.Items {
			if !reFichaLine.MatchString(it.Text) || len(it.Cites) == 0 {
				t.Fatalf("ficha line %q (cites %v)", it.Text, it.Cites)
			}
		}
	}
	var golds, forbid []string
	for _, q := range append(append([]StudyQuestion{}, use...), test...) {
		golds = append(golds, q.Gold)
	}
	for _, q := range test {
		forbid = append(forbid, q.Question)
	}
	lr, err := StudyLeakCheck(ctx, s1, golds, forbid)
	if err != nil {
		t.Fatal(err)
	}
	if !lr.OK || lr.Outcomes != 3 || lr.Retrievals != 3 || lr.DerivedTexts == 0 {
		t.Fatalf("leak check %+v", lr)
	}
	// A test question in the retrievals is caught.
	if lr, _ := StudyLeakCheck(ctx, s1, golds, []string{use[0].Question}); lr.OK || len(lr.TestQuestionsInRetrievals) != 1 {
		t.Fatalf("planted test question not caught: %+v", lr)
	}

	t0, err := StudyAsk(ctx, o, s0, "S0", test)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := StudyAsk(ctx, guided, s1, "S1", test)
	if err != nil {
		t.Fatal(err)
	}
	if len(t0) != 5 || len(t1) != 5 {
		t.Fatalf("test asks %d %d", len(t0), len(t1))
	}
	sleepItems := 0
	for _, l := range t1 {
		sleepItems += l.SleepItems
		if l.SleepShare > 0.3+1e-9 && l.SleepTokens > 600 {
			t.Fatalf("%s: sleep items take %d tokens (%.2f of the block) over the 0.3 share", l.QuestionID, l.SleepTokens, l.SleepShare)
		}
		for _, it := range l.Items {
			if it.Type == "page" && len(it.OriginEventIDs) == 0 {
				t.Fatalf("%s: page item without origins %+v", l.QuestionID, it)
			}
		}
	}
	cited := 0
	for _, l := range t1 {
		for _, e := range l.Evidence.Turns {
			if e.Cited {
				cited++
			}
		}
		if l.Evidence.ReachedOrCited < l.Evidence.Reached {
			t.Fatalf("%s: secondary count below primary %+v", l.QuestionID, l.Evidence)
		}
	}
	if sleepItems == 0 || cited == 0 {
		t.Fatalf("S1 test blocks: %d sleep items, %d evidence turns cited by a ficha", sleepItems, cited)
	}
	for _, l := range t0 {
		if l.SleepItems != 0 {
			t.Fatalf("S0 block has sleep items: %+v", l)
		}
	}
	fi, err := StudyFichas(ctx, s1)
	if err != nil || len(fi) != len(r1.Fichas) {
		t.Fatalf("fichas %v %d", err, len(fi))
	}

	rag, err := StudyRAG(ctx, emb, conv, test, 2000, "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(rag) != 5 || !strings.Contains(rag[0].Context, "Session Date: 10:37 am on 27 June, 2023") || rag[0].RetrievalID != "" {
		t.Fatalf("rag %+v", rag[0])
	}
	// The whole tiny conversation fits in 2,000 tokens: every valid
	// evidence turn reaches the plain-RAG block.
	for _, l := range rag {
		if !l.Evidence.Full {
			t.Fatalf("rag evidence %s %+v", l.QuestionID, l.Evidence)
		}
	}
	if !strings.Contains(rag[0].Context, "user: Ana: I ran my first half marathon in Lisbon") {
		t.Fatalf("rag rendering:\n%s", rag[0].Context)
	}
	// JSONL round trip.
	p := filepath.Join(root, "t1.jsonl")
	if err := WriteJSONL(p, t1); err != nil {
		t.Fatal(err)
	}
	back, err := ReadJSONL[*StudyAskLine](p)
	if err != nil || len(back) != 5 || back[0].RetrievalID != t1[0].RetrievalID {
		t.Fatalf("round trip %v", err)
	}
	mm, err := ReadStudyMeta(s1)
	if err != nil || mm.CopiedFrom == "" || len(mm.Steps) < 5 {
		t.Fatalf("meta %v %+v", err, mm)
	}
}

// A LongMemEval haystack ingested as condition D does, then asked.
func TestStudyEndToEndLME(t *testing.T) {
	if testing.Short() {
		t.Skip("starts chanda servers")
	}
	ctx := t.Context()
	o := studyTestOptions(t)
	emb, err := OpenEmbedder(ctx, o.Chanda.Embed)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	var q *Question
	if err := Each(tiny, func(x *Question) error {
		if x.QuestionID == "q1" {
			q = x
		}
		return nil
	}); err != nil || q == nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "q1")
	m := &StudyMeta{Embedder: "hash", EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Dataset: tiny}
	if err := StudyIngestLME(ctx, o, dir, q, m); err != nil {
		t.Fatal(err)
	}
	lines, err := StudyAsk(ctx, o, dir, "D", []StudyQuestion{LMEStudyQuestion(q)})
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if l.Condition != "D" || l.QuestionDate == "" || !strings.Contains(l.Context, "Business Administration") ||
		len(l.Evidence.Turns) != 1 || l.Evidence.Turns[0].Key != "s1t0" || !l.Evidence.Full {
		t.Fatalf("lme ask %+v", l)
	}
}
