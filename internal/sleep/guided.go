package sleep

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/internal/window"
)

// Guided sleep (docs/decisions/0142-usage-guided-sleep.md). With
// sleep.mode: guided, phase 2 digests only what retrieval keeps returning:
//
//  1. priority per live event, from the learning layer only (retrievals
//     and outcomes; never query_text or params);
//  2. groups: each retrieval's top k is a candidate group, merged by
//     Jaccard overlap and capped;
//  3. one ficha (a page of kind summary) per digested group, one item per
//     line, extractive (no LLM) or written by the LLM and verified;
//  4. staged and promoted through the gate like lesson items.

// StepGuidedDigest is the event_processing step of the members of a
// digested group.
const StepGuidedDigest = "guided_digest"

// GuidedCounts is what the guided digest did (Counts.Guided; nil in
// uniform mode).
type GuidedCounts struct {
	Digest          string         `json:"digest"`           // extractive or llm
	CandidateGroups int            `json:"candidate_groups"` // non-empty retrieval top-k sets
	Groups          int            `json:"groups"`           // after merging
	GroupsDigested  int            `json:"groups_digested"`
	GroupsSkipped   int            `json:"groups_skipped"` // every member already digested
	GroupsLeft      int            `json:"groups_left"`    // not reached: budget, max_groups, or larger than the whole token budget
	LinesWritten    int            `json:"lines_written"`
	LinesDiscarded  map[string]int `json:"lines_discarded"` // by reason
	Fichas          []string       `json:"fichas"`          // page ids created
}

// ficha is one planned digest page.
type ficha struct {
	members []string // every member of the group, sorted
	llm     bool     // the LLM read the members: a forgotten member drops the whole ficha
	title   string
	lines   []fichaLine
}

type fichaLine struct {
	origins []string // ordered by date, session, ordinal, id
	body    string   // the line's own text
	text    string   // the full item text: "date | author | ids | body"
}

// guidedMember is a member event as the digest reads it.
type guidedMember struct {
	id, content, zone, role, session, date string
	ordinal                                int64
}

// ---------------------------------------------------------------------------
// Priority and groups (pure).

// guidedRetrieval is one retrieval as priority needs it.
type guidedRetrieval struct {
	ts        time.Time
	top       []string // first top_k event candidates, logged order
	delivered []string
	failed    bool // an outcome with value < 0
}

type guidedWeights struct {
	demand, nearMiss, failure, fragmentation, halfLifeHours float64
}

func weightsOf(g config.GuidedConfig) guidedWeights {
	return guidedWeights{g.DemandWeight(), g.NearMissWeight(), g.FailureWeight(), g.FragmentationWeight(), g.HalfLifeHours()}
}

// eventSignals are the four signals of one event and its priority.
type eventSignals struct {
	demand        float64
	nearMiss      int
	failure       int
	fragmentation int
	priority      float64
}

// scoreEvents computes the signals of every live event in some top k.
// live lists the live events; session maps an event to its session ("" for
// none).
func scoreEvents(rets []guidedRetrieval, live map[string]bool, session map[string]string, now time.Time, w guidedWeights) map[string]eventSignals {
	out := map[string]eventSignals{}
	frag := map[string]map[string]bool{}
	for _, r := range rets {
		age := now.Sub(r.ts).Hours()
		if age < 0 {
			age = 0
		}
		decay := 1.0
		if w.halfLifeHours > 0 {
			decay = math.Pow(0.5, age/w.halfLifeHours)
		}
		delivered := map[string]bool{}
		for _, id := range r.delivered {
			delivered[id] = true
		}
		var sessions []string
		for _, id := range r.delivered {
			if live[id] && session[id] != "" {
				sessions = append(sessions, session[id])
			}
		}
		for _, e := range r.top {
			if !live[e] {
				continue
			}
			s := out[e]
			s.demand += decay
			if !delivered[e] {
				s.nearMiss++
			}
			if r.failed {
				s.failure++
			}
			out[e] = s
			if frag[e] == nil {
				frag[e] = map[string]bool{}
			}
			for _, x := range sessions {
				if x != session[e] {
					frag[e][x] = true
				}
			}
		}
	}
	for e, s := range out {
		s.fragmentation = len(frag[e])
		s.priority = w.demand*s.demand + w.nearMiss*float64(s.nearMiss) + w.failure*float64(s.failure) +
			w.fragmentation*float64(s.fragmentation)
		out[e] = s
	}
	return out
}

