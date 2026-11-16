package read

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/felipemarts/chanda/internal/store"
)

// Exact keyword top-k with common terms set aside (decision 0122).
//
// FTS5 computes bm25() for every row that matches the OR query, so a term
// present in half of the rows costs half of the table on every query. BM25
// is a sum over the query's phrases, each bounded by idf·(k1+1), so the
// rows that hold only common terms cannot outrank a row whose score is at
// least the sum of the common terms' bounds (MaxScore). The query terms are
// split into common C (in at least a quarter of the rows) and rare R:
//
//   - E1 = (C) AND (R) gives, for every row holding a term of each group,
//     its exact full score (bm25 sums the phrases a row holds; phrase order
//     C then R, the order of the full expression, so the sums are the same
//     to the bit);
//   - E2 = R, best first, gives the exact score of every row holding only
//     rare terms (the common phrases add exactly 0.0 to them);
//
// and the k-th best eligible row of E1 ∪ E2 must score at least UB(C) (no
// row holding only common terms can reach it) and strictly more than the
// last row E2 returned (no unread rare-only row can). Otherwise E2 grows or
// the full query runs, so the result is always the full query's.
//
// Row counts and document frequencies come from the FTS table itself and
// are cached while its structure record (the %_data row 10 that every
// write to the index rewrites) is unchanged.

const (
	bm25K1      = 1.2
	commonShare = 4 // a term in at least 1/commonShare of the rows is common
)

// ftsStats caches per FTS table an upper bound of its row count and the
// document frequency of terms.
type ftsStats struct {
	mu     sync.Mutex
	tables map[string]*ftsTableStats
}

type ftsTableStats struct {
	structure []byte
	maxRow    int64
	df        map[string]int64
}

func newFTSStats() *ftsStats { return &ftsStats{tables: map[string]*ftsTableStats{}} }

