package scenarios

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite" // independent reader of the database copy

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// chandaBin is the chanda binary built from ./cmd/chanda by TestMain, or,
// in e5 mode (-lme), the lme harness binary, which runs the chanda CLI as
// "lme chanda ..." with the lme-cached embedding provider (decision 0139).
var chandaBin string

// e5 mode: chandaPrefix goes before every chanda command ("chanda" for
// lme) and chandaEnv is added to its environment (the lme-cached provider
// reads the embed server's URL from it). Both are empty in the default run.
var (
	chandaPrefix []string
	chandaEnv    []string
)

// e5ModelID and e5Dim are the embed settings of the e5 mode: the model the
// embed server must serve (lme-cached refuses another one).
const (
	e5ModelID = "e5-small@hugot0.8.6+simd"
	e5Dim     = 384
)

// chandaCommand returns the command that runs one chanda command.
func chandaCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(chandaBin, append(append([]string{}, chandaPrefix...), args...)...)
	if len(chandaEnv) > 0 {
		cmd.Env = append(os.Environ(), chandaEnv...)
	}
	return cmd
}

// buildChanda compiles ./cmd/chanda into dir and returns the binary path.
func buildChanda(dir string) (string, error) {
	bin := filepath.Join(dir, "chanda")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/chanda")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/chanda: %v\n%s", err, out)
	}
	return bin, nil
}

// ---------------------------------------------------------------------------
// Scenario state, steps and checks.

// Check is one expectation of a scenario and what was measured.
type Check struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Measured string `json:"measured"`
	Pass     bool   `json:"pass"`
}

