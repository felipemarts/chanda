package forget

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/prov"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Entity mention rule (decision 0130). The entity scope finds the records
// that name the entity by matching its names against the text itself, not
// only through `mentions` edges: an entity is often created after the text
// that names it, and nothing guarantees an edge exists.
//
// A name (the canonical name or an alias of the entity or of any entity in
// its merge set) is folded the way FTS5 unicode61 remove_diacritics 2 folds
// text: lowercase, diacritics removed, every rune that is not a letter or a
// number a separator. It matches as a whole-token phrase: the same tokens,
// consecutive, in the same order. A name is used only when it is
// distinctive enough not to sweep unrelated text:
//
//   - two or more tokens with at least minPhraseRunes letters and digits in
//     all ("Ysolde Karvenni", "Abertura de chamados de TI");
//   - a single token of at least minTokenRunes runes ("Zyntharix").
//
// Shorter names ("Owner", "Ana", "Brasil", "TI") are reported as skipped and
// match nothing; the entity's facts and explicit `mentions` edges still
// apply.
const (
	minPhraseRunes = 6
	minTokenRunes  = 8
)

// Term skip reasons.
const (
	SkipShortToken  = "single token shorter than 8 runes"
	SkipShortPhrase = "phrase shorter than 6 letters and digits"
	SkipEmpty       = "no letters or digits"
)

// EntityTerm is one name of the scope's entities and whether the mention
// rule uses it. Returned to the caller only, never stored: the tombstone
// keeps counts.
type EntityTerm struct {
	Entity string `json:"entity"`
	Term   string `json:"term"` // folded form: lowercase, no diacritics, single spaces
	Used   bool   `json:"used"`
	Skip   string `json:"skip,omitempty"`
}

// Mention is one record whose text names the entity, with the folded term
// that matched (the first, in term order, when several did). Returned to
// the caller only, never stored.
type Mention struct {
	ID   string `json:"id"`
	Term string `json:"term"`
}

// fold returns the tokens of s as FTS5 unicode61 with remove_diacritics 2
// sees them: NFD, combining marks dropped, lowercase, split on every rune
// that is not a letter or a number.
func fold(s string) []string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Fields(norm.NFC.String(b.String()))
}

// termRule applies the mention rule to one name: its folded tokens, and the
// reason it is skipped ("" when it is used).
func termRule(name string) ([]string, string) {
	toks := fold(name)
	n := 0
	for _, t := range toks {
		n += utf8.RuneCountInString(t)
	}
	switch {
	case len(toks) == 0:
		return nil, SkipEmpty
	case len(toks) == 1 && n < minTokenRunes:
		return toks, SkipShortToken
	case len(toks) > 1 && n < minPhraseRunes:
		return toks, SkipShortPhrase
	}
	return toks, ""
}

