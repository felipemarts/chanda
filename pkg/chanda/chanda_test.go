package chanda

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// toolTypes pairs every tool with its Input and Output types, in BASE.md order.
var toolTypes = []struct {
	name    string
	in, out any
}{
	{ToolContext, ContextInput{}, ContextOutput{}},
	{ToolSearch, SearchInput{}, SearchOutput{}},
	{ToolGet, GetInput{}, GetOutput{}},
	{ToolTimeline, TimelineInput{}, TimelineOutput{}},
	{ToolExplain, ExplainInput{}, ExplainOutput{}},
	{ToolStats, StatsInput{}, StatsOutput{}},
	{ToolRemember, RememberInput{}, RememberOutput{}},
	{ToolAssertFact, AssertFactInput{}, AssertFactOutput{}},
	{ToolIngest, IngestInput{}, IngestOutput{}},
	{ToolLogSession, LogSessionInput{}, LogSessionOutput{}},
	{ToolOutcome, OutcomeInput{}, OutcomeOutput{}},
	{ToolFlag, FlagInput{}, FlagOutput{}},
	{ToolFlush, FlushInput{}, FlushOutput{}},
	{ToolForget, ForgetInput{}, ForgetOutput{}},
	{ToolPromote, PromoteInput{}, PromoteOutput{}},
	{ToolSleepRun, SleepRunInput{}, SleepRunOutput{}},
	{ToolVerify, VerifyInput{}, VerifyOutput{}},
	{ToolRankingReset, RankingResetInput{}, RankingResetOutput{}},
	{ToolLearnedKeys, LearnedKeysInput{}, LearnedKeysOutput{}},
	{ToolConflicts, ConflictsInput{}, ConflictsOutput{}},
}

func TestTwentyToolsInTableOrder(t *testing.T) {
	names := ToolNames()
	if len(names) != 20 || len(toolTypes) != 20 {
		t.Fatalf("got %d names, %d type pairs; want 20", len(names), len(toolTypes))
	}
	for i, tt := range toolTypes {
		if names[i] != tt.name {
			t.Errorf("tool %d: %q, want %q", i, names[i], tt.name)
		}
	}
}

// BASE.md: "Every tool receives namespace".
func TestEveryToolReceivesNamespace(t *testing.T) {
	for _, tt := range toolTypes {
		f, ok := reflect.TypeOf(tt.in).FieldByName("Namespace")
		if !ok || f.Type.Kind() != reflect.String || f.Tag.Get("json") != "namespace,omitempty" {
			t.Errorf("%s input: Namespace field missing or mistagged (%v)", tt.name, f.Tag)
		}
	}
}

// Every output embeds Meta, so the envelope can surface audit_id and notices.
func TestEveryOutputCarriesMeta(t *testing.T) {
	for _, tt := range toolTypes {
		if _, ok := tt.out.(MetaCarrier); !ok {
			t.Errorf("%s output does not embed Meta", tt.name)
		}
	}
}

var snake = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// The wire contract: every exported field has a snake_case json tag.
func TestWireFieldsAreSnakeCase(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(path string, rt reflect.Type)
	walk = func(path string, rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for _, f := range reflect.VisibleFields(rt) {
			if !f.IsExported() || f.Anonymous {
				continue
			}
			tag := f.Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if !snake.MatchString(name) {
				t.Errorf("%s.%s: json tag %q is not snake_case", path, f.Name, tag)
			}
			walk(path+"."+name, f.Type)
		}
	}
	for _, tt := range toolTypes {
		walk(tt.name+".in", reflect.TypeOf(tt.in))
		walk(tt.name+".out", reflect.TypeOf(tt.out))
	}
	walk("envelope", reflect.TypeOf(Envelope{}))
}

func TestMetaStaysOutOfData(t *testing.T) {
	out := RememberOutput{
		Meta:        Meta{AuditID: 7, Notices: []string{"n"}},
		WriteResult: WriteResult{ID: "evt_1", Action: ActionCreated, Reason: "new", Flags: []string{}},
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"evt_1","action":"created","reason":"new","flags":[]}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
}

