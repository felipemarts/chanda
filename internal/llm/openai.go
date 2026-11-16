package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

// Response formats (config llm.response_format).
const (
	FormatAuto       = "auto"        // json_schema; on a 400/422 that names response_format, json_object from then on
	FormatJSONSchema = "json_schema" // structured outputs
	FormatJSONObject = "json_object" // JSON mode; the schema goes in the system prompt
)

// maxErrBody bounds how much of an error body is quoted in an error message.
const maxErrBody = 200

// OpenAICompatible calls POST {endpoint}/chat/completions on any server that
// speaks the OpenAI chat API (a local model server or an external API).
type OpenAICompatible struct {
	endpoint  string
	model     string
	apiKey    string
	maxTokens int
	client    *http.Client

	mu     sync.Mutex
	format string // current format; auto degrades to json_object once
	auto   bool
}

var _ core.LLMProvider = (*OpenAICompatible)(nil)

// NewOpenAICompatible builds the provider from cfg. The API key is read from
// the environment variable named by cfg.APIKeyEnv (it may be empty for local
// servers). The timeout applies to each call.
func NewOpenAICompatible(cfg config.LLMConfig, getenv func(string) string) (*OpenAICompatible, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, core.Errf(core.CodeUnavailable, "llm: no endpoint configured")
	}
	if cfg.Model == "" {
		return nil, core.Errf(core.CodeInvalidArgument, "llm: model is required")
	}
	format := cfg.ResponseFormat
	if format == "" {
		format = FormatAuto
	}
	if format != FormatAuto && format != FormatJSONSchema && format != FormatJSONObject {
		return nil, core.Errf(core.CodeInvalidArgument, "llm: unknown response_format %q", format)
	}
	key := ""
	if cfg.APIKeyEnv != "" && getenv != nil {
		key = getenv(cfg.APIKeyEnv)
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	p := &OpenAICompatible{
		endpoint:  strings.TrimRight(cfg.Endpoint, "/"),
		model:     cfg.Model,
		apiKey:    key,
		maxTokens: cfg.MaxTokens,
		client:    &http.Client{Timeout: timeout},
		format:    format,
		auto:      format == FormatAuto,
	}
	if p.auto {
		p.format = FormatJSONSchema
	}
	return p, nil
}

// SetHTTPClient replaces the HTTP client (tests use an httptest client).
func (p *OpenAICompatible) SetHTTPClient(c *http.Client) { p.client = c }

// Name identifies the provider and model in reports.
func (p *OpenAICompatible) Name() string { return "openai-compatible:" + p.model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete sends one chat completion and validates the answer against
// req.JSONSchema. Invalid answers return an error wrapping ErrInvalidOutput
// together with the token usage; they are never retried. The only resend is
// the one-time capability fallback of FormatAuto, when the server rejects
// response_format json_schema before producing any answer.
func (p *OpenAICompatible) Complete(ctx context.Context, req core.LLMRequest) (core.LLMResponse, error) {
	if len(req.JSONSchema) == 0 {
		return core.LLMResponse{}, core.Errf(core.CodeInvalidArgument, "llm: request has no JSON schema")
	}
	p.mu.Lock()
	format := p.format
	p.mu.Unlock()

	status, body, err := p.post(ctx, req, format)
	if err != nil {
		return core.LLMResponse{}, err
	}
	if p.auto && format == FormatJSONSchema && unsupportedFormat(status, body) {
		p.mu.Lock()
		p.format = FormatJSONObject
		p.mu.Unlock()
		status, body, err = p.post(ctx, req, FormatJSONObject)
		if err != nil {
			return core.LLMResponse{}, err
		}
	}
	if status < 200 || status > 299 {
		return core.LLMResponse{}, core.Errf(core.CodeUnavailable, "llm: http %d: %s", status, snippet(body))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return core.LLMResponse{}, core.Wrap(core.CodeUnavailable, err, "llm: malformed chat response")
	}
	resp := core.LLMResponse{InputTokens: cr.Usage.PromptTokens, OutputTokens: cr.Usage.CompletionTokens}
	if len(cr.Choices) == 0 {
		return resp, invalidOutput("no choices")
	}
	ch := cr.Choices[0]
	if ch.Message.Refusal != nil && *ch.Message.Refusal != "" {
		return resp, invalidOutput("model refused")
	}
	if ch.FinishReason == "length" {
		return resp, invalidOutput("answer truncated at max_tokens")
	}
	if ch.Message.Content == nil {
		return resp, invalidOutput("empty content")
	}
	content := stripFence(*ch.Message.Content)
	if err := Validate(req.JSONSchema, []byte(content)); err != nil {
		if IsInvalidOutput(err) {
			return resp, core.Wrap(core.CodeInvalidArgument, err, "llm answer discarded")
		}
		return resp, core.Wrap(core.CodeInvalidArgument, err, "llm: bad request schema")
	}
	resp.JSON = []byte(content)
	return resp, nil
}

func (p *OpenAICompatible) post(ctx context.Context, req core.LLMRequest, format string) (int, []byte, error) {
	system := req.System
	var rf json.RawMessage
	switch format {
	case FormatJSONSchema:
		rf, _ = json.Marshal(map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "chanda_output",
				"strict": true,
				"schema": json.RawMessage(req.JSONSchema),
			},
		})
	case FormatJSONObject:
		rf = json.RawMessage(`{"type":"json_object"}`)
		system = strings.TrimSpace(system + "\n\nAnswer with one JSON object that matches this JSON Schema exactly, and nothing else:\n" + string(req.JSONSchema))
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}
	payload, err := json.Marshal(chatRequest{
		Model:          p.model,
		Messages:       []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: req.Prompt}},
		Temperature:    0,
		MaxTokens:      maxTokens,
		ResponseFormat: rf,
	})
	if err != nil {
		return 0, nil, core.Wrap(core.CodeInternal, err, "llm: encode request")
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, core.Wrap(core.CodeInvalidArgument, err, "llm: build request")
	}
	hr.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		hr.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	res, err := p.client.Do(hr)
	if err != nil {
		return 0, nil, core.Wrap(core.CodeUnavailable, err, "llm: request failed")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return 0, nil, core.Wrap(core.CodeUnavailable, err, "llm: read response")
	}
	return res.StatusCode, body, nil
}

func unsupportedFormat(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	b := strings.ToLower(string(body))
	return strings.Contains(b, "response_format") || strings.Contains(b, "json_schema")
}

// stripFence removes surrounding whitespace and one optional ``` fence, which
// some local models add even in JSON mode. Nothing else is repaired.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") || len(s) < 6 {
		return s
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "```"), "```")
	s = strings.TrimPrefix(s, "json")
	return strings.TrimSpace(s)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrBody {
		s = s[:maxErrBody] + "..."
	}
	return fmt.Sprintf("%q", s)
}
