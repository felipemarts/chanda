package lme

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConflictsAction(t *testing.T) {
	for _, a := range []string{"scan", "stats", "list", "reset"} {
		if err := CheckConflictsAction(a, ConflictsArgs{}); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
	}
	if err := CheckConflictsAction("revert", ConflictsArgs{}); err == nil || !strings.Contains(err.Error(), "scan|stats|list|reset") {
		t.Fatalf("revert: %v", err)
	}
	if err := CheckConflictsAction("stats", ConflictsArgs{Write: true}); err == nil {
		t.Fatal("-write on stats accepted")
	}
	if err := CheckConflictsAction("scan", ConflictsArgs{MinCosine: 1.5}); err == nil {
		t.Fatal("cosine 1.5 accepted")
	}
	in := ConflictsArgs{Details: true, MinCosine: 0.75}.toolArgs("scan")
	b, _ := json.Marshal(in)
	if string(b) != `{"action":"scan","details":true,"min_cosine":0.75,"write":false}` {
		t.Fatalf("scan args %s", b)
	}
	b, _ = json.Marshal(ConflictsArgs{}.toolArgs("scan"))
	if string(b) != `{"action":"scan","details":false,"write":false}` {
		t.Fatalf("scan args without cosine %s", b)
	}
	b, _ = json.Marshal(ConflictsArgs{}.toolArgs("list"))
	if string(b) != `{"action":"list"}` {
		t.Fatalf("list args %s", b)
	}
}

// conflictFixture is a minimal database with decision 0148's tables as the
// design names them, clean for the question "Where do I live now?" (gold
// "Botafogo, Rio").
func conflictFixture(t *testing.T, mutate ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "chanda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE events (id TEXT PRIMARY KEY, kind TEXT NOT NULL, content TEXT NOT NULL)`,
		`CREATE TABLE retrievals (id TEXT PRIMARY KEY, query_text TEXT NOT NULL, candidates TEXT NOT NULL, delivered TEXT NOT NULL)`,
		`CREATE TABLE conflict_candidates (id TEXT PRIMARY KEY, older_id TEXT, newer_id TEXT, cosine REAL, differs TEXT, shared TEXT, status TEXT, verdict TEXT)`,
		`CREATE TABLE supersessions (id TEXT PRIMARY KEY, old_id TEXT, new_id TEXT, confidence REAL)`,
		`CREATE TABLE sleep_runs (id TEXT PRIMARY KEY, report TEXT, outputs TEXT)`,
		`CREATE TABLE audit_log (id INTEGER PRIMARY KEY, action TEXT, details TEXT)`,
		`CREATE VIRTUAL TABLE events_fts USING fts5(content)`,
		// Turns hold the gold and even the question: not checked.
		`INSERT INTO events VALUES ('evt_1', 'turn', 'I moved to Botafogo, Rio last month. Where do I live now? CORRECT')`,
		`INSERT INTO events VALUES ('evt_2', 'turn', 'I live in Vila Mariana')`,
		`INSERT INTO events_fts VALUES ('Where do I live now? Botafogo, Rio')`,
		`INSERT INTO conflict_candidates VALUES ('cc_1', 'evt_2', 'evt_1', 0.83, 'entities', '["live"]', 'judged',
			'{"verdict":"supersedes","current":"B","confidence":0.9}')`,
		`INSERT INTO supersessions VALUES ('sup_1', 'evt_2', 'evt_1', 0.9)`,
		`INSERT INTO sleep_runs VALUES ('slp_1', '- conflicts: 1 judged, 1 applied', '{"counts":{"conflicts_judged":1}}')`,
		`INSERT INTO audit_log (action, details) VALUES ('conflicts.scan', '{"candidates":1}')`,
	}
	for _, s := range append(stmts, mutate...) {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return db
}

func conflictQuestion() []StudyQuestion {
	return []StudyQuestion{{ID: "q", Question: "Where do I live now?", Gold: "Botafogo, Rio"}}
}