func TestNewEnvelope(t *testing.T) {
	audited := FlagOutput{Meta: Meta{AuditID: 12, Notices: []string{"already flagged"}}, FlagID: "flg_1", ID: "fct_1", Status: "flagged"}
	tests := []struct {
		name string
		out  any
		err  error
		want string
	}{
		{"success surfaces audit id and notices", audited, nil,
			`{"ok":true,"data":{"flag_id":"flg_1","id":"fct_1","status":"flagged"},"notices":["already flagged"],"audit_id":12}`},
		{"success without audit has empty notices", StatsOutput{Namespace: "ns"}, nil,
			`{"ok":true,"data":{"namespace":"ns","counts":{"sources":0,"documents":0,"events":0,"retired_events":0,"entities":0,"facts":0,"active_facts":0,"quarantined_facts":0,"pages":0,"embeddings":0,"retrievals":0,"outcomes":0,"tombstones":0},"db_size_bytes":0,"embed_pending":0,"ranking":{"enabled":false,"lambda":0}},"notices":[]}`},
		{"coded error keeps its code", GetOutput{}, Errf(CodeNotFound, "record %s not found", "evt_x"),
			`{"ok":false,"data":null,"notices":[],"error":{"code":"not_found","message":"record evt_x not found"}}`},
		{"wrapped coded error keeps code and cause", GetOutput{}, &Error{Code: CodeIntegrity, Message: "chain", Err: errors.New("hash mismatch")},
			`{"ok":false,"data":null,"notices":[],"error":{"code":"integrity","message":"chain: hash mismatch"}}`},
		{"plain error becomes internal", nil, errors.New("boom"),
			`{"ok":false,"data":null,"notices":[],"error":{"code":"internal","message":"boom"}}`},
		{"audited refusal keeps audit id", PromoteOutput{Meta: Meta{AuditID: 3}}, Errf(CodePermissionDenied, "human actor required"),
			`{"ok":false,"data":null,"notices":[],"audit_id":3,"error":{"code":"permission_denied","message":"human actor required"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := NewEnvelope(tc.out, tc.err)
			b, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Fatalf("got  %s\nwant %s", b, tc.want)
			}
			if (env.Err() == nil) != (tc.err == nil) {
				t.Fatalf("Err() = %v", env.Err())
			}
			if tc.err != nil && CodeOf(env.Err()) != CodeOf(tc.err) {
				t.Fatalf("round-trip code %s, want %s", CodeOf(env.Err()), CodeOf(tc.err))
			}
		})
	}
}

func TestActorContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := ActorFrom(ctx); ok {
		t.Fatal("actor present in empty context")
	}
	a, ok := ActorFrom(WithActor(ctx, Actor{Name: "alice", Human: true}))
	if !ok || a.Name != "alice" || !a.Human {
		t.Fatalf("got %+v %v", a, ok)
	}
}

func TestUnwiredIsUnavailable(t *testing.T) {
	ctx := context.Background()
	var api API = Unwired{}
	calls := map[string]func() error{
		ToolContext:      func() error { _, err := api.Context(ctx, ContextInput{}); return err },
		ToolSearch:       func() error { _, err := api.Search(ctx, SearchInput{}); return err },
		ToolGet:          func() error { _, err := api.Get(ctx, GetInput{}); return err },
		ToolTimeline:     func() error { _, err := api.Timeline(ctx, TimelineInput{}); return err },
		ToolExplain:      func() error { _, err := api.Explain(ctx, ExplainInput{}); return err },
		ToolStats:        func() error { _, err := api.Stats(ctx, StatsInput{}); return err },
		ToolRemember:     func() error { _, err := api.Remember(ctx, RememberInput{}); return err },
		ToolAssertFact:   func() error { _, err := api.AssertFact(ctx, AssertFactInput{}); return err },
		ToolIngest:       func() error { _, err := api.Ingest(ctx, IngestInput{}); return err },
		ToolLogSession:   func() error { _, err := api.LogSession(ctx, LogSessionInput{}); return err },
		ToolOutcome:      func() error { _, err := api.Outcome(ctx, OutcomeInput{}); return err },
		ToolFlag:         func() error { _, err := api.Flag(ctx, FlagInput{}); return err },
		ToolFlush:        func() error { _, err := api.Flush(ctx, FlushInput{}); return err },
		ToolForget:       func() error { _, err := api.Forget(ctx, ForgetInput{}); return err },
		ToolPromote:      func() error { _, err := api.Promote(ctx, PromoteInput{}); return err },
		ToolSleepRun:     func() error { _, err := api.SleepRun(ctx, SleepRunInput{}); return err },
		ToolVerify:       func() error { _, err := api.Verify(ctx, VerifyInput{}); return err },
		ToolRankingReset: func() error { _, err := api.RankingReset(ctx, RankingResetInput{}); return err },
		ToolLearnedKeys:  func() error { _, err := api.LearnedKeys(ctx, LearnedKeysInput{}); return err },
		ToolConflicts:    func() error { _, err := api.Conflicts(ctx, ConflictsInput{}); return err },
	}
	if len(calls) != 20 {
		t.Fatalf("%d calls", len(calls))
	}
	for name, call := range calls {
		if err := call(); CodeOf(err) != CodeUnavailable || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: %v", name, err)
		}
	}
	custom := Errf(CodeConflict, "x")
	if _, err := (Unwired{Err: custom}).Verify(ctx, VerifyInput{}); err != custom {
		t.Errorf("custom error not returned: %v", err)
	}
}

// BASE.md: "Errors use stable, documented codes".
func TestErrorCodesAreDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/errors.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range Codes() {
		if !strings.Contains(string(b), "| `"+string(c)+"` |") {
			t.Errorf("code %q missing from docs/errors.md table", c)
		}
	}
}
