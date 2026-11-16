package sleep

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

// Phase 2 works in two steps so the database connection is never held
// during an LLM call:
//
//  1. collect: read the pending work and call the LLM, within the budget,
//     building a plan. Nothing is written.
//  2. applyGated (gate.go): snapshot, then one transaction that stages the
//     plan, runs the evaluation gate and commits or rolls back.

type extractBatch struct {
	events []string // ids sent to the LLM, sorted
	cands  []Candidate
}

type verdict struct {
	kind    string // "fact" or "merge"
	id      string
	verdict string
}

type lesson struct {
	events []string // every event shown to the LLM; all are cited
	skip   bool
	pageID string // existing page, or "" for a new one
	title  string
	text   string
}

type plan struct {
	batches  []extractBatch
	skipped  []string // events larger than the whole token budget: marked processed with no facts
	verdicts []verdict
	lessons  []lesson
	fichas   []ficha // guided digest (decision 0142)
}

func (p *plan) empty() bool {
	return len(p.batches) == 0 && len(p.skipped) == 0 && len(p.verdicts) == 0 && len(p.lessons) == 0 && len(p.fichas) == 0
}

// phase2 is LLM consolidation (BASE.md "Phase 2").
func (s *Sleeper) phase2(ctx context.Context, r *run) error {
	if s.env.Cfg.Sleep.IsGuided() {
		return s.phase2Guided(ctx, r)
	}
	if s.d.LLM == nil {
		r.res.notice("phase 2 skipped: no LLM provider configured; phase 1 only")
		return nil
	}
	if s.d.Evaluator == nil {
		r.res.notice("phase 2 skipped: no evaluator configured, so staged changes could never pass the gate")
		return nil
	}
	p := &plan{}
	if s.d.Facts == nil {
		r.res.notice("fact sink not configured; extraction skipped")
	} else if err := s.collectExtraction(ctx, r, p); err != nil {
		return err
	}
	if err := s.collectVerdicts(ctx, r, p); err != nil {
		return err
	}
	if err := s.collectLessons(ctx, r, p); err != nil {
		return err
	}
	if p.empty() {
		r.res.notice("phase 2: nothing to consolidate")
		return nil
	}
	return s.applyGated(ctx, r, p)
}

// ---------------------------------------------------------------------------
// Budget and calls.

func (s *Sleeper) estimate(system, prompt string) int {
	return llm.EstimateTokens(system) + llm.EstimateTokens(prompt) + s.env.Cfg.LLM.MaxTokens
}

func (r *run) ceiling(what string) {
	r.partial, r.stopLLM = true, true
	if !r.budgetNoticed {
		r.budgetNoticed = true
		r.res.notice("budget: " + what + " ceiling reached; the rest is left for the next run")
	}
}

// call sends one request. It returns false when the answer is unusable: an
// invalid answer (discarded, never retried; the run continues) or a
// provider failure (no further calls this run; status partial).
func (s *Sleeper) call(ctx context.Context, r *run, system, prompt string, schema []byte, out any) (bool, error) {
	if r.stopLLM {
		return false, nil
	}
	if s.estimate(system, prompt) > r.tokensLeft() {
		r.ceiling("token")
		return false, nil
	}
	resp, err := s.d.LLM.Complete(ctx, core.LLMRequest{
		System: system, Prompt: prompt, JSONSchema: schema, MaxTokens: s.env.Cfg.LLM.MaxTokens,
	})
	r.res.Cost.LLMCalls++
	r.res.Cost.InputTokens += resp.InputTokens
	r.res.Cost.OutputTokens += resp.OutputTokens
	if err == nil {
		// Defence in depth: never trust a provider to have validated.
		err = llm.Validate(schema, resp.JSON)
	}
	if err == nil {
		if e := json.Unmarshal(resp.JSON, out); e != nil {
			err = fmt.Errorf("%w: %v", llm.ErrInvalidOutput, e)
		}
	}
	switch {
	case err == nil:
		return true, nil
	case ctx.Err() != nil:
		return false, ctx.Err()
	case llm.IsInvalidOutput(err):
		r.res.Counts.InvalidAnswers++
		return false, nil
	default:
		r.res.Counts.LLMErrors++
		r.partial, r.stopLLM = true, true
		r.res.notice("llm provider error; no further calls this run: " + err.Error())
		return false, nil
	}
}

