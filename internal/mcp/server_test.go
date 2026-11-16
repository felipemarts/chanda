package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/pkg/chanda"
)

func testConfig(profiles ...string) *config.Config {
	cfg := config.Default()
	cfg.Namespace = "acme"
	cfg.MCP.Profiles = profiles
	cfg.MCP.Actor = "hermes"
	return cfg
}

// connect starts s over in-memory transports and returns a client session.
func connect(t *testing.T, s *Server) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	ss, err := s.SDK().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func newTestServer(t *testing.T, api chanda.API, cfg *config.Config) (*Server, *sdk.ClientSession) {
	t.Helper()
	s, err := New(api, cfg, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return s, connect(t, s)
}

func listNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

// callEnv calls a tool and decodes the envelope from structuredContent,
// checking that the text content carries the same JSON.
func callEnv(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (chanda.Envelope, *sdk.CallToolResult) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	sb, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var env chanda.Envelope
	if err := json.Unmarshal(sb, &env); err != nil {
		t.Fatalf("%s: structured content is not an envelope: %v (%s)", name, err, sb)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d content blocks", name, len(res.Content))
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T", name, res.Content[0])
	}
	var fromText, fromStruct any
	_ = json.Unmarshal([]byte(tc.Text), &fromText)
	_ = json.Unmarshal(sb, &fromStruct)
	if !reflect.DeepEqual(fromText, fromStruct) {
		t.Fatalf("%s: text content differs from structured content:\n%s\n%s", name, tc.Text, sb)
	}
	if res.IsError == env.OK {
		t.Fatalf("%s: isError=%v but ok=%v", name, res.IsError, env.OK)
	}
	return env, res
}

func toolSet(p ...Profile) []string {
	var out []string
	for _, s := range SpecsFor(p) {
		out = append(out, s.Name)
	}
	slices.Sort(out)
	return out
}

// BASE.md: "grouped into three profiles. The profile is chosen in the configuration".
func TestProfilesRegisterOnlyTheirTools(t *testing.T) {
	read := []string{"context", "explain", "get", "search", "stats", "timeline"}
	standard := []string{"assert_fact", "flag", "flush", "ingest", "log_session", "outcome", "remember"}
	admin := []string{"conflicts", "forget", "learned_keys", "promote", "ranking_reset", "sleep_run", "verify"}
	tests := []struct {
		profiles []string
		want     []string
	}{
		{[]string{"read"}, read},
		{[]string{"standard"}, standard},
		{[]string{"admin"}, admin},
		{[]string{"read", "standard"}, toolSet(ProfileRead, ProfileStandard)},
		{[]string{"admin", "read", "standard", "read"}, toolSet(ProfileRead, ProfileStandard, ProfileAdmin)},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.profiles, "+"), func(t *testing.T) {
			s, cs := newTestServer(t, &fakeAPI{}, testConfig(tc.profiles...))
			got := listNames(t, cs)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("tools %v, want %v", got, tc.want)
			}
			if len(s.Tools()) != len(tc.want) {
				t.Fatalf("Tools() = %v", s.Tools())
			}
		})
	}
	if n := len(toolSet(ProfileRead, ProfileStandard, ProfileAdmin)); n != 20 {
		t.Fatalf("all profiles give %d tools, want 20", n)
	}
}

// A tool outside the configured profiles cannot be called at all.
func TestToolOutsideProfileIsUnreachable(t *testing.T) {
	api := &fakeAPI{}
	_, cs := newTestServer(t, api, testConfig("read"))
	for _, name := range []string{"remember", "forget", "promote", "ranking_reset"} {
		_, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{"id": "x"}})
		if err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("%s: err = %v, want unknown tool", name, err)
		}
	}
	if api.count() != 0 {
		t.Fatalf("API reached %d times", api.count())
	}
}

