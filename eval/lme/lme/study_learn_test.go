package lme

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormWordsAndTextsOf(t *testing.T) {
	if got := normWords(`  What's  Ana's "cat"?`); got != " what s ana s cat " {
		t.Fatalf("normWords %q", got)
	}
	if normWords("") != " " || normWords("!!") != " " {
		t.Fatalf("empty normWords %q %q", normWords(""), normWords("!!"))
	}
	// Whole words only: "class" is not in "classes".
	if strings.Contains(normWords("pottery classes"), normWords("pottery class")) {
		t.Fatal("word boundary")
	}
	got := textsOf(`[{"id":"evt_1","bm25":2022.5,"dropped":"a Pixel phone","features":{"x":1}}]`)
	if strings.Join(got, "|") != "a Pixel phone|evt_1" {
		t.Fatalf("textsOf JSON %q", got)
	}
	if got := textsOf("plain [text"); len(got) != 1 || got[0] != "plain [text" {
		t.Fatalf("textsOf plain %q", got)
	}
}

// learnFixture builds a minimal database with the tables the check reads
// (and a FTS5 table, a blob table and a WITHOUT ROWID table), all clean.
func learnFixture(t *testing.T, keyTable string, mutate ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "chanda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE events (id TEXT PRIMARY KEY, kind TEXT NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE retrievals (id TEXT PRIMARY KEY, query_text TEXT NOT NULL, params TEXT NOT NULL, candidates TEXT NOT NULL, delivered TEXT NOT NULL)`,
		`CREATE TABLE outcomes (id TEXT PRIMARY KEY, retrieval_id TEXT NOT NULL, value REAL NOT NULL, used_ids TEXT NOT NULL, actor TEXT NOT NULL)`,
		`CREATE TABLE pages (id TEXT PRIMARY KEY, title TEXT, body TEXT)`,
		`CREATE TABLE audit_log (id INTEGER PRIMARY KEY, action TEXT, details TEXT)`,
		`CREATE TABLE embeddings (record_id TEXT, vector BLOB, PRIMARY KEY (record_id)) WITHOUT ROWID`,
		`CREATE VIRTUAL TABLE events_fts USING fts5(content, record_id UNINDEXED)`,
		// Turns hold anything, even a test question or a judge word: not checked.
		`INSERT INTO events VALUES ('evt_1', 'turn', 'Ana: What did Ana buy? CORRECT, I bought a Pixel phone')`,
		`INSERT INTO events VALUES ('evt_2', 'turn', 'Bruno: I run in Lisbon')`,
		`INSERT INTO events_fts VALUES ('Ana: What did Ana buy?', 'evt_1')`,
		`INSERT INTO embeddings VALUES ('evt_1', x'00ff')`,
		// Use questions as retrieval queries; the first holds the test
		// question "What did Ana buy?" and the gold "Lisbon" inside its text.
		`INSERT INTO retrievals VALUES ('ret_1', 'What did Ana buy in Lisbon?', '{"budget":2000}',
			'[{"id":"evt_1","type":"event","bm25":1.5,"features":{"key_rank":0}},{"id":"evt_2","type":"event","features":{}}]', '["evt_1","evt_2"]')`,
		`INSERT INTO retrievals VALUES ('ret_2', 'Where does  Bruno run?', '{}', '[]', '["evt_2"]')`,
		`INSERT INTO outcomes VALUES ('out_1', 'ret_1', 1, '["evt_1"]', 'agent')`,
		`INSERT INTO outcomes VALUES ('out_2', 'ret_2', -1, '[]', 'agent')`,
		`INSERT INTO pages VALUES ('pg_1', 'Digest', 'Ana bought a phone')`,
		`INSERT INTO audit_log (action, details) VALUES ('outcome', '{"value":1}')`,
	}
	if keyTable != "" {
		stmts = append(stmts,
			`CREATE TABLE `+keyTable+` (id TEXT PRIMARY KEY, record_id TEXT NOT NULL, retrieval_id TEXT NOT NULL, text TEXT NOT NULL)`,
			`INSERT INTO `+keyTable+` VALUES ('key_1', 'evt_1', 'ret_1', 'what did ana buy in lisbon?')`,
			`CREATE VIRTUAL TABLE `+keyTable+`_fts USING fts5(text)`,
			`INSERT INTO `+keyTable+`_fts VALUES ('What did Ana buy?')`)
	}
	for _, s := range append(stmts, mutate...) {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return db
}

