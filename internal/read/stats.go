package read

import (
	"context"
	"database/sql"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Stats summarizes one namespace.
type Stats struct {
	Namespace   string           `json:"namespace"`
	Counts      map[string]int64 `json:"counts"` // "<table>" and "<table>.<status>" keys
	DBSizeBytes int64            `json:"db_size_bytes"`
	LastSleep   *SleepRun        `json:"last_sleep,omitempty"`
	Ranking     *RankState       `json:"ranking,omitempty"`
	EmbedQueue  int64            `json:"embed_queue"`
}

// SleepRun is the last sleep run of the namespace.
type SleepRun struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
}

// RankState is the namespace's ranking state (rank_state row).
type RankState struct {
	Enabled     bool    `json:"enabled"`
	Lambda      float64 `json:"lambda"`
	OutcomeN    int64   `json:"outcome_n"`
	ActiveModel string  `json:"active_model,omitempty"`
	UpdatedAt   string  `json:"updated_at"`
}

// Stats returns counts per table and status, the database size
// (page_count × page_size, the whole file), the last sleep run, the ranking
// state and the embedding queue length.
func (r *Reader) Stats(ctx context.Context, ns string) (Stats, error) {
	if ns == "" {
		ns = r.e.Cfg.Namespace
	}
	db := r.e.Store.DB()
	st := Stats{Namespace: ns, Counts: map[string]int64{}}
	plain := []string{"sources", "documents", "entities", "provenance", "embeddings", "retrievals", "outcomes", "flags", "page_items", "sleep_runs", "tombstones"}
	for _, t := range plain {
		var n int64
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t+` WHERE namespace = ?`, ns).Scan(&n); err != nil {
			return Stats{}, core.Wrap(core.CodeInternal, err, "count "+t)
		}
		st.Counts[t] = n
	}
	grouped := []struct{ table, expr string }{
		{"events", `CASE WHEN retired_at IS NULL THEN 'live' ELSE 'retired' END`},
		{"facts", "status"},
		{"pages", "status"},
	}
	for _, g := range grouped {
		rows, err := db.QueryContext(ctx, `SELECT `+g.expr+` AS k, COUNT(*) FROM `+g.table+` WHERE namespace = ? GROUP BY k ORDER BY k`, ns)
		if err != nil {
			return Stats{}, core.Wrap(core.CodeInternal, err, "count "+g.table)
		}
		total := int64(0)
		for rows.Next() {
			var k string
			var n int64
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return Stats{}, err
			}
			st.Counts[g.table+"."+k] = n
			total += n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Stats{}, err
		}
		st.Counts[g.table] = total
	}
	var pageCount, pageSize int64
	if err := db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return Stats{}, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return Stats{}, err
	}
	st.DBSizeBytes = pageCount * pageSize

	var run SleepRun
	var ended sql.NullString
	err := db.QueryRowContext(ctx, `SELECT id, status, started_at, ended_at FROM sleep_runs WHERE namespace = ?
		ORDER BY started_at DESC, id DESC LIMIT 1`, ns).Scan(&run.ID, &run.Status, &run.StartedAt, &ended)
	switch {
	case err == nil:
		run.EndedAt = ended.String
		st.LastSleep = &run
	case err != sql.ErrNoRows:
		return Stats{}, core.Wrap(core.CodeInternal, err, "sleep runs")
	}

	var rs RankState
	var enabled int64
	var model sql.NullString
	err = db.QueryRowContext(ctx, `SELECT enabled, lambda, outcome_n, active_model, updated_at FROM rank_state WHERE namespace = ?`, ns).
		Scan(&enabled, &rs.Lambda, &rs.OutcomeN, &model, &rs.UpdatedAt)
	switch {
	case err == nil:
		rs.Enabled, rs.ActiveModel = enabled == 1, model.String
		st.Ranking = &rs
	case err != sql.ErrNoRows:
		return Stats{}, core.Wrap(core.CodeInternal, err, "rank state")
	}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embed_queue WHERE namespace = ?`, ns).Scan(&st.EmbedQueue); err != nil {
		return Stats{}, core.Wrap(core.CodeInternal, err, "embed queue")
	}
	return st, nil
}