func TestParseProfilesRejectsUnknownOrEmpty(t *testing.T) {
	for _, p := range [][]string{nil, {}, {"root"}, {"read", "Admin"}} {
		if _, err := New(&fakeAPI{}, testConfig(p...), Options{}); chanda.CodeOf(err) != chanda.CodeInvalidArgument {
			t.Errorf("%v: err = %v", p, err)
		}
	}
}

// minimalArgs are valid arguments for each tool.
var minimalArgs = map[string]map[string]any{
	"context":       {"query": "who owns billing?"},
	"search":        {"query": "billing", "filters": map[string]any{"types": []any{"fact"}, "zones": []any{"internal"}}, "limit": 5},
	"get":           {"id": "fct_01"},
	"timeline":      {"subject": "Billing", "predicate": "owner"},
	"explain":       {"retrieval_id": "ret_02", "record_id": "fct_01"},
	"stats":         {},
	"remember":      {"content": "We chose SQLite.", "source": map[string]any{"kind": "session", "uri": "s1"}},
	"assert_fact":   {"subject": "Billing", "predicate": "owner", "value": "Ana"},
	"ingest":        {"path": "/docs"},
	"log_session":   {"session_id": "s1", "turns": []any{map[string]any{"role": "user", "content": "hi"}}},
	"outcome":       {"retrieval_id": "ret_01", "value": 1, "used_ids": []any{"fct_01"}},
	"flag":          {"id": "fct_01", "reason": "outdated"},
	"flush":         {},
	"forget":        {"scope": map[string]any{"kind": "record", "value": "evt_01"}, "dry_run": true},
	"promote":       {"id": "evt_01", "zone": "user"},
	"sleep_run":     {"phases": []any{1}},
	"verify":        {},
	"ranking_reset": {},
	"learned_keys":  {"action": "stats"},
	"conflicts":     {"action": "stats"},
}

// auditedTools are the tools whose fake output carries an AuditID.
var auditedTools = []string{"remember", "assert_fact", "ingest", "log_session", "outcome", "flag", "forget", "promote", "sleep_run", "ranking_reset", "learned_keys", "conflicts"}

// BASE.md: "Every tool receives namespace and answers in the same envelope:
// ok, data, notices and audit_id".
func TestEveryToolAnswersInTheEnvelope(t *testing.T) {
	cfg := testConfig("read", "standard", "admin")
	cfg.MCP.HumanActor = true
	api := &fakeAPI{}
	_, cs := newTestServer(t, api, cfg)
	if len(minimalArgs) != 20 {
		t.Fatalf("minimalArgs has %d tools", len(minimalArgs))
	}
	for _, name := range chanda.ToolNames() {
		t.Run(name, func(t *testing.T) {
			env, _ := callEnv(t, cs, name, minimalArgs[name])
			if !env.OK || env.Error != nil || env.Data == nil || env.Notices == nil {
				t.Fatalf("envelope %+v", env)
			}
			c := api.last()
			if c.tool != name {
				t.Fatalf("API saw %q", c.tool)
			}
			if !c.hasActor || c.actor != (chanda.Actor{Name: "hermes", Human: true, Remote: true, MaxZone: chanda.ZoneInternal}) {
				t.Fatalf("actor %+v (set=%v)", c.actor, c.hasActor)
			}
			if ns := reflect.ValueOf(c.in).FieldByName("Namespace").String(); ns != "acme" {
				t.Fatalf("namespace %q, want the configured default", ns)
			}
			wantAudit := slices.Contains(auditedTools, name)
			if (env.AuditID != 0) != wantAudit || (wantAudit && env.AuditID != int64(api.count())) {
				t.Fatalf("audit_id %d (audited=%v, call %d)", env.AuditID, wantAudit, api.count())
			}
		})
	}
}