// Result is the outcome of one scenario.
type Result struct {
	ID          string   `json:"id"`
	Area        string   `json:"area"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	Checks      []Check  `json:"checks"`
	Status      string   `json:"status"` // PASS or FAIL
}

// Scenario is one scripted case.
type Scenario struct {
	ID          string
	Area        string
	Description string
	Run         func(s *S)
}

// abort stops a scenario on an unexpected error (protocol failure, a call
// that had to succeed); the runner records it as a failed check.
type abort struct{ err error }

// S is the per-scenario environment: a temporary directory with the
// configuration, the database and a docs folder, and at most one running
// chanda serve process.
type S struct {
	dir, docs, db, cfg string
	steps              []string
	checks             []Check

	cs     *sdk.ClientSession
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	nInsp  int
}

func newS(dir string) (*S, error) {
	s := &S{dir: dir, docs: filepath.Join(dir, "docs"), db: filepath.Join(dir, "chanda.db"), cfg: filepath.Join(dir, "chanda.yaml")}
	if err := os.MkdirAll(s.docs, 0o755); err != nil {
		return nil, err
	}
	return s, s.writeConfig("internal")
}

// writeConfig writes chanda.yaml. maxZone is the channel's zone ceiling
// (mcp.max_zone). The sleep scheduler is off so nothing runs on its own.
func (s *S) writeConfig(maxZone string) error {
	embed := "embed:\n  poll_interval_ms: 50\n"
	if len(chandaEnv) > 0 {
		embed += fmt.Sprintf("  provider: lme-cached\n  model_id: %q\n  dim: %d\n", e5ModelID, e5Dim)
	}
	cfg := fmt.Sprintf(`db_path: %q
namespace: scen
id_seed: scenarios
write:
  ingest_roots: [%q]
  extractors:
    - {name: header, kind: header}
    - {name: tables, kind: kv_table}
%ssleep:
  scheduler: false
mcp:
  actor: scenario-agent
  max_zone: %s
`, s.db, s.docs, embed, maxZone)
	return os.WriteFile(s.cfg, []byte(cfg), 0o644)
}

func (s *S) fail(err error)           { panic(abort{err}) }
func (s *S) failf(f string, a ...any) { s.fail(fmt.Errorf(f, a...)) }

// step records a step description.
func (s *S) step(f string, a ...any) { s.steps = append(s.steps, fmt.Sprintf(f, a...)) }

// check records a check.
func (s *S) check(name, expected, measured string, pass bool) {
	s.checks = append(s.checks, Check{Name: name, Expected: expected, Measured: measured, Pass: pass})
}

func (s *S) checkInt(name string, want, got int) {
	s.check(name, fmt.Sprint(want), fmt.Sprint(got), want == got)
}

func (s *S) checkStr(name, want, got string) {
	s.check(name, want, got, want == got)
}

func (s *S) checkBool(name string, want, got bool) {
	s.check(name, fmt.Sprint(want), fmt.Sprint(got), want == got)
}

// ---------------------------------------------------------------------------
// The server.

// start launches chanda serve over stdio with the given tool profiles.
func (s *S) start(profiles string) {
	if s.cs != nil {
		s.failf("server already running")
	}
	s.stderr = &bytes.Buffer{}
	cmd := chandaCommand("serve", "--config", s.cfg, "--profiles", profiles)
	cmd.Stderr = s.stderr
	cmd.Dir = s.dir
	tr := &sdk.CommandTransport{Command: cmd, TerminateDuration: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "scenarios", Version: "v1"}, nil).Connect(ctx, tr, nil)
	if err != nil {
		s.failf("start chanda serve: %v\n%s", err, s.stderr)
	}
	s.cs, s.cmd = cs, cmd
}

// stop closes stdin and waits for the server to exit (it closes the
// database, folding and removing the WAL).
func (s *S) stop() {
	if s.cs == nil {
		return
	}
	err := s.cs.Close()
	cmd := s.cmd
	s.cs, s.cmd = nil, nil
	if err != nil && !errors.Is(err, io.EOF) {
		s.failf("stop chanda serve: %v\n%s", err, s.stderr)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() || cmd.ProcessState.ExitCode() != 0 {
		s.failf("chanda serve did not exit cleanly: %v\n%s", cmd.ProcessState, s.stderr)
	}
}

// call invokes a tool and returns its envelope with data left raw.
func (s *S) call(tool string, args map[string]any) (chanda.Envelope, json.RawMessage) {
	if s.cs == nil {
		s.failf("%s: no server running", tool)
	}
	if args == nil {
		args = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := s.cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		s.failf("%s: protocol error: %v", tool, err)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		s.failf("%s: %v", tool, err)
	}
	var env struct {
		chanda.Envelope
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		s.failf("%s: decoding envelope: %v (%s)", tool, err, b)
	}
	return env.Envelope, env.Data
}

// ok calls a tool that must succeed and decodes its data into out.
func (s *S) ok(tool string, args map[string]any, out any) chanda.Envelope {
	env, data := s.call(tool, args)
	if !env.OK {
		s.failf("%s(%v) failed: %+v", tool, args, env.Error)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			s.failf("%s: decoding data: %v (%s)", tool, err, data)
		}
	}
	return env
}

func (s *S) flush() {
	var f chanda.FlushOutput
	s.ok(chanda.ToolFlush, nil, &f)
	if !f.Complete || f.Pending != 0 {
		s.failf("flush incomplete: %+v", f)
	}
}

func (s *S) search(query string, extra map[string]any) chanda.SearchOutput {
	args := map[string]any{"query": query}
	for k, v := range extra {
		args[k] = v
	}
	var out chanda.SearchOutput
	s.ok(chanda.ToolSearch, args, &out)
	return out
}

func (s *S) context(query string, extra map[string]any) chanda.ContextOutput {
	args := map[string]any{"query": query}
	for k, v := range extra {
		args[k] = v
	}
	var out chanda.ContextOutput
	s.ok(chanda.ToolContext, args, &out)
	return out
}

func (s *S) remember(content string, extra map[string]any) chanda.RememberOutput {
	args := map[string]any{"content": content}
	for k, v := range extra {
		args[k] = v
	}
	var out chanda.RememberOutput
	s.ok(chanda.ToolRemember, args, &out)
	return out
}

func (s *S) assertFact(subject, predicate, value, validFrom string, source map[string]any) chanda.AssertFactOutput {
	args := map[string]any{"subject": subject, "predicate": predicate, "value": value}
	if validFrom != "" {
		args["valid_from"] = validFrom
	}
	if source != nil {
		args["source"] = source
	}
	var out chanda.AssertFactOutput
	s.ok(chanda.ToolAssertFact, args, &out)
	return out
}

// timeline returns the timeline, or ok=false with the error code when the
// call fails (e.g. not_found after a forget).
func (s *S) timeline(subject, predicate, asOf string) (chanda.TimelineOutput, string) {
	args := map[string]any{"subject": subject}
	if predicate != "" {
		args["predicate"] = predicate
	}
	if asOf != "" {
		args["as_of"] = asOf
	}
	env, data := s.call(chanda.ToolTimeline, args)
	var out chanda.TimelineOutput
	if !env.OK {
		return out, string(env.Error.Code)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		s.failf("timeline: %v", err)
	}
	return out, "ok"
}

func (s *S) stats() chanda.StatsOutput {
	var out chanda.StatsOutput
	s.ok(chanda.ToolStats, nil, &out)
	return out
}

// writeDoc writes a file into the docs folder (the only MCP ingest root).
func (s *S) writeDoc(name, body string) string {
	p := filepath.Join(s.docs, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		s.fail(err)
	}
	return p
}

// cli runs a one-shot chanda command (a person at a terminal: human actor,
// user zone ceiling). The server must not be running.
func (s *S) cli(args ...string) (chanda.Envelope, json.RawMessage) {
	if s.cs != nil {
		s.failf("cli while the server runs")
	}
	full := append([]string{args[0], "--config", s.cfg}, args[1:]...)
	cmd := chandaCommand(full...)
	cmd.Dir = s.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run() // exit 1 is a failed envelope, decoded below
	var env struct {
		chanda.Envelope
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		s.failf("chanda %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.Bytes(), stderr.Bytes())
	}
	return env.Envelope, env.Data
}

// ---------------------------------------------------------------------------
// Independent inspection of the files.

// fileSet lists the database file and its WAL and SHM companions.
func (s *S) fileSet() []string { return []string{s.db, s.db + "-wal", s.db + "-shm"} }

// scan reads the database, WAL and SHM files as raw bytes and counts the
// case-insensitive occurrences of each marker. A missing file counts zero
// and is listed in absent.
func (s *S) scan(markers []string) (hits map[string]int, absent []string) {
	hits = map[string]int{}
	for _, f := range s.fileSet() {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			absent = append(absent, filepath.Base(f))
			continue
		}
		if err != nil {
			s.fail(err)
		}
		low := bytes.ToLower(b)
		for _, m := range markers {
			if n := bytes.Count(low, []byte(strings.ToLower(m))); n > 0 {
				hits[filepath.Base(f)+":"+m] += n
			}
		}
	}
	return hits, absent
}

// scanTotal is the total number of marker occurrences in the files.
func (s *S) scanTotal(markers []string) int {
	hits, _ := s.scan(markers)
	n := 0
	for _, v := range hits {
		n += v
	}
	return n
}

// fmtHits renders scan hits in a stable order ("none" when empty).
func fmtHits(h map[string]int) string {
	if len(h) == 0 {
		return "none"
	}
	var ks []string
	for k, v := range h {
		ks = append(ks, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

// inspect copies the database files (the server must be stopped) into a
// fresh directory and opens the copy with an independent SQLite reader, so
// the original files are never touched.
func (s *S) inspect() *sql.DB {
	if s.cs != nil {
		s.failf("inspect while the server runs")
	}
	s.nInsp++
	dir := filepath.Join(s.dir, fmt.Sprintf("inspect-%d", s.nInsp))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(err)
	}
	for _, f := range s.fileSet() {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			s.fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o644); err != nil {
			s.fail(err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, filepath.Base(s.db)))
	if err != nil {
		s.fail(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

// countIn counts rows of query (with one "(%s)" placeholder) over ids.
func (s *S) countIn(db *sql.DB, query string, ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var n int
	if err := db.QueryRow(strings.Replace(query, "(%s)", "("+ph+")", 1), args...).Scan(&n); err != nil {
		s.failf("%s: %v", query, err)
	}
	return n
}

// tablesHolding scans every column of every row of every table (FTS shadow
// tables included) of the copy and returns "table:marker" for each table
// whose rows hold a marker, case-insensitively.
func (s *S) tablesHolding(db *sql.DB, markers []string) []string {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		s.fail(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			s.fail(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	found := map[string]bool{}
	for _, t := range tables {
		r, err := db.Query(`SELECT * FROM "` + t + `"`)
		if err != nil {
			s.failf("scan %s: %v", t, err)
		}
		cols, _ := r.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for r.Next() {
			if err := r.Scan(ptrs...); err != nil {
				s.failf("scan %s: %v", t, err)
			}
			for _, v := range vals {
				var b []byte
				switch x := v.(type) {
				case string:
					b = []byte(x)
				case []byte:
					b = x
				default:
					continue
				}
				low := bytes.ToLower(b)
				for _, m := range markers {
					if bytes.Contains(low, []byte(strings.ToLower(m))) {
						found[t+":"+m] = true
					}
				}
			}
		}
		r.Close()
	}
	var out []string
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// vocabHits returns the FTS5 vocabulary entries (index terms, independent
// of the stored text) equal to a marker, as "table:term".
func (s *S) vocabHits(db *sql.DB, markers []string) []string {
	var out []string
	for _, t := range []string{"events_fts", "facts_fts", "pages_fts"} {
		v := "temp.vocab_" + t
		if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS ` + v + ` USING fts5vocab(main, '` + t + `', 'row')`); err != nil {
			s.failf("fts5vocab %s: %v", t, err)
		}
		for _, m := range markers {
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM `+v+` WHERE term = ?`, strings.ToLower(m)).Scan(&n); err != nil {
				s.failf("vocab %s: %v", t, err)
			}
			if n > 0 {
				out = append(out, t+":"+m)
			}
		}
	}
	return out
}

func listOrNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
}

// ---------------------------------------------------------------------------
// Item helpers.

// withText returns the items whose text holds sub (case-insensitive).
func withText(items []chanda.Item, sub string) []chanda.Item {
	var out []chanda.Item
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Text), strings.ToLower(sub)) {
			out = append(out, it)
		}
	}
	return out
}

func itemIDs(items []chanda.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// describe renders items without ids or scores (which vary between runs):
// type, zone, status and text.
func describe(items []chanda.Item) string {
	if len(items) == 0 {
		return "none"
	}
	var parts []string
	for _, it := range items {
		p := string(it.Type) + "/" + string(it.Zone)
		if it.Status != "" {
			p += "/" + it.Status
		}
		if len(it.Flags) > 0 {
			p += "/" + strings.Join(it.Flags, "+")
		}
		parts = append(parts, p+" "+strconvQuote(it.Text))
	}
	return strings.Join(parts, "; ")
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}
