package read

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
)

// Stale values in raw excerpts (decision 0120).
//
// Decision 0093 drops, in context mode, an excerpt that stated a fact later
// replaced. It relies on the provenance edge from the fact to the excerpt,
// so it misses prose that states a value without being the origin of the
// fact: a later announcement ("from July the price is ..."), an FAQ, a
// session turn whose statement was asserted through assert_fact (the fact
// derives from the fact_assertion event, not from the turn). This filter
// closes that gap deterministically, with the fact normalizers:
//
// A chain is the set of single-valued facts of one (subject, predicate),
// the subject resolved through accepted merges, that holds at least one
// superseded fact. At as_of a value of the chain holds when one of its facts
// with that value is valid then (without an explicit as_of: is active).
// A raw excerpt is stale when, for a chain it is about, it contains a value
// of that chain that does not hold at as_of. An excerpt is about a chain
// when it names the chain's subject (an alias of the entity, matched as
// whole words in its text or heading path) or when it is a turn of the
// session in which a fact of the chain was asserted. The value is matched
// on norm.Value of the excerpt (numbers and dates canonical) as whole
// words. Stale excerpts are dropped (dropStaleValue): the value that holds
// is delivered as a fact, which comes first in the block.

// dropStaleValue: context, a raw excerpt states a value of a fact chain that
// does not hold at as_of.
const dropStaleValue = "stale_value"

// minStaleValueRunes and minStaleAliasRunes keep very short values and
// names (a digit, an initial) from matching unrelated text.
const (
	minStaleValueRunes = 3
	minStaleAliasRunes = 3
)

// staleChain is one (subject, predicate) chain: the values that hold at
// as_of and those that do not.
type staleChain struct {
	holding, stale []string // value_norm, sorted, distinct; stale excludes holding values
}

type staleAlias struct {
	text   string // norm.Alias, padded with spaces
	chains []*staleChain
}

// staleScope is everything the filter needs for one query.
type staleScope struct {
	byWord    map[string][]*staleAlias // first word of an alias -> aliases
	bySession map[string][]*staleChain // session id -> chains asserted in it
}

func (s *staleScope) empty() bool { return s == nil || (len(s.byWord) == 0 && len(s.bySession) == 0) }