// ---------------------------------------------------------------------------
// (a) Extraction.

type evRow struct {
	promptEvent
	validFrom, docDate string
}

func (s *Sleeper) collectExtraction(ctx context.Context, r *run, p *plan) error {
	var pending int
	if err := s.db().QueryRowContext(ctx, `SELECT count(*) FROM events e
		WHERE e.namespace=? AND e.retired_at IS NULL AND e.kind <> 'fact_assertion'
		AND NOT EXISTS (SELECT 1 FROM event_processing x WHERE x.event_id=e.id AND x.step=?)`,
		r.ns, StepExtractFacts).Scan(&pending); err != nil {
		return err
	}
	limit := r.budget.Events - r.eventsUsed
	if pending == 0 {
		return nil
	}
	if limit <= 0 {
		r.ceiling("event")
		return nil
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.id, e.trust_zone, e.recorded_at, e.content,
		COALESCE(e.valid_from,''), COALESCE(d.doc_date,'')
		FROM events e LEFT JOIN documents d ON d.id=e.doc_id
		WHERE e.namespace=? AND e.retired_at IS NULL AND e.kind <> 'fact_assertion'
		AND NOT EXISTS (SELECT 1 FROM event_processing x WHERE x.event_id=e.id AND x.step=?)
		ORDER BY e.recorded_at, e.id LIMIT ?`, r.ns, StepExtractFacts, limit)
	if err != nil {
		return err
	}
	var evs []evRow
	for rows.Next() {
		var e evRow
		if err := rows.Scan(&e.id, &e.zone, &e.recordedAt, &e.content, &e.validFrom, &e.docDate); err != nil {
			rows.Close()
			return err
		}
		evs = append(evs, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if pending > len(evs) {
		r.ceiling("event")
	}
	stop := r.stopLLM
	r.stopLLM = false // the event ceiling still lets this run use its share
	defer func() { r.stopLLM = r.stopLLM || stop }()

	batchMax := max(1, s.env.Cfg.Sleep.BatchEvents)
	for i := 0; i < len(evs) && !r.stopLLM; {
		var batch []promptEvent
		for j := i; j < len(evs) && len(batch) < batchMax; j++ {
			next := append(append([]promptEvent{}, batch...), evs[j].promptEvent)
			if s.estimate(systemExtract, extractPrompt(next)) > r.tokensLeft() {
				break
			}
			batch = next
		}
		if len(batch) == 0 {
			if s.estimate(systemExtract, extractPrompt([]promptEvent{evs[i].promptEvent})) > r.budget.Tokens {
				// It could never fit: mark it processed with no facts instead of blocking every run.
				p.skipped = append(p.skipped, evs[i].id)
				r.res.Counts.EventsSkipped = append(r.res.Counts.EventsSkipped, evs[i].id)
				i++
				continue
			}
			r.ceiling("token")
			break
		}
		byID := map[string]evRow{}
		for _, e := range evs[i : i+len(batch)] {
			byID[e.id] = e
		}
		i += len(batch)
		r.eventsUsed += len(batch)
		var out extractOut
		ok, err := s.call(ctx, r, systemExtract, extractPrompt(batch), schemaExtract, &out)
		if err != nil {
			return err
		}
		if !ok {
			continue // invalid answer: these events stay unprocessed for a later run
		}
		b := extractBatch{}
		for id := range byID {
			b.events = append(b.events, id)
		}
		sort.Strings(b.events)
		for _, f := range out.Facts {
			c, ok := candidate(f.EventIDs, f.Subject, f.SubjectType, f.Predicate, f.Value, f.ValidFrom, byID)
			if !ok {
				r.res.Counts.CandidatesDrop++
				continue
			}
			b.cands = append(b.cands, c)
		}
		r.res.Counts.EventsExtracted += len(batch)
		p.batches = append(p.batches, b)
	}
	return nil
}

// candidate validates one extracted fact. Every cited id must be an event of
// the batch (an id the model was not shown is a provenance forgery: the
// whole candidate is dropped).
func candidate(eventIDs []string, subject, subjectType, predicate, value, validFrom string, batch map[string]evRow) (Candidate, bool) {
	c := Candidate{
		Subject:     strings.TrimSpace(subject),
		SubjectType: strings.TrimSpace(strings.ToLower(subjectType)),
		Predicate:   snake(predicate),
		Value:       strings.TrimSpace(value),
	}
	if c.Subject == "" || c.Predicate == "" || c.Value == "" || len(eventIDs) == 0 {
		return c, false
	}
	for _, id := range eventIDs {
		if _, ok := batch[id]; !ok {
			return c, false
		}
	}
	c.OriginEventIDs = uniqueSorted(eventIDs)
	if t, err := core.ParseTime(strings.TrimSpace(validFrom)); err == nil {
		c.ValidFrom = core.FormatTime(t)
		return c, true
	}
	first := batch[c.OriginEventIDs[0]]
	for _, v := range []string{first.validFrom, first.docDate, first.recordedAt} {
		if t, err := core.ParseTime(v); err == nil && v != "" {
			c.ValidFrom = core.FormatTime(t)
			break
		}
	}
	return c, true
}

// ---------------------------------------------------------------------------
// (b) Judging pending items.

type judgeItem struct {
	kind, id, text string
}

func (s *Sleeper) collectVerdicts(ctx context.Context, r *run, p *plan) error {
	items, err := s.pendingFacts(ctx, r.ns)
	if err != nil {
		return err
	}
	merges, err := s.pendingMerges(ctx, r.ns)
	if err != nil {
		return err
	}
	items = append(items, merges...)
	batchMax := max(1, s.env.Cfg.Sleep.BatchEvents)
	for i := 0; i < len(items) && !r.stopLLM; i += batchMax {
		batch := items[i:min(i+batchMax, len(items))]
		ids := make([]string, len(batch))
		kinds := map[string]string{}
		var b strings.Builder
		for k, it := range batch {
			ids[k] = it.id
			kinds[it.id] = it.kind
			b.WriteString(it.text)
		}
		var out judgeOut
		ok, err := s.call(ctx, r, systemJudge, b.String(), judgeSchema(ids), &out)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, v := range out.Verdicts {
			if seen[v.ID] {
				continue // first verdict per item wins
			}
			seen[v.ID] = true
			p.verdicts = append(p.verdicts, verdict{kind: kinds[v.ID], id: v.ID, verdict: v.Verdict})
		}
	}
	return nil
}

func (s *Sleeper) pendingFacts(ctx context.Context, ns string) ([]judgeItem, error) {
	type qf struct{ id, subject, name, typ, pred, value, zone, validFrom string }
	rows, err := s.db().QueryContext(ctx, `SELECT f.id, f.subject, COALESCE(en.canonical_name, f.subject), COALESCE(en.type,''),
		f.predicate, f.value, f.trust_zone, f.valid_from
		FROM facts f LEFT JOIN entities en ON en.id=f.subject
		WHERE f.namespace=? AND f.status='quarantined' ORDER BY f.id`, ns)
	if err != nil {
		return nil, err
	}
	var fs []qf
	for rows.Next() {
		var f qf
		if err := rows.Scan(&f.id, &f.subject, &f.name, &f.typ, &f.pred, &f.value, &f.zone, &f.validFrom); err != nil {
			rows.Close()
			return nil, err
		}
		fs = append(fs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []judgeItem
	for _, f := range fs {
		rows, err := s.db().QueryContext(ctx, `SELECT value, trust_zone, valid_from FROM facts
			WHERE namespace=? AND subject=? AND predicate=? AND status='active' ORDER BY id`, ns, f.subject, f.pred)
		if err != nil {
			return nil, err
		}
		var cur []string
		for rows.Next() {
			var v, z, vf string
			if err := rows.Scan(&v, &z, &vf); err != nil {
				rows.Close()
				return nil, err
			}
			cur = append(cur, fmt.Sprintf("%q (zone %s, valid from %s)", quote(v), z, vf))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(cur) == 0 {
			cur = []string{"none"}
		}
		text := fmt.Sprintf("<item id=%q kind=\"quarantined_fact\">\nSubject: %q (%s)\nAttribute: %s\nProposed value: %q (zone %s, valid from %s)\nCurrent active values: %s\n</item>\n",
			f.id, quote(f.name), f.typ, f.pred, quote(f.value), f.zone, f.validFrom, strings.Join(cur, "; "))
		out = append(out, judgeItem{kind: "fact", id: f.id, text: text})
	}
	return out, nil
}

func (s *Sleeper) pendingMerges(ctx context.Context, ns string) ([]judgeItem, error) {
	type pm struct {
		id, a, b string
		score    float64
		reason   string
	}
	rows, err := s.db().QueryContext(ctx, `SELECT p.id, p.entity_a, p.entity_b, p.score, p.reason
		FROM entity_merge_proposals p JOIN entities a ON a.id=p.entity_a JOIN entities b ON b.id=p.entity_b
		WHERE p.namespace=? AND p.status='pending' AND a.merged_into IS NULL AND b.merged_into IS NULL
		ORDER BY p.id`, ns)
	if err != nil {
		return nil, err
	}
	var ps []pm
	for rows.Next() {
		var x pm
		if err := rows.Scan(&x.id, &x.a, &x.b, &x.score, &x.reason); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, nil
	}
	ents, err := s.loadEntities(ctx, s.db(), ns)
	if err != nil {
		return nil, err
	}
	byID := map[string]entityNames{}
	for _, e := range ents {
		byID[e.id] = e
	}
	describe := func(id string) string {
		var name string
		_ = s.db().QueryRowContext(ctx, `SELECT canonical_name FROM entities WHERE id=?`, id).Scan(&name)
		e := byID[id]
		return fmt.Sprintf("%q (type %q; names: %s)", quote(name), e.typ, quote(strings.Join(e.names, ", ")))
	}
	var out []judgeItem
	for _, x := range ps {
		text := fmt.Sprintf("<item id=%q kind=\"entity_merge\">\nEntity A: %s\nEntity B: %s\nSimilarity: %.3f (%s)\n</item>\n",
			x.id, describe(x.a), describe(x.b), x.score, x.reason)
		out = append(out, judgeItem{kind: "merge", id: x.id, text: text})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// (c) Lessons.

// collectLessons groups similar events deterministically and asks the LLM
// for one lesson item per group. Pool: the most recent live events of the
// namespace (sleep.lesson_pool_events) that are not fact assertions, not
// marked instruction_like, not yet cited by a page item and not yet
// reviewed. Similar: Jaccard overlap of key terms ≥ sleep.lesson_similarity;
// groups are the connected components of that relation (single linkage),
// kept when they have at least sleep.lesson_min_events events.
func (s *Sleeper) collectLessons(ctx context.Context, r *run, p *plan) error {
	cfg := s.env.Cfg.Sleep
	if r.stopLLM || cfg.LessonMinEvents <= 0 {
		return nil
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.id, e.trust_zone, e.recorded_at, e.content FROM events e
		WHERE e.namespace=? AND e.retired_at IS NULL AND e.kind <> 'fact_assertion'
		AND instr(','||e.flags||',', ',`+core.FlagInstructionLike+`,') = 0
		AND NOT EXISTS (SELECT 1 FROM provenance x WHERE x.source_type='event' AND x.source_id=e.id
			AND x.derived_type='page_item' AND x.relation='cites')
		AND NOT EXISTS (SELECT 1 FROM event_processing x WHERE x.event_id=e.id AND x.step=?)
		ORDER BY e.recorded_at DESC, e.id DESC LIMIT ?`, r.ns, StepLessonReview, cfg.LessonPoolEvents)
	if err != nil {
		return err
	}
	var pool []promptEvent
	for rows.Next() {
		var e promptEvent
		if err := rows.Scan(&e.id, &e.zone, &e.recordedAt, &e.content); err != nil {
			rows.Close()
			return err
		}
		pool = append(pool, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].id < pool[j].id })
	clusters, terms := clusterEvents(pool, cfg.LessonSimilarity, cfg.LessonMinEvents, max(1, cfg.BatchEvents))
	if len(clusters) == 0 {
		return nil
	}
	pages, err := s.lessonPages(ctx, r.ns)
	if err != nil {
		return err
	}
	for _, cl := range clusters {
		if r.stopLLM {
			break
		}
		if r.eventsUsed+len(cl) > r.budget.Events {
			r.ceiling("event")
			break
		}
		evs := make([]promptEvent, len(cl))
		var clTerms []string
		for k, i := range cl {
			evs[k] = pool[i]
			clTerms = append(clTerms, terms[i]...)
		}
		clTerms = uniqueSorted(clTerms)
		cands := closestPages(pages, clTerms, max(1, cfg.BatchEvents))
		ids := make([]string, len(cands))
		for k, pg := range cands {
			ids[k] = pg.id
		}
		var out lessonOut
		r.eventsUsed += len(cl)
		ok, err := s.call(ctx, r, systemLesson, lessonPrompt(evs, cands), lessonSchema(ids), &out)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		l := lesson{skip: out.Skip}
		for _, e := range evs {
			l.events = append(l.events, e.id)
		}
		if !l.skip {
			l.text = strings.TrimSpace(out.Item)
			if l.text == "" {
				r.res.Counts.InvalidAnswers++ // a lesson without text is not a valid answer
				continue
			}
			if out.PageID != "new" && out.PageID != "" {
				l.pageID = out.PageID
			}
			l.title = strings.TrimSpace(out.Title)
			if l.title == "" {
				l.title = "Lesson: " + strings.Join(clTerms[:min(3, len(clTerms))], ", ")
			}
		}
		p.lessons = append(p.lessons, l)
	}
	return nil
}

