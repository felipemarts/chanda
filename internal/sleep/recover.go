package sleep

import (
	"context"
	"database/sql"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// RecoverStale marks as failed every run still "running" that started more
// than sleep.stale_run_minutes ago (a process that crashed or was killed in
// the middle of a run: its staging transaction was rolled back by SQLite,
// but the row was never closed). Each recovery is audited
// (sleep_recover). It returns the recovered run ids, sorted. A run of this
// process is never recovered: the caller holds no run, or holds the run
// lock (Run).
func (s *Sleeper) RecoverStale(ctx context.Context) ([]string, error) {
	mins := s.env.Cfg.Sleep.StaleRunMinutes
	if mins <= 0 {
		return nil, nil
	}
	now := s.env.Clock.Now()
	cutoff := core.FormatTime(now.Add(-time.Duration(mins) * time.Minute))
	var out []string
	err := s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, namespace FROM sleep_runs WHERE status = ? AND started_at < ? ORDER BY id`,
			StatusRunning, cutoff)
		if err != nil {
			return err
		}
		type stale struct{ id, ns string }
		var found []stale
		for rows.Next() {
			var r stale
			if err := rows.Scan(&r.id, &r.ns); err != nil {
				rows.Close()
				return err
			}
			found = append(found, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		ts := core.FormatTime(now)
		for _, r := range found {
			if _, err := tx.ExecContext(ctx, `UPDATE sleep_runs SET status = ?, ended_at = ?,
				report = report || ? WHERE id = ? AND status = ?`, StatusFailed, ts,
				"\n\nRecovered as failed: the run was still marked running after "+time.Duration(mins*int(time.Minute)).String()+
					" (the process stopped during the run; uncommitted work was rolled back).\n", r.id, StatusRunning); err != nil {
				return err
			}
			if _, err := s.env.Audit.Append(ctx, tx, audit.Entry{Namespace: r.ns, Actor: Actor, Action: audit.ActSleepRecover,
				Target: r.id, Details: map[string]any{"status": StatusFailed, "stale_run_minutes": mins}}); err != nil {
				return err
			}
			out = append(out, r.id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