// termStats returns max(rowid) of fts (≥ its row count) and the number of
// rows matching each term.
func (st *ftsStats) termStats(ctx context.Context, db store.Queryer, fts string, terms []string) (int64, []int64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var structure []byte
	err := db.QueryRowContext(ctx, `SELECT block FROM `+fts+`_data WHERE id = 10`).Scan(&structure)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, nil, err
	}
	ts := st.tables[fts]
	if ts == nil || !bytes.Equal(ts.structure, structure) {
		ts = &ftsTableStats{structure: structure, df: map[string]int64{}}
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(max(rowid), 0) FROM `+fts).Scan(&ts.maxRow); err != nil {
			return 0, nil, err
		}
		st.tables[fts] = ts
	}
	out := make([]int64, len(terms))
	for i, t := range terms {
		n, ok := ts.df[t]
		if !ok {
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+fts+` WHERE `+fts+` MATCH ?`, matchExpr([]string{t})).Scan(&n); err != nil {
				return 0, nil, err
			}
			ts.df[t] = n
		}
		out[i] = n
	}
	return ts.maxRow, out, nil
}

// idfBound is an upper bound of FTS5's bm25 idf for a term in df of at most
// maxRow rows: log((N − df + 0.5)/(df + 0.5)) grows with N, and FTS5 uses
// 1e-6 when it is not positive.
func idfBound(maxRow, df int64) float64 {
	idf := math.Log((float64(maxRow-df) + 0.5) / (float64(df) + 0.5))
	if idf <= 0 {
		return 1e-6
	}
	return idf
}

// keywordTop returns ftsTop's result for the OR of terms, with common terms
// set aside when that is cheaper (see above). Inside a scoring transaction
// (r.q set) it always runs the full query: pending writes would make the
// cached statistics wrong.
func (r *Reader) keywordTop(ctx context.Context, s ftsSpec, terms []string, ns string, k int, cond string, args []any) ([]hit, error) {
	db := r.db()
	if r.q != nil || r.fts == nil || len(terms) < 2 || k <= 0 {
		return ftsTop(ctx, db, s.fts, s.base, s.bm25, matchExpr(terms), ns, k, cond, args)
	}
	maxRow, df, err := r.fts.termStats(ctx, db, s.fts, terms)
	if err != nil {
		return nil, err
	}
	var common, rare []string
	ub := 0.0
	var dfAll, dfRare int64
	for i, t := range terms {
		dfAll += df[i]
		if df[i]*commonShare >= maxRow && maxRow > 0 {
			common = append(common, t)
			ub += idfBound(maxRow, df[i]) * (bm25K1 + 1)
		} else {
			rare = append(rare, t)
			dfRare += df[i]
		}
	}
	full := matchExpr(append(append([]string{}, common...), rare...))
	// Worth it only when the rare terms hold a small share of the postings:
	// E1 and E2 score rows holding a rare term, the full query every row.
	if len(common) == 0 || len(rare) == 0 || dfRare*commonShare > dfAll {
		return ftsTop(ctx, db, s.fts, s.base, s.bm25, full, ns, k, cond, args)
	}
	ub *= 1 + 1e-9 // the C library's log may differ from Go's in the last bit
	out, ok, err := splitTop(ctx, db, s, "("+matchExpr(common)+") AND ("+matchExpr(rare)+")", matchExpr(rare), ub, ns, k, cond, args)
	if err != nil || ok {
		return out, err
	}
	return ftsTop(ctx, db, s.fts, s.base, s.bm25, full, ns, k, cond, args)
}

// splitTop runs E1 (all rows) and E2 (best n rows, growing) and returns the
// top k when the bounds prove it equals the full query's, else ok = false.
func splitTop(ctx context.Context, db store.Queryer, s ftsSpec, e1, e2 string, ub float64, ns string, k int, cond string, args []any) ([]hit, bool, error) {
	scan := func(q string, a ...any) ([]ftsMatch, error) {
		rows, err := db.QueryContext(ctx, q, a...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ms []ftsMatch
		for rows.Next() {
			var m ftsMatch
			if err := rows.Scan(&m.rowid, &m.bm); err != nil {
				return nil, err
			}
			ms = append(ms, m)
		}
		return ms, rows.Err()
	}
	both, err := scan(`SELECT rowid, `+s.bm25+` FROM `+s.fts+` WHERE `+s.fts+` MATCH ?`, e1)
	if err != nil {
		return nil, false, err
	}
	inBoth := make(map[int64]bool, len(both))
	for _, m := range both {
		inBoth[m.rowid] = true
	}
	for n := k + k/2 + 16; ; n *= 4 {
		rare, err := scan(`SELECT rowid, `+s.bm25+` FROM `+s.fts+` WHERE `+s.fts+` MATCH ? ORDER BY `+s.bm25+` LIMIT ?`, e2, n)
		if err != nil {
			return nil, false, err
		}
		ms := append([]ftsMatch(nil), both...)
		for _, m := range rare {
			if !inBoth[m.rowid] {
				ms = append(ms, m)
			}
		}
		el, err := eligibleMatches(ctx, db, s.fts, s.base, ns, cond, args, ms)
		if err != nil {
			return nil, false, err
		}
		truncated := len(rare) == n
		if len(el) < k {
			if truncated {
				continue // more rare-only rows may be eligible
			}
			return nil, false, nil // rows holding only common terms are needed
		}
		theta := -el[k-1].bm
		if theta < ub {
			return nil, false, nil
		}
		if truncated && theta <= -rare[len(rare)-1].bm {
			continue
		}
		out := make([]hit, k)
		for i, m := range el[:k] {
			out[i] = hit{id: m.id, score: -m.bm, rank: i + 1}
		}
		return out, true, nil
	}
}

// sortMatches orders matches best first: bm25 ascending, then record id.
func sortMatches(el []ftsMatch) {
	sort.SliceStable(el, func(i, j int) bool {
		if el[i].bm != el[j].bm {
			return el[i].bm < el[j].bm
		}
		return el[i].id < el[j].id
	})
}
