package rank

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// SignalTaskOutcome is the only outcomes.signal written today.
const SignalTaskOutcome = "task_outcome"

// Notices returned by Outcome (in the tool envelope's notices).
const (
	NoticeExternalIgnored = "outcome_ignored_external_zone"
	NoticeNotDelivered    = "used_ids_not_delivered_ignored"
)

// OutcomeInput is the `outcome` tool call.
type OutcomeInput struct {
	Namespace   string
	RetrievalID string
	Value       float64  // task outcome in [-1, 1]
	UsedIDs     []string // records the agent actually used
	Actor       string
	ActorZone   core.Zone // zone of the caller's channel; external signals are ignored
}

// OutcomeResult reports what the outcome changed.
type OutcomeResult struct {
	ID       string   `json:"id,omitempty"` // empty when the signal was ignored
	AuditID  int64    `json:"audit_id,omitempty"`
	Centred  float64  `json:"centred"`  // value minus the namespace moving average
	Credited []string `json:"credited"` // used ids that were delivered: they get the credit
	Ignored  []string `json:"ignored"`  // used ids not delivered in that retrieval: no effect
	Notices  []string `json:"notices"`
}

// Outcome records a task outcome for a retrieval and updates utility in the
// same transaction (BASE.md "Signals" and "Stage 1").
//
// Rules: signals from the external zone are ignored (notice, nothing stored);
// at most cfg.Ranking.OutcomeRatePerMinute outcomes per namespace per minute
// of env.Clock time (≤ 0 disables the limit), CodeRateLimited beyond; one
// outcome per retrieval (CodeConflict on a repeat); only used ids that were
// delivered in that retrieval get credit — the others are reported in
// Ignored and change nothing. The outcome row stores all used ids given, so
// RecomputeUtility re-applies the same filter.
func (r *Ranker) Outcome(ctx context.Context, in OutcomeInput) (OutcomeResult, error) {
	res := OutcomeResult{Credited: []string{}, Ignored: []string{}, Notices: []string{}}
	switch {
	case in.Namespace == "":
		return res, core.Errf(core.CodeInvalidArgument, "namespace is required")
	case in.RetrievalID == "":
		return res, core.Errf(core.CodeInvalidArgument, "retrieval_id is required")
	case math.IsNaN(in.Value) || in.Value < -1 || in.Value > 1:
		return res, core.Errf(core.CodeInvalidArgument, "value must be in [-1, 1]")
	case in.Actor == "":
		return res, core.Errf(core.CodeInvalidArgument, "actor is required")
	case !in.ActorZone.Valid():
		return res, core.Errf(core.CodeInvalidArgument, "invalid actor zone %q", in.ActorZone)
	}
	if in.ActorZone == core.ZoneExternal {
		res.Notices = append(res.Notices, NoticeExternalIgnored)
		return res, nil
	}
	used := uniqueSorted(in.UsedIDs)
	if len(used) > 0 && used[0] == "" {
		used = used[1:]
	}
	cfg := r.env.Cfg.Ranking
	nowT := r.env.Clock.Now()
	now := core.FormatTime(nowT)

	err := r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		if cfg.OutcomeRatePerMinute > 0 {
			var n int
			since := core.FormatTime(nowT.Add(-time.Minute))
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outcomes WHERE namespace = ? AND ts > ?`, in.Namespace, since).Scan(&n); err != nil {
				return err
			}
			if n >= cfg.OutcomeRatePerMinute {
				return core.Errf(core.CodeRateLimited, "more than %d outcomes per minute in namespace", cfg.OutcomeRatePerMinute)
			}
		}
		var candJSON, delivJSON string
		err := tx.QueryRowContext(ctx, `SELECT candidates, delivered FROM retrievals WHERE id = ? AND namespace = ?`, in.RetrievalID, in.Namespace).
			Scan(&candJSON, &delivJSON)
		if err == sql.ErrNoRows {
			return core.Errf(core.CodeNotFound, "retrieval %s not found", in.RetrievalID)
		}
		if err != nil {
			return err
		}
		var dup int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outcomes WHERE retrieval_id = ?`, in.RetrievalID).Scan(&dup); err != nil {
			return err
		}
		if dup > 0 {
			return core.Errf(core.CodeConflict, "retrieval %s already has an outcome", in.RetrievalID)
		}
		cands, delivered := ParseCandidates(candJSON), ParseIDs(delivJSON)

		st, err := loadState(ctx, tx, in.Namespace)
		if err != nil {
			return err
		}
		rows, err := loadURows(ctx, tx, in.Namespace, delivered)
		if err != nil {
			return err
		}
		eff := applyOutcome(&st, rows, cfg, cands, delivered, used, in.Value, now)
		if err := writeURows(ctx, tx, in.Namespace, rows); err != nil {
			return err
		}
		if err := saveState(ctx, tx, in.Namespace, st, now); err != nil {
			return err
		}
		usedJSON, _ := json.Marshal(used)
		res.ID = r.env.IDs.New(core.PrefixOutcome)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO outcomes(id, namespace, retrieval_id, signal, value, used_ids, actor, ts) VALUES (?,?,?,?,?,?,?,?)`,
			res.ID, in.Namespace, in.RetrievalID, SignalTaskOutcome, in.Value, string(usedJSON), in.Actor, now); err != nil {
			return err
		}
		res.Centred = eff.Centred
		if eff.Credited != nil {
			res.Credited = eff.Credited
		}
		if eff.Ignored != nil {
			res.Ignored = eff.Ignored
			res.Notices = append(res.Notices, fmt.Sprintf("%s: %d", NoticeNotDelivered, len(eff.Ignored)))
		}
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: in.Namespace, Actor: in.Actor, Action: audit.ActOutcome, Target: res.ID,
			Details: map[string]any{
				"retrieval_id": in.RetrievalID, "value": in.Value, "centred": eff.Centred,
				"credited": len(res.Credited), "ignored": len(res.Ignored), "delivered": len(delivered),
			},
		})
		return err
	})
	if err != nil {
		return OutcomeResult{Credited: []string{}, Ignored: []string{}, Notices: []string{}}, err
	}
	return res, nil
}
