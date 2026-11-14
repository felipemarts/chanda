package read

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// SearchInput is the free read (BASE.md mode "search").
type SearchInput struct {
	Namespace      string
	Query          string
	Types          []core.RecordType // event, fact, page; empty = all
	Zones          []core.Zone       // empty = all
	AsOf           time.Time         // zero = now
	Limit          int               // 0 = read.search_default_limit; capped at read.search_max_limit
	IncludeHistory bool              // also superseded facts
	SessionID      string
	Gate           bool // query made for the gate set: logged with params {"gate": true}
}

// SearchResult is a ranked list. instruction_like items are returned with
// the mark in Flags; external items carry their zone.
type SearchResult struct {
	RetrievalID string   `json:"retrieval_id"`
	Items       []Item   `json:"items"`
	Reason      string   `json:"reason,omitempty"`
	Notices     []string `json:"notices,omitempty"`
}

// ContextInput asks for the pre-context (BASE.md mode "context").
type ContextInput struct {
	Namespace    string
	Query        string
	BudgetTokens int       // 0 = read.context_default_tokens; capped at read.context_max_tokens
	AsOf         time.Time // zero = now
	SessionID    string
	Gate         bool // query made for the gate set: logged with params {"gate": true}
}

// ContextResult is the rendered pre-context. Text is empty when nothing is
// relevant (Reason = no_relevant_memory): the block is never filled with the
// least bad item (principle 8).
type ContextResult struct {
	RetrievalID string   `json:"retrieval_id"`
	Text        string   `json:"text"`
	Items       []Item   `json:"items"`
	Reason      string   `json:"reason,omitempty"`
	TokensUsed  int      `json:"tokens_used"`
	Notices     []string `json:"notices,omitempty"`
}

// Search runs the read path in search mode: no relevance cut, item limit,
// output by score, history on request.
func (r *Reader) Search(ctx context.Context, in SearchInput) (SearchResult, error) {
	limit := in.Limit
	if limit < 0 {
		return SearchResult{}, core.Errf(core.CodeInvalidArgument, "limit must be ≥ 0")
	}
	if limit == 0 {
		limit = r.e.Cfg.Read.SearchDefaultLimit
	}
	if m := r.e.Cfg.Read.SearchMaxLimit; m > 0 && limit > m {
		limit = m
	}
	out, err := r.run(ctx, request{
		mode: ModeSearch, ns: in.Namespace, text: in.Query, types: in.Types, zones: in.Zones,
		asOf: in.AsOf, limit: limit, includeHistory: in.IncludeHistory, sessionID: in.SessionID, gate: in.Gate,
	})
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{RetrievalID: out.retrievalID, Items: out.items, Reason: out.reason, Notices: out.notices}, nil
}

// Context runs the read path in context mode: relevance cut, token budget,
// facts then pages then raw excerpts, never instruction_like content,
// external items as labelled quotations, rendered in a delimited block.
func (r *Reader) Context(ctx context.Context, in ContextInput) (ContextResult, error) {
	budget := in.BudgetTokens
	if budget < 0 {
		return ContextResult{}, core.Errf(core.CodeInvalidArgument, "budget_tokens must be ≥ 0")
	}
	if budget == 0 {
		budget = r.e.Cfg.Read.ContextDefaultTokens
	}
	if m := r.e.Cfg.Read.ContextMaxTokens; m > 0 && budget > m {
		budget = m
	}
	out, err := r.run(ctx, request{
		mode: ModeContext, ns: in.Namespace, text: in.Query, asOf: in.AsOf, budget: budget, sessionID: in.SessionID, gate: in.Gate,
	})
	if err != nil {
		return ContextResult{}, err
	}
	return ContextResult{RetrievalID: out.retrievalID, Text: out.text, Items: out.items, Reason: out.reason,
		TokensUsed: out.tokens, Notices: out.notices}, nil
}

type request struct {
	mode           Mode
	ns, text       string
	types          []core.RecordType
	zones          []core.Zone
	asOf           time.Time
	limit, budget  int
	includeHistory bool
	sessionID      string
	gate           bool
}

type result struct {
	retrievalID string
	items       []Item
	reason      string
	notices     []string
	text        string
	tokens      int
}

