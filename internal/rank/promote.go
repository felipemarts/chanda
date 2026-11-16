package rank

import (
	"context"
	"database/sql"
	"math"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/store"
)

// Evaluator runs the namespace's fixed gate set and returns its score (in the
// units of cfg.Ranking.PromotionMargin, e.g. points of recall@k). With
// useLearned false it must rank exactly as the read path does with λ = 0 (the
// baseline); with useLearned true it must rank with Final(relevance, term, λ)
// where term comes from Learned().ScoreAll — the model being evaluated.
// Evaluate is called outside any transaction, so it may query the store.
type Evaluator func(useLearned bool, lambda float64) (float64, error)

// Notices returned by Promote.
const (
	NoticeLearningDisabled = "learning_disabled"
	NoticeNoModel          = "no_model"
	NoticeGateFailed       = "gate_failed"
)

// PromoteResult reports a promotion attempt.
type PromoteResult struct {
	ModelID    string   `json:"model_id,omitempty"`
	Passed     bool     `json:"passed"`
	Promoted   bool     `json:"promoted"` // the model became the live model in this call
	LambdaFrom float64  `json:"lambda_from"`
	LambdaTo   float64  `json:"lambda_to"`
	Baseline   float64  `json:"baseline"`
	Learned    float64  `json:"learned"`
	Notices    []string `json:"notices"`
	AuditID    int64    `json:"audit_id,omitempty"`
}

// Promote is the only way λ leaves 0 (BASE.md "Promotion by evaluation",
// principle 9). It evaluates the newest non-retired model on the gate set
// against the baseline. If learned − baseline ≥ PromotionMargin (and learned
// > baseline), the model becomes (or stays) the promoted one and λ rises by
// LambdaStep, capped at LambdaMax; a newly promoted model starts at one step
// and retires the previous one (ActRankPromote). Otherwise λ returns to 0 and
// no model is live (ActRankAdjust). With learning off it evaluates nothing
// and returns a notice.
func (r *Ranker) Promote(ctx context.Context, ns, actor string, evaluate Evaluator) (PromoteResult, error) {
	res := PromoteResult{Notices: []string{}}
	if err := requireNS(ns); err != nil {
		return res, err
	}
	if actor == "" || evaluate == nil {
		return res, core.Errf(core.CodeInvalidArgument, "actor and evaluator are required")
	}
	cfg := r.env.Cfg.Ranking
	db := r.env.Store.DB()
	st, err := loadState(ctx, db, ns)
	if err != nil {
		return res, err
	}
	res.LambdaFrom = st.Lambda
	if !r.learningOn(st) {
		res.Notices = append(res.Notices, NoticeLearningDisabled)
		return res, nil
	}
	cand, err := newestModel(ctx, db, ns)
	if err != nil {
		return res, err
	}
	if cand == "" {
		res.LambdaTo = st.Lambda
		res.Notices = append(res.Notices, NoticeNoModel)
		return res, nil
	}
	res.ModelID = cand
	from := 0.0
	if st.ActiveModel == cand {
		from = st.Lambda
	}
	next := math.Min(from+cfg.LambdaStep, cfg.LambdaMax)
	if !(next > 0) {
		return res, core.Errf(core.CodeInvalidArgument, "ranking.lambda_step and lambda_max must be positive")
	}
	if res.Baseline, err = evaluate(false, 0); err != nil {
		return res, err
	}
	if res.Learned, err = evaluate(true, next); err != nil {
		return res, err
	}
	if math.IsNaN(res.Baseline) || math.IsNaN(res.Learned) {
		return res, core.Errf(core.CodeInvalidArgument, "evaluator returned NaN")
	}
	res.Passed = res.Learned > res.Baseline && res.Learned-res.Baseline >= cfg.PromotionMargin

	now := r.now()
	err = r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		st, err := loadState(ctx, tx, ns)
		if err != nil {
			return err
		}
		if latest, err := newestModel(ctx, tx, ns); err != nil {
			return err
		} else if latest != cand || st.Lambda != res.LambdaFrom {
			return core.Errf(core.CodeConflict, "ranking state changed during evaluation")
		}
		action := audit.ActRankAdjust
		setStatus := func(id, status string) error {
			_, err := tx.ExecContext(ctx, `UPDATE ranker_models SET status = ? WHERE id = ?`, status, id)
			return err
		}
		if res.Passed {
			if st.ActiveModel != cand {
				if st.ActiveModel != "" {
					if err := setStatus(st.ActiveModel, "retired"); err != nil {
						return err
					}
				}
				if err := setStatus(cand, "promoted"); err != nil {
					return err
				}
				action, res.Promoted = audit.ActRankPromote, true
			}
			st.ActiveModel, st.Lambda = cand, next
		} else {
			if st.ActiveModel != "" {
				status := "retired"
				if st.ActiveModel == cand {
					status = "shadow"
				}
				if err := setStatus(st.ActiveModel, status); err != nil {
					return err
				}
			}
			st.ActiveModel, st.Lambda = "", 0
			res.Notices = append(res.Notices, NoticeGateFailed)
		}
		res.LambdaTo = st.Lambda
		if err := saveState(ctx, tx, ns, st, now); err != nil {
			return err
		}
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: action, Target: cand,
			Details: map[string]any{"op": "promote", "passed": res.Passed, "baseline": res.Baseline, "learned": res.Learned,
				"margin": cfg.PromotionMargin, "lambda_from": res.LambdaFrom, "lambda_to": res.LambdaTo},
		})
		return err
	})
	if err != nil {
		return PromoteResult{Notices: []string{}}, err
	}
	return res, nil
}

// newestModel returns the id of the newest non-retired model of ns, or "".
func newestModel(ctx context.Context, q store.Queryer, ns string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM ranker_models WHERE namespace = ? AND status IN ('shadow','promoted')
ORDER BY created_at DESC, id DESC LIMIT 1`, ns).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