func learnInput() LearnLeakInput {
	return LearnLeakInput{
		Use: []StudyQuestion{
			{Half: HalfUse, Question: "What did Ana buy in Lisbon?", Gold: "a Pixel phone"},
			{Half: HalfUse, Question: "Where does Bruno run?", Gold: "Lisbon"},
		},
		Test: []StudyQuestion{
			{Half: HalfTest, Question: "What did Ana buy?", Gold: "a phone"},
			{Half: HalfTest, Question: "When did Bruno move to Porto?", Gold: "May 2023"},
			{Half: HalfTest, Question: "Is it?", Gold: "Yes"},
		},
	}
}

func TestLeakCheckLearnClean(t *testing.T) {
	for _, kt := range []string{"learned_keys", "my_keys", ""} {
		db := learnFixture(t, kt)
		r, err := LeakCheckLearnDB(t.Context(), db, learnInput())
		if err != nil {
			t.Fatal(err)
		}
		if !r.OK || len(r.Failures) != 0 {
			t.Fatalf("%q: clean fixture failed: %+v", kt, r)
		}
		if r.KeyTable != kt || (kt == "") != (r.KeyNote == "no key table") || (kt != "" && r.Keys != 1) {
			t.Fatalf("%q: key table %q note %q keys %d", kt, r.KeyTable, r.KeyNote, r.Keys)
		}
		if r.Retrievals != 2 || r.Outcomes != 2 || r.GoldsChecked != 4 || r.GoldsTooShort != 1 {
			t.Fatalf("%q: counts %+v", kt, r)
		}
		// Listed, not stops: the test question and a gold inside a use question.
		var inUse []string
		for _, h := range r.TestQuestionInUse {
			inUse = append(inUse, h.Table+"."+h.Column)
		}
		want := "retrievals.query_text"
		if kt != "" {
			want = kt + ".text," + want
		}
		if strings.Join(inUse, ",") != want {
			t.Fatalf("%q: test questions inside use questions %v, want %s", kt, inUse, want)
		}
		if len(r.GoldInUseQuestion) == 0 || r.GoldInUseQuestion[0].Via == "" {
			t.Fatalf("%q: gold inside a use question not listed: %+v", kt, r.GoldInUseQuestion)
		}
		skipped := strings.Join(r.Skipped, "|")
		if !strings.Contains(skipped, "events_fts (virtual)") || !strings.Contains(skipped, "events_fts_data (shadow of events_fts)") ||
			!strings.Contains(skipped, "kind turn") {
			t.Fatalf("%q: skipped %s", kt, skipped)
		}
	}
}

