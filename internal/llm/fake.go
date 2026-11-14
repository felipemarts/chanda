package llm

import (
	"context"
	"sync"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// FakeResponse is one scripted answer. With Err set, the call fails with it
// (no tokens); otherwise JSON is validated like a real answer. Zero token
// counts are replaced by EstimateTokens of the request and of the answer.
type FakeResponse struct {
	JSON         string
	Err          error
	InputTokens  int
	OutputTokens int
}

// Fake is a scripted, deterministic provider for tests. Each call consumes
// the next element of Script; when Handler is set it is used instead and may
// look at the request. Every request is recorded in Calls.
type Fake struct {
	Script  []FakeResponse
	Handler func(req core.LLMRequest) FakeResponse

	mu    sync.Mutex
	next  int
	calls []core.LLMRequest
}

var _ core.LLMProvider = (*Fake)(nil)

// Name identifies the fake in reports.
func (f *Fake) Name() string { return "fake" }

// Calls returns a copy of the recorded requests.
func (f *Fake) Calls() []core.LLMRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.LLMRequest(nil), f.calls...)
}

// Complete returns the next scripted answer, validated against req.JSONSchema.
func (f *Fake) Complete(ctx context.Context, req core.LLMRequest) (core.LLMResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	var r FakeResponse
	switch {
	case f.Handler != nil:
		f.mu.Unlock()
		r = f.Handler(req)
	case f.next < len(f.Script):
		r = f.Script[f.next]
		f.next++
		f.mu.Unlock()
	default:
		f.mu.Unlock()
		return core.LLMResponse{}, core.Errf(core.CodeUnavailable, "llm fake: script exhausted")
	}
	if r.Err != nil {
		return core.LLMResponse{}, r.Err
	}
	resp := core.LLMResponse{InputTokens: r.InputTokens, OutputTokens: r.OutputTokens}
	if resp.InputTokens == 0 {
		resp.InputTokens = EstimateTokens(req.System) + EstimateTokens(req.Prompt)
	}
	if resp.OutputTokens == 0 {
		resp.OutputTokens = EstimateTokens(r.JSON)
	}
	if err := Validate(req.JSONSchema, []byte(r.JSON)); err != nil {
		return resp, core.Wrap(core.CodeInvalidArgument, err, "llm answer discarded")
	}
	resp.JSON = []byte(r.JSON)
	return resp, nil
}
