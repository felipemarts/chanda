package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// recAPI records the last call; everything it does not override is Unwired.
type recAPI struct {
	chanda.Unwired
	in    any
	actor chanda.Actor
}

func (r *recAPI) Ingest(ctx context.Context, in chanda.IngestInput) (chanda.IngestOutput, error) {
	r.in, r.actor = in, actorOf(ctx)
	return chanda.IngestOutput{Meta: chanda.Meta{AuditID: 5}, Documents: []chanda.DocumentResult{{URI: in.Path, Version: 1, Action: "created"}}}, nil
}

func (r *recAPI) SleepRun(ctx context.Context, in chanda.SleepRunInput) (chanda.SleepRunOutput, error) {
	r.in, r.actor = in, actorOf(ctx)
	return chanda.SleepRunOutput{RunID: "run_1", Status: "completed", Phases: in.Phases}, nil
}

func (r *recAPI) Forget(ctx context.Context, in chanda.ForgetInput) (chanda.ForgetOutput, error) {
	r.in, r.actor = in, actorOf(ctx)
	return chanda.ForgetOutput{DryRun: in.DryRun}, nil
}

func actorOf(ctx context.Context) chanda.Actor { a, _ := chanda.ActorFrom(ctx); return a }

// withAPI returns the default CLI over api.
func withAPI(api chanda.API) CLI {
	cl := Default()
	cl.NewAPI = func(*config.Config) (chanda.API, func() error, error) { return api, func() error { return nil }, nil }
	return cl
}

func runWith(cl CLI, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := cl.Run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func runCLI(args ...string) (int, string, string) { return runWith(Default(), args...) }

func decodeEnv(t *testing.T, s string) chanda.Envelope {
	t.Helper()
	var env chanda.Envelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		t.Fatalf("stdout is not an envelope: %v\n%s", err, s)
	}
	return env
}

func TestVersionAndUsage(t *testing.T) {
	for _, a := range []string{"version", "--version", "-v"} {
		if code, out, _ := runCLI(a); code != 0 || out != "chanda dev\n" {
			t.Errorf("%s: %d %q", a, code, out)
		}
	}
	if code, _, errs := runCLI(); code != 2 || !strings.Contains(errs, "usage") {
		t.Errorf("no args: %d %q", code, errs)
	}
	if code, _, _ := runCLI("frobnicate"); code != 2 {
		t.Errorf("unknown command: %d", code)
	}
	if code, _, _ := runCLI("ingest"); code != 2 {
		t.Errorf("ingest without path: %d", code)
	}
	if code, _, _ := runCLI("sleep", "--phases", "3"); code != 2 {
		t.Errorf("bad phase: %d", code)
	}
	if code, _, _ := runCLI("forget"); code != 2 {
		t.Errorf("forget without kind: %d", code)
	}
}

// tempConfig writes a chanda.yaml whose database lives in a temp dir, plus
// extra YAML, and returns its path.
func tempConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "chanda.yaml")
	body := "db_path: " + filepath.Join(dir, "chanda.db") + "\n" + extra
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The commands run on the real core.
func TestCommandsUseTheCore(t *testing.T) {
	// No write-burst pause, so sleep may run right after the ingest.
	cfg := tempConfig(t, "sleep:\n  burst_seconds: 0\n")
	doc := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(doc, []byte("# Notes\n\nThe release train leaves on Fridays."), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		code chanda.Code // "" = ok
	}{
		{[]string{"ingest", "--config", cfg, doc}, ""},
		{[]string{"ingest", "--config", cfg, filepath.Join(t.TempDir(), "missing")}, chanda.CodeNotFound},
		{[]string{"sleep", "--config", cfg, "--phases", "1"}, ""},
		{[]string{"forget", "--config", cfg, "--kind", "search", "--query", "release train", "--dry-run"}, ""},
		{[]string{"forget", "--config", cfg, "--kind", "record", "--value", "evt_1"}, chanda.CodeNotFound},
	} {
		code, out, _ := runCLI(tc.args...)
		env := decodeEnv(t, out)
		switch {
		case tc.code == "" && (code != 0 || !env.OK):
			t.Errorf("%v: code %d env %+v", tc.args, code, env)
		case tc.code != "" && (code != 1 || env.OK || env.Error.Code != tc.code):
			t.Errorf("%v: code %d env %+v, want %s", tc.args, code, env, tc.code)
		}
	}
}