// run is the whole read path (BASE.md read steps 1 to 11).
func (r *Reader) run(ctx context.Context, q request) (*result, error) {
	// 1. Receive the query.
	if q.ns == "" {
		q.ns = r.e.Cfg.Namespace
	}
	q.text = strings.TrimSpace(q.text)
	if q.text == "" {
		return nil, core.Errf(core.CodeInvalidArgument, "query is empty")
	}
	o := filterOpts{ns: q.ns, mode: q.mode, includeHistory: q.includeHistory && q.mode == ModeSearch}
	kf := keywordFilter{withSuperseded: o.includeHistory || !q.asOf.IsZero()}
	if len(q.types) > 0 {
		o.types, kf.types = map[core.RecordType]bool{}, map[core.RecordType]bool{}
		for _, t := range q.types {
			if !searchable(t) {
				return nil, core.Errf(core.CodeInvalidArgument, "unsupported record type %q", t)
			}
			o.types[t], kf.types[t] = true, true
		}
	}
	if len(q.zones) > 0 {
		o.zones = map[core.Zone]bool{}
		for _, z := range q.zones {
			if !z.Valid() {
				return nil, core.Errf(core.CodeInvalidArgument, "unknown zone %q", z)
			}
			if !o.zones[z] {
				kf.zones = append(kf.zones, z)
			}
			o.zones[z] = true
		}
		sort.Slice(kf.zones, func(i, j int) bool { return kf.zones[i] < kf.zones[j] })
	}
	now := r.e.Clock.Now()
	o.asOf = q.asOf.UTC()
	if q.asOf.IsZero() {
		o.asOf = now
	} else {
		o.asOfExplicit = true
	}
	res := &result{retrievalID: r.e.IDs.New(core.PrefixRetrieval)}
	terms := queryTerms(q.text, r.e.Cfg.Read.MaxQueryTerms)

	// 2. Keyword. 3. Vector.
	kw, err := r.keywordSearch(ctx, q.ns, terms, kf)
	if err != nil {
		return nil, err
	}
	lists := 1
	var vec []hit
	if r.vectorEnabled() {
		var typeList []core.RecordType
		for _, t := range []core.RecordType{core.RecEvent, core.RecFact, core.RecPage} {
			if o.types[t] {
				typeList = append(typeList, t)
			}
		}
		vec, err = r.vectorSearch(ctx, q.ns, q.text, typeList)
		if err != nil {
			res.notices = append(res.notices, "vector_search_unavailable")
			vec = nil
		} else {
			lists = 2
		}
	}

	// Candidates from both lists.
	byID := map[string]*cand{}
	var order []string
	get := func(t core.RecordType, id string) *cand {
		c := byID[id]
		if c == nil {
			c = &cand{rec: &record{id: id, typ: t}}
			byID[id] = c
			order = append(order, id)
		}
		return c
	}
	for _, t := range []core.RecordType{core.RecEvent, core.RecFact, core.RecPage} {
		for _, h := range kw[t] {
			c := get(t, h.id)
			c.kwRank, c.bm25, c.hasBM25 = h.rank, h.score, true
		}
	}
	for _, h := range vec {
		c := get(h.typ, h.id)
		if c.vecRank == 0 {
			c.vecRank, c.cosine, c.hasCos = h.rank, h.score, true
		}
	}
	byType := map[core.RecordType][]string{}
	for _, id := range order {
		byType[byID[id].rec.typ] = append(byType[byID[id].rec.typ], id)
	}
	recs, err := r.loadRecords(ctx, q.ns, byType)
	if err != nil {
		return nil, err
	}

	// 4. Rule filter, then near-duplicate collapse.
	eligible := map[string]*record{}
	for _, id := range order {
		c := byID[id]
		rec := recs[id]
		if rec != nil && rec.typ != c.rec.typ {
			rec = nil // id/type mismatch from an index: treat as unknown
		}
		if reason := ruleFilter(rec, o); reason != "" {
			c.dropped = reason
			continue
		}
		c.rec = rec
		eligible[id] = rec
	}
	rep, extra, err := r.collapseDuplicates(ctx, eligible, o)
	if err != nil {
		return nil, err
	}
	for _, id := range sortedKeys(extra) {
		c := get(extra[id].typ, id)
		c.rec = extra[id]
	}
	var alive []*cand
	for _, id := range order {
		c := byID[id]
		if c.dropped != "" {
			continue
		}
		if rp, ok := rep[id]; ok && rp != id {
			byID[rp].merge(c)
			c.dropped = dropDuplicate + ":" + rp
			continue
		}
		alive = append(alive, c)
	}
	for _, c := range alive {
		c.ageSec = ageSeconds(c.rec.ts, now)
	}

	// 5. Fuse. 6. Rerank. 7. Utility (and the shadow learned score).
	fuse(alive, r.e.Cfg.Read.RRFK, lists)
	r.computeEvidence(alive, terms)
	reranked, err := r.rerankStep(ctx, q.text, alive)
	if err != nil {
		res.notices = append(res.notices, "rerank_unavailable")
	}
	lambda, err := r.lambda(ctx, q.ns)
	if err != nil {
		res.notices = append(res.notices, "utility_unavailable")
		lambda = 0
	}
	if err := r.adjust(ctx, q.ns, alive, lambda); err != nil {
		res.notices = append(res.notices, "utility_unavailable")
		for _, c := range alive {
			c.final, c.hasUtil = c.relevance, false
		}
	}
	r.shadow(ctx, q.ns, alive)
	sortByFinal(alive)

	// 8. Cut (context only). Relevance and evidence only, never utility.
	if q.mode == ModeContext {
		kept := alive[:0:0]
		for _, c := range alive {
			switch {
			case c.relevance < r.e.Cfg.Read.RelevanceThreshold:
				c.dropped = dropCut
			case !r.hasEvidence(c):
				c.dropped = dropEvidence
			default:
				kept = append(kept, c)
			}
		}
		alive = kept
	}

	// 9. Diversify and respect the budget. 10. Assemble.
	if err := r.selectItems(ctx, q, res, alive); err != nil {
		return nil, err
	}

	// Namespace defence in depth: checked again before the response.
	res.items = r.guardNamespace(q.ns, res.items, byID, res)

	if q.mode == ModeContext {
		res.items = contextOrder(res.items)
		if len(res.items) > 0 {
			res.text = renderContext(res.retrievalID, res.items)
			res.tokens = estimateTokens(res.text)
		}
	}
	if len(res.items) == 0 && res.reason == "" {
		res.reason = ReasonNoRelevantMemory
	}

	// 11. Log.
	all := make([]*cand, 0, len(byID))
	for _, id := range order {
		all = append(all, byID[id])
	}
	if err := r.logRetrieval(ctx, q, res, all, now, map[string]any{
		"lambda": lambda, "lists": lists, "reranked": reranked, "terms": len(terms), "as_of": core.FormatTime(o.asOf),
	}); err != nil {
		return nil, err
	}
	if res.items == nil {
		res.items = []Item{}
	}
	return res, nil
}

