package rank

import (
	"context"
	"database/sql"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
)

// SetEnabled is the per-namespace runtime toggle of learned ranking (the
// configuration option ranking.enabled is the global one; both must be on).
// Turning it off also sets λ to 0, so turning it back on does not restore the
// learned order by itself: the model must pass Promote again. Signals keep
// being recorded while off. Audited as ActRankToggle.
func (r *Ranker) SetEnabled(ctx context.Context, ns string, enabled bool, actor string) (int64, error) {
	if err := requireNS(ns); err != nil {
		return 0, err
	}
	if actor == "" {
		return 0, core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	now := r.now()
	var auditID int64
	err := r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		st, err := loadState(ctx, tx, ns)
		if err != nil {
			return err
		}
		from := st.Lambda
		st.Enabled = enabled
		if !enabled {
			st.Lambda = 0
		}
		if err := saveState(ctx, tx, ns, st, now); err != nil {
			return err
		}
		auditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActRankToggle, Target: ns,
			Details: map[string]any{"enabled": enabled, "lambda_from": from, "lambda_to": st.Lambda},
		})
		return err
	})
	return auditID, err
}

// ResetResult reports what Reset cleared.
type ResetResult struct {
	AuditID     int64 `json:"audit_id"`
	UtilityRows int64 `json:"utility_rows"`
	Models      int64 `json:"models"`
}

// Reset is ranking_reset: it deletes the namespace's utility rows and ranker
// models and resets rank_state (λ 0, moving average 0, no active model),
// keeping the runtime toggle as it was. It stamps rank_state.reset_at so
// RecomputeUtility and Train ignore outcomes and flags up to the reset.
// Outcomes, flags and retrievals stay (they are the signal log; flagged
// records stay out of reads). Audited as ActRankReset.
func (r *Ranker) Reset(ctx context.Context, ns, actor string) (ResetResult, error) {
	var res ResetResult
	if err := requireNS(ns); err != nil {
		return res, err
	}
	if actor == "" {
		return res, core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	now := r.now()
	err := r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		st, err := loadState(ctx, tx, ns)
		if err != nil {
			return err
		}
		out, err := tx.ExecContext(ctx, `DELETE FROM utility WHERE namespace = ?`, ns)
		if err != nil {
			return err
		}
		res.UtilityRows, _ = out.RowsAffected()
		if out, err = tx.ExecContext(ctx, `DELETE FROM ranker_models WHERE namespace = ?`, ns); err != nil {
			return err
		}
		res.Models, _ = out.RowsAffected()
		fresh := defaultState()
		fresh.Enabled, fresh.ResetAt = st.Enabled, now
		if err := saveState(ctx, tx, ns, fresh, now); err != nil {
			return err
		}
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActRankReset, Target: ns,
			Details: map[string]any{"utility_rows": res.UtilityRows, "models": res.Models, "lambda_from": st.Lambda},
		})
		return err
	})
	if err != nil {
		return ResetResult{}, err
	}
	return res, nil
}
