package read

import (
	"context"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
)

// maxAliasWords bounds the query n-grams looked up as entity aliases.
const maxAliasWords = 8

// queryEntities returns the entities the query names (an alias of the
// entity, as whole words of norm.Alias(query)), resolved through accepted
// merges, and the aliases that matched.
func (r *Reader) queryEntities(ctx context.Context, ns, text string) (map[string]bool, []string, error) {
	words := strings.Fields(norm.Alias(text))
	grams := map[string]bool{}
	for i := range words {
		for n := 1; n <= maxAliasWords && i+n <= len(words); n++ {
			g := strings.Join(words[i:i+n], " ")
			if len([]rune(g)) >= minStaleAliasRunes {
				grams[g] = true
			}
		}
	}
	if len(grams) == 0 {
		return nil, nil, nil
	}
	list := sortedKeys(grams)
	ents, aliases := map[string]bool{}, []string{}
	for start := 0; start < len(list); start += inBatch {
		batch := list[start:min(start+inBatch, len(list))]
		args := []any{ns}
		for _, g := range batch {
			args = append(args, g)
		}
		rows, err := r.db().QueryContext(ctx, `SELECT alias_norm, entity_id FROM entity_aliases
			WHERE namespace = ? AND alias_norm IN (`+placeholders(len(batch))+`) ORDER BY alias_norm, entity_id`, args...)
		if err != nil {
			return nil, nil, core.Wrap(core.CodeInternal, err, "query entities")
		}
		for rows.Next() {
			var a, id string
			if err := rows.Scan(&a, &id); err != nil {
				rows.Close()
				return nil, nil, err
			}
			ents[id] = true
			aliases = append(aliases, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	if len(ents) == 0 {
		return nil, nil, nil
	}
	into, err := r.mergeTargets(ctx, ns, sortedKeys(ents))
	if err != nil {
		return nil, nil, err
	}
	out := map[string]bool{}
	for id := range ents {
		out[resolveMerge(into, id)] = true
	}
	return out, aliases, nil
}

func resolveMerge(into map[string]string, id string) string {
	for hop := 0; hop < maxMergeHops; hop++ {
		to, ok := into[id]
		if !ok {
			break
		}
		id = to
	}
	return id
}

// markEntityMatch sets the entity_match feature (BASE.md "Signals": the
// query names an entity the record is about): a fact whose subject is an
// entity the query names; an event that states a fact about one, or whose
// text or heading path names it with an alias the query used; a page whose
// title or body does. It is a ranking feature only: it never changes the
// relevance, the cut or the order at λ = 0.
func (r *Reader) markEntityMatch(ctx context.Context, ns, text string, cs []*cand) error {
	if len(cs) == 0 {
		return nil
	}
	ents, aliases, err := r.queryEntities(ctx, ns, text)
	if err != nil || len(ents) == 0 {
		return err
	}
	// Subjects of the facts the events state, resolved through merges.
	subjects := map[string]bool{}
	for _, c := range cs {
		for _, d := range c.rec.derived {
			subjects[d.subject] = true
		}
	}
	into, err := r.mergeTargets(ctx, ns, sortedKeys(subjects))
	if err != nil {
		return err
	}
	names := func(s string) bool {
		t := " " + norm.Alias(s) + " "
		for _, a := range aliases {
			if strings.Contains(t, " "+a+" ") {
				return true
			}
		}
		return false
	}
	for _, c := range cs {
		switch c.rec.typ {
		case core.RecFact:
			c.entityMatch = ents[c.rec.subject]
		case core.RecEvent:
			for _, d := range c.rec.derived {
				if ents[resolveMerge(into, d.subject)] {
					c.entityMatch = true
					break
				}
			}
			if !c.entityMatch {
				c.entityMatch = names(c.rec.headingPath + " " + c.rec.text)
			}
		default:
			c.entityMatch = names(c.rec.searchText())
		}
	}
	return nil
}