func TestCommandsCallTheAPI(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "chanda.yaml")
	if err := os.WriteFile(cfgPath, []byte("namespace: team\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want any
	}{
		{[]string{"ingest", "--config", cfgPath, "--kind", "document", "docs/"},
			chanda.IngestInput{Namespace: "team", Path: "docs/", Source: chanda.SourceRef{Kind: chanda.SourceDocument}}},
		{[]string{"sleep", "--config", cfgPath, "--namespace", "other", "--phases", "1,2", "--budget-tokens", "100"},
			chanda.SleepRunInput{Namespace: "other", Phases: []int{1, 2}, Budget: chanda.SleepBudget{Tokens: 100}}},
		{[]string{"forget", "--config", cfgPath, "--kind", "date_range", "--from", "2026-01-01", "--to", "2026-02-01", "--dry-run"},
			chanda.ForgetInput{Namespace: "team", Scope: chanda.ForgetScope{Kind: "date_range", From: "2026-01-01", To: "2026-02-01"}, DryRun: true}},
	}
	for _, tc := range tests {
		t.Run(tc.args[0], func(t *testing.T) {
			api := &recAPI{}
			code, out, errs := runWith(withAPI(api), tc.args...)
			env := decodeEnv(t, out)
			if code != 0 || !env.OK {
				t.Fatalf("code %d env %+v stderr %s", code, env, errs)
			}
			if !reflect.DeepEqual(api.in, tc.want) {
				t.Fatalf("API got %+v\nwant %+v", api.in, tc.want)
			}
			if api.actor != (chanda.Actor{Name: "cli", Human: true, MaxZone: chanda.ZoneUser}) {
				t.Fatalf("actor %+v", api.actor)
			}
		})
	}
	_, out, _ := runWith(withAPI(&recAPI{}), "ingest", "--config", cfgPath, "a.md")
	if env := decodeEnv(t, out); env.AuditID != 5 {
		t.Fatalf("audit_id not surfaced: %+v", env)
	}
}

func TestEvalHook(t *testing.T) {
	cl := Default()
	var got []string
	cl.Eval = func(_ context.Context, args []string, stdout, _ io.Writer) int {
		got = args
		_, _ = io.WriteString(stdout, "report\n")
		return 3
	}
	code, out, _ := runWith(cl, "eval", "--config", "none.yaml", "--systems", "B1")
	if code != 3 || out != "report\n" || !slices.Equal(got, []string{"--config", "none.yaml", "--systems", "B1"}) {
		t.Fatalf("code %d out %q args %v", code, out, got)
	}
	cl.Eval = nil
	code, out, _ = runWith(cl, "eval")
	if env := decodeEnv(t, out); code != 1 || env.Error.Code != chanda.CodeUnavailable {
		t.Fatalf("unwired eval: code %d %+v", code, env)
	}
	// The default hook is the harness: a usage error exits 2.
	if code, _, _ := runCLI("eval", "-systems", "Z9", "-dataset", t.TempDir()); code == 0 {
		t.Fatalf("eval with an unknown system and an empty dataset succeeded")
	}
}

func TestVersionIsStamped(t *testing.T) {
	cl := Default()
	cl.Version = "v1.2.3-4-gabc"
	if code, out, _ := runWith(cl, "version"); code != 0 || out != "chanda v1.2.3-4-gabc\n" {
		t.Fatalf("%d %q", code, out)
	}
}

// buildBinary compiles the chanda command into a temp dir.
func buildBinary(t *testing.T) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not in PATH")
	}
	bin := filepath.Join(t.TempDir(), "chanda")
	cmd := exec.Command(gobin, "build", "-o", bin, "../../cmd/chanda")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// Acceptance (BASE.md phase 2): an MCP client lists and calls the tools of
