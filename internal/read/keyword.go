package read

import (
	"context"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
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
	if matchExpr(terms) == "" {
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
		cond := s.extra + ` AND NOT EXISTS (SELECT 1 FROM flags fl WHERE fl.record_id = b.id)`
		var args []any
		if len(f.zones) > 0 {
			cond += ` AND b.trust_zone IN (` + placeholders(len(f.zones)) + `)`
			for _, z := range f.zones {
				args = append(args, string(z))
			}
		}
		list, err := r.keywordTop(ctx, s, terms, ns, k, cond, args)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "keyword search "+string(s.typ))
		}
		for i := range list {
			list[i].typ = s.typ
		}
		out[s.typ] = list
	}
	return out, nil
}

// TopEventMatches returns the ids of the best k events of namespace ns for
// the FTS5 expression expr by BM25 (ties by id) among the events row b that
// pass cond (SQL over alias b, with args). The near-duplicate guard of the
// write path uses it inside the write transaction (q).
func TopEventMatches(ctx context.Context, q store.Queryer, ns, expr string, k int, cond string, args ...any) ([]string, error) {
	if cond == "" {
		cond = "1=1"
	}
	hs, err := ftsTop(ctx, q, "events_fts", "events", "bm25(events_fts)", expr, ns, k, cond, args)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(hs))
	for i, h := range hs {
		ids[i] = h.id
	}
	return ids, nil
}

// ftsTop returns the k best rows of the FTS5 table fts for expr whose base
// row b (table base, b.id = record_id) is in namespace ns and passes cond,
// best first: bm25 ascending, ties by record id; score = -bm25.
//
// It equals
//
//	SELECT record_id, -bm25 FROM fts JOIN base b ... WHERE fts MATCH expr
//	  AND namespace AND cond ORDER BY bm25, record_id LIMIT k
//
// but runs in two phases (decision 0100): the FTS index alone ranks the
// matches by rowid and bm25 (no content row is read), then only the best n
// rowids are resolved to records and filtered. n starts at 1.5k+16 and grows
// until the k-th eligible row ranks strictly before the n-th match (so no
// tie or filtered row beyond n could change the answer) or the matches run
// out. A common query term matching most of the corpus no longer costs a
// content and base row read per match.
func ftsTop(ctx context.Context, db store.Queryer, fts, base, bm25, expr, ns string, k int, cond string, condArgs []any) ([]hit, error) {
	if k <= 0 || expr == "" {
		return nil, nil
	}
	n := k + k/2 + 16 // a little room for rows the filter drops; grows ×4 when short
	for {
		rows, err := db.QueryContext(ctx, `SELECT rowid, `+bm25+` FROM `+fts+` WHERE `+fts+` MATCH ? ORDER BY `+bm25+` LIMIT ?`, expr, n)
		if err != nil {
			return nil, err
		}
		var ms []ftsMatch
		for rows.Next() {
			var m ftsMatch
			if err := rows.Scan(&m.rowid, &m.bm); err != nil {
				rows.Close()
				return nil, err
			}
			ms = append(ms, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		full := len(ms) == n
		el, err := eligibleMatches(ctx, db, fts, base, ns, cond, condArgs, ms)
		if err != nil {
			return nil, err
		}
		if !full || (len(el) >= k && el[k-1].bm < ms[len(ms)-1].bm) {
			el = el[:min(k, len(el))]
			out := make([]hit, len(el))
			for i, m := range el {
				out[i] = hit{id: m.id, score: -m.bm, rank: i + 1}
			}
			return out, nil
		}
		n *= 4
	}
}

// ftsMatch is one FTS5 row: its rowid, bm25() and, once resolved, record id.
type ftsMatch struct {
	rowid int64
	bm    float64
	id    string
}

// eligibleMatches resolves the rowids of ms to record ids of namespace ns
// and keeps those whose base row passes cond, best first (sortMatches).
func eligibleMatches(ctx context.Context, db store.Queryer, fts, base, ns, cond string, condArgs []any, ms []ftsMatch) ([]ftsMatch, error) {
	byRow := map[int64]string{}
	for start := 0; start < len(ms); start += inBatch {
		batch := ms[start:min(start+inBatch, len(ms))]
		args := make([]any, 0, len(batch)+1)
		for _, m := range batch {
			args = append(args, m.rowid)
		}
		args = append(args, ns)
		rows, err := db.QueryContext(ctx, `SELECT rowid, record_id FROM `+fts+` WHERE rowid IN (`+placeholders(len(batch))+`)
			AND namespace = ? ORDER BY rowid`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var rid int64
			var id string
			if err := rows.Scan(&rid, &id); err != nil {
				rows.Close()
				return nil, err
			}
			byRow[rid] = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	var ids []string
	for i := range ms {
		ms[i].id = byRow[ms[i].rowid]
		if ms[i].id != "" {
			ids = append(ids, ms[i].id)
		}
	}
	ok := map[string]bool{}
	ids = uniqSorted(ids)
	for start := 0; start < len(ids); start += inBatch {
		batch := ids[start:min(start+inBatch, len(ids))]
		args := []any{ns}
		for _, id := range batch {
			args = append(args, id)
		}
		args = append(args, condArgs...)
		got, err := queryStringsOn(ctx, db, `SELECT b.id FROM `+base+` b WHERE +b.namespace = ? AND b.id IN (`+
			placeholders(len(batch))+`) AND (`+cond+`) ORDER BY b.id`, args...)
		if err != nil {
			return nil, err
		}
		for _, id := range got {
			ok[id] = true
		}
	}
	var el []ftsMatch
	for _, m := range ms {
		if ok[m.id] {
			el = append(el, m)
		}
	}
	sortMatches(el)
	return el, nil
}

// queryStringsOn runs a one-column query on q and collects the result.
func queryStringsOn(ctx context.Context, q store.Queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