// containsPhrase reports whether the token sequence phrase occurs in text,
// folded, on token boundaries.
func containsPhrase(text string, phrase []string) bool {
	toks := fold(text)
	for i := 0; i+len(phrase) <= len(toks); i++ {
		ok := true
		for j, p := range phrase {
			if toks[i+j] != p {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// mentionTerm is a used term with its tokens.
type mentionTerm struct {
	term string
	toks []string
}

// ftsQuery is the FTS5 phrase query for the term. FTS5 tokenizes the quoted
// string with the table's own tokenizer, so case and diacritics fold the
// same way on both sides.
func (m mentionTerm) ftsQuery() string { return ftsPhrase(m.term) }

// entitySet resolves ref to the entities it names: the entity (by id,
// canonical name or alias), followed up its merged_into chain to the entity
// that absorbed it, plus every entity merged into that one, directly or
// through a chain. An accepted merge says they are the same real-world
// entity, and reads already resolve names that way.
func entitySet(ctx context.Context, q store.Queryer, ns, ref string) ([]string, error) {
	seeds, err := ids(ctx, q, `SELECT id FROM entities WHERE namespace = ?1 AND (id = ?2 OR canonical_name = ?2 COLLATE NOCASE)
		UNION SELECT entity_id FROM entity_aliases WHERE namespace = ?1 AND alias_norm IN (?2, ?3) ORDER BY 1`,
		ns, ref, strings.Join(fold(ref), " "))
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, id := range seeds {
		root, err := mergeRoot(ctx, q, ns, id)
		if err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	var out []string
	for _, root := range prov.SortedUnique(roots) {
		set, err := ids(ctx, q, `WITH RECURSIVE m(id, depth) AS (
				SELECT ?1, 0
				UNION SELECT e.id, m.depth + 1 FROM entities e JOIN m ON e.merged_into = m.id
				WHERE e.namespace = ?2 AND m.depth < ?3)
			SELECT DISTINCT id FROM m ORDER BY id`, root, ns, maxMergeHops)
		if err != nil {
			return nil, err
		}
		out = append(out, set...)
	}
	return prov.SortedUnique(out), nil
}

// maxMergeHops bounds a merged_into chain walk (as internal/facts does).
const maxMergeHops = 16

func mergeRoot(ctx context.Context, q store.Queryer, ns, id string) (string, error) {
	for i := 0; i < maxMergeHops; i++ {
		var into sql.NullString
		err := q.QueryRowContext(ctx, `SELECT merged_into FROM entities WHERE namespace = ? AND id = ?`, ns, id).Scan(&into)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		if !into.Valid || into.String == "" {
			return id, nil
		}
		id = into.String
	}
	return "", core.Errf(core.CodeIntegrity, "forget: entity merge chain too long at %s", id)
}

// entityTerms lists every name of the entities (canonical names and
// aliases) with the rule applied, sorted by term then entity, and the used
// terms, unique and sorted.
func entityTerms(ctx context.Context, q store.Queryer, ns string, ents []string) ([]EntityTerm, []mentionTerm, error) {
	var all []EntityTerm
	seen := map[string]bool{}
	var used []mentionTerm
	for _, ent := range ents {
		names, err := ids(ctx, q, `SELECT canonical_name FROM entities WHERE namespace = ?1 AND id = ?2
			UNION SELECT alias_norm FROM entity_aliases WHERE namespace = ?1 AND entity_id = ?2 ORDER BY 1`, ns, ent)
		if err != nil {
			return nil, nil, err
		}
		byTerm := map[string]EntityTerm{}
		for _, name := range names {
			toks, skip := termRule(name)
			t := EntityTerm{Entity: ent, Term: strings.Join(toks, " "), Used: skip == "", Skip: skip}
			if _, ok := byTerm[t.Term]; ok {
				continue // canonical name and alias fold to the same term
			}
			byTerm[t.Term] = t
			if t.Used && !seen[t.Term] {
				seen[t.Term] = true
				used = append(used, mentionTerm{term: t.Term, toks: toks})
			}
		}
		for _, t := range byTerm {
			all = append(all, t)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Term != all[j].Term {
			return all[i].Term < all[j].Term
		}
		return all[i].Entity < all[j].Entity
	})
	sort.Slice(used, func(i, j int) bool { return used[i].term < used[j].term })
	return all, used, nil
}

// mentionHits are the records of the namespace whose text names a used
// term: events (content), facts (text), pages whose title names it, and
// page items (text) of pages whose title or body names it.
type mentionHits struct {
	events, facts, pages, items []Mention
}

func findMentions(ctx context.Context, q store.Queryer, ns string, terms []mentionTerm) (mentionHits, error) {
	var h mentionHits
	first := map[string]bool{}
	add := func(l *[]Mention, id, term string) {
		if !first[id] {
			first[id] = true
			*l = append(*l, Mention{ID: id, Term: term})
		}
	}
	for _, t := range terms {
		evs, err := ids(ctx, q, `SELECT record_id FROM events_fts WHERE events_fts MATCH ? AND namespace = ? ORDER BY record_id`, t.ftsQuery(), ns)
		if err != nil {
			return h, err
		}
		for _, id := range evs {
			add(&h.events, id, t.term)
		}
		fs, err := ids(ctx, q, `SELECT record_id FROM facts_fts WHERE facts_fts MATCH ? AND namespace = ? ORDER BY record_id`, t.ftsQuery(), ns)
		if err != nil {
			return h, err
		}
		for _, id := range fs {
			add(&h.facts, id, t.term)
		}
		titled, err := ids(ctx, q, `SELECT record_id FROM pages_fts WHERE pages_fts MATCH ? AND namespace = ? ORDER BY record_id`, `title : `+t.ftsQuery(), ns)
		if err != nil {
			return h, err
		}
		for _, id := range titled {
			add(&h.pages, id, t.term)
		}
		pages, err := ids(ctx, q, `SELECT record_id FROM pages_fts WHERE pages_fts MATCH ? AND namespace = ? ORDER BY record_id`, t.ftsQuery(), ns)
		if err != nil {
			return h, err
		}
		for _, pg := range pages {
			items, err := pageItemTexts(ctx, q, pg)
			if err != nil {
				return h, err
			}
			for _, it := range items {
				if containsPhrase(it[1], t.toks) {
					add(&h.items, it[0], t.term)
				}
			}
		}
	}
	for _, l := range []*[]Mention{&h.events, &h.facts, &h.pages, &h.items} {
		sort.Slice(*l, func(i, j int) bool { return (*l)[i].ID < (*l)[j].ID })
	}
	return h, nil
}

func pageItemTexts(ctx context.Context, q store.Queryer, page string) ([][2]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, text FROM page_items WHERE page_id = ? ORDER BY id`, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var r [2]string
		if err := rows.Scan(&r[0], &r[1]); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func mentionIDs(ms []Mention) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

// entity resolves the entity scope (decision 0050, completed by 0130): the
// events that name the entity (mention rule) or carry a `mentions` edge to
// it; facts whose subject is the entity, that name it or that carry a
// `mentions` edge, and the events they came from; page items that name it
// and pages whose title names it. Derived records of those events follow in
// cascade like any other scope.
func (r *resolved) entity(ctx context.Context, q store.Queryer, ns, ref string) error {
	ents, err := entitySet(ctx, q, ns, ref)
	if err != nil {
		return err
	}
	if len(ents) == 0 {
		return core.Errf(core.CodeNotFound, "forget: entity not found")
	}
	r.entityScope = true
	r.desc["entities"] = ents
	r.entities = append(r.entities, ents...)
	if r.terms, r.used, err = entityTerms(ctx, q, ns, ents); err != nil {
		return err
	}
	r.desc["terms_used"] = len(r.used)
	r.desc["terms_skipped"] = len(r.terms) - len(r.used)

	hits, err := findMentions(ctx, q, ns, r.used)
	if err != nil {
		return err
	}
	for _, l := range [][]Mention{hits.events, hits.facts, hits.pages, hits.items} {
		r.mentions = append(r.mentions, l...)
	}
	sort.Slice(r.mentions, func(i, j int) bool { return r.mentions[i].ID < r.mentions[j].ID })
	r.roots = append(r.roots, mentionIDs(hits.events)...)
	r.pages = append(r.pages, mentionIDs(hits.pages)...)
	r.items = append(r.items, mentionIDs(hits.items)...)
	facts := mentionIDs(hits.facts)

	for _, ent := range ents {
		// Explicit mention edges, when a writer recorded them.
		evs, err := ids(ctx, q, `SELECT derived_id FROM provenance WHERE source_type = 'entity' AND source_id = ?
			AND relation = 'mentions' AND derived_type = 'event' ORDER BY derived_id`, ent)
		if err != nil {
			return err
		}
		r.roots = append(r.roots, evs...)
		fs, err := ids(ctx, q, `SELECT id FROM facts WHERE namespace = ?1 AND subject = ?2
			UNION SELECT derived_id FROM provenance WHERE source_type = 'entity' AND source_id = ?2
			AND relation = 'mentions' AND derived_type = 'fact' ORDER BY 1`, ns, ent)
		if err != nil {
			return err
		}
		facts = append(facts, fs...)
	}
	facts = prov.SortedUnique(facts)
	r.facts = append(r.facts, facts...)
	// The events the facts came from.
	for _, f := range facts {
		origins, err := prov.Origins(ctx, q, core.RecFact, f)
		if err != nil {
			return err
		}
		for _, o := range origins {
			r.roots = append(r.roots, o.ID)
		}
	}
	return nil
}
