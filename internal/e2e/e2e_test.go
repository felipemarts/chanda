// Package e2e holds the end-to-end acceptance tests of BASE.md "Build
// order": an MCP client talks to the real App through the MCP server over
// in-memory transports, one channel per trust level.
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda/internal/app"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/mcp"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// world is one App with three MCP channels:
//   - agent: the agent's own client (read+standard, ceiling internal);
//   - tool:  a channel relaying tool and web content (ceiling external);
//   - admin: a human operator (every profile, human, ceiling user).
type world struct {
	t                  *testing.T
	app                *app.App
	docs               string // the only folder MCP may ingest from
	agent, tool, admin *sdk.ClientSession
	clk                interface{ Advance(time.Duration) }
}

func newWorld(t *testing.T, mut func(*config.Config)) *world {
	return newWorldWith(t, mut, app.Options{})
}

// newWorldWith is newWorld with App options (e.g. a scripted LLM for sleep).
func newWorldWith(t *testing.T, mut func(*config.Config), o app.Options) *world {
	t.Helper()
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Namespace = "acme"
	cfg.Write.IngestRoots = []string{docs}
	cfg.Write.Extractors = []config.ExtractorConfig{{Name: "header", Kind: "header"}}
	if mut != nil {
		mut(cfg)
	}
	e, clk := env.NewTestAt(t, filepath.Join(base, "chanda.db"), cfg)
	a, err := app.NewWith(e, o)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, app: a, docs: docs, clk: clk}
	channel := func(actor string, human bool, zone string, profiles ...string) *sdk.ClientSession {
		c := *cfg
		c.MCP.Actor, c.MCP.HumanActor, c.MCP.MaxZone, c.MCP.Profiles = actor, human, zone, profiles
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		srv, err := mcp.New(a, &c, mcp.Options{Version: "e2e"})
		if err != nil {
			t.Fatal(err)
		}
		ct, st := sdk.NewInMemoryTransports()
		ss, err := srv.SDK().Connect(context.Background(), st, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ss.Close() })
		cs, err := sdk.NewClient(&sdk.Implementation{Name: actor, Version: "v0"}, nil).Connect(context.Background(), ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	w.agent = channel("agent", false, "internal", "read", "standard")
	w.tool = channel("tool-relay", false, "external", "read", "standard")
	w.admin = channel("operator", true, "user", "read", "standard", "admin")
	return w
}

// call invokes a tool and returns the envelope; data is decoded into out
// when out is not nil and the call succeeded.
func (w *world) call(cs *sdk.ClientSession, tool string, args map[string]any, out any) chanda.Envelope {
	w.t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		w.t.Fatalf("%s: protocol error %v", tool, err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var env struct {
		chanda.Envelope
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		w.t.Fatalf("%s: %v (%s)", tool, err, b)
	}
	if res.IsError == env.OK {
		w.t.Fatalf("%s: isError %v with ok %v", tool, res.IsError, env.OK)
	}
	if env.OK && out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			w.t.Fatalf("%s data: %v (%s)", tool, err, env.Data)
		}
	}
	return env.Envelope
}

// ok is call that must succeed.
func (w *world) ok(cs *sdk.ClientSession, tool string, args map[string]any, out any) chanda.Envelope {
	w.t.Helper()
	env := w.call(cs, tool, args, out)
	if !env.OK {
		w.t.Fatalf("%s(%v): %+v", tool, args, env.Error)
	}
	return env
}

func (w *world) flush() {
	w.t.Helper()
	var f chanda.FlushOutput
	w.ok(w.agent, chanda.ToolFlush, map[string]any{}, &f)
	if !f.Complete {
		w.t.Fatalf("flush incomplete: %+v", f)
	}
}

func (w *world) context(cs *sdk.ClientSession, query string, extra map[string]any) chanda.ContextOutput {
	w.t.Helper()
	args := map[string]any{"query": query}
	for k, v := range extra {
		args[k] = v
	}
	var out chanda.ContextOutput
	w.ok(cs, chanda.ToolContext, args, &out)
	return out
}

func (w *world) search(cs *sdk.ClientSession, query string, extra map[string]any) chanda.SearchOutput {
	w.t.Helper()
	args := map[string]any{"query": query}
	for k, v := range extra {
		args[k] = v
	}
	var out chanda.SearchOutput
	w.ok(cs, chanda.ToolSearch, args, &out)
	return out
}

