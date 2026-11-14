package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TagHeader carries the condition/role tag of a call. It wins over the
// /tag/<name>/ path prefix, which wins over Config.DefaultTag.
const TagHeader = "X-Chanda-Eval-Tag"

var tagRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Config configures a Proxy.
type Config struct {
	Upstream         string // base URL; requests go to Upstream + "/chat/completions"
	APIKey           string // sent upstream as a bearer token; client credentials are dropped
	CapUSD           float64
	Prices           *PriceTable
	LogPath          string // append-only JSONL, one record per handled request
	CostPath         string // running totals, rewritten atomically
	DefaultTag       string
	DefaultMaxTokens int    // injected when the client sets no max_tokens
	Thinking         string // "disabled" (force non-thinking mode) or "passthrough"
	RejectJSONSchema bool   // answer response_format json_schema with a local 400
	OffPeakOnly      bool
	PeakGuard        time.Duration // with OffPeakOnly, also refuse when now+guard is peak
	Window           *DailyWindow  // optional allowed UTC window
	Now              func() time.Time
	Client           *http.Client
	MaxBodyBytes     int64
}

// Totals are running counters for one tag or for all calls.
type Totals struct {
	Calls            int64   `json:"calls"`             // forwarded upstream
	Errors           int64   `json:"errors"`            // forwarded, non-2xx or transport error
	Refused          int64   `json:"refused"`           // stopped by the cap or the time window
	Rejected         int64   `json:"rejected"`          // malformed or unsupported requests
	PromptTokens     int64   `json:"prompt_tokens"`     // as reported by upstream
	CacheHitTokens   int64   `json:"cache_hit_tokens"`  // prompt tokens billed at the cache-hit rate
	CacheMissTokens  int64   `json:"cache_miss_tokens"` // prompt tokens billed at the cache-miss rate
	CompletionTokens int64   `json:"completion_tokens"` // includes reasoning tokens
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CostUSD          float64 `json:"cost_usd"`       // from upstream usage x price table
	UnverifiedUSD    float64 `json:"unverified_usd"` // reservation kept when usage is unknown
}

func (t *Totals) add(r *Record) {
	switch r.Outcome {
	case OutcomeForwarded:
		t.Calls++
		if r.Status < 200 || r.Status > 299 {
			t.Errors++
		}
	case OutcomeRefused:
		t.Refused++
	case OutcomeRejected:
		t.Rejected++
	}
	t.PromptTokens += r.PromptTokens
	t.CacheHitTokens += r.CacheHitTokens
	t.CacheMissTokens += r.CacheMissTokens
	t.CompletionTokens += r.CompletionTokens
	t.ReasoningTokens += r.ReasoningTokens
	t.CostUSD += r.CostUSD
	t.UnverifiedUSD += r.UnverifiedUSD
}

// CostFile is the content of eval/cost.json.
type CostFile struct {
	CapUSD      float64            `json:"cap_usd"`
	SpentUSD    float64            `json:"spent_usd"` // cost_usd + unverified_usd: what the cap is checked against
	UpdatedAt   string             `json:"updated_at"`
	PriceSource string             `json:"price_source"`
	Total       Totals             `json:"total"`
	ByTag       map[string]*Totals `json:"by_tag"`
}

// Outcomes of a handled request.
const (
	OutcomeForwarded = "forwarded"
	OutcomeRefused   = "refused"
	OutcomeRejected  = "rejected"
)

// Record is one JSONL line.
type Record struct {
	Seq              int64   `json:"seq"`
	Time             string  `json:"time"` // request start, UTC RFC 3339
	Tag              string  `json:"tag"`
	Model            string  `json:"model"`
	Outcome          string  `json:"outcome"`
	Status           int     `json:"status"`
	LatencyMS        int64   `json:"latency_ms"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CacheHitTokens   int64   `json:"cache_hit_tokens"`
	CacheMissTokens  int64   `json:"cache_miss_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	MaxTokens        int64   `json:"max_tokens"`
	PriceWindow      string  `json:"price_window,omitempty"`
	EstimateUSD      float64 `json:"estimate_usd"`
	CostUSD          float64 `json:"cost_usd"`
	UnverifiedUSD    float64 `json:"unverified_usd,omitempty"`
	FinishReason     string  `json:"finish_reason,omitempty"`
	RequestSHA256    string  `json:"request_sha256,omitempty"` // of the body sent upstream
	Error            string  `json:"error,omitempty"`
}

// Proxy is the HTTP handler. It is safe for concurrent use.
type Proxy struct {
	cfg Config

	mu       sync.Mutex
	seq      int64
	reserved float64
	file     CostFile
	log      *os.File
}

