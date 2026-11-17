package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/felipemarts/chanda/internal/core"
)

// Agentic search (decision 0151). Before context and search, the LLM turns
// the query into search terms: names, dates, numbers, the topic and a few
// related words. The read path then runs on the query followed by those
// terms, so the caller's own words always stay first and are never lost.
// The read package itself stays free of LLMs (principle 2): this step is
// here, before it, and any failure falls back to the plain query.

const agenticSystem = "You turn a message sent to an AI agent into search terms for the agent's long-term memory " +
	"(a keyword index plus embeddings). Return the words the stored memories that answer it would contain: " +
	"names, places, dates, numbers, the topic, and a few synonyms or closely related words. " +
	"Write in the language of the message. Do not answer the message and do not invent facts it does not state. " +
	"At most 24 words, no sentence."

var agenticSchema = []byte(`{"type":"object","properties":{"terms":{"type":"string","maxLength":400}},` +
	`"required":["terms"],"additionalProperties":false}`)

// agenticMaxTokens is the output ceiling of the call: 24 words fit easily.
const agenticMaxTokens = 200

// Notices of agentic search.
const (
	noticeAgentic       = "agentic_search: the query was expanded with LLM search terms"
	noticeAgenticFailed = "agentic_search_failed: the LLM call failed, the query was searched as given"
)

// agenticQuery returns the query the read path searches: q followed by the
// LLM's search terms when read.agentic_search is on and an LLM is
// configured, else q unchanged.
func (a *App) agenticQuery(ctx context.Context, c *call, q string) string {
	if a.llm == nil || !a.e.Cfg.Read.Agentic() || strings.TrimSpace(q) == "" {
		return q
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(a.e.Cfg.Read.AgenticTimeout())*time.Millisecond)
	defer cancel()
	resp, err := a.llm.Complete(ctx, core.LLMRequest{
		System: agenticSystem, Prompt: q, JSONSchema: agenticSchema, MaxTokens: agenticMaxTokens,
	})
	var out struct {
		Terms string `json:"terms"`
	}
	if err == nil {
		err = json.Unmarshal(resp.JSON, &out)
	}
	if err != nil {
		c.notice(noticeAgenticFailed)
		if a.opt.Logf != nil {
			a.opt.Logf("agentic search: %v", err)
		}
		return q
	}
	terms := strings.Join(strings.Fields(out.Terms), " ")
	if terms == "" || strings.EqualFold(terms, strings.Join(strings.Fields(q), " ")) {
		return q
	}
	c.notice(noticeAgentic)
	return q + "\n" + terms
}