func leakConflictsOn(t *testing.T, db *sql.DB) *ConflictLeakReport {
	t.Helper()
	var path string
	if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	r, err := StudyLeakCheckConflicts(t.Context(), filepath.Dir(path), conflictQuestion())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLeakCheckConflicts(t *testing.T) {
	r := leakConflictsOn(t, conflictFixture(t))
	if !r.OK || r.ValuesScanned == 0 || r.GoldsChecked != 1 || r.Retrievals != 0 || len(r.Failures) != 0 {
		b, _ := json.MarshalIndent(r, "", " ")
		t.Fatalf("clean fixture failed:\n%s", b)
	}
	joined := strings.Join(r.Skipped, "|")
	if !strings.Contains(joined, "events_fts (virtual)") || !strings.Contains(joined, "kind turn") {
		t.Fatalf("skipped %v", r.Skipped)
	}

	cases := []struct {
		name, stmt, failure string
	}{
		{"question in the queue", `UPDATE conflict_candidates SET shared = '["where do I live now"]'`, "question text"},
		{"gold in a supersession", `INSERT INTO supersessions VALUES ('sup_2', 'botafogo rio', 'evt_1', 0.8)`, "gold answer"},
		{"gold in the sleep report", `UPDATE sleep_runs SET report = 'kept: Botafogo,  Rio.'`, "gold answer"},
		{"gold in audit details", `UPDATE audit_log SET details = '{"note":"BOTAFOGO RIO"}'`, "gold answer"},
		{"judge output", `UPDATE sleep_runs SET outputs = '{"x":"WRONG"}'`, "judge output"},
		{"a retrieval", `INSERT INTO retrievals VALUES ('ret_1', 'what is new', '[]', '[]')`, "retrieval"},
		{"question as a retrieval", `INSERT INTO retrievals VALUES ('ret_1', 'Where do I live now?', '[]', '[]')`, "question text"},
		{"non-turn event", `INSERT INTO events VALUES ('evt_3', 'note', 'Botafogo Rio')`, "gold answer"},
	}
	for _, c := range cases {
		r := leakConflictsOn(t, conflictFixture(t, c.stmt))
		if r.OK || !strings.Contains(strings.Join(r.Failures, "; "), c.failure) {
			t.Errorf("%s: ok=%t failures %v", c.name, r.OK, r.Failures)
		}
		if c.failure == "gold answer" && (len(r.GoldInTurns) != 1 || r.GoldInTurns[0] != "Botafogo, Rio") {
			t.Errorf("%s: gold in turns %v", c.name, r.GoldInTurns)
		}
	}
	// Whole words: "Botafogo, Rios" is not the gold; a short gold is not checked.
	r = leakConflictsOn(t, conflictFixture(t, `UPDATE sleep_runs SET report = 'Botafogo Rios'`))
	if !r.OK {
		t.Fatalf("partial word matched: %v", r.Failures)
	}
	db := conflictFixture(t, `UPDATE sleep_runs SET report = 'yes'`)
	var path string
	_ = db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path)
	r, err := StudyLeakCheckConflicts(t.Context(), filepath.Dir(path), []StudyQuestion{{Question: "Is it?", Gold: "yes"}})
	if err != nil || !r.OK || r.GoldsTooShort != 1 || r.GoldsChecked != 0 {
		t.Fatalf("short gold: %v %+v", err, r)
	}
}