// NewProxy opens the log, loads existing totals from CostPath (so the cap
// holds across restarts) and returns the handler.
func NewProxy(cfg Config) (*Proxy, error) {
	if cfg.Prices == nil {
		return nil, errors.New("no price table")
	}
	if cfg.CapUSD <= 0 {
		return nil, errors.New("cap must be > 0")
	}
	if cfg.Thinking != "disabled" && cfg.Thinking != "passthrough" {
		return nil, fmt.Errorf("thinking %q: want disabled or passthrough", cfg.Thinking)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Minute}
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 32 << 20
	}
	if cfg.DefaultMaxTokens <= 0 {
		cfg.DefaultMaxTokens = 4096
	}
	cfg.Upstream = strings.TrimRight(cfg.Upstream, "/")
	p := &Proxy{cfg: cfg}
	p.file = CostFile{ByTag: map[string]*Totals{}}
	if b, err := os.ReadFile(cfg.CostPath); err == nil {
		if err := json.Unmarshal(b, &p.file); err != nil {
			return nil, fmt.Errorf("existing %s is not valid: %w", cfg.CostPath, err)
		}
		if p.file.ByTag == nil {
			p.file.ByTag = map[string]*Totals{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	p.file.CapUSD = cfg.CapUSD
	p.file.PriceSource = cfg.Prices.Source
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	p.log = f
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.writeCostLocked(); err != nil {
		f.Close()
		return nil, err
	}
	return p, nil
}

// Close closes the log file.
func (p *Proxy) Close() error { return p.log.Close() }

// Snapshot returns a copy of the current totals.
func (p *Proxy) Snapshot() CostFile {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.file
	c.ByTag = map[string]*Totals{}
	for k, v := range p.file.ByTag {
		t := *v
		c.ByTag[k] = &t
	}
	return c
}

func (p *Proxy) spentLocked() float64 { return p.file.Total.CostUSD + p.file.Total.UnverifiedUSD }

// writeCostLocked rewrites CostPath atomically (temp file + fsync + rename).
func (p *Proxy) writeCostLocked() error {
	p.file.SpentUSD = p.spentLocked()
	p.file.UpdatedAt = p.cfg.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.MarshalIndent(p.file, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(p.cfg.CostPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cost-*.json")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(b, '\n'))
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p.cfg.CostPath); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// commit releases a reservation, adds the record to the totals, appends it
// to the log and rewrites the totals file, all under the lock.
func (p *Proxy) commit(r *Record, reservation float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reserved -= reservation
	if p.reserved < 1e-12 {
		p.reserved = 0
	}
	p.seq++
	r.Seq = p.seq
	p.file.Total.add(r)
	t := p.file.ByTag[r.Tag]
	if t == nil {
		t = &Totals{}
		p.file.ByTag[r.Tag] = t
	}
	t.add(r)
	line, _ := json.Marshal(r)
	if _, err := p.log.Write(append(line, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "lmeproxy: log write failed: %v\n", err)
	} else {
		_ = p.log.Sync()
	}
	if err := p.writeCostLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "lmeproxy: cost file write failed: %v\n", err)
	}
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	var e apiError
	e.Error.Message = msg
	e.Error.Type = "lmeproxy_error"
	e.Error.Code = code
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// route returns the path tag and whether the path is a chat completions
// endpoint. Accepted: /v1/chat/completions, /chat/completions and both
// under /tag/<name>.
func route(path string) (tag string, ok bool) {
	if rest, found := strings.CutPrefix(path, "/tag/"); found {
		name, sub, found := strings.Cut(rest, "/")
		if !found {
			return "", false
		}
		tag, path = name, "/"+sub
	}
	return tag, path == "/v1/chat/completions" || path == "/chat/completions"
}

// ServeHTTP handles one request.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.URL.Path == "/healthz":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
		return
	case req.URL.Path == "/totals":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.Snapshot())
		return
	}
	pathTag, ok := route(req.URL.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "lmeproxy serves POST [/tag/<name>]/v1/chat/completions only")
		return
	}
	if req.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	start := p.cfg.Now()
	rec := &Record{Time: start.UTC().Format(time.RFC3339Nano)}
	tag := req.Header.Get(TagHeader)
	if tag == "" {
		tag = pathTag
	}
	if tag == "" {
		tag = p.cfg.DefaultTag
	}
	rec.Tag = tag
	reject := func(status int, code, msg string) {
		rec.Outcome, rec.Status, rec.Error = OutcomeRejected, status, code+": "+msg
		if rec.Tag == "" || !tagRE.MatchString(rec.Tag) {
			rec.Tag = "_untagged"
		}
		p.commit(rec, 0)
		writeErr(w, status, code, msg)
	}
	if tag == "" {
		reject(http.StatusBadRequest, "missing_tag", "every call needs a tag: header "+TagHeader+", path /tag/<name>/v1/..., or -default-tag")
		return
	}
	if !tagRE.MatchString(tag) {
		reject(http.StatusBadRequest, "bad_tag", "tag must match "+tagRE.String())
		return
	}

	raw, err := io.ReadAll(io.LimitReader(req.Body, p.cfg.MaxBodyBytes+1))
	if err != nil {
		reject(http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if int64(len(raw)) > p.cfg.MaxBodyBytes {
		reject(http.StatusRequestEntityTooLarge, "body_too_large", "request body over limit")
		return
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		reject(http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	var model string
	if err := json.Unmarshal(body["model"], &model); err != nil || model == "" {
		reject(http.StatusBadRequest, "bad_model", "model must be a non-empty string")
		return
	}
	rec.Model = model
	if _, ok := p.cfg.Prices.Models[model]; !ok {
		reject(http.StatusBadRequest, "unpriced_model", fmt.Sprintf("model %q is not in the price table; refusing to spend at an unknown price", model))
		return
	}
	if v, ok := body["stream"]; ok {
		var s bool
		if json.Unmarshal(v, &s) == nil && s {
			reject(http.StatusBadRequest, "stream_unsupported", "lmeproxy does not support stream=true; send a non-streaming request")
			return
		}
		delete(body, "stream")
		delete(body, "stream_options")
	}
	if v, ok := body["n"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n > 1 {
			reject(http.StatusBadRequest, "n_unsupported", "n > 1 is not allowed")
			return
		}
	}
	if v, ok := body["response_format"]; ok && p.cfg.RejectJSONSchema {
		var rf struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(v, &rf) == nil && rf.Type == "json_schema" {
			// DeepSeek accepts only text and json_object. The message names
			// response_format so Chanda's "auto" mode falls back to json_object.
			reject(http.StatusBadRequest, "unsupported_response_format", "response_format type json_schema is not supported upstream; use json_object")
			return
		}
	}

	// Rewrites: temperature 0, thinking off, bounded max_tokens.
	body["temperature"] = json.RawMessage("0")
	if p.cfg.Thinking == "disabled" {
		body["thinking"] = json.RawMessage(`{"type":"disabled"}`)
		delete(body, "reasoning_effort")
	}
	var maxTokens int64
	if v, ok := body["max_completion_tokens"]; ok {
		if _, has := body["max_tokens"]; !has {
			body["max_tokens"] = v
		}
		delete(body, "max_completion_tokens")
	}
	if v, ok := body["max_tokens"]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &maxTokens); err != nil || maxTokens <= 0 {
			reject(http.StatusBadRequest, "bad_max_tokens", "max_tokens must be a positive integer")
			return
		}
	} else {
		maxTokens = int64(p.cfg.DefaultMaxTokens)
		body["max_tokens"] = json.RawMessage(strconv.FormatInt(maxTokens, 10))
	}
	rec.MaxTokens = maxTokens
	out, err := json.Marshal(body)
	if err != nil {
		reject(http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	sum := sha256.Sum256(out)
	rec.RequestSHA256 = hex.EncodeToString(sum[:])

	// Time window.
	if msg := p.windowViolation(start); msg != "" {
		rec.Outcome, rec.Status, rec.Error = OutcomeRefused, http.StatusForbidden, "outside_window: "+msg
		p.commit(rec, 0)
		writeErr(w, http.StatusForbidden, "outside_window", msg)
		return
	}

	// Cap: reserve the worst case before forwarding. Input tokens are
	// over-estimated as body bytes / 3, all priced as cache misses, and the
	// output as max_tokens, all at the highest (peak) rate.
	rate, _ := p.cfg.Prices.MaxRate(model)
	estIn := (int64(len(out)) + 2) / 3
	estimate := rate.Cost(0, estIn, maxTokens)
	rec.EstimateUSD = estimate
	p.mu.Lock()
	spent := p.spentLocked()
	switch {
	case spent >= p.cfg.CapUSD:
		p.mu.Unlock()
		msg := fmt.Sprintf("spending cap reached: spent %.6f of %.2f USD", spent, p.cfg.CapUSD)
		rec.Outcome, rec.Status, rec.Error = OutcomeRefused, http.StatusPaymentRequired, "cap_reached: "+msg
		p.commit(rec, 0)
		writeErr(w, http.StatusPaymentRequired, "cap_reached", msg)
		return
	case spent+p.reserved+estimate > p.cfg.CapUSD:
		inflight := p.reserved
		p.mu.Unlock()
		msg := fmt.Sprintf("call would cross the cap: spent %.6f + in flight %.6f + worst case %.6f > %.2f USD", spent, inflight, estimate, p.cfg.CapUSD)
		rec.Outcome, rec.Status, rec.Error = OutcomeRefused, http.StatusPaymentRequired, "would_exceed_cap: "+msg
		p.commit(rec, 0)
		writeErr(w, http.StatusPaymentRequired, "would_exceed_cap", msg)
		return
	}
	p.reserved += estimate
	p.mu.Unlock()

	status, respBody, header, ferr := p.forward(req.Context(), out)
	end := p.cfg.Now()
	rec.Outcome = OutcomeForwarded
	rec.LatencyMS = end.Sub(start).Milliseconds()
	rec.Status = status
	if ferr != nil {
		// The request may have reached upstream and been billed: keep the
		// reservation as unverified spend.
		rec.Error = "upstream: " + ferr.Error()
		var op *net.OpError
		if !(errors.As(ferr, &op) && op.Op == "dial") {
			rec.UnverifiedUSD = estimate // a dial failure never reached upstream
		}
		p.commit(rec, estimate)
		writeErr(w, http.StatusBadGateway, "upstream_error", ferr.Error())
		return
	}
	if status >= 200 && status <= 299 {
		if u, finish, ok := parseUsage(respBody); ok {
			r, window, _ := p.cfg.Prices.RateFor(model, start, end)
			rec.PriceWindow = window
			rec.PromptTokens = u.prompt
			rec.CacheHitTokens = u.hit
			rec.CacheMissTokens = u.miss
			rec.CompletionTokens = u.completion
			rec.ReasoningTokens = u.reasoning
			rec.FinishReason = finish
			rec.CostUSD = r.Cost(u.hit, u.miss, u.completion)
		} else {
			rec.Error = "usage missing from a 2xx response; reservation kept as unverified spend"
			rec.UnverifiedUSD = estimate
		}
	} else {
		rec.Error = "upstream status " + strconv.Itoa(status) + ": " + snippet(respBody)
	}
	p.commit(rec, estimate)
	if ct := header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Lmeproxy-Cost-Usd", strconv.FormatFloat(rec.CostUSD+rec.UnverifiedUSD, 'f', 8, 64))
	w.WriteHeader(status)
	_, _ = w.Write(respBody)
}

func (p *Proxy) windowViolation(now time.Time) string {
	if p.cfg.OffPeakOnly {
		if p.cfg.Prices.IsPeak(now) {
			return "peak hours (UTC) per the price table; -offpeak-only is set"
		}
		if p.cfg.PeakGuard > 0 && p.cfg.Prices.IsPeak(now.Add(p.cfg.PeakGuard)) {
			return fmt.Sprintf("peak hours start within %s; -offpeak-only is set", p.cfg.PeakGuard)
		}
	}
	if p.cfg.Window != nil && !p.cfg.Window.Contains(now) {
		return "outside the allowed UTC window set by -window"
	}
	return ""
}

func (p *Proxy) forward(ctx context.Context, body []byte) (int, []byte, http.Header, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Upstream+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json")
	if p.cfg.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	res, err := p.cfg.Client.Do(hr)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return res.StatusCode, nil, res.Header, err
	}
	return res.StatusCode, b, res.Header, nil
}

type usage struct {
	prompt, hit, miss, completion, reasoning int64
}

// parseUsage reads the DeepSeek/OpenAI usage block. DeepSeek reports
// prompt_cache_hit_tokens and prompt_cache_miss_tokens; OpenAI-style servers
// report prompt_tokens_details.cached_tokens.
func parseUsage(b []byte) (usage, string, bool) {
	var r struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens          int64  `json:"prompt_tokens"`
			CompletionTokens      int64  `json:"completion_tokens"`
			PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
			PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
			PromptTokensDetails   *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	// DeepSeek may prefix a non-streaming body with keep-alive blank lines.
	if err := json.Unmarshal(bytes.TrimSpace(b), &r); err != nil || r.Usage == nil {
		return usage{}, "", false
	}
	u := usage{prompt: r.Usage.PromptTokens, completion: r.Usage.CompletionTokens}
	switch {
	case r.Usage.PromptCacheHitTokens != nil:
		u.hit = *r.Usage.PromptCacheHitTokens
	case r.Usage.PromptTokensDetails != nil:
		u.hit = r.Usage.PromptTokensDetails.CachedTokens
	}
	if r.Usage.PromptCacheMissTokens != nil {
		u.miss = *r.Usage.PromptCacheMissTokens
	} else {
		u.miss = u.prompt - u.hit
	}
	if u.miss < 0 {
		u.miss = 0
	}
	if r.Usage.CompletionTokensDetails != nil {
		u.reasoning = r.Usage.CompletionTokensDetails.ReasoningTokens
	}
	finish := ""
	if len(r.Choices) > 0 {
		finish = r.Choices[0].FinishReason
	}
	return u, finish, true
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