func TestLeakCheckLearnStops(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     []string
		check      func(*LearnLeakReport) bool
	}{
		{"test question in a page", "test question text in the database",
			[]string{`UPDATE pages SET body = 'Q: When did Bruno move to Porto? A: soon'`},
			func(r *LearnLeakReport) bool { return len(r.TestQuestionIn) == 1 && r.TestQuestionIn[0].Table == "pages" }},
		{"test question in a non-turn event", "test question text in the database",
			[]string{`INSERT INTO events VALUES ('evt_3', 'statement', 'when did bruno move to porto')`},
			func(r *LearnLeakReport) bool { return len(r.TestQuestionIn) == 1 && r.TestQuestionIn[0].Row == "evt_3" }},
		{"test question in a JSON value", "test question text in the database",
			[]string{`UPDATE retrievals SET params = '{"note":"When did Bruno move to Porto?"}' WHERE id = 'ret_2'`},
			func(r *LearnLeakReport) bool { return len(r.TestQuestionIn) == 1 && r.TestQuestionIn[0].Column == "params" }},
		{"test question as a retrieval query", "retrieval query that is not a use-half question",
			[]string{`INSERT INTO retrievals VALUES ('ret_3', 'When did Bruno move to Porto?', '{}', '[]', '[]')`},
			func(r *LearnLeakReport) bool { return len(r.RetrievalsNotUse) == 1 && len(r.TestQuestionIn) == 1 }},
		{"key text not a use question", "key text that is not a use-half question",
			[]string{`INSERT INTO learned_keys VALUES ('key_2', 'evt_2', 'ret_2', 'Where does Bruno live?')`},
			func(r *LearnLeakReport) bool { return len(r.KeysNotUse) == 1 && strings.HasPrefix(r.KeysNotUse[0], "key_2: ") }},
		{"outcome value", "outcome not",
			[]string{`UPDATE outcomes SET value = 0.5 WHERE id = 'out_2'`},
			func(r *LearnLeakReport) bool { return len(r.BadOutcomes) == 1 && strings.Contains(r.BadOutcomes[0], "value 0.5") }},
		{"used id not delivered", "outcome not",
			[]string{`UPDATE outcomes SET used_ids = '["evt_1"]' WHERE id = 'out_2'`},
			func(r *LearnLeakReport) bool { return len(r.BadOutcomes) == 1 && strings.Contains(r.BadOutcomes[0], "evt_1 not delivered") }},
		{"outcome of a missing retrieval", "outcome not",
			[]string{`INSERT INTO outcomes VALUES ('out_3', 'ret_9', 1, '[]', 'agent')`},
			func(r *LearnLeakReport) bool { return len(r.BadOutcomes) == 1 && strings.Contains(r.BadOutcomes[0], "missing") }},
		{"judge word", "judge output",
			[]string{`INSERT INTO audit_log (action, details) VALUES ('flag', '{"reason":"WRONG"}')`},
			func(r *LearnLeakReport) bool { return len(r.JudgeOutput) == 1 && r.JudgeOutput[0].Match == "WRONG" }},
		{"gold answer in a retrieval", "gold answer",
			[]string{`UPDATE retrievals SET candidates = '[{"id":"evt_2","dropped":"May 2023"}]' WHERE id = 'ret_2'`},
			func(r *LearnLeakReport) bool { return len(r.GoldHits) == 1 && r.GoldHits[0].Match == "May 2023" }},
		{"gold answer in an outcome", "gold answer",
			[]string{`UPDATE outcomes SET actor = 'a pixel phone' WHERE id = 'out_2'`},
			func(r *LearnLeakReport) bool { return len(r.GoldHits) == 1 && r.GoldHits[0].Table == "outcomes" }},
	}
	for _, c := range cases {
		db := learnFixture(t, "learned_keys", c.mutate...)
		r, err := LeakCheckLearnDB(t.Context(), db, learnInput())
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if r.OK || !strings.Contains(strings.Join(r.Failures, "|"), c.want) || !c.check(r) {
			b, _ := json.MarshalIndent(r, "", " ")
			t.Fatalf("%s: not caught as %q:\n%s", c.name, c.want, b)
		}
	}
	// A gold answer in a page (not keys, retrievals or outcomes) is the sleep
	// check's business, not a stop here; a short gold is not checked.
	db := learnFixture(t, "learned_keys", `UPDATE pages SET body = 'May 2023'`, `UPDATE outcomes SET actor = 'yes' WHERE id = 'out_2'`)
	if r, err := LeakCheckLearnDB(t.Context(), db, learnInput()); err != nil || !r.OK {
		t.Fatalf("gold in a page / short gold: %+v %v", r, err)
	}
	// A key table that does not exist is an error, not a pass.
	if _, err := LeakCheckLearnDB(t.Context(), learnFixture(t, ""), LearnLeakInput{KeyTable: "nope"}); err == nil {
		t.Fatal("missing -key-table: want an error")
	}
}