// The steps on a real Chanda database: the leak check passes on a fresh
// ingestion and stops once the question was asked; the conflicts step
// either answers or says the tool is missing (then the rest is skipped).
func TestStudyConflictsOnChandaDB(t *testing.T) {
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
	base := filepath.Join(t.TempDir(), "q1")
	m := &StudyMeta{Embedder: "hash", EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Dataset: tiny}
	if err := StudyIngestLME(ctx, o, base, q, m); err != nil {
		t.Fatal(err)
	}
	qs := []StudyQuestion{LMEStudyQuestion(q)}
	r, err := StudyLeakCheckConflicts(ctx, base, qs)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.ValuesScanned == 0 || len(r.Scanned) < 5 || r.GoldsChecked != 1 {
		b, _ := json.MarshalIndent(r, "", " ")
		t.Fatalf("fresh ingestion failed the check:\n%s", b)
	}

	_, cerr := StudyConflicts(ctx, o, base, "stats", ConflictsArgs{})
	haveTool := cerr == nil
	if cerr != nil && !errors.Is(cerr, ErrNoConflictsTool) {
		t.Fatalf("conflicts stats: %v", cerr)
	}
	if _, err := StudyConflicts(ctx, o, base, "bogus", ConflictsArgs{}); err == nil {
		t.Fatal("unknown action accepted")
	}

	asked := filepath.Join(t.TempDir(), "asked")
	if err := StudyCopy(base, asked); err != nil {
		t.Fatal(err)
	}
	lines, err := StudyAsk(ctx, o, asked, "D", qs)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines[0].DroppedSuperseded) != 0 {
		t.Fatalf("superseded drops without conflicts: %v", lines[0].DroppedSuperseded)
	}
	r, err = StudyLeakCheckConflicts(ctx, asked, qs)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || r.Retrievals != 1 || len(r.QuestionHits) == 0 {
		t.Fatalf("asked copy not caught: ok=%t retrievals=%d question hits %d", r.OK, r.Retrievals, len(r.QuestionHits))
	}
	if !haveTool {
		t.Skipf("conflicts step not exercised: %v", cerr)
	}
	sc, err := StudyConflicts(ctx, withSets(o, t, "sleep.conflicts.enabled=true"), base, "scan", ConflictsArgs{Details: true, MinCosine: 0.75})
	if err != nil || len(sc.Output) == 0 || sc.Args["min_cosine"] != 0.75 {
		t.Fatalf("conflicts scan: %v %+v", err, sc)
	}
	r, err = StudyLeakCheckConflicts(ctx, base, qs)
	if err != nil || !r.OK {
		t.Fatalf("scan left a leak: %v %v", err, r.Failures)
	}
}

// A suite pair: two sessions, the older first, opaque session ids.
func TestStudyIngestPair(t *testing.T) {
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
	path := filepath.Join("..", "..", "conflicts", "suite", "pairs.json")
	p, sum, err := LoadSuitePair(path, "u07")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSuitePair(path, "x99"); err == nil {
		t.Fatal("unknown pair accepted")
	}
	dir := filepath.Join(t.TempDir(), "u07")
	m := &StudyMeta{Embedder: "hash", EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Dataset: path, DatasetSHA256: sum}
	if err := StudyIngestPair(ctx, o, dir, p, m); err != nil {
		t.Fatal(err)
	}
	mm, err := ReadStudyMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mm.Source != SourceSuite || mm.QuestionID != "u07" || len(mm.Turns) != 4 {
		t.Fatalf("meta %+v", mm)
	}
	ev := mm.KeyEvents()
	if ev["s0t0"] == "" || ev["s1t0"] == "" || ev["s0t0"] == ev["s1t0"] {
		t.Fatalf("user turns s0t0 %q s1t0 %q", ev["s0t0"], ev["s1t0"])
	}
	for _, tr := range mm.Turns {
		if !strings.HasPrefix(tr.SessionID, "u07-s") || strings.Contains(tr.SessionID, "old") || strings.Contains(tr.SessionID, "new") {
			t.Fatalf("session id %q", tr.SessionID)
		}
	}
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ts0, ts1 string
	if err := db.QueryRow(`SELECT COALESCE(valid_from, recorded_at) FROM events WHERE id = ?`, ev["s0t0"]).Scan(&ts0); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT COALESCE(valid_from, recorded_at) FROM events WHERE id = ?`, ev["s1t0"]).Scan(&ts1)
	if !strings.HasPrefix(ts0, "2023-01-05") || !strings.HasPrefix(ts1, "2023-07-01") {
		t.Fatalf("turn times %q %q", ts0, ts1)
	}
}