func TestArgumentsReachTheAPI(t *testing.T) {
	api := &fakeAPI{}
	_, cs := newTestServer(t, api, testConfig("read", "standard"))
	callEnv(t, cs, "search", map[string]any{
		"namespace": "other", "query": "q", "limit": 3, "include_history": true,
		"filters": map[string]any{"types": []any{"fact", "page"}, "as_of": "2026-03-01"},
	})
	want := chanda.SearchInput{Namespace: "other", Query: "q", Limit: 3, IncludeHistory: true,
		Filters: chanda.SearchFilters{Types: []chanda.RecordType{chanda.RecFact, chanda.RecPage}, AsOf: "2026-03-01"}}
	if got := api.last().in; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	callEnv(t, cs, "outcome", map[string]any{"retrieval_id": "ret_1", "value": -0.5})
	if got := api.last().in.(chanda.OutcomeInput); got.Value != -0.5 || got.Namespace != "acme" {
		t.Fatalf("outcome input %+v", got)
	}
}

// Errors use stable, documented codes; isError mirrors !ok.
func TestErrorsMapToEnvelopeCodes(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		fail error
		want chanda.Code
	}{
		{"missing required field", "search", map[string]any{}, nil, chanda.CodeInvalidArgument},
		{"unknown field", "search", map[string]any{"query": "q", "bogus": 1}, nil, chanda.CodeInvalidArgument},
		{"wrong type", "search", map[string]any{"query": "q", "limit": "ten"}, nil, chanda.CodeInvalidArgument},
		{"enum violation", "remember", map[string]any{"content": "x", "source": map[string]any{"kind": "god"}}, nil, chanda.CodeInvalidArgument},
		{"outcome out of range", "outcome", map[string]any{"retrieval_id": "r", "value": 2}, nil, chanda.CodeInvalidArgument},
		{"forget scope kind", "forget", map[string]any{"scope": map[string]any{"kind": "everything"}}, nil, chanda.CodeInvalidArgument},
		{"not found from API", "get", map[string]any{"id": "x"}, chanda.Errf(chanda.CodeNotFound, "no such record"), chanda.CodeNotFound},
		{"rejected from API", "remember", map[string]any{"content": "x"}, chanda.Errf(chanda.CodeRejected, "too short"), chanda.CodeRejected},
		{"rate limited from API", "outcome", map[string]any{"retrieval_id": "r", "value": 1}, chanda.Errf(chanda.CodeRateLimited, "slow down"), chanda.CodeRateLimited},
		{"uncoded error is internal", "stats", map[string]any{}, errors.New("disk on fire"), chanda.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{fail: map[string]error{tc.tool: tc.fail}}
			_, cs := newTestServer(t, api, testConfig("read", "standard", "admin"))
			env, res := callEnv(t, cs, tc.tool, tc.args)
			if env.OK || !res.IsError || env.Error == nil || env.Error.Code != tc.want || env.Data != nil {
				t.Fatalf("envelope %+v error %+v", env, env.Error)
			}
			if tc.fail == nil && api.count() != 0 {
				t.Fatal("invalid arguments reached the API")
			}
		})
	}
}

func TestAuditIDSurvivesFailure(t *testing.T) {
	api := &fakeAPI{fail: map[string]error{"flag": chanda.Errf(chanda.CodeConflict, "already flagged")}}
	_, cs := newTestServer(t, api, testConfig("standard"))
	env, _ := callEnv(t, cs, "flag", map[string]any{"id": "fct_01", "reason": "wrong"})
	if env.OK || env.AuditID != 1 || env.Error.Code != chanda.CodeConflict {
		t.Fatalf("envelope %+v", env)
	}
}

func TestPanicBecomesInternalError(t *testing.T) {
	api := &fakeAPI{panicOn: "stats"}
	_, cs := newTestServer(t, api, testConfig("read"))
	env, _ := callEnv(t, cs, "stats", nil)
	if env.OK || env.Error.Code != chanda.CodeInternal || strings.Contains(env.Error.Message, "fake panic") {
		t.Fatalf("envelope %+v", env.Error)
	}
	// The server keeps serving.
	if env, _ := callEnv(t, cs, "context", map[string]any{"query": "q"}); !env.OK {
		t.Fatalf("after panic: %+v", env)
	}
}