// selectItems walks candidates in final order applying, per item: the
// search limit; for context, the inherited instruction_like exclusion; at
// most read.max_per_document items per source document; and, for context,
// the token budget (truncating an item only when at least
// read.context_min_item_tokens remain).
func (r *Reader) selectItems(ctx context.Context, q request, res *result, alive []*cand) error {
	perDoc := map[string]int{}
	maxPerDoc := r.e.Cfg.Read.MaxPerDocument
	used := 0
	if q.mode == ModeContext {
		used = estimateTokens(contextHeader(res.retrievalID)) + estimateTokens(contextFooter)
	}
	for _, c := range alive {
		if q.mode == ModeSearch && len(res.items) >= q.limit {
			c.dropped = dropLimit
			continue
		}
		it, docKey, err := r.buildItem(ctx, q.ns, c)
		if err != nil {
			return err
		}
		if q.mode == ModeContext && containsStr(it.Flags, core.FlagInstructionLike) {
			c.dropped = dropInstruction
			continue
		}
		if docKey != "" && maxPerDoc > 0 && perDoc[docKey] >= maxPerDoc {
			c.dropped = dropDiversity
			continue
		}
		if q.mode == ModeContext {
			remaining := q.budget - used
			block := renderItem(it)
			cost := estimateTokens(block)
			if cost > remaining {
				var ok bool
				if it, cost, ok = truncateToFit(it, remaining, r.e.Cfg.Read.ContextMinItemTokens); !ok {
					c.dropped = dropBudget
					continue
				}
			}
			used += cost
		}
		perDoc[docKey]++
		res.items = append(res.items, it)
	}
	if q.mode == ModeContext && len(res.items) == 0 {
		for _, c := range alive {
			if c.dropped == dropBudget {
				res.reason = ReasonBudgetTooSmall
				break
			}
		}
	}
	return nil
}