// The check on a real Chanda database (the tiny LoCoMo fixture): clean after
// the use half and its outcomes, caught once the test half was asked. The
// keys step either works or says the tool is missing.
func TestStudyLearnOnChandaDB(t *testing.T) {
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
	convs, err := LoadLocomo(locomoTiny)
	if err != nil {
		t.Fatal(err)
	}
	conv := convs[0]
	sum, _, _ := FileSHA256(locomoTiny)
	abs, _ := filepath.Abs(locomoTiny)
	base := filepath.Join(t.TempDir(), "base")
	m := &StudyMeta{Embedder: "hash", EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Dataset: abs, DatasetSHA256: sum}
	if err := StudyIngestLocomo(ctx, o, base, conv, m); err != nil {
		t.Fatal(err)
	}
	sp, err := ReadLocomoSplit("testdata/split-tiny.json")
	if err != nil {
		t.Fatal(err)
	}
	use, _ := LocomoStudyQuestions(conv, sp, HalfUse)
	test, _ := LocomoStudyQuestions(conv, sp, HalfTest)

	if _, err := StudyKeys(ctx, o, base, "bogus"); err == nil || !strings.Contains(err.Error(), "rebuild|stats") {
		t.Fatalf("unknown action: %v", err)
	}
	_, kerr := StudyKeys(ctx, o, base, "stats")
	haveKeys := kerr == nil
	if kerr != nil && !errors.Is(kerr, ErrNoKeysTool) {
		t.Fatalf("keys stats: %v", kerr)
	}
	ao := o
	if haveKeys {
		ao = withSets(o, t, "write.learned_keys=true")
	}
	ask, err := StudyAsk(ctx, ao, base, "base", use)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ask {
		if l.KeyCandidates != 0 {
			t.Fatalf("%s: key candidates with keys off: %d", l.QuestionID, l.KeyCandidates)
		}
	}
	answers := map[string]StudyAnswer{
		"conv-t1-q0": {QuestionID: "conv-t1-q0", Answer: "Pixel", Verdict: json.RawMessage(`true`)},
		"conv-t1-q1": {QuestionID: "conv-t1-q1", Answer: "Not mentioned", Verdict: json.RawMessage(`false`)},
		"conv-t1-q3": {QuestionID: "conv-t1-q3", Answer: "a bowl from pottery class", Verdict: json.RawMessage(`true`)},
	}
	if _, err := StudyOutcome(ctx, ao, base, ask, answers, false); err != nil {
		t.Fatal(err)
	}
	in := LearnLeakInput{Use: use, Test: test}
	r, err := StudyLeakCheckLearn(ctx, base, in)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.Retrievals != 3 || r.Outcomes != 3 || r.ValuesScanned == 0 || len(r.Scanned) < 10 {
		b, _ := json.MarshalIndent(r, "", " ")
		t.Fatalf("clean base database failed the check:\n%s", b)
	}
	// q0's gold "Pixel" is inside use question q3's text: listed, not a stop.
	listed := false
	for _, h := range r.GoldInUseQuestion {
		listed = listed || (h.Match == "Pixel" && h.Table == "retrievals")
	}
	if !listed {
		t.Fatalf("gold inside a use question not listed: %+v", r.GoldInUseQuestion)
	}
	if haveKeys {
		if r.KeyTable == "" || r.Keys == 0 {
			t.Fatalf("keys on, positive outcomes recorded, but key table %q keys %d", r.KeyTable, r.Keys)
		}
	} else if r.KeyTable != "" || r.KeyNote != "no key table" {
		t.Fatalf("no keys tool but key table %q (%s)", r.KeyTable, r.KeyNote)
	}

	if _, err := StudyAsk(ctx, o, base, "U0", test); err != nil {
		t.Fatal(err)
	}
	r, err = StudyLeakCheckLearn(ctx, base, in)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || len(r.RetrievalsNotUse) != len(test) || len(r.TestQuestionIn) < len(test) {
		t.Fatalf("test half in the retrievals not caught: ok=%t not-use=%d test-in=%d", r.OK, len(r.RetrievalsNotUse), len(r.TestQuestionIn))
	}
	if !haveKeys {
		t.Skipf("keys step not exercised: %v", kerr)
	}
	// With the tool: rebuild is idempotent and stats answers.
	if _, err := StudyKeys(ctx, withSets(o, t, "write.learned_keys=true"), base, "rebuild"); err != nil {
		t.Fatal(err)
	}
	st, err := StudyKeys(ctx, o, base, "stats")
	if err != nil || len(st.Output) == 0 {
		t.Fatalf("keys stats: %v %s", err, st.Output)
	}
}
