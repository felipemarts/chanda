package main

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

// useAPI swaps newAPI for the duration of the test.
func useAPI(t *testing.T, api chanda.API) {
	t.Helper()
	old := newAPI
	newAPI = func(*config.Config) (chanda.API, func() error, error) { return api, func() error { return nil }, nil }
	t.Cleanup(func() { newAPI = old })
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

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

func TestCommandsReportUnwiredCore(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "none.yaml") // missing file: defaults apply
	for _, args := range [][]string{
		{"ingest", "--config", cfg, "x"},
		{"sleep", "--config", cfg},
		{"eval", "--config", cfg},
		{"forget", "--config", cfg, "--kind", "record", "--value", "evt_1"},
	} {
		code, out, _ := runCLI(args...)
		env := decodeEnv(t, out)
		if code != 1 || env.OK || env.Error.Code != chanda.CodeUnavailable {
			t.Errorf("%v: code %d env %+v", args, code, env)
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
			useAPI(t, api)
			code, out, errs := runCLI(tc.args...)
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
	api := &recAPI{}
	useAPI(t, api)
	_, out, _ := runCLI("ingest", "--config", cfgPath, "a.md")
	if env := decodeEnv(t, out); env.AuditID != 5 {
		t.Fatalf("audit_id not surfaced: %+v", env)
	}
}

func TestEvalHook(t *testing.T) {
	old := runEval
	t.Cleanup(func() { runEval = old })
	var got []string
	runEval = func(_ context.Context, _ *config.Config, args []string, stdout, _ io.Writer) error {
		got = args
		_, err := io.WriteString(stdout, "report\n")
		return err
	}
	code, out, _ := runCLI("eval", "--config", "none.yaml", "--suite", "B1")
	if code != 0 || out != "report\n" || !slices.Equal(got, []string{"--suite", "B1"}) {
		t.Fatalf("code %d out %q args %v", code, out, got)
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
	cmd := exec.Command(gobin, "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// Acceptance (BASE.md phase 2): an MCP client lists and calls the tools of
// the real binary over stdio. The core is not wired, so tools answer
// unavailable in the envelope; stdout must carry nothing but MCP frames.
func TestServeStdioBinary(t *testing.T) {
	bin := buildBinary(t)
	cfgPath := filepath.Join(t.TempDir(), "chanda.yaml")
	if err := os.WriteFile(cfgPath, []byte("mcp:\n  profiles: [read, standard]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		extra []string
		want  int
	}{
		{"config profiles", nil, 13},
		{"flag override", []string{"--profiles", "admin"}, 5},
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
			name, args := chanda.ToolSearch, map[string]any{"query": "q"}
			if tc.want == 5 {
				name, args = chanda.ToolVerify, map[string]any{}
			}
			res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(res.StructuredContent)
			env := decodeEnv(t, string(b))
			if !res.IsError || env.OK || env.Error.Code != chanda.CodeUnavailable {
				t.Fatalf("%s: %s", name, b)
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
		args := []string{"serve", "--config", filepath.Join(t.TempDir(), "none.yaml"), "--http", "127.0.0.1:0", "--profiles", "read"}
		done <- run(srvCtx, args, io.Discard, &stderr)
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