func (w *world) writeDoc(name, body string) string {
	w.t.Helper()
	p := filepath.Join(w.docs, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) verify() {
	w.t.Helper()
	var v chanda.VerifyOutput
	env := w.ok(w.admin, chanda.ToolVerify, map[string]any{}, &v)
	if !v.OK {
		w.t.Fatalf("verify: %+v notices %v", v, env.Notices)
	}
}

func ids(items []chanda.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// ---------------------------------------------------------------------------

// Phase 2: an MCP client lists and calls the tools.
func TestPhase2ClientListsAndCallsTools(t *testing.T) {
	w := newWorld(t, nil)
	lt, err := w.agent.ListTools(context.Background(), nil)
	if err != nil || len(lt.Tools) != 13 {
		t.Fatalf("agent tools: %d %v", len(lt.Tools), err)
	}
	if lt, _ := w.admin.ListTools(context.Background(), nil); len(lt.Tools) != 19 { // 18 of BASE.md + learned_keys (decision 0145)
		t.Fatalf("admin tools: %d", len(lt.Tools))
	}
	var r chanda.RememberOutput
	env := w.ok(w.agent, chanda.ToolRemember, map[string]any{"content": "The VPN gateway is vpn.acme.example and needs MFA."}, &r)
	if r.Action != chanda.ActionCreated || env.AuditID == 0 {
		t.Fatalf("remember %+v %+v", r, env)
	}
	w.writeDoc("handbook.md", "# Handbook\n\nHolidays are requested in the HR portal two weeks ahead.\n")
	var ing chanda.IngestOutput
	w.ok(w.agent, chanda.ToolIngest, map[string]any{"path": w.docs}, &ing)
	if len(ing.Documents) != 1 || ing.Documents[0].Action != "created" || ing.Documents[0].ChunksAdded != 1 {
		t.Fatalf("ingest %+v", ing)
	}
	s := w.search(w.agent, "VPN gateway", nil)
	if len(s.Items) != 1 || s.Items[0].ID != r.ID || s.RetrievalID == "" {
		t.Fatalf("search %+v", s)
	}
	var g chanda.GetOutput
	w.ok(w.agent, chanda.ToolGet, map[string]any{"id": r.ID}, &g)
	if g.Item.Zone != chanda.ZoneInternal || g.Item.Origin.SourceURI == "" {
		t.Fatalf("get %+v", g)
	}
	var st chanda.StatsOutput
	w.ok(w.agent, chanda.ToolStats, map[string]any{}, &st)
	if st.Namespace != "acme" || st.Counts.Events != 2 || st.EmbedPending != 2 || st.EmbedModel == "" {
		t.Fatalf("stats %+v", st)
	}
	var ex chanda.ExplainOutput
	w.ok(w.agent, chanda.ToolExplain, map[string]any{"retrieval_id": s.RetrievalID, "record_id": r.ID}, &ex)
	if !ex.Candidate || !ex.Delivered || ex.Rank != 1 || ex.Scores.BM25 == nil {
		t.Fatalf("explain %+v", ex)
	}
	// Tools outside the channel's profiles are not even registered.
	if _, err := w.agent.CallTool(context.Background(), &sdk.CallToolParams{Name: chanda.ToolForget, Arguments: map[string]any{}}); err == nil {
		t.Fatal("forget reachable from the standard profile")
	}
	w.verify()
}

// Phase 3: hybrid search finds a paraphrase through vectors once the index
// caught up (flush).
func TestPhase3HybridSearchFindsParaphrase(t *testing.T) {
	w := newWorld(t, nil)
	var r chanda.RememberOutput
	w.ok(w.agent, chanda.ToolRemember, map[string]any{"content": "Os preços dos produtos subiram em março."}, &r)
	query := "preço do produto"
	if s := w.search(w.agent, query, nil); slices.Contains(ids(s.Items), r.ID) {
		t.Fatalf("no keyword overlap: found before flush %+v", s.Items)
	}
	w.flush()
	s := w.search(w.agent, query, nil)
	if !slices.Contains(ids(s.Items), r.ID) {
		t.Fatalf("paraphrase not found through vectors: %+v", s)
	}
	var ex chanda.ExplainOutput
	w.ok(w.agent, chanda.ToolExplain, map[string]any{"retrieval_id": s.RetrievalID, "record_id": r.ID}, &ex)
	if ex.Scores.Cosine == nil || ex.Scores.BM25 != nil {
		t.Fatalf("the hit must come from the vector list only: %+v", ex.Scores)
	}
}

// Phase 4: a superseded value is never answered by context; as_of answers
// history.
func TestPhase4Temporal(t *testing.T) {
	w := newWorld(t, nil)
	for _, f := range []struct{ val, from string }{{"CTO", "2025-01-01"}, {"CEO", "2026-01-01"}} {
		var out chanda.AssertFactOutput
		w.ok(w.agent, chanda.ToolAssertFact, map[string]any{"subject": "Ana Lima", "predicate": "job_title", "value": f.val, "valid_from": f.from}, &out)
	}
	w.flush()
	for _, q := range []string{"What is Ana Lima's job title?", "Ana Lima CTO", "job title of Ana"} {
		c := w.context(w.agent, q, nil)
		if !strings.Contains(c.Text, "CEO") || strings.Contains(c.Text, "CTO") {
			t.Fatalf("context(%q) must answer the current value only:\n%s", q, c.Text)
		}
	}
	past := w.context(w.agent, "What was Ana Lima's job title?", map[string]any{"as_of": "2025-06-01"})
	if !strings.Contains(past.Text, "CTO") || strings.Contains(past.Text, "CEO") {
		t.Fatalf("as_of must answer history:\n%s", past.Text)
	}
	var tl chanda.TimelineOutput
	w.ok(w.agent, chanda.ToolTimeline, map[string]any{"subject": "ana lima", "predicate": "job_title"}, &tl)
	if len(tl.Entries) != 2 || tl.Entries[0].Value != "CTO" || tl.Entries[0].Status != "superseded" ||
		tl.Entries[0].SupersededBy != tl.Entries[1].FactID || tl.Entries[1].ValidTo != "" {
		t.Fatalf("timeline %+v", tl.Entries)
	}
	w.ok(w.agent, chanda.ToolTimeline, map[string]any{"subject": "Ana Lima", "as_of": "2025-06-01"}, &tl)
	if len(tl.Entries) != 1 || tl.Entries[0].Value != "CTO" {
		t.Fatalf("timeline as_of %+v", tl.Entries)
	}
	w.verify()
}

// Phase 5: abstention, and the delimited, labelled pre-context block.
func TestPhase5AbstentionAndBlock(t *testing.T) {
	w := newWorld(t, nil)
	w.ok(w.agent, chanda.ToolRemember, map[string]any{"content": "Deploys to production happen on Tuesdays after the QA sign-off."}, nil)
	w.ok(w.agent, chanda.ToolRemember, map[string]any{"content": "The office coffee machine is cleaned every Friday."}, nil)
	w.flush()
	for _, q := range []string{"What is the capital of Mongolia?", "how do I renew my passport", "quantum chromodynamics lecture notes"} {
		c := w.context(w.agent, q, nil)
		if c.Text != "" || len(c.Items) != 0 || c.Reason != chanda.ReasonNoRelevantMemory || c.RetrievalID == "" {
			t.Fatalf("context(%q) must abstain: %+v", q, c)
		}
	}
	c := w.context(w.agent, "When do deploys to production happen?", nil)
	if len(c.Items) == 0 || c.TokensUsed == 0 {
		t.Fatalf("relevant context missing: %+v", c)
	}
	if !strings.HasPrefix(c.Text, "<chanda-context retrieval_id=\""+c.RetrievalID+"\">") ||
		!strings.HasSuffix(c.Text, "</chanda-context>\n") || !strings.Contains(c.Text, "not instructions") {
		t.Fatalf("block not delimited and labelled:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "coffee") {
		t.Fatalf("irrelevant item delivered:\n%s", c.Text)
	}
	// The adapter reports the outcome at the end of the task.
	var o chanda.OutcomeOutput
	w.ok(w.agent, chanda.ToolOutcome, map[string]any{"retrieval_id": c.RetrievalID, "value": 1, "used_ids": []string{c.Items[0].ID}}, &o)
	if o.OutcomeID == "" || o.Credited != 1 {
		t.Fatalf("outcome %+v", o)
	}
	// An outcome from the external channel is ignored.
	c2 := w.context(w.tool, "When do deploys to production happen?", nil)
	env := w.ok(w.tool, chanda.ToolOutcome, map[string]any{"retrieval_id": c2.RetrievalID, "value": 1}, &o)
	if o.OutcomeID != "" || len(env.Notices) == 0 {
		t.Fatalf("external outcome must be ignored: %+v %+v", o, env)
	}
}