// BASE.md: "Only the promote tool, called by a human actor, raises a record's zone".
func TestPromoteRequiresHumanActor(t *testing.T) {
	for _, human := range []bool{false, true} {
		cfg := testConfig("admin")
		cfg.MCP.HumanActor = human
		api := &fakeAPI{}
		_, cs := newTestServer(t, api, cfg)
		env, _ := callEnv(t, cs, "promote", map[string]any{"id": "evt_01", "zone": "user"})
		if human {
			if !env.OK || api.count() != 1 {
				t.Fatalf("human promote: %+v", env)
			}
			continue
		}
		if env.OK || env.Error.Code != chanda.CodePermissionDenied || api.count() != 0 {
			t.Fatalf("non-human promote: %+v (API calls %d)", env, api.count())
		}
	}
}

func TestDefaultActorName(t *testing.T) {
	cfg := testConfig("read")
	cfg.MCP.Actor = ""
	api := &fakeAPI{}
	_, cs := newTestServer(t, api, cfg)
	callEnv(t, cs, "stats", nil)
	if a := api.last().actor; a.Name != DefaultActor || a.Human {
		t.Fatalf("actor %+v", a)
	}
}

func TestToolSchemasForLLMClients(t *testing.T) {
	_, cs := newTestServer(t, &fakeAPI{}, testConfig("read", "standard", "admin"))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*sdk.Tool{}
	for _, tl := range res.Tools {
		byName[tl.Name] = tl
	}
	for _, sp := range Specs() {
		tl := byName[sp.Name]
		if tl == nil {
			t.Fatalf("%s not listed", sp.Name)
		}
		if len(tl.Description) < 80 || tl.Title == "" {
			t.Errorf("%s: description too short or no title", sp.Name)
		}
		in := tl.InputSchema.(map[string]any)
		props := in["properties"].(map[string]any)
		if _, ok := props["namespace"]; !ok {
			t.Errorf("%s: input schema lacks namespace", sp.Name)
		}
		out := tl.OutputSchema.(map[string]any)
		oprops := out["properties"].(map[string]any)
		for _, k := range []string{"ok", "data", "notices", "audit_id", "error"} {
			if _, ok := oprops[k]; !ok {
				t.Errorf("%s: output schema lacks %s", sp.Name, k)
			}
		}
		if tl.Annotations == nil || tl.Annotations.ReadOnlyHint != sp.ReadOnly || *tl.Annotations.DestructiveHint != sp.Destructive {
			t.Errorf("%s: annotations %+v", sp.Name, tl.Annotations)
		}
	}
	kind := byName["forget"].InputSchema.(map[string]any)["properties"].(map[string]any)["scope"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)
	if got := kind["enum"]; !reflect.DeepEqual(got, []any{"record", "document", "source", "entity", "date_range", "search"}) {
		t.Errorf("forget scope kind enum %v", got)
	}
	req := byName["context"].InputSchema.(map[string]any)["required"]
	if !reflect.DeepEqual(req, []any{"query"}) {
		t.Errorf("context required %v", req)
	}
}

// Streamable HTTP serves the same tools (loopback only; no external network).
func TestStreamableHTTP(t *testing.T) {
	api := &fakeAPI{}
	s, err := New(api, testConfig("read"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.HTTPHandler())
	defer ts.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + HTTPPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if got := listNames(t, cs); !slices.Equal(got, toolSet(ProfileRead)) {
		t.Fatalf("tools %v", got)
	}
	env, _ := callEnv(t, cs, "stats", nil)
	if !env.OK || api.last().tool != "stats" {
		t.Fatalf("envelope %+v", env)
	}
}