// guardNamespace drops any item whose namespace is not ns. The namespace is
// already filtered in every query; this is the second check BASE.md asks
// for ("applied in the query and again after the search").
func (r *Reader) guardNamespace(ns string, items []Item, byID map[string]*cand, res *result) []Item {
	out := items[:0:0]
	for _, it := range items {
		if it.namespace != ns {
			if c := byID[it.ID]; c != nil {
				c.dropped = dropNamespace
			}
			res.notices = append(res.notices, "namespace_violation_dropped")
			continue
		}
		out = append(out, it)
	}
	return out
}

// buildItem assembles one item: text, validity, origin (origin events, their
// sources and documents), zone, flags (own and inherited from origin events)
// and scores. It returns the diversity key: the smallest document id among
// the origin events, or "" when the item comes from no document.
func (r *Reader) buildItem(ctx context.Context, ns string, c *cand) (Item, string, error) {
	rec := c.rec
	it := Item{ID: rec.id, Type: rec.typ, Text: rec.text, Zone: rec.zone, Scores: c.scores(), namespace: rec.ns}
	switch rec.typ {
	case core.RecEvent:
		it.Kind, it.RecordedAt = rec.kind, rec.ts
	case core.RecFact:
		it.Status, it.Subject, it.Predicate, it.ValidFrom, it.ValidTo, it.RecordedAt =
			rec.status, rec.subject, rec.predicate, rec.validFrom, rec.validTo, rec.ts
	case core.RecPage:
		it.Kind, it.Title, it.Status, it.RecordedAt = rec.kind, rec.title, rec.status, rec.ts
	}
	origin, flags, docKey, err := r.resolveOrigin(ctx, ns, rec)
	if err != nil {
		return Item{}, "", err
	}
	it.Origin, it.Flags = origin, flags
	return it, docKey, nil
}

// resolveOrigin returns the origin of rec, the union of its own flags and its
// origin events' flags, and its diversity key.
func (r *Reader) resolveOrigin(ctx context.Context, ns string, rec *record) (Origin, []string, string, error) {
	evIDs, _, err := r.originEvents(ctx, ns, rec.typ, rec.id)
	if err != nil {
		return Origin{}, nil, "", err
	}
	evs := map[string]*record{rec.id: rec}
	if rec.typ != core.RecEvent {
		if evs, err = r.loadRecords(ctx, ns, map[core.RecordType][]string{core.RecEvent: evIDs}); err != nil {
			return Origin{}, nil, "", err
		}
	}
	flagSet := map[string]bool{}
	for _, f := range rec.flags {
		flagSet[f] = true
	}
	srcSet, docSet := map[string]bool{}, map[string]bool{}
	var o Origin
	for _, id := range evIDs {
		ev := evs[id]
		if ev == nil {
			continue
		}
		o.EventIDs = append(o.EventIDs, id)
		for _, f := range ev.flags {
			flagSet[f] = true
		}
		srcSet[ev.sourceID] = true
		if ev.docID != "" {
			docSet[ev.docID] = true
		}
	}
	o.DocIDs = sortedKeys(docSet)
	srcs := sortedKeys(srcSet)
	if o.SourceURIs, err = r.uris(ctx, "sources", ns, srcs); err != nil {
		return Origin{}, nil, "", err
	}
	if o.DocURIs, err = r.uris(ctx, "documents", ns, o.DocIDs); err != nil {
		return Origin{}, nil, "", err
	}
	docKey := ""
	if len(o.DocIDs) > 0 {
		docKey = o.DocIDs[0]
	}
	return o, sortedKeys(flagSet), docKey, nil
}

// uris returns the sorted distinct uris of ids in table (sources or documents).
func (r *Reader) uris(ctx context.Context, table, ns string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{ns}
	for _, id := range ids {
		args = append(args, id)
	}
	return r.queryStrings(ctx, `SELECT DISTINCT uri FROM `+table+` WHERE namespace = ? AND id IN (`+placeholders(len(ids))+`) ORDER BY uri`, args...)
}

// contextOrder: current facts, then consolidated pages, then raw excerpts;
// score order within each group (items arrive in score order).
func contextOrder(items []Item) []Item {
	rank := map[core.RecordType]int{core.RecFact: 0, core.RecPage: 1, core.RecEvent: 2}
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Type] < rank[out[j].Type] })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