// dropStaleValues marks the raw excerpts of cs that state a value of a fact
// chain that does not hold at as_of, and returns the others.
func (r *Reader) dropStaleValues(ctx context.Context, o filterOpts, cs []*cand) ([]*cand, error) {
	var evs []*cand
	sessions := map[string]bool{}
	for _, c := range cs {
		if c.rec.typ == core.RecEvent {
			evs = append(evs, c)
			if c.rec.sessionID != "" {
				sessions[c.rec.sessionID] = true
			}
		}
	}
	if len(evs) == 0 {
		return cs, nil
	}
	sc, err := r.loadStaleScope(ctx, o, sortedKeys(sessions))
	if err != nil {
		return nil, err
	}
	if sc.empty() {
		return cs, nil
	}
	out := cs[:0:0]
	for _, c := range cs {
		if c.rec.typ == core.RecEvent && sc.stale(c.rec) {
			c.dropped = dropStaleValue
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// stale reports whether the event states a value that does not hold.
func (s *staleScope) stale(rec *record) bool {
	var chains []*staleChain
	chains = append(chains, s.bySession[rec.sessionID]...)
	if len(s.byWord) > 0 {
		text := " " + norm.Alias(rec.headingPath+" "+rec.text) + " "
		seen := map[string]bool{}
		for _, w := range strings.Fields(text) {
			if seen[w] {
				continue
			}
			seen[w] = true
			for _, a := range s.byWord[w] {
				if strings.Contains(text, a.text) {
					chains = append(chains, a.chains...)
				}
			}
		}
	}
	if len(chains) == 0 {
		return false
	}
	value := norm.Value(rec.text)
	for _, ch := range chains {
		for _, v := range ch.stale {
			if containsWord(value, v) {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether v occurs in s with no letter or digit
// immediately before or after it.
func containsWord(s, v string) bool {
	for from := 0; from <= len(s)-len(v); {
		i := strings.Index(s[from:], v)
		if i < 0 {
			return false
		}
		i += from
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		rest := s[i+len(v):]
		after, _ := utf8.DecodeRuneInString(rest)
		// A number goes on through a decimal point: "120" is not in "120.5".
		decimal := len(rest) > 1 && (rest[0] == '.' || rest[0] == ',') && rest[1] >= '0' && rest[1] <= '9'
		if (i == 0 || !wordRune(before)) && (rest == "" || !wordRune(after)) && !decimal {
			return true
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
	return false
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

type chainRow struct {
	id, subject, predicate, value, status, validFrom, validTo string
}

// loadStaleScope reads the fact chains of the namespace (single-valued
// facts linked by supersession), their subjects' aliases and the chains
// asserted in sessions.
func (r *Reader) loadStaleScope(ctx context.Context, o filterOpts, sessions []string) (*staleScope, error) {
	// Every active or superseded single-valued fact of a (subject,
	// predicate) that has a superseded fact (index sleep_facts_ns_status for
	// the chains, facts_sp for their facts).
	rows, err := r.db().QueryContext(ctx, `SELECT f.id, f.subject, f.predicate, f.value_norm, f.status, f.valid_from, COALESCE(f.valid_to,'')
		FROM (SELECT DISTINCT subject, predicate FROM facts WHERE namespace = ? AND status = 'superseded' AND is_single = 1) k
		JOIN facts f ON f.namespace = ? AND f.subject = k.subject AND f.predicate = k.predicate
		WHERE f.status IN ('active','superseded') AND f.is_single = 1
		ORDER BY f.subject, f.predicate, f.valid_from, f.id`, o.ns, o.ns)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "fact chains")
	}
	type sp struct{ s, p string }
	var facts []chainRow
	for rows.Next() {
		var f chainRow
		if err := rows.Scan(&f.id, &f.subject, &f.predicate, &f.value, &f.status, &f.validFrom, &f.validTo); err != nil {
			rows.Close()
			return nil, err
		}
		facts = append(facts, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(facts) == 0 {
		return &staleScope{}, err
	}
	// Resolve subjects through accepted merges: chains of merged entities join.
	subjects := map[string]bool{}
	for _, f := range facts {
		subjects[f.subject] = true
	}
	into, err := r.mergeTargets(ctx, o.ns, sortedKeys(subjects))
	if err != nil {
		return nil, err
	}
	resolve := func(s string) string {
		for hop := 0; hop < maxMergeHops; hop++ {
			to, ok := into[s]
			if !ok {
				break
			}
			s = to
		}
		return s
	}
	type group struct {
		holding, all map[string]bool
	}
	groups := map[sp]*group{}
	for _, f := range facts {
		k := sp{resolve(f.subject), f.predicate}
		g := groups[k]
		if g == nil {
			g = &group{holding: map[string]bool{}, all: map[string]bool{}}
			groups[k] = g
		}
		g.all[f.value] = true
		holds := f.status == string(core.FactActive)
		if o.asOfExplicit {
			holds = validAt(f.validFrom, f.validTo, o.asOf)
		}
		if holds {
			g.holding[f.value] = true
		}
	}
	chains := map[sp]*staleChain{}
	for k, g := range groups {
		ch := &staleChain{holding: sortedKeys(g.holding)}
		for _, v := range sortedKeys(g.all) {
			if !g.holding[v] && utf8.RuneCountInString(v) >= minStaleValueRunes {
				ch.stale = append(ch.stale, v)
			}
		}
		if len(ch.stale) > 0 {
			chains[k] = ch
		}
	}
	sc := &staleScope{byWord: map[string][]*staleAlias{}, bySession: map[string][]*staleChain{}}
	if len(chains) == 0 {
		return sc, nil
	}
	bySubject := map[string][]*staleChain{}
	var chainKeys []sp
	for k := range chains {
		chainKeys = append(chainKeys, k)
	}
	sort.Slice(chainKeys, func(i, j int) bool {
		if chainKeys[i].s != chainKeys[j].s {
			return chainKeys[i].s < chainKeys[j].s
		}
		return chainKeys[i].p < chainKeys[j].p
	})
	for _, k := range chainKeys {
		bySubject[k.s] = append(bySubject[k.s], chains[k])
	}
	// Attribute labels: an excerpt that names the predicate ("diária",
	// "centro de custo") and states a value that no longer holds for it.
	// Only values that hold for no chain of that predicate count, so a value
	// current for one subject never removes an excerpt through another's.
	holdingByPred, staleByPred := map[string]map[string]bool{}, map[string]map[string]bool{}
	for k, g := range groups {
		if holdingByPred[k.p] == nil {
			holdingByPred[k.p], staleByPred[k.p] = map[string]bool{}, map[string]bool{}
		}
		for v := range g.holding {
			holdingByPred[k.p][v] = true
		}
		if ch := chains[k]; ch != nil {
			for _, v := range ch.stale {
				staleByPred[k.p][v] = true
			}
		}
	}
	for _, p := range sortedKeys(staleByPred) {
		ch := &staleChain{holding: sortedKeys(holdingByPred[p])}
		for _, v := range sortedKeys(staleByPred[p]) {
			if !holdingByPred[p][v] {
				ch.stale = append(ch.stale, v)
			}
		}
		label := norm.Alias(strings.ReplaceAll(p, "_", " "))
		if len(ch.stale) == 0 || utf8.RuneCountInString(label) < minStaleAliasRunes {
			continue
		}
		w := strings.Fields(label)[0]
		sc.byWord[w] = append(sc.byWord[w], &staleAlias{text: " " + label + " ", chains: []*staleChain{ch}})
	}
	// Aliases of every entity whose facts are in a chain (merged ones too).
	ents := map[string]bool{}
	for s := range subjects {
		if len(bySubject[resolve(s)]) > 0 {
			ents[s], ents[resolve(s)] = true, true
		}
	}
	entList := sortedKeys(ents)
	for start := 0; start < len(entList); start += inBatch {
		batch := entList[start:min(start+inBatch, len(entList))]
		args := []any{o.ns}
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := r.db().QueryContext(ctx, `SELECT alias_norm, entity_id FROM entity_aliases
			WHERE namespace = ? AND entity_id IN (`+placeholders(len(batch))+`) ORDER BY alias_norm, entity_id`, args...)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "entity aliases")
		}
		for rows.Next() {
			var alias, ent string
			if err := rows.Scan(&alias, &ent); err != nil {
				rows.Close()
				return nil, err
			}
			alias = norm.Alias(alias)
			if utf8.RuneCountInString(alias) < minStaleAliasRunes {
				continue
			}
			w := strings.Fields(alias)[0]
			sc.byWord[w] = append(sc.byWord[w], &staleAlias{text: " " + alias + " ", chains: bySubject[resolve(ent)]})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	// Chains asserted in the candidates' sessions: the fact_assertion events
	// of a session and the facts derived from them.
	for start := 0; start < len(sessions); start += inBatch {
		batch := sessions[start:min(start+inBatch, len(sessions))]
		args := []any{o.ns}
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := r.db().QueryContext(ctx, `SELECT DISTINCT a.session_id, f.subject, f.predicate
			FROM events a JOIN provenance p ON p.source_type = 'event' AND p.source_id = a.id
			  AND p.derived_type = 'fact' AND p.relation IN ('derived_from','confirmed_by')
			JOIN facts f ON f.id = p.derived_id
			WHERE a.namespace = ? AND a.kind = 'fact_assertion' AND a.session_id IN (`+placeholders(len(batch))+`)
			ORDER BY 1, 2, 3`, args...)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "session assertions")
		}
		for rows.Next() {
			var sess, subj, pred string
			if err := rows.Scan(&sess, &subj, &pred); err != nil {
				rows.Close()
				return nil, err
			}
			if ch := chains[sp{resolve(subj), pred}]; ch != nil {
				sc.bySession[sess] = append(sc.bySession[sess], ch)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return sc, nil
}

// mergeTargets returns merged_into for the entities reachable from ids.
func (r *Reader) mergeTargets(ctx context.Context, ns string, ids []string) (map[string]string, error) {
	into := map[string]string{}
	seen := map[string]bool{}
	frontier := append([]string(nil), ids...)
	for _, id := range ids {
		seen[id] = true
	}
	for hop := 0; hop < maxMergeHops && len(frontier) > 0; hop++ {
		sort.Strings(frontier)
		var next []string
		for start := 0; start < len(frontier); start += inBatch {
			batch := frontier[start:min(start+inBatch, len(frontier))]
			args := []any{ns}
			for _, id := range batch {
				args = append(args, id)
			}
			rows, err := r.db().QueryContext(ctx, `SELECT id, merged_into FROM entities
				WHERE +namespace = ? AND merged_into IS NOT NULL AND id IN (`+placeholders(len(batch))+`) ORDER BY id`, args...)
			if err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "entity merges")
			}
			for rows.Next() {
				var id, to string
				if err := rows.Scan(&id, &to); err != nil {
					rows.Close()
					return nil, err
				}
				into[id] = to
				if !seen[to] {
					seen[to] = true
					next = append(next, to)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		frontier = next
	}
	return into, nil
}