// clusterEvents returns groups of pool indexes (each sorted, capped at
// maxSize, groups ordered by their first index) and the key terms per event.
func clusterEvents(pool []promptEvent, threshold float64, minSize, maxSize int) ([][]int, [][]string) {
	terms := make([][]string, len(pool))
	for i, e := range pool {
		terms[i] = keyTerms(e.content)
	}
	parent := make([]int, len(pool))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range pool {
		if len(terms[i]) == 0 {
			continue
		}
		for j := i + 1; j < len(pool); j++ {
			if len(terms[j]) > 0 && jaccard(terms[i], terms[j]) >= threshold {
				a, b := find(i), find(j)
				if a != b {
					parent[max(a, b)] = min(a, b)
				}
			}
		}
	}
	groups := map[int][]int{}
	for i := range pool {
		root := find(i)
		groups[root] = append(groups[root], i)
	}
	var out [][]int
	for _, g := range groups {
		if len(g) >= minSize {
			sort.Ints(g)
			out = append(out, g[:min(len(g), maxSize)])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out, terms
}

func (s *Sleeper) lessonPages(ctx context.Context, ns string) ([]lessonPage, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT id, title FROM pages WHERE namespace=? AND kind='lesson' AND status='active' ORDER BY id`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lessonPage
	for rows.Next() {
		var p lessonPage
		if err := rows.Scan(&p.id, &p.title); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// closestPages ranks pages by key-term overlap of their title with the
// cluster terms (ties by id) and keeps the first n.
func closestPages(pages []lessonPage, clTerms []string, n int) []lessonPage {
	type scored struct {
		p lessonPage
		s float64
	}
	sc := make([]scored, len(pages))
	for i, p := range pages {
		sc[i] = scored{p, jaccard(keyTerms(p.title), clTerms)}
	}
	sort.SliceStable(sc, func(i, j int) bool {
		if sc[i].s != sc[j].s {
			return sc[i].s > sc[j].s
		}
		return sc[i].p.id < sc[j].p.id
	})
	out := make([]lessonPage, 0, min(n, len(sc)))
	for _, x := range sc[:min(n, len(sc))] {
		out = append(out, x.p)
	}
	return out
}