// the real binary over stdio, on the real core; stdout must carry nothing
// but MCP frames.
func TestServeStdioBinary(t *testing.T) {
	bin := buildBinary(t)
	cfgPath := tempConfig(t, "mcp:\n  profiles: [read, standard]\n")
	tests := []struct {
		name  string
		extra []string
		want  int
	}{
		{"config profiles", nil, 13},
		{"flag override", []string{"--profiles", "admin"}, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var stderr bytes.Buffer
			cmd := exec.Command(bin, append([]string{"serve", "--config", cfgPath}, tc.extra...)...)
			cmd.Stderr = &stderr
			cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
				Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
			if err != nil {
				t.Fatalf("connect: %v\nstderr: %s", err, stderr.String())
			}
			defer cs.Close()
			lt, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(lt.Tools) != tc.want {
				t.Fatalf("%d tools, want %d", len(lt.Tools), tc.want)
			}
			call := func(name string, args map[string]any) chanda.Envelope {
				t.Helper()
				res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(res.StructuredContent)
				env := decodeEnv(t, string(b))
				if res.IsError || !env.OK {
					t.Fatalf("%s: %s", name, b)
				}
				return env
			}
			if tc.want == 6 { // the admin profile
				if env := call(chanda.ToolVerify, map[string]any{}); env.Data.(map[string]any)["ok"] != true {
					t.Fatalf("verify: %+v", env)
				}
				return
			}
			call(chanda.ToolRemember, map[string]any{"content": "The release train leaves on Fridays."})
			env := call(chanda.ToolSearch, map[string]any{"query": "release train"})
			if items := env.Data.(map[string]any)["items"].([]any); len(items) != 1 {
				t.Fatalf("search after remember: %+v", env.Data)
			}
		})
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var urlRe = regexp.MustCompile(`url=(http://\S+)`)

// serve --http starts Streamable HTTP on loopback and stops when ctx ends.
func TestServeHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srvCtx, stop := context.WithCancel(ctx)
	defer stop()
	var stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		args := []string{"serve", "--config", tempConfig(t, ""), "--http", "127.0.0.1:0", "--profiles", "read"}
		done <- Run(srvCtx, args, io.Discard, &stderr)
	}()
	var url string
	for url == "" {
		if m := urlRe.FindStringSubmatch(stderr.String()); m != nil {
			url = m[1]
			break
		}
		select {
		case code := <-done:
			t.Fatalf("serve exited %d: %s", code, stderr.String())
		case <-ctx.Done():
			t.Fatalf("no url logged: %s", stderr.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lt, err := cs.ListTools(ctx, nil)
	if err != nil || len(lt.Tools) != 6 {
		t.Fatalf("list: %v %+v", err, lt)
	}
	cs.Close()
	stop()
	if code := <-done; code != 0 {
		t.Fatalf("serve exit %d: %s", code, stderr.String())
	}
}

// The gate set and learned ranking are reviewed by a person at the CLI
// (decision 0092): add, list, approve and retire; switch ranking.
func TestGateAndRankingCommands(t *testing.T) {
	cfg := tempConfig(t, "")
	run := func(args ...string) (int, chanda.Envelope) {
		full := append(append(append([]string{}, args[:2]...), "--config", cfg), args[2:]...)
		code, out, errs := runCLI(full...)
		if code == 2 {
			t.Fatalf("%v: usage error: %s", args, errs)
		}
		return code, decodeEnv(t, out)
	}
	code, env := run("gate", "add", "--question", "Qual o prazo de reembolso?", "--answer", "30 dias", "--as-of", "2026-03-01")
	if code != 0 || !env.OK {
		t.Fatalf("add: %d %+v", code, env)
	}
	id := env.Data.(map[string]any)["question"].(map[string]any)["id"].(string)
	code, env = run("gate", "list", "--status", "approved")
	if qs := env.Data.(map[string]any)["questions"].([]any); code != 0 || len(qs) != 1 {
		t.Fatalf("list: %d %+v", code, env)
	}
	if code, env = run("gate", "retire", id); code != 0 || env.Data.(map[string]any)["to"] != "retired" {
		t.Fatalf("retire: %d %+v", code, env)
	}
	if code, env = run("gate", "retire", id); code != 1 || env.Error.Code != chanda.CodeConflict {
		t.Fatalf("retire twice: %d %+v", code, env)
	}
	if code, env = run("ranking", "off"); code != 0 || env.Data.(map[string]any)["enabled"] != false {
		t.Fatalf("ranking off: %d %+v", code, env)
	}
	// Learned keys (decision 0145): the switch and the counts.
	if code, env = run("keys", "disable"); code != 0 || env.Data.(map[string]any)["enabled"] != false || env.AuditID == 0 {
		t.Fatalf("keys disable: %d %+v", code, env)
	}
	if code, env = run("keys", "stats"); code != 0 || env.Data.(map[string]any)["enabled"] != false || env.Data.(map[string]any)["keys"] != 0.0 {
		t.Fatalf("keys stats: %d %+v", code, env)
	}
	for _, args := range [][]string{{"gate"}, {"gate", "frob"}, {"gate", "add"}, {"gate", "approve"}, {"ranking"}, {"ranking", "maybe"},
		{"keys"}, {"keys", "frob"}} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Errorf("%v: code %d, want usage error", args, code)
		}
	}
}
