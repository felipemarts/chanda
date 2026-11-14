package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

// Saturday 2026-11-14 12:00 UTC: off-peak. Monday 2026-11-16 02:00 UTC: peak.
var (
	offPeakTime = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	peakTime    = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)
)

// fakeUpstream is a local OpenAI-compatible server. It never reaches the
// network; it records what the proxy sent.
type fakeUpstream struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	auth   []string
	calls  atomic.Int64
	// respond builds the response; default: a fixed usage block.
	respond func(body map[string]any) (int, string)
	delay   time.Duration
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	f := &fakeUpstream{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "bad path "+r.URL.Path, 404)
			return
		}
		f.calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		f.mu.Lock()
		f.bodies = append(f.bodies, m)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		status, out := 200, usageJSON("ok", 1000, 600, 200, "stop")
		if f.respond != nil {
			status, out = f.respond(m)
		}
		if status == -1 { // hang up after reading the request
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func usageJSON(content string, prompt, hit, completion int, finish string) string {
	c, _ := json.Marshal(content)
	return `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":` + string(c) +
		`},"finish_reason":"` + finish + `"}],"usage":{"prompt_tokens":` + itoa(prompt) + `,"completion_tokens":` + itoa(completion) +
		`,"total_tokens":` + itoa(prompt+completion) + `,"prompt_cache_hit_tokens":` + itoa(hit) + `,"prompt_cache_miss_tokens":` + itoa(prompt-hit) +
		`,"prompt_tokens_details":{"cached_tokens":` + itoa(hit) + `},"completion_tokens_details":{"reasoning_tokens":0}}}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func testPrices(t *testing.T) *PriceTable {
	t.Helper()
	pt, err := ParsePriceTable(defaultPrices)
	if err != nil {
		t.Fatal(err)
	}
	return pt
}

type harness struct {
	p    *Proxy
	srv  *httptest.Server
	up   *fakeUpstream
	dir  string
	now  atomic.Pointer[time.Time]
	conf Config
}

func newHarness(t *testing.T, mod func(*Config)) *harness {
	t.Helper()
	h := &harness{up: newFakeUpstream(t), dir: t.TempDir()}
	tm := offPeakTime
	h.now.Store(&tm)
	h.conf = Config{
		Upstream:         h.up.srv.URL,
		APIKey:           "upstream-secret",
		CapUSD:           8,
		Prices:           testPrices(t),
		LogPath:          filepath.Join(h.dir, "calls.jsonl"),
		CostPath:         filepath.Join(h.dir, "cost.json"),
		DefaultMaxTokens: 4096,
		Thinking:         "disabled",
		RejectJSONSchema: true,
		Now:              func() time.Time { return *h.now.Load() },
	}
	if mod != nil {
		mod(&h.conf)
	}
	p, err := NewProxy(h.conf)
	if err != nil {
		t.Fatal(err)
	}
	h.p = p
	h.srv = httptest.NewServer(p)
	t.Cleanup(func() { h.srv.Close(); p.Close() })
	return h
}

func (h *harness) setNow(tm time.Time) { h.now.Store(&tm) }

func (h *harness) post(t *testing.T, path, tag, body string) (int, map[string]any, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer client-junk")
	if tag != "" {
		req.Header.Set(TagHeader, tag)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return res.StatusCode, m, res.Header
}

func readLog(t *testing.T, path string) []Record {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("bad log line %q: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

func readCost(t *testing.T, path string) CostFile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c CostFile
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

const simpleBody = `{"model":"deepseek-flash","messages":[{"role":"user","content":"hi"}],"temperature":0.7,"reasoning_effort":"high","max_tokens":100}`

func TestForwardRewritesAndRecords(t *testing.T) {
	h := newHarness(t, nil)
	status, body, hdr := h.post(t, "/v1/chat/completions", "A-reader", simpleBody)
	if status != 200 {
		t.Fatalf("status %d body %v", status, body)
	}
	if body["choices"] == nil {
		t.Fatalf("upstream body not passed through: %v", body)
	}
	sent := h.up.bodies[0]
	if sent["temperature"] != float64(0) {
		t.Errorf("temperature not forced to 0: %v", sent["temperature"])
	}
	if th, _ := sent["thinking"].(map[string]any); th["type"] != "disabled" {
		t.Errorf("thinking not disabled: %v", sent["thinking"])
	}
	if _, ok := sent["reasoning_effort"]; ok {
		t.Error("reasoning_effort should be dropped when thinking is disabled")
	}
	if sent["max_tokens"] != float64(100) {
		t.Errorf("client max_tokens not kept: %v", sent["max_tokens"])
	}
	if h.up.auth[0] != "Bearer upstream-secret" {
		t.Errorf("upstream auth = %q, want the proxy's key, never the client's", h.up.auth[0])
	}
	// 600 hit, 400 miss, 200 out at off-peak: (600*0.003 + 400*0.15 + 200*0.6)/1e6
	want := (600*0.003 + 400*0.15 + 200*0.6) / 1e6
	recs := readLog(t, h.conf.LogPath)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.Tag != "A-reader" || r.Model != "deepseek-flash" || r.Outcome != OutcomeForwarded || r.Status != 200 {
		t.Errorf("record fields: %+v", r)
	}
	if r.PromptTokens != 1000 || r.CacheHitTokens != 600 || r.CacheMissTokens != 400 || r.CompletionTokens != 200 {
		t.Errorf("token fields: %+v", r)
	}
	if !near(r.CostUSD, want) || r.PriceWindow != "offpeak" {
		t.Errorf("cost %v window %s, want %v offpeak", r.CostUSD, r.PriceWindow, want)
	}
	if r.RequestSHA256 == "" || r.Time == "" || r.FinishReason != "stop" {
		t.Errorf("missing fields: %+v", r)
	}
	if hdr.Get("X-Lmeproxy-Cost-Usd") == "" {
		t.Error("cost header missing")
	}
	c := readCost(t, h.conf.CostPath)
	if c.Total.Calls != 1 || !near(c.Total.CostUSD, want) || !near(c.SpentUSD, want) || c.CapUSD != 8 {
		t.Errorf("cost file total: %+v", c)
	}
	if bt := c.ByTag["A-reader"]; bt == nil || bt.Calls != 1 || !near(bt.CostUSD, want) {
		t.Errorf("cost file by tag: %+v", c.ByTag)
	}
}

func TestDefaultMaxTokensAndPassthroughThinking(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Thinking = "passthrough"; c.DefaultMaxTokens = 321 })
	status, _, _ := h.post(t, "/v1/chat/completions", "t", `{"model":"deepseek-flash","messages":[{"role":"user","content":"x"}],"reasoning_effort":"high"}`)
	if status != 200 {
		t.Fatal(status)
	}
	sent := h.up.bodies[0]
	if sent["max_tokens"] != float64(321) {
		t.Errorf("default max_tokens not injected: %v", sent["max_tokens"])
	}
	if _, ok := sent["thinking"]; ok {
		t.Error("passthrough must not add thinking")
	}
	if sent["reasoning_effort"] != "high" {
		t.Error("passthrough must keep reasoning_effort")
	}
	// max_completion_tokens is mapped to max_tokens (DeepSeek does not list it).
	status, _, _ = h.post(t, "/v1/chat/completions", "t", `{"model":"deepseek-flash","messages":[{"role":"user","content":"x"}],"max_completion_tokens":77}`)
	if status != 200 {
		t.Fatal(status)
	}
	if s := h.up.bodies[1]; s["max_tokens"] != float64(77) || s["max_completion_tokens"] != nil {
		t.Errorf("max_completion_tokens mapping: %v", s)
	}
}

func TestPeakPricing(t *testing.T) {
	h := newHarness(t, nil)
	h.setNow(peakTime)
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Fatal(status)
	}
	r := readLog(t, h.conf.LogPath)[0]
	want := (600*0.006 + 400*0.30 + 200*1.2) / 1e6
	if !near(r.CostUSD, want) || r.PriceWindow != "peak" {
		t.Errorf("cost %v window %s, want %v peak", r.CostUSD, r.PriceWindow, want)
	}
}

func TestTagSources(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.DefaultTag = "dflt" })
	h.post(t, "/tag/C-ingest/v1/chat/completions", "", simpleBody) // path tag
	h.post(t, "/tag/C-ingest/v1/chat/completions", "hdr", simpleBody)
	h.post(t, "/v1/chat/completions", "", simpleBody)          // default tag
	h.post(t, "/tag/E-sleep/chat/completions", "", simpleBody) // no /v1
	recs := readLog(t, h.conf.LogPath)
	got := []string{}
	for _, r := range recs {
		got = append(got, r.Tag)
	}
	want := []string{"C-ingest", "hdr", "dflt", "E-sleep"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tags %v want %v", got, want)
	}

	h2 := newHarness(t, nil) // no default tag
	status, body, _ := h2.post(t, "/v1/chat/completions", "", simpleBody)
	if status != 400 || errCode(body) != "missing_tag" {
		t.Errorf("untagged: %d %v", status, body)
	}
	status, body, _ = h2.post(t, "/v1/chat/completions", "bad tag!", simpleBody)
	if status != 400 || errCode(body) != "bad_tag" {
		t.Errorf("bad tag: %d %v", status, body)
	}
	if h2.up.calls.Load() != 0 {
		t.Error("rejected requests reached upstream")
	}
	if status, _, _ := h2.post(t, "/v1/embeddings", "x", "{}"); status != 404 {
		t.Errorf("unknown path: %d", status)
	}
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func errMsg(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

func TestRejections(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		name, body, code string
	}{
		{"stream", `{"model":"deepseek-flash","messages":[],"stream":true}`, "stream_unsupported"},
		{"json_schema", `{"model":"deepseek-flash","messages":[],"response_format":{"type":"json_schema","json_schema":{}}}`, "unsupported_response_format"},
		{"unpriced", `{"model":"deepseek-chat","messages":[]}`, "unpriced_model"},
		{"n", `{"model":"deepseek-flash","messages":[],"n":2}`, "n_unsupported"},
		{"bad json", `{`, "bad_json"},
		{"bad max_tokens", `{"model":"deepseek-flash","messages":[],"max_tokens":-1}`, "bad_max_tokens"},
	}
	for _, c := range cases {
		status, body, _ := h.post(t, "/v1/chat/completions", "t", c.body)
		if status != 400 || errCode(body) != c.code {
			t.Errorf("%s: %d %v", c.name, status, body)
		}
	}
	if h.up.calls.Load() != 0 {
		t.Error("rejected requests reached upstream")
	}
	// stream:false is accepted and stripped.
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", `{"model":"deepseek-flash","messages":[],"stream":false}`); status != 200 {
		t.Errorf("stream:false refused: %d", status)
	}
	// json_object passes.
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", `{"model":"deepseek-flash","messages":[],"response_format":{"type":"json_object"}}`); status != 200 {
		t.Errorf("json_object refused: %d", status)
	}
	c := readCost(t, h.conf.CostPath)
	if c.Total.Rejected != int64(len(cases)) || c.Total.Calls != 2 {
		t.Errorf("rejected %d calls %d", c.Total.Rejected, c.Total.Calls)
	}
}

// capBody asks for at most 300 output tokens: its reservation (300 * 1.2e-6
// + body bytes/3 * 0.3e-6) is above what the fake upstream bills for it
// (perCall), as it is for real calls whose output is bounded by max_tokens.
const capBody = `{"model":"deepseek-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":300}`

var perCall = (600*0.003 + 400*0.15 + 200*0.6) / 1e6

func TestWouldExceedCap(t *testing.T) {
	capUSD := 0.001
	h := newHarness(t, func(c *Config) { c.CapUSD = capUSD })
	ok := 0
	for i := 0; i < 10; i++ {
		status, body, _ := h.post(t, "/v1/chat/completions", "t", capBody)
		switch status {
		case 200:
			ok++
		case 402:
			if c := errCode(body); c != "would_exceed_cap" {
				t.Errorf("402 code %q", c)
			}
			if !strings.Contains(errMsg(body), "cap") {
				t.Errorf("402 message unclear: %v", body)
			}
		default:
			t.Fatalf("status %d", status)
		}
	}
	recs := readLog(t, h.conf.LogPath)
	est := recs[0].EstimateUSD
	if est <= perCall {
		t.Fatalf("test premise: estimate %v must exceed the billed %v", est, perCall)
	}
	// Admission rule: spent + estimate <= cap.
	want := 0
	for spent := 0.0; spent+est <= capUSD; spent += perCall {
		want++
	}
	if ok != want || int64(ok) != h.up.calls.Load() {
		t.Errorf("admitted %d (upstream saw %d), want %d", ok, h.up.calls.Load(), want)
	}
	c := readCost(t, h.conf.CostPath)
	if c.SpentUSD > capUSD {
		t.Errorf("spent %v over cap", c.SpentUSD)
	}
	if c.Total.Refused != int64(10-ok) {
		t.Errorf("refused %d", c.Total.Refused)
	}

	// A single huge max_tokens is refused even when nothing is spent.
	h2 := newHarness(t, func(c *Config) { c.CapUSD = 0.01 })
	status, body, _ := h2.post(t, "/v1/chat/completions", "t", `{"model":"deepseek-flash","messages":[],"max_tokens":100000}`)
	if status != 402 || errCode(body) != "would_exceed_cap" || h2.up.calls.Load() != 0 {
		t.Errorf("huge max_tokens: %d %v", status, body)
	}
}

func TestCapReachedAfterRestart(t *testing.T) {
	dir := t.TempDir()
	cost := filepath.Join(dir, "cost.json")
	// A previous run spent 1.0 USD.
	prev := CostFile{CapUSD: 1, Total: Totals{Calls: 3, CostUSD: 1.0}, ByTag: map[string]*Totals{"C-ingest": {Calls: 3, CostUSD: 1.0}}}
	b, _ := json.Marshal(prev)
	if err := os.WriteFile(cost, b, 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) { c.CostPath = cost; c.CapUSD = 1.0 })
	status, body, _ := h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 402 || errCode(body) != "cap_reached" {
		t.Fatalf("after restart with spent == cap: %d %v", status, body)
	}
	if h.up.calls.Load() != 0 {
		t.Error("refused call reached upstream")
	}
	c := readCost(t, cost)
	if c.ByTag["C-ingest"].Calls != 3 || c.ByTag["t"].Refused != 1 || !near(c.SpentUSD, 1.0) {
		t.Errorf("totals not resumed: %+v", c)
	}
	// A larger cap admits calls and keeps adding to the resumed totals.
	h2 := newHarness(t, func(c *Config) { c.CostPath = cost; c.CapUSD = 2.0 })
	if status, _, _ := h2.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Fatal(status)
	}
	c = readCost(t, cost)
	if c.Total.Calls != 4 || c.CapUSD != 2.0 {
		t.Errorf("resumed totals: %+v", c.Total)
	}
}

func TestConcurrentCapNeverExceeded(t *testing.T) {
	// Every call reports the same usage; with a slow upstream all requests
	// are in flight together, so only reservations stop the overshoot.
	cap := 0.004
	h := newHarness(t, func(c *Config) { c.CapUSD = cap })
	h.up.delay = 50 * time.Millisecond
	var wg sync.WaitGroup
	var okN, refusedN atomic.Int64
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := []string{"B-reader", "C-ingest", "D-judge", "E-sleep"}[i%4]
			status, _, _ := h.post(t, "/tag/"+tag+"/v1/chat/completions", "", capBody)
			switch status {
			case 200:
				okN.Add(1)
			case 402:
				refusedN.Add(1)
			default:
				t.Errorf("status %d", status)
			}
		}(i)
	}
	wg.Wait()
	c := readCost(t, h.conf.CostPath)
	if c.SpentUSD > cap {
		t.Fatalf("spent %v > cap %v", c.SpentUSD, cap)
	}
	if okN.Load() == 0 || refusedN.Load() == 0 {
		t.Fatalf("expected both admitted and refused calls: ok %d refused %d", okN.Load(), refusedN.Load())
	}
	if okN.Load() != h.up.calls.Load() {
		t.Errorf("admitted %d upstream %d", okN.Load(), h.up.calls.Load())
	}
	// Totals file equals the sum of the log.
	recs := readLog(t, h.conf.LogPath)
	if len(recs) != 80 {
		t.Fatalf("log has %d records, want 80", len(recs))
	}
	var sum float64
	byTag := map[string]float64{}
	seqs := map[int64]bool{}
	for _, r := range recs {
		sum += r.CostUSD
		byTag[r.Tag] += r.CostUSD
		seqs[r.Seq] = true
	}
	if len(seqs) != 80 {
		t.Error("sequence numbers not unique")
	}
	if !near(sum, c.Total.CostUSD) || !near(sum, float64(okN.Load())*perCall) {
		t.Errorf("log sum %v, totals %v, ok*perCall %v", sum, c.Total.CostUSD, float64(okN.Load())*perCall)
	}
	for tag, v := range byTag {
		if !near(v, c.ByTag[tag].CostUSD) {
			t.Errorf("tag %s: log %v totals %v", tag, v, c.ByTag[tag].CostUSD)
		}
	}
	h.p.mu.Lock()
	reserved := h.p.reserved
	h.p.mu.Unlock()
	if reserved != 0 {
		t.Errorf("reservations leaked: %v", reserved)
	}
}

func TestOffPeakOnlyAndWindow(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.OffPeakOnly = true; c.PeakGuard = 15 * time.Minute })
	h.setNow(peakTime)
	status, body, _ := h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 403 || errCode(body) != "outside_window" {
		t.Errorf("peak: %d %v", status, body)
	}
	h.setNow(time.Date(2026, 11, 16, 0, 50, 0, 0, time.UTC)) // Monday, peak at 01:00
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 403 {
		t.Errorf("guard: %d", status)
	}
	h.setNow(time.Date(2026, 11, 16, 0, 40, 0, 0, time.UTC))
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Errorf("before guard: %d", status)
	}
	h.setNow(time.Date(2026, 11, 16, 4, 0, 0, 0, time.UTC)) // gap 04:00-06:00 is off-peak
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Errorf("04:00: %d", status)
	}
	h.setNow(time.Date(2026, 11, 14, 2, 0, 0, 0, time.UTC)) // Saturday 02:00 off-peak
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Errorf("weekend: %d", status)
	}
	if h.up.calls.Load() != 3 {
		t.Errorf("upstream calls %d", h.up.calls.Load())
	}

	w, err := ParseDailyWindow("22:00-06:00")
	if err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, func(c *Config) { c.Window = w })
	h2.setNow(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	if status, _, _ := h2.post(t, "/v1/chat/completions", "t", simpleBody); status != 403 {
		t.Errorf("outside -window: %d", status)
	}
	h2.setNow(time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC))
	if status, _, _ := h2.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Errorf("inside -window: %d", status)
	}
}

func TestUnverifiedSpend(t *testing.T) {
	h := newHarness(t, nil)
	h.up.respond = func(map[string]any) (int, string) { return 200, `{"choices":[]}` }
	status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 200 {
		t.Fatal(status)
	}
	r := readLog(t, h.conf.LogPath)[0]
	if r.UnverifiedUSD <= 0 || r.UnverifiedUSD != r.EstimateUSD || r.CostUSD != 0 {
		t.Errorf("usage missing should keep the reservation: %+v", r)
	}
	// Upstream error status: nothing billed.
	h.up.respond = func(map[string]any) (int, string) { return 500, `{"error":{"message":"boom"}}` }
	status, _, _ = h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 500 {
		t.Errorf("status not passed through: %d", status)
	}
	// Upstream hangs up after reading the request: 502, reservation kept
	// (the call may have been billed).
	h.up.respond = func(map[string]any) (int, string) { return -1, "" }
	status, body, _ := h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 502 || errCode(body) != "upstream_error" {
		t.Errorf("hang-up: %d %v", status, body)
	}
	// Upstream unreachable (dial error): 502, nothing billed.
	h.up.srv.Close()
	status, body, _ = h.post(t, "/v1/chat/completions", "t", simpleBody)
	if status != 502 || errCode(body) != "upstream_error" {
		t.Errorf("dial error: %d %v", status, body)
	}
	recs := readLog(t, h.conf.LogPath)
	if recs[1].CostUSD != 0 || recs[1].UnverifiedUSD != 0 || recs[1].Status != 500 {
		t.Errorf("500 record: %+v", recs[1])
	}
	if recs[2].UnverifiedUSD <= 0 {
		t.Errorf("hang-up record: %+v", recs[2])
	}
	if recs[3].UnverifiedUSD != 0 {
		t.Errorf("dial error record: %+v", recs[3])
	}
	c := readCost(t, h.conf.CostPath)
	if !near(c.SpentUSD, recs[0].UnverifiedUSD+recs[2].UnverifiedUSD) || c.Total.Errors != 3 {
		t.Errorf("spent %v errors %d", c.SpentUSD, c.Total.Errors)
	}
}

func TestKeepAliveBlankLinesBeforeBody(t *testing.T) {
	h := newHarness(t, nil)
	h.up.respond = func(map[string]any) (int, string) { return 200, "\n\n" + usageJSON("ok", 10, 0, 5, "stop") }
	if status, _, _ := h.post(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Fatal(status)
	}
	if r := readLog(t, h.conf.LogPath)[0]; r.PromptTokens != 10 || r.UnverifiedUSD != 0 {
		t.Errorf("%+v", r)
	}
}

func TestPriceTableValidation(t *testing.T) {
	bad := []string{
		`{"models":{}}`,
		`{"models":{"m":{"peak":{"input_cache_hit":0,"input_cache_miss":1,"output":1},"offpeak":{"input_cache_hit":1,"input_cache_miss":1,"output":1}}}}`,
		`{"models":{"m":{"peak":{"input_cache_hit":1,"input_cache_miss":1,"output":1},"offpeak":{"input_cache_hit":1,"input_cache_miss":1,"output":1}}},"peak_windows_utc":[{"days":"Mon-Fri","start":"10:00","end":"09:00"}]}`,
		`{"models":{"m":{"peak":{"input_cache_hit":1,"input_cache_miss":1,"output":1},"offpeak":{"input_cache_hit":1,"input_cache_miss":1,"output":1}}},"extra":1}`,
	}
	for _, b := range bad {
		if _, err := ParsePriceTable([]byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
	pt := testPrices(t)
	if pt.IsPeak(offPeakTime) || !pt.IsPeak(peakTime) {
		t.Error("peak detection")
	}
	if !pt.IsPeak(time.Date(2026, 11, 16, 9, 59, 0, 0, time.UTC)) || pt.IsPeak(time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)) {
		t.Error("window end is exclusive")
	}
	// A call that starts off-peak and ends in peak is billed at peak.
	_, w, _ := pt.RateFor("deepseek-flash", time.Date(2026, 11, 16, 0, 59, 0, 0, time.UTC), time.Date(2026, 11, 16, 1, 1, 0, 0, time.UTC))
	if w != "peak" {
		t.Errorf("straddling call window %s", w)
	}
}

// TestChandaSleepClientThroughProxy drives Chanda's own sleep-phase-2 client
// (internal/llm, response_format auto) through the proxy: the json_schema
// attempt is answered locally with a 400 naming response_format, Chanda falls
// back to json_object, and only that call reaches upstream.
func TestChandaSleepClientThroughProxy(t *testing.T) {
	h := newHarness(t, nil)
	h.up.respond = func(m map[string]any) (int, string) {
		return 200, usageJSON(`{"ok":true}`, 300, 0, 10, "stop")
	}
	cfg := config.Default().LLM
	cfg.Endpoint = h.srv.URL + "/tag/E-sleep/v1"
	cfg.Model = "deepseek-flash"
	cfg.ResponseFormat = "auto"
	p, err := llm.NewOpenAICompatible(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(context.Background(), core.LLMRequest{
		System:     "Answer in JSON.",
		Prompt:     "Say ok.",
		JSONSchema: []byte(`{"type":"object","required":["ok"],"additionalProperties":false,"properties":{"ok":{"type":"boolean"}}}`),
		MaxTokens:  50,
	})
	if err != nil {
		t.Fatalf("chanda client through proxy: %v", err)
	}
	if !bytes.Contains(resp.JSON, []byte(`"ok":true`)) || resp.InputTokens != 300 {
		t.Errorf("resp %+v", resp)
	}
	if h.up.calls.Load() != 1 {
		t.Errorf("upstream calls %d, want 1 (json_object only)", h.up.calls.Load())
	}
	rf, _ := h.up.bodies[0]["response_format"].(map[string]any)
	if rf["type"] != "json_object" {
		t.Errorf("forwarded response_format %v", rf)
	}
	c := readCost(t, h.conf.CostPath)
	if c.ByTag["E-sleep"].Calls != 1 || c.ByTag["E-sleep"].Rejected != 1 {
		t.Errorf("E-sleep totals %+v", c.ByTag["E-sleep"])
	}
}

// --- record/replay store ---

func newStoreHarness(t *testing.T, replay bool, mod func(*Config)) (*harness, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "llmstore")
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) {
		c.Store = st
		c.Replay = replay
		if mod != nil {
			mod(c)
		}
	})
	return h, dir
}

func (h *harness) postRaw(t *testing.T, path, tag, body string) (int, []byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer client-junk")
	if tag != "" {
		req.Header.Set(TagHeader, tag)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

// storeFiles lists the entry files and leftover temp files of a store.
func storeFiles(t *testing.T, dir string) (entries, temps []string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			temps = append(temps, p)
		} else {
			entries = append(entries, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries, temps
}

// withModel adds the response model and system_fingerprint to a usageJSON body.
func withModel(s string) string {
	return strings.Replace(s, `"object":"chat.completion",`, `"object":"chat.completion","model":"deepseek-v4-flash","system_fingerprint":"fp_test",`, 1)
}

func TestStoreRecordAndReplay(t *testing.T) {
	h, dir := newStoreHarness(t, true, nil)
	// Leading blank lines and HTML-ish characters must survive verbatim.
	upBody := "\n\n" + withModel(usageJSON("stored <answer> & more", 1000, 600, 200, "stop"))
	h.up.respond = func(map[string]any) (int, string) { return 200, upBody }

	status, first, hdr := h.postRaw(t, "/v1/chat/completions", "A-reader", simpleBody)
	if status != 200 || string(first) != upBody {
		t.Fatalf("first call: %d %q", status, first)
	}
	if hdr.Get(ReplayHeader) != "miss" {
		t.Errorf("first call replay header %q, want miss", hdr.Get(ReplayHeader))
	}
	entries, temps := storeFiles(t, dir)
	if len(entries) != 1 || len(temps) != 0 {
		t.Fatalf("store files %v temps %v", entries, temps)
	}
	rec := readLog(t, h.conf.LogPath)[0]
	if !rec.Stored {
		t.Error("forwarded record should say stored")
	}
	key := rec.RequestSHA256
	if want := filepath.Join(dir, key[:2], key+".json.gz"); entries[0] != want {
		t.Errorf("entry path %s, want %s", entries[0], want)
	}
	e, err := (&Store{dir: dir}).Get(key)
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := json.Marshal(h.up.bodies[0])
	var fwd map[string]any
	_ = json.Unmarshal(e.Request, &fwd)
	resent, _ := json.Marshal(fwd)
	if string(sent) != string(resent) || fwd["temperature"] != float64(0) {
		t.Errorf("stored request %s, upstream saw %s", e.Request, sent)
	}
	if e.Version != storeVersion || e.Key != key || e.Model != "deepseek-flash" || e.UpstreamPath != "/chat/completions" ||
		e.FirstTag != "A-reader" || e.Time != rec.Time || e.PriceWindow != "offpeak" || e.FinishReason != "stop" ||
		e.ResponseModel != "deepseek-v4-flash" || e.SystemFingerprint != "fp_test" || e.ContentType != "application/json" {
		t.Errorf("entry fields: %+v", e)
	}
	if e.Usage != (StoreUsage{PromptTokens: 1000, CacheHitTokens: 600, CacheMissTokens: 400, CompletionTokens: 200}) || !near(e.CostUSD, perCall) {
		t.Errorf("entry usage %+v cost %v", e.Usage, e.CostUSD)
	}
	if e.Response != upBody {
		t.Errorf("stored response not verbatim: %q", e.Response)
	}

	// Same body under another tag, with a temperature the proxy overrides
	// anyway: same forwarded body, so a hit. No upstream call.
	other := strings.Replace(simpleBody, `"temperature":0.7`, `"temperature":1.3`, 1)
	status, second, hdr := h.postRaw(t, "/tag/B-reader/v1/chat/completions", "", other)
	if status != 200 || string(second) != string(first) {
		t.Fatalf("replay: %d %q, want byte-identical %q", status, second, first)
	}
	if hdr.Get(ReplayHeader) != "hit" || hdr.Get("Content-Type") != "application/json" || hdr.Get("X-Lmeproxy-Cost-Usd") != "0.00000000" {
		t.Errorf("replay headers %v", hdr)
	}
	if h.up.calls.Load() != 1 {
		t.Errorf("upstream calls %d, want 1", h.up.calls.Load())
	}
	recs := readLog(t, h.conf.LogPath)
	r := recs[1]
	if r.Outcome != OutcomeReplayed || r.Tag != "B-reader" || r.Status != 200 || r.CostUSD != 0 || r.EstimateUSD != 0 ||
		!near(r.SavedUSD, perCall) || r.PromptTokens != 1000 || r.CompletionTokens != 200 || r.RequestSHA256 != key || r.FinishReason != "stop" {
		t.Errorf("replay record %+v", r)
	}

	// A different body is a miss and goes upstream.
	status, _, hdr = h.postRaw(t, "/v1/chat/completions", "A-reader", capBody)
	if status != 200 || hdr.Get(ReplayHeader) != "miss" || h.up.calls.Load() != 2 {
		t.Errorf("miss: %d %q upstream %d", status, hdr.Get(ReplayHeader), h.up.calls.Load())
	}
	if entries, _ := storeFiles(t, dir); len(entries) != 2 {
		t.Errorf("store entries %d, want 2", len(entries))
	}

	c := readCost(t, h.conf.CostPath)
	if c.Total.Calls != 2 || c.Total.Replayed != 1 || !near(c.Total.SavedUSD, perCall) || !near(c.Total.CostUSD, 2*perCall) ||
		!near(c.SpentUSD, 2*perCall) || c.Total.PromptTokens != 2000 {
		t.Errorf("total %+v spent %v", c.Total, c.SpentUSD)
	}
	if b := c.ByTag["B-reader"]; b == nil || b.Calls != 0 || b.Replayed != 1 || !near(b.SavedUSD, perCall) || b.CostUSD != 0 || b.PromptTokens != 0 {
		t.Errorf("B-reader totals %+v", b)
	}
	if a := c.ByTag["A-reader"]; a == nil || a.Calls != 2 || a.Replayed != 0 || a.SavedUSD != 0 {
		t.Errorf("A-reader totals %+v", a)
	}
}

func TestStoreRecordsWithoutReplay(t *testing.T) {
	h, dir := newStoreHarness(t, false, nil)
	for _, tag := range []string{"first", "second"} {
		status, _, hdr := h.postRaw(t, "/v1/chat/completions", tag, simpleBody)
		if status != 200 || hdr.Get(ReplayHeader) != "" {
			t.Fatalf("%s: %d replay header %q", tag, status, hdr.Get(ReplayHeader))
		}
	}
	if h.up.calls.Load() != 2 {
		t.Errorf("without -replay every call goes upstream: %d", h.up.calls.Load())
	}
	entries, _ := storeFiles(t, dir)
	if len(entries) != 1 {
		t.Fatalf("entries %v", entries)
	}
	recs := readLog(t, h.conf.LogPath)
	if !recs[0].Stored || recs[1].Stored {
		t.Errorf("stored flags %v %v, want true false", recs[0].Stored, recs[1].Stored)
	}
	e, err := (&Store{dir: dir}).Get(recs[0].RequestSHA256)
	if err != nil || e.FirstTag != "first" {
		t.Errorf("first tag kept: %v %+v", err, e)
	}
	if _, err := NewProxy(Config{Prices: testPrices(t), CapUSD: 1, Thinking: "disabled", Replay: true,
		LogPath: filepath.Join(t.TempDir(), "l"), CostPath: filepath.Join(t.TempDir(), "c")}); err == nil {
		t.Error("replay without a store accepted")
	}
}

func TestStoreSkipsUnstorable(t *testing.T) {
	h, dir := newStoreHarness(t, true, nil)
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"server error", 500, `{"error":{"message":"boom"}}`},
		{"rate limited", 429, `{"error":{"message":"slow down"}}`},
		{"blank content", 200, usageJSON("  \n ", 100, 0, 1, "stop")},
		{"empty content", 200, usageJSON("", 100, 0, 1, "stop")},
		{"null content", 200, strings.Replace(usageJSON("x", 100, 0, 1, "stop"), `"content":"x"`, `"content":null`, 1)},
		{"content filter", 200, usageJSON("partial", 100, 0, 1, "content_filter")},
		{"refusal", 200, strings.Replace(usageJSON("x", 100, 0, 1, "stop"), `"content":"x"`, `"content":"x","refusal":"I can't help with that"`, 1)},
		{"no usage", 200, `{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`},
		{"no choices", 200, `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`},
	}
	for i, c := range cases {
		h.up.respond = func(map[string]any) (int, string) { return c.status, c.body }
		body := `{"model":"deepseek-flash","messages":[{"role":"user","content":"case ` + itoa(i) + `"}]}`
		for round := 0; round < 2; round++ { // the second round must not hit
			status, _, hdr := h.postRaw(t, "/v1/chat/completions", "t", body)
			if status != c.status || hdr.Get(ReplayHeader) != "miss" {
				t.Errorf("%s round %d: %d %q", c.name, round, status, hdr.Get(ReplayHeader))
			}
		}
	}
	if want := int64(2 * len(cases)); h.up.calls.Load() != want {
		t.Errorf("upstream calls %d, want %d", h.up.calls.Load(), want)
	}
	// Calls refused by the window or the cap never reach upstream and are
	// not stored either.
	h2, dir2 := newStoreHarness(t, false, func(c *Config) { c.OffPeakOnly = true; c.CapUSD = 0.0001 })
	h2.setNow(peakTime)
	if status, _, _ := h2.postRaw(t, "/v1/chat/completions", "t", simpleBody); status != 403 {
		t.Errorf("peak: %d", status)
	}
	h2.setNow(offPeakTime)
	if status, _, _ := h2.postRaw(t, "/v1/chat/completions", "t", simpleBody); status != 402 {
		t.Errorf("cap: %d", status)
	}
	for _, d := range []string{dir, dir2} {
		if entries, temps := storeFiles(t, d); len(entries)+len(temps) != 0 {
			t.Errorf("store %s not empty: %v %v", d, entries, temps)
		}
	}
	for _, r := range readLog(t, h.conf.LogPath) {
		if r.Stored {
			t.Errorf("record marked stored: %+v", r)
		}
	}
}

func TestReplayIgnoresCapAndPeak(t *testing.T) {
	// Record off-peak with an ample cap.
	h, dir := newStoreHarness(t, false, nil)
	if status, _, _ := h.postRaw(t, "/v1/chat/completions", "rec", simpleBody); status != 200 {
		t.Fatal(status)
	}
	// Another run: cap already reached, -offpeak-only, at peak.
	cost := filepath.Join(t.TempDir(), "cost.json")
	prev := CostFile{CapUSD: 1, Total: Totals{Calls: 3, CostUSD: 1}, ByTag: map[string]*Totals{"old": {Calls: 3, CostUSD: 1}}}
	b, _ := json.Marshal(prev)
	if err := os.WriteFile(cost, b, 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := OpenStore(dir)
	h2 := newHarness(t, func(c *Config) {
		c.Store, c.Replay, c.CostPath, c.CapUSD, c.OffPeakOnly, c.PeakGuard = st, true, cost, 1, true, 15*time.Minute
	})
	h2.setNow(peakTime)
	for i := 0; i < 5; i++ {
		status, _, hdr := h2.postRaw(t, "/v1/chat/completions", "replay", simpleBody)
		if status != 200 || hdr.Get(ReplayHeader) != "hit" {
			t.Fatalf("replay at peak with the cap reached: %d %q", status, hdr.Get(ReplayHeader))
		}
	}
	// A miss is still refused (peak first, then the cap off-peak).
	if status, _, _ := h2.postRaw(t, "/v1/chat/completions", "replay", capBody); status != 403 {
		t.Errorf("miss at peak: %d", status)
	}
	h2.setNow(offPeakTime)
	if status, _, _ := h2.postRaw(t, "/v1/chat/completions", "replay", capBody); status != 402 {
		t.Errorf("miss with the cap reached: %d", status)
	}
	if h2.up.calls.Load() != 0 {
		t.Errorf("upstream calls %d", h2.up.calls.Load())
	}
	h2.p.mu.Lock()
	reserved := h2.p.reserved
	h2.p.mu.Unlock()
	c := readCost(t, cost)
	if reserved != 0 || !near(c.SpentUSD, 1) || c.Total.Replayed != 5 || !near(c.Total.SavedUSD, 5*perCall) || c.Total.Refused != 2 || c.Total.Calls != 3 {
		t.Errorf("reserved %v cost %+v spent %v", reserved, c.Total, c.SpentUSD)
	}
}

func TestStoreConcurrentIdentical(t *testing.T) {
	h, dir := newStoreHarness(t, true, nil)
	h.up.delay = 30 * time.Millisecond
	const n = 24
	var wg sync.WaitGroup
	bodies := make([][]byte, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, b, _ := h.postRaw(t, "/v1/chat/completions", "c"+itoa(i), simpleBody)
			if status != 200 {
				t.Errorf("status %d", status)
			}
			bodies[i] = b
		}(i)
	}
	wg.Wait()
	entries, temps := storeFiles(t, dir)
	if len(entries) != 1 || len(temps) != 0 {
		t.Fatalf("entries %v temps %v", entries, temps)
	}
	stored := 0
	for _, r := range readLog(t, h.conf.LogPath) {
		if r.Stored {
			stored++
		}
	}
	if stored != 1 {
		t.Errorf("%d records claim the new entry, want 1", stored)
	}
	key := readLog(t, h.conf.LogPath)[0].RequestSHA256
	e, err := (&Store{dir: dir}).Get(key)
	if err != nil || !strings.HasPrefix(e.FirstTag, "c") {
		t.Fatalf("entry: %v %+v", err, e)
	}
	before := h.up.calls.Load()
	status, b, hdr := h.postRaw(t, "/v1/chat/completions", "after", simpleBody)
	if status != 200 || hdr.Get(ReplayHeader) != "hit" || string(b) != string(bodies[0]) || h.up.calls.Load() != before {
		t.Errorf("replay after concurrent recording: %d %q", status, hdr.Get(ReplayHeader))
	}
}

func TestStoreCorruptEntryIsAMiss(t *testing.T) {
	h, dir := newStoreHarness(t, true, nil)
	if status, _, _ := h.postRaw(t, "/v1/chat/completions", "t", simpleBody); status != 200 {
		t.Fatal(status)
	}
	entries, _ := storeFiles(t, dir)
	if err := os.WriteFile(entries[0], []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _, hdr := h.postRaw(t, "/v1/chat/completions", "t", simpleBody)
	if status != 200 || hdr.Get(ReplayHeader) != "miss" || h.up.calls.Load() != 2 {
		t.Fatalf("corrupt entry: %d %q upstream %d", status, hdr.Get(ReplayHeader), h.up.calls.Load())
	}
	// The forwarded call rewrote the entry, so the next one hits.
	if _, _, hdr := h.postRaw(t, "/v1/chat/completions", "t", simpleBody); hdr.Get(ReplayHeader) != "hit" {
		t.Errorf("entry not repaired: %q", hdr.Get(ReplayHeader))
	}
}

func TestStoreStats(t *testing.T) {
	h, dir := newStoreHarness(t, false, nil)
	h.postRaw(t, "/v1/chat/completions", "A-reader", simpleBody)
	h.postRaw(t, "/v1/chat/completions", "D-judge", capBody)
	h.postRaw(t, "/v1/chat/completions", "D-judge", capBody) // already stored
	st, err := (&Store{dir: dir}).Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 2 || st.Bytes <= 0 || st.Unreadable != 0 || !near(st.CostUSD, 2*perCall) {
		t.Errorf("stats %+v", st)
	}
	if st.ByModel["deepseek-flash"].Entries != 2 || st.ByFirstTag["A-reader"].Entries != 1 || st.ByFirstTag["D-judge"].Entries != 1 {
		t.Errorf("groups %+v %+v", st.ByModel, st.ByFirstTag)
	}
	var buf bytes.Buffer
	st.WriteText(&buf)
	if !strings.Contains(buf.String(), "2 entries") || !strings.Contains(buf.String(), "D-judge") {
		t.Errorf("text:\n%s", buf.String())
	}
	if err := run([]string{"-store", dir, "-store-stats"}); err != nil {
		t.Errorf("-store-stats: %v", err)
	}
	if err := run([]string{"-replay"}); err == nil {
		t.Error("-replay without -store accepted")
	}
}
