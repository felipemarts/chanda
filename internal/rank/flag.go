package rank

import (
	"context"
	"database/sql"
	"strings"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
)

// FlagResult reports a flag.
type FlagResult struct {
	ID         string          `json:"id"`
	AuditID    int64           `json:"audit_id"`
	RecordType core.RecordType `json:"record_type"`
	FactStatus string          `json:"fact_status,omitempty"` // new status when a fact changed
}

// flaggable maps the record types that can be flagged to their tables.
var flaggable = map[core.RecordType]string{
	core.RecEvent:    "events",
	core.RecFact:     "facts",
	core.RecPage:     "pages",
	core.RecPageItem: "page_items",
}

// Flag marks a record as wrong or outdated (BASE.md "Signals": strong
// negative; removes the record from reads). In one transaction it inserts a
// flags row, adds cfg.Ranking.FlagPenalty to the record's utility beta and
// audits ActFlag. An active fact becomes 'flagged' (which also frees its
// single-predicate slot); facts in other statuses are already out of current
// reads and keep their status. Events are immutable and pages have no
// flagged status, so for them the flags row is the exclusion: the read path
// must filter every record type on the flags table (ExcludeFlaggedSQL).
// Flag never deletes anything. Flagging again adds another row and penalty.
func (r *Ranker) Flag(ctx context.Context, ns, id, reason, actor string) (FlagResult, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case ns == "":
		return FlagResult{}, core.Errf(core.CodeInvalidArgument, "namespace is required")
	case id == "":
		return FlagResult{}, core.Errf(core.CodeInvalidArgument, "id is required")
	case reason == "":
		return FlagResult{}, core.Errf(core.CodeInvalidArgument, "reason is required")
	case actor == "":
		return FlagResult{}, core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	rt := core.RecordTypeOf(id)
	table, ok := flaggable[rt]
	if !ok {
		return FlagResult{}, core.Errf(core.CodeInvalidArgument, "records of type %q cannot be flagged", rt)
	}
	res := FlagResult{RecordType: rt}
	now := r.now()
	err := r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ? AND namespace = ?`, id, ns).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return core.Errf(core.CodeNotFound, "%s %s not found", rt, id)
		}
		res.ID = r.env.IDs.New(core.PrefixFlag)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO flags(id, namespace, record_type, record_id, reason, actor, ts) VALUES (?,?,?,?,?,?,?)`,
			res.ID, ns, string(rt), id, reason, actor, now); err != nil {
			return err
		}
		if rt == core.RecFact {
			out, err := tx.ExecContext(ctx, `UPDATE facts SET status = ? WHERE id = ? AND namespace = ? AND status = ?`,
				string(core.FactFlagged), id, ns, string(core.FactActive))
			if err != nil {
				return err
			}
			if k, _ := out.RowsAffected(); k > 0 {
				res.FactStatus = string(core.FactFlagged)
			}
		}
		rows, err := loadURows(ctx, tx, ns, []string{id})
		if err != nil {
			return err
		}
		applyFlag(rows, r.env.Cfg.Ranking, rt, id, now)
		if err := writeURows(ctx, tx, ns, rows); err != nil {
			return err
		}
		details := map[string]any{"flag_id": res.ID, "record_type": rt, "penalty": r.env.Cfg.Ranking.FlagPenalty}
		if res.FactStatus != "" {
			details["fact_status"] = res.FactStatus
		}
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActFlag, Target: id, Details: details,
		})
		return err
	})
	if err != nil {
		return FlagResult{}, err
	}
	return res, nil
}

// ExcludeFlaggedSQL returns a SQL condition, for the read path's WHERE
// clause, that is true when the record whose id is in column col has no flag:
//
//	... WHERE e.namespace = ? AND ` + rank.ExcludeFlaggedSQL("e.id") + `
//
// Record ids are globally unique, so no namespace term is needed.
func ExcludeFlaggedSQL(col string) string {
	return "NOT EXISTS (SELECT 1 FROM flags WHERE flags.record_id = " + col + ")"
}

// IsFlagged reports whether id has been flagged in ns.
func (r *Ranker) IsFlagged(ctx context.Context, ns, id string) (bool, error) {
	var n int
	err := r.env.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM flags WHERE namespace = ? AND record_id = ?`, ns, id).Scan(&n)
	return n > 0, err
}

// FlaggedIDs returns, sorted, the ids among ids that are flagged in ns; with
// ids nil it returns every flagged id of the namespace. The read path calls it
// after candidate generation to drop flagged records.
func (r *Ranker) FlaggedIDs(ctx context.Context, ns string, ids []string) ([]string, error) {
	db := r.env.Store.DB()
	var out []string
	collect := func(q string, args ...any) error {
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	}
	if ids == nil {
		if err := collect(`SELECT DISTINCT record_id FROM flags WHERE namespace = ? ORDER BY record_id`, ns); err != nil {
			return nil, err
		}
		return out, nil
	}
	for _, chunk := range chunks(uniqueSorted(ids), 500) {
		if err := collect(`SELECT DISTINCT record_id FROM flags WHERE namespace = ? AND record_id IN (`+placeholders(len(chunk))+`) ORDER BY record_id`,
			append([]any{ns}, toAny(chunk)...)...); err != nil {
			return nil, err
		}
	}
	return out, nil
}