// guidedGroup is a group of member events with its priority.
type guidedGroup struct {
	members  []string // sorted by id
	priority float64
}

// buildGroups turns each retrieval's top k (events with priority > 0) into
// a candidate group, visits candidates by descending priority (ties by
// smallest member id), merges a candidate into the accepted group it
// overlaps most when the Jaccard overlap reaches threshold (ties: the
// earlier accepted group), caps each group at maxMembers (highest priority
// first, ties by id) and orders the groups by descending priority (ties by
// smallest member id). It also returns the number of candidate groups.
func buildGroups(rets []guidedRetrieval, prio map[string]float64, threshold float64, maxMembers int) ([]guidedGroup, int) {
	sum := func(ms []string) float64 {
		t := 0.0
		for _, m := range ms {
			t += prio[m]
		}
		return t
	}
	var cands []guidedGroup
	for _, r := range rets {
		var ms []string
		for _, e := range r.top {
			if prio[e] > 0 {
				ms = append(ms, e)
			}
		}
		if ms = uniqueSorted(ms); len(ms) > 0 {
			cands = append(cands, guidedGroup{members: ms, priority: sum(ms)})
		}
	}
	byPriority := func(gs []guidedGroup) {
		sort.SliceStable(gs, func(i, j int) bool {
			if gs[i].priority != gs[j].priority {
				return gs[i].priority > gs[j].priority
			}
			return gs[i].members[0] < gs[j].members[0]
		})
	}
	byPriority(cands)
	var groups [][]string
	for _, c := range cands {
		best, bestJ := -1, -1.0
		for k, g := range groups {
			if j := jaccard(c.members, g); j > bestJ {
				best, bestJ = k, j
			}
		}
		if best >= 0 && bestJ >= threshold {
			groups[best] = uniqueSorted(append(append([]string{}, groups[best]...), c.members...))
		} else {
			groups = append(groups, c.members)
		}
	}
	out := make([]guidedGroup, 0, len(groups))
	for _, g := range groups {
		ms := append([]string{}, g...)
		if maxMembers > 0 && len(ms) > maxMembers {
			sort.SliceStable(ms, func(i, j int) bool {
				if prio[ms[i]] != prio[ms[j]] {
					return prio[ms[i]] > prio[ms[j]]
				}
				return ms[i] < ms[j]
			})
			ms = ms[:maxMembers]
			sort.Strings(ms)
		}
		out = append(out, guidedGroup{members: ms, priority: sum(ms)})
	}
	byPriority(out)
	return out, len(cands)
}

// ---------------------------------------------------------------------------
// Lines (pure).

// eventDate is valid_from, else recorded_at, as YYYY-MM-DD ("" when neither parses).
func eventDate(validFrom, recordedAt string) string {
	for _, v := range []string{validFrom, recordedAt} {
		if v == "" {
			continue
		}
		if t, err := core.ParseTime(v); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

func memberLess(a, b guidedMember) bool {
	if a.date != b.date {
		return a.date < b.date
	}
	if a.session != b.session {
		return a.session < b.session
	}
	if a.ordinal != b.ordinal {
		return a.ordinal < b.ordinal
	}
	return a.id < b.id
}

// collapse joins the words of s with single spaces (a ficha line is one line).
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// makeLine formats a line from its origins (all members): the earliest
// date, the common role (else "mixed"; "-" for none) and the ids, in
// date, session, ordinal, id order.
func makeLine(origins []string, text string, byID map[string]guidedMember) fichaLine {
	ms := make([]guidedMember, 0, len(origins))
	for _, id := range uniqueSorted(origins) {
		ms = append(ms, byID[id])
	}
	sort.SliceStable(ms, func(i, j int) bool { return memberLess(ms[i], ms[j]) })
	ids := make([]string, len(ms))
	author := ""
	for i, m := range ms {
		ids[i] = m.id
		role := m.role
		if role == "" {
			role = "-"
		}
		switch {
		case i == 0:
			author = role
		case author != role:
			author = "mixed"
		}
	}
	date := ms[0].date
	if date == "" {
		date = "-"
	}
	body := collapse(text)
	return fichaLine{origins: ids, body: body, text: date + " | " + author + " | " + strings.Join(ids, ",") + " | " + body}
}

// sortLines orders lines by their first origin (date, session, ordinal, id).
func sortLines(lines []fichaLine, byID map[string]guidedMember) {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := byID[lines[i].origins[0]], byID[lines[j].origins[0]]
		if a.id == b.id {
			return lines[i].text < lines[j].text
		}
		return memberLess(a, b)
	})
}

