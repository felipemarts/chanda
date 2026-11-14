package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

const simpleSchema = `{"type":"object","required":["ok"],"additionalProperties":false,"properties":{"ok":{"type":"boolean"}}}`

type recorder struct {
	mu     sync.Mutex
	bodies []map[string]any
	auth   []string
}

func (r *recorder) record(req *http.Request) map[string]any {
	b, _ := io.ReadAll(req.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	r.mu.Lock()
	r.bodies = append(r.bodies, m)
	r.auth = append(r.auth, req.Header.Get("Authorization"))
	r.mu.Unlock()
	return m
}

func chatJSON(content string, in, out int, finish string) string {
	c, _ := json.Marshal(content)
	return `{"choices":[{"message":{"role":"assistant","content":` + string(c) + `},"finish_reason":"` + finish + `"}],` +
		`"usage":{"prompt_tokens":` + itoa(in) + `,"completion_tokens":` + itoa(out) + `}}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func newProvider(t *testing.T, srv *httptest.Server, format string) *llm.OpenAICompatible {
	t.Helper()
	cfg := config.Default().LLM
	cfg.Endpoint = srv.URL + "/v1/"
	cfg.Model = "m"
	cfg.APIKeyEnv = "CHANDA_TEST_KEY"
	cfg.ResponseFormat = format
	p, err := llm.NewOpenAICompatible(cfg, func(k string) string {
		if k == "CHANDA_TEST_KEY" {
			return "secret"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	p.SetHTTPClient(srv.Client())
	return p
}

func req() core.LLMRequest {
	return core.LLMRequest{System: "sys", Prompt: "p", JSONSchema: []byte(simpleSchema), MaxTokens: 50}
}

func TestOpenAICompatibleSendsJSONSchemaAndReportsUsage(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		rec.record(r)
		io.WriteString(w, chatJSON(`{"ok":true}`, 11, 3, "stop"))
	}))
	defer srv.Close()
	p := newProvider(t, srv, "auto")
	resp, err := p.Complete(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"ok":true}` || resp.InputTokens != 11 || resp.OutputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	body := rec.bodies[0]
	rf := body["response_format"].(map[string]any)
	if rf["type"] != "json_schema" || body["model"] != "m" || body["temperature"].(float64) != 0 || body["max_tokens"].(float64) != 50 {
		t.Fatalf("request body = %v", body)
	}
	if rec.auth[0] != "Bearer secret" {
		t.Fatalf("auth header = %q", rec.auth[0])
	}
}

// BASE.md Sleep safeguard: "An invalid answer is discarded, with no automatic retry."
func TestInvalidAnswerDiscardedWithoutRetry(t *testing.T) {
	cases := []struct{ name, content, finish string }{
		{"not json", "sure! ok", "stop"},
		{"schema mismatch", `{"ok":"yes"}`, "stop"},
		{"truncated", `{"ok":true}`, "length"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				io.WriteString(w, chatJSON(c.content, 7, 5, c.finish))
			}))
			defer srv.Close()
			p := newProvider(t, srv, "auto")
			resp, err := p.Complete(context.Background(), req())
			if !llm.IsInvalidOutput(err) {
				t.Fatalf("expected invalid output, got %v", err)
			}
			if resp.JSON != nil || resp.InputTokens != 7 || resp.OutputTokens != 5 {
				t.Fatalf("invalid answer must carry usage and no JSON: %+v", resp)
			}
			if len(rec.bodies) != 1 {
				t.Fatalf("expected exactly one request, got %d", len(rec.bodies))
			}
		})
	}
}

func TestFencedJSONAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, chatJSON("```json\n{\"ok\":false}\n```", 1, 1, "stop"))
	}))
	defer srv.Close()
	resp, err := newProvider(t, srv, "json_schema").Complete(context.Background(), req())
	if err != nil || string(resp.JSON) != `{"ok":false}` {
		t.Fatalf("resp=%s err=%v", resp.JSON, err)
	}
}

func TestAutoFallsBackToJSONObjectOnce(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := rec.record(r)
		rf := body["response_format"].(map[string]any)
		if rf["type"] == "json_schema" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"response_format json_schema is not supported"}}`)
			return
		}
		io.WriteString(w, chatJSON(`{"ok":true}`, 1, 1, "stop"))
	}))
	defer srv.Close()
	p := newProvider(t, srv, "auto")
	for i := 0; i < 2; i++ {
		if _, err := p.Complete(context.Background(), req()); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.bodies) != 3 {
		t.Fatalf("expected 1 rejected + 2 json_object requests, got %d", len(rec.bodies))
	}
	last := rec.bodies[2]
	sys := last["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, `"required":["ok"]`) {
		t.Fatalf("json_object mode must carry the schema in the system prompt: %q", sys)
	}
}

func TestStrictJSONSchemaModeDoesNotFallBack(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `response_format unsupported`)
	}))
	defer srv.Close()
	_, err := newProvider(t, srv, "json_schema").Complete(context.Background(), req())
	if core.CodeOf(err) != core.CodeUnavailable || n != 1 {
		t.Fatalf("err=%v requests=%d", err, n)
	}
}

func TestHTTPErrorIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newProvider(t, srv, "auto").Complete(context.Background(), req())
	if core.CodeOf(err) != core.CodeUnavailable || llm.IsInvalidOutput(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestFromConfigWithoutEndpointDisablesProvider(t *testing.T) {
	p, err := llm.FromConfig(config.Default().LLM, nil)
	if err != nil || p != nil {
		t.Fatalf("p=%v err=%v", p, err)
	}
	cfg := config.Default().LLM
	cfg.Endpoint = "http://127.0.0.1:1"
	if _, err := llm.FromConfig(cfg, nil); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("missing model must be rejected, got %v", err)
	}
	cfg.Model, cfg.ResponseFormat = "m", "xml"
	if _, err := llm.FromConfig(cfg, nil); err == nil {
		t.Fatal("unknown response format must be rejected")
	}
}

func TestFakeScriptedAndValidated(t *testing.T) {
	f := &llm.Fake{Script: []llm.FakeResponse{{JSON: `{"ok":true}`}, {JSON: `nope`}}}
	r, err := f.Complete(context.Background(), req())
	if err != nil || string(r.JSON) != `{"ok":true}` || r.InputTokens == 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := f.Complete(context.Background(), req()); !llm.IsInvalidOutput(err) {
		t.Fatalf("expected invalid output, got %v", err)
	}
	if _, err := f.Complete(context.Background(), req()); core.CodeOf(err) != core.CodeUnavailable {
		t.Fatalf("exhausted script: %v", err)
	}
	if len(f.Calls()) != 3 {
		t.Fatalf("calls = %d", len(f.Calls()))
	}
}
