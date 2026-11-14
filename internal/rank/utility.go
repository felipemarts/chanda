package rank

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// urow mirrors one utility row: a Beta(alpha, beta) distribution of the
// record's usefulness (stage 1). The prior is Beta(1,1), score 0.5.
type urow struct {
	Type       core.RecordType
	ID         string
	NShown     int64
	NUsed      int64
	NSuccess   int64
	Alpha      float64
	Beta       float64
	Score      float64
	UpdatedAt  string
	LastUsedAt string
}

func newURow(t core.RecordType, id string) *urow {
	return &urow{Type: t, ID: id, Alpha: 1, Beta: 1, Score: 0.5}
}

func (u *urow) rescore() { u.Score = u.Alpha / (u.Alpha + u.Beta) }

// rowSet holds utility rows being updated, created on first touch.
type rowSet map[string]*urow

func (s rowSet) get(t core.RecordType, id string) *urow {
	if u, ok := s[id]; ok {
		return u
	}
	u := newURow(t, id)
	s[id] = u
	return u
}

// sorted returns the rows ordered by (record_type, record_id).
func (s rowSet) sorted() []*urow {
	out := make([]*urow, 0, len(s))
	for _, u := range s {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// outcomeEffect is what applyOutcome did.
type outcomeEffect struct {
	Centred  float64
	Credited []string // used and delivered, in delivered order
	Ignored  []string // used but not delivered in that retrieval: no credit
}

// applyOutcome is the single implementation of the stage-1 rules, shared by
// the incremental Outcome and by RecomputeUtility so both produce the same
// table bit for bit:
//
//  1. centred = value − namespace moving average (taken BEFORE this outcome);
//     then the average absorbs value: mean += α·(value − mean), n++.
//  2. each delivered id: n_shown++.
//  3. used and delivered: n_used++, last_used_at = ts; centred > 0 → alpha += 1
//     and n_success++; centred < 0 → beta += 1; centred == 0 → no change.
//  4. delivered and not used: beta += unused penalty.
//  5. used but not delivered: ignored (no credit, no penalty).
func applyOutcome(st *rankState, rows rowSet, cfg config.RankingConfig, cands []Candidate, delivered, used []string, value float64, ts string) outcomeEffect {
	eff := outcomeEffect{Centred: value - st.Mean}
	st.Mean += cfg.MovingAverageAlpha * (value - st.Mean)
	st.N++

	types := map[string]core.RecordType{}
	for _, c := range cands {
		if _, ok := types[c.ID]; !ok {
			types[c.ID] = c.RecordType()
		}
	}
	usedSet := map[string]bool{}
	for _, id := range used {
		usedSet[id] = true
	}
	deliveredSet := map[string]bool{}
	for _, id := range delivered {
		deliveredSet[id] = true
		t, ok := types[id]
		if !ok || t == "" {
			t = core.RecordTypeOf(id)
		}
		u := rows.get(t, id)
		u.NShown++
		u.UpdatedAt = ts
		if usedSet[id] {
			eff.Credited = append(eff.Credited, id)
			u.NUsed++
			u.LastUsedAt = ts
			switch {
			case eff.Centred > 0:
				u.Alpha++
				u.NSuccess++
			case eff.Centred < 0:
				u.Beta++
			}
		} else {
			u.Beta += cfg.UnusedPenalty
		}
		u.rescore()
	}
	for _, id := range used {
		if !deliveredSet[id] {
			eff.Ignored = append(eff.Ignored, id)
		}
	}
	return eff
}

// applyFlag adds the strong negative of a flag.
func applyFlag(rows rowSet, cfg config.RankingConfig, t core.RecordType, id, ts string) {
	u := rows.get(t, id)
	u.Beta += cfg.FlagPenalty
	u.UpdatedAt = ts
	u.rescore()
}

const utilityCols = `record_type, record_id, n_shown, n_used, n_success, alpha, beta, score, updated_at, last_used_at`

func scanURows(rows *sql.Rows) ([]*urow, error) {
	defer rows.Close()
	var out []*urow
	for rows.Next() {
		var u urow
		var last sql.NullString
		if err := rows.Scan(&u.Type, &u.ID, &u.NShown, &u.NUsed, &u.NSuccess, &u.Alpha, &u.Beta, &u.Score, &u.UpdatedAt, &last); err != nil {
			return nil, err
		}
		u.LastUsedAt = last.String
		out = append(out, &u)
	}
	return out, rows.Err()
}

// loadURows reads the utility rows of ids in ns (absent ids are absent).
func loadURows(ctx context.Context, q store.Queryer, ns string, ids []string) (rowSet, error) {
	set := rowSet{}
	for _, chunk := range chunks(uniqueSorted(ids), 500) {
		rows, err := q.QueryContext(ctx, `SELECT `+utilityCols+` FROM utility WHERE namespace = ? AND record_id IN (`+placeholders(len(chunk))+`) ORDER BY record_type, record_id`,
			append([]any{ns}, toAny(chunk)...)...)
		if err != nil {
			return nil, err
		}
		us, err := scanURows(rows)
		if err != nil {
			return nil, err
		}
		for _, u := range us {
			set[u.ID] = u
		}
	}
	return set, nil
}

func writeURows(ctx context.Context, q store.Queryer, ns string, rows rowSet) error {
	for _, u := range rows.sorted() {
		_, err := q.ExecContext(ctx, `
INSERT INTO utility(namespace, `+utilityCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(record_type, record_id) DO UPDATE SET n_shown = excluded.n_shown, n_used = excluded.n_used,
  n_success = excluded.n_success, alpha = excluded.alpha, beta = excluded.beta, score = excluded.score,
  updated_at = excluded.updated_at, last_used_at = excluded.last_used_at`,
			ns, u.Type, u.ID, u.NShown, u.NUsed, u.NSuccess, u.Alpha, u.Beta, u.Score, u.UpdatedAt, nullStr(u.LastUsedAt))
		if err != nil {
			return err
		}
	}
	return nil
}

func uniqueSorted(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func chunks(s []string, n int) [][]string {
	var out [][]string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