// sharedTerms are the key terms that occur in at least two members, by
// member count (descending) then alphabetically.
func sharedTerms(members []guidedMember) []string {
	n := map[string]int{}
	for _, m := range members {
		for _, t := range keyTerms(m.content) {
			n[t]++
		}
	}
	var out []string
	for t, k := range n {
		if k >= 2 {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if n[out[i]] != n[out[j]] {
			return n[out[i]] > n[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func fichaTitle(shared []string) string {
	if len(shared) == 0 {
		return "Digest"
	}
	return "Digest: " + strings.Join(shared[:min(3, len(shared))], ", ")
}

// windowTokens is the tokenizer of the extractive window: sleep's
// normalized words.
func windowTokens(s string) []string { return strings.Fields(normalize(s)) }

// memberTerms are the terms that score a member's window: the group's
// shared terms plus the member's digit tokens and capitalised words.
func memberTerms(shared []string, content string) []string {
	terms := append([]string{}, shared...)
	for _, t := range windowTokens(content) {
		if strings.IndexFunc(t, unicode.IsDigit) >= 0 {
			terms = append(terms, t)
		}
	}
	for _, w := range strings.Fields(content) {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if w == "" || !unicode.IsUpper([]rune(w)[0]) {
			continue
		}
		for _, t := range windowTokens(w) {
			if !stopwords[t] {
				terms = append(terms, t)
			}
		}
	}
	return uniqueSorted(terms)
}

// extractiveLines digests a group without an LLM: one line per member, the
// best literal window of lineTokens of its content (the whole content when
// shorter). A line whose normalized text equals or is contained in an
// earlier line is dropped (counted as repeated).
func extractiveLines(members []guidedMember, lineTokens int, discarded map[string]int) []fichaLine {
	byID := map[string]guidedMember{}
	for _, m := range members {
		byID[m.id] = m
	}
	shared := sharedTerms(members)
	var lines []fichaLine
	for _, m := range members {
		text := strings.TrimSpace(m.content)
		if llm.EstimateTokens(text) > lineTokens {
			w, _, ok := window.Best(text, lineTokens, llm.EstimateTokens, windowTokens, memberTerms(shared, text))
			if !ok {
				discarded[DiscardEmpty]++
				continue
			}
			text = w
		}
		if normalize(text) == "" {
			discarded[DiscardEmpty]++
			continue
		}
		lines = append(lines, makeLine([]string{m.id}, text, byID))
	}
	sortLines(lines, byID)
	var out []fichaLine
	var seen []string
	for _, l := range lines {
		norm := normalize(l.body)
		dup := false
		for _, s := range seen {
			if strings.Contains(s, norm) {
				dup = true
				break
			}
		}
		if dup {
			discarded[DiscardRepeated]++
			continue
		}
		seen = append(seen, norm)
		out = append(out, l)
	}
	return out
}

// ---------------------------------------------------------------------------
// Phase 2, guided.

func (s *Sleeper) phase2Guided(ctx context.Context, r *run) error {
	g := s.env.Cfg.Sleep.Guided
	gc := &GuidedCounts{Digest: g.DigestKind(), LinesDiscarded: map[string]int{}, Fichas: []string{}}
	r.res.Counts.Guided = gc
	if gc.Digest == config.DigestLLM && s.d.LLM == nil {
		r.res.notice("phase 2 skipped: guided llm digest needs an LLM provider; phase 1 only")
		return nil
	}
	if s.d.Evaluator == nil {
		r.res.notice("phase 2 skipped: no evaluator configured, so staged changes could never pass the gate")
		return nil
	}
	p := &plan{}
	// Conflict verdicts first (decision 0148; nothing when off).
	if s.env.Cfg.Sleep.Conflicts.On() && s.d.LLM == nil {
		r.res.notice("conflicts: no LLM provider; the conflict queue waits")
	} else if err := s.collectConflicts(ctx, r, p); err != nil {
		return err
	}
	if err := s.collectGuided(ctx, r, p); err != nil {
		return err
	}
	if p.empty() {
		r.res.notice("phase 2: nothing to digest")
		return nil
	}
	return s.applyGated(ctx, r, p)
}

// loadGuidedRetrievals reads the learning layer of ns: retrievals (logged
// candidates' id and type, delivered, ts) and the outcome sign. Never
// query_text or params.
func (s *Sleeper) loadGuidedRetrievals(ctx context.Context, ns string, topK int) ([]guidedRetrieval, error) {
	failed := map[string]bool{}
	ids, err := queryStrings(ctx, s.db(), `SELECT DISTINCT retrieval_id FROM outcomes WHERE namespace=? AND value < 0`, ns)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		failed[id] = true
	}
	rows, err := s.db().QueryContext(ctx, `SELECT id, candidates, delivered, ts FROM retrievals WHERE namespace=? ORDER BY ts, id`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guidedRetrieval
	for rows.Next() {
		var id, cands, deliv, ts string
		if err := rows.Scan(&id, &cands, &deliv, &ts); err != nil {
			return nil, err
		}
		var cs []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(cands), &cs); err != nil {
			continue // not a candidate list this code can read: the retrieval adds nothing
		}
		r := guidedRetrieval{failed: failed[id]}
		for _, c := range cs {
			if c.Type == string(core.RecEvent) && len(r.top) < topK {
				r.top = append(r.top, c.ID)
			}
		}
		_ = json.Unmarshal([]byte(deliv), &r.delivered)
		if t, err := core.ParseTime(ts); err == nil {
			r.ts = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// eventMeta returns, for the given ids, which are live events of ns and
// their sessions (metadata only, never content).
func (s *Sleeper) eventMeta(ctx context.Context, ns string, ids []string) (map[string]bool, map[string]string, error) {
	live, session := map[string]bool{}, map[string]string{}
	const chunk = 400
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := []any{ns}
		for _, id := range part {
			args = append(args, id)
		}
		rows, err := s.db().QueryContext(ctx, `SELECT id, COALESCE(session_id,'') FROM events
			WHERE namespace=? AND retired_at IS NULL AND id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id, sess string
			if err := rows.Scan(&id, &sess); err != nil {
				rows.Close()
				return nil, nil, err
			}
			live[id], session[id] = true, sess
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	return live, session, nil
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// digestedSet returns the ids among ids that carry the guided_digest step.
func (s *Sleeper) digestedSet(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	const chunk = 400
	for i := 0; i < len(ids); i += chunk {
		part := ids[i:min(i+chunk, len(ids))]
		args := []any{StepGuidedDigest}
		for _, id := range part {
			args = append(args, id)
		}
		got, err := queryStrings(ctx, s.db(), `SELECT event_id FROM event_processing WHERE step=? AND event_id IN (`+placeholders(len(part))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for _, id := range got {
			out[id] = true
		}
	}
	return out, nil
}

// loadMembers reads the group's own events (live only), in id order.
func (s *Sleeper) loadMembers(ctx context.Context, ns string, ids []string) ([]guidedMember, error) {
	args := []any{ns}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db().QueryContext(ctx, `SELECT id, content, trust_zone, COALESCE(role,''), COALESCE(session_id,''),
		COALESCE(ordinal,0), COALESCE(valid_from,''), recorded_at FROM events
		WHERE namespace=? AND retired_at IS NULL AND id IN (`+placeholders(len(ids))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guidedMember
	for rows.Next() {
		var m guidedMember
		var validFrom, recordedAt string
		if err := rows.Scan(&m.id, &m.content, &m.zone, &m.role, &m.session, &m.ordinal, &validFrom, &recordedAt); err != nil {
			return nil, err
		}
		m.date = eventDate(validFrom, recordedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// collectGuided builds the plan of the guided digest: priority, groups, and
// one ficha per group digested, within the run budget and max_groups.
func (s *Sleeper) collectGuided(ctx context.Context, r *run, p *plan) error {
	g := s.env.Cfg.Sleep.Guided
	gc := r.res.Counts.Guided
	rets, err := s.loadGuidedRetrievals(ctx, r.ns, g.TopKEvents())
	if err != nil {
		return err
	}
	var refs []string
	for _, x := range rets {
		refs = append(refs, x.top...)
		refs = append(refs, x.delivered...)
	}
	live, session, err := s.eventMeta(ctx, r.ns, uniqueSorted(refs))
	if err != nil {
		return err
	}
	sig := scoreEvents(rets, live, session, s.env.Clock.Now(), weightsOf(g))
	prio := make(map[string]float64, len(sig))
	for e, x := range sig {
		prio[e] = x.priority
	}
	groups, ncand := buildGroups(rets, prio, g.MergeJaccard(), g.MaxMembers())
	gc.CandidateGroups, gc.Groups = ncand, len(groups)
	if len(groups) == 0 {
		return nil
	}
	var all []string
	for _, gr := range groups {
		all = append(all, gr.members...)
	}
	done, err := s.digestedSet(ctx, uniqueSorted(all))
	if err != nil {
		return err
	}
	useLLM := g.DigestKind() == config.DigestLLM
	planned := 0
	tooLarge := false
	for k, gr := range groups {
		pending := false
		for _, m := range gr.members {
			pending = pending || !done[m]
		}
		if !pending {
			gc.GroupsSkipped++
			continue
		}
		stop := ""
		switch {
		case g.GroupCap() > 0 && planned >= g.GroupCap():
			stop = "max_groups"
		case r.eventsUsed+len(gr.members) > r.budget.Events:
			stop = "event"
		case useLLM && r.stopLLM:
			stop = "token"
		}
		if stop != "" {
			if stop != "token" {
				r.ceiling(stop)
			}
			for _, rest := range groups[k:] {
				for _, m := range rest.members {
					if !done[m] {
						gc.GroupsLeft++
						break
					}
				}
			}
			break
		}
		members, err := s.loadMembers(ctx, r.ns, gr.members)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			continue
		}
		f := ficha{llm: useLLM, title: fichaTitle(sharedTerms(members))}
		for _, m := range members {
			f.members = append(f.members, m.id)
		}
		if useLLM {
			turns := make([]digestTurn, len(members))
			for i, m := range members {
				role := m.role
				if role == "" {
					role = "-"
				}
				turns[i] = digestTurn{id: m.id, date: m.date, role: role, content: m.content}
			}
			prompt := digestPrompt(turns)
			if s.estimate(systemDigest, prompt) > r.budget.Tokens {
				gc.GroupsLeft++
				if !tooLarge {
					tooLarge = true
					r.res.notice("guided: a group is larger than the whole token budget; skipped")
				}
				continue
			}
			r.eventsUsed += len(members)
			var out digestOut
			ok, err := s.call(ctx, r, systemDigest, prompt, schemaDigest, &out)
			if err != nil {
				return err
			}
			if !ok {
				if r.stopLLM {
					gc.GroupsLeft++
				}
				continue // invalid answer or provider failure: the group stays pending
			}
			f.lines = verifiedLines(out, members, gc.LinesDiscarded)
		} else {
			r.eventsUsed += len(members)
			f.lines = extractiveLines(members, g.WindowTokens(), gc.LinesDiscarded)
		}
		for _, m := range f.members {
			done[m] = true
		}
		planned++
		p.fichas = append(p.fichas, f)
	}
	return nil
}

// verifiedLines keeps the LLM lines that pass the verifier, formatted with
// the date and author of their cited events.
func verifiedLines(out digestOut, members []guidedMember, discarded map[string]int) []fichaLine {
	byID := map[string]guidedMember{}
	ver := map[string]verifyEvent{}
	for _, m := range members {
		byID[m.id] = m
		ver[m.id] = verifyEvent{content: m.content, date: m.date}
	}
	var lines []fichaLine
	for _, l := range out.Lines {
		if reason := verifyLine(l.Kind, l.Text, l.EventIDs, ver); reason != "" {
			discarded[reason]++
			continue
		}
		lines = append(lines, makeLine(l.EventIDs, strings.TrimSpace(l.Text), byID))
	}
	sortLines(lines, byID)
	return lines
}

// ---------------------------------------------------------------------------
// Staging.

// stageFicha writes one ficha as a staged summary page with one staged item
// per line, each citing its origins, zone the lowest of theirs; the page is
// queued for embedding. Members get the guided_digest step. A line with an
// origin no longer live is dropped; when the LLM read a member that was
// forgotten meanwhile, the whole ficha is dropped and the group stays
// pending. Returns the page id, or "" when nothing was written.
func (s *Sleeper) stageFicha(ctx context.Context, tx *sql.Tx, r *run, f ficha) (string, error) {
	gc := r.res.Counts.Guided
	c := &r.res.Counts
	st, zones, err := eventStates(ctx, tx, r.ns, f.members)
	if err != nil {
		return "", err
	}
	var marked []string
	for _, id := range f.members {
		if st[id] == evGone && f.llm {
			gc.LinesDiscarded[DiscardOriginNotLive] += len(f.lines)
			return "", nil
		}
		if st[id] != evGone {
			marked = append(marked, id)
		}
	}
	if err := s.markProcessed(ctx, tx, r, StepGuidedDigest, marked); err != nil {
		return "", err
	}
	gc.GroupsDigested++
	type staged struct {
		text string
		zone core.Zone
		orig []string
	}
	var items []staged
	var pageZones []core.Zone
	for _, l := range f.lines {
		var zs []core.Zone
		ok := true
		for _, id := range l.origins {
			if st[id] != evLive {
				ok = false
				break
			}
			zs = append(zs, zones[id])
		}
		if !ok {
			gc.LinesDiscarded[DiscardOriginNotLive]++
			continue
		}
		z := core.MinZone(zs...)
		items = append(items, staged{l.text, z, l.origins})
		pageZones = append(pageZones, z)
	}
	if len(items) == 0 {
		return "", nil
	}
	now := s.now()
	pid := s.env.IDs.For(core.RecPage)
	if _, err := tx.ExecContext(ctx, `INSERT INTO pages(id, namespace, kind, title, body, version, sleep_run_id, status, trust_zone, created_at, updated_at)
		VALUES (?,?,'summary',?,'',0,?,'staged',?,?,?)`, pid, r.ns, f.title, r.res.RunID, string(core.MinZone(pageZones...)), now, now); err != nil {
		return "", err
	}
	c.PagesCreated = append(c.PagesCreated, pid)
	gc.Fichas = append(gc.Fichas, pid)
	for k, it := range items {
		item := s.env.IDs.For(core.RecPageItem)
		if _, err := tx.ExecContext(ctx, `INSERT INTO page_items(id, namespace, page_id, ordinal, text, status, trust_zone, sleep_run_id, created_at)
			VALUES (?,?,?,?,?,'staged',?,?,?)`, item, r.ns, pid, k+1, it.text, string(it.zone), r.res.RunID, now); err != nil {
			return "", err
		}
		for _, id := range it.orig {
			if _, err := tx.ExecContext(ctx, `INSERT INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
				VALUES (?,'page_item',?,'event',?,?,?)`, r.ns, item, id, core.RelCites, now); err != nil {
				return "", err
			}
		}
		c.PageItemsAdded = append(c.PageItemsAdded, item)
		gc.LinesWritten++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at)
		VALUES (?,?,?,?) ON CONFLICT(record_type, record_id) DO NOTHING`, r.ns, string(core.RecPage), pid, now); err != nil {
		return "", err
	}
	return pid, nil
}
