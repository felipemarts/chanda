package read

import (
	"context"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// hit is one entry of a retrieval list (keyword or vector).
type hit struct {
	typ   core.RecordType
	id    string
	score float64 // keyword: -bm25 (higher is better); vector: cosine
	rank  int     // 1-based within its list
}

// keywordFilter narrows the keyword query. These conditions repeat cheap
// parts of the rule filter inside SQL so that ineligible rows do not use up
// the top-K; the full rule filter still runs afterwards.
type keywordFilter struct {
	types          map[core.RecordType]bool // empty = all
	zones          []core.Zone              // empty = all
	withSuperseded bool                     // include_history or an explicit as_of
}

// ftsSpec describes one FTS5 table and its base table.
type ftsSpec struct {
	typ   core.RecordType
	fts   string
	base  string
	bm25  string // bm25() call with column weights
	extra string // eligibility condition on the base table alias b
}

func ftsSpecs(withSuperseded bool) []ftsSpec {
	factStatus := `b.status = 'active'`
	if withSuperseded {
		factStatus = `b.status IN ('active','superseded')`
	}
	return []ftsSpec{
		{core.RecEvent, "events_fts", "events", "bm25(events_fts)", "b.retired_at IS NULL AND b.kind <> 'fact_assertion'"},
		{core.RecFact, "facts_fts", "facts", "bm25(facts_fts)", factStatus},
		// Page titles weigh twice the body.
		{core.RecPage, "pages_fts", "pages", "bm25(pages_fts, 2.0, 1.0)", "b.status = 'active'"},
	}
}

// keywordSearch runs FTS5 BM25 over each requested record type and returns
// one ranked list per type (top cfg.Read.KeywordTopK each). The namespace is
// filtered in the query, both on the FTS row and on the base row.
func (r *Reader) keywordSearch(ctx context.Context, ns string, terms []string, f keywordFilter) (map[core.RecordType][]hit, error) {
	out := map[core.RecordType][]hit{}
	expr := matchExpr(terms)
	if expr == "" {
		return out, nil
	}
	k := r.e.Cfg.Read.KeywordTopK
	if k <= 0 {
		return out, nil
	}
	for _, s := range ftsSpecs(f.withSuperseded) {
		if len(f.types) > 0 && !f.types[s.typ] {
			continue
		}
		q := `SELECT ` + s.fts + `.record_id, -` + s.bm25 + ` FROM ` + s.fts +
			` JOIN ` + s.base + ` b ON b.id = ` + s.fts + `.record_id` +
			` WHERE ` + s.fts + ` MATCH ? AND ` + s.fts + `.namespace = ? AND b.namespace = ? AND ` + s.extra +
			` AND NOT EXISTS (SELECT 1 FROM flags fl WHERE fl.record_id = b.id)`
		args := []any{expr, ns, ns}
		if len(f.zones) > 0 {
			q += ` AND b.trust_zone IN (` + placeholders(len(f.zones)) + `)`
			for _, z := range f.zones {
				args = append(args, string(z))
			}
		}
		q += ` ORDER BY ` + s.bm25 + `, ` + s.fts + `.record_id LIMIT ?`
		args = append(args, k)
		rows, err := r.e.Store.DB().QueryContext(ctx, q, args...)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "keyword search "+string(s.typ))
		}
		var list []hit
		for rows.Next() {
			var h hit
			if err := rows.Scan(&h.id, &h.score); err != nil {
				rows.Close()
				return nil, err
			}
			h.typ = s.typ
			h.rank = len(list) + 1
			list = append(list, h)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out[s.typ] = list
	}
	return out, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
