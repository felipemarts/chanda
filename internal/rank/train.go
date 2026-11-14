package rank

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Example is one training row: a delivered candidate of a retrieval that
// received an outcome, with its features as of the retrieval time.
type Example struct {
	RetrievalID string
	ID          string
	X           []float64
	Y           float64 // 1 if used and the task succeeded (centred outcome > 0)
	Relevance   float64 // baseline score (Candidate.Relevance)
}

// Examples extracts stage-2 training rows from the logged retrievals of ns.
// Signals are replayed chronologically, so the utility features of each row
// are those that existed when the retrieval happened (no leakage from its own
// or later outcomes). Retrievals logged with params {"gate": true} are left
// out: the gate set never enters training. Rows are grouped by retrieval, in
// outcome order.
func (r *Ranker) Examples(ctx context.Context, ns string) ([]Example, error) {
	return examples(ctx, r.env.Store.DB(), r, ns)
}

func examples(ctx context.Context, q store.Queryer, r *Ranker, ns string) ([]Example, error) {
	st, err := loadState(ctx, q, ns)
	if err != nil {
		return nil, err
	}
	evs, err := loadSignals(ctx, q, ns, st.ResetAt, true)
	if err != nil {
		return nil, err
	}
	st.Mean, st.N = 0, 0
	rows := rowSet{}
	pending := map[string][]Example{}
	var out []Example
	onSnapshot := func(ev signalEvent) {
		if ev.Gate {
			return
		}
		now, err := core.ParseTime(ev.TS)
		if err != nil {
			return
		}
		byID := map[string]Candidate{}
		for _, c := range ev.Cands {
			if _, ok := byID[c.ID]; !ok {
				byID[c.ID] = c
			}
		}
		var exs []Example
		for _, id := range ev.Delivered {
			c, ok := byID[id]
			if !ok {
				c = Candidate{ID: id}
			}
			exs = append(exs, Example{RetrievalID: ev.RetrievalID, ID: id, X: features(c, rows[id], now), Relevance: c.Relevance()})
		}
		pending[ev.RetrievalID] = exs
	}
	onOutcome := func(ev signalEvent, eff outcomeEffect) {
		exs, ok := pending[ev.RetrievalID]
		if !ok {
			return
		}
		delete(pending, ev.RetrievalID)
		credited := map[string]bool{}
		for _, id := range eff.Credited {
			credited[id] = true
		}
		for _, ex := range exs {
			if credited[ex.ID] && eff.Centred > 0 {
				ex.Y = 1
			}
			out = append(out, ex)
		}
	}
	replay(evs, r.env.Cfg.Ranking, &st, rows, onSnapshot, onOutcome)
	return out, nil
}

// TrainResult describes a newly trained model.
type TrainResult struct {
	ModelID       string  `json:"model_id"`
	Kind          string  `json:"kind"`
	NExamples     int     `json:"n_examples"`
	NHoldout      int     `json:"n_holdout"`
	EvalScore     float64 `json:"eval_score"`     // held-out AUC of the model
	BaselineScore float64 `json:"baseline_score"` // held-out AUC of relevance alone
	AuditID       int64   `json:"audit_id"`
}

// Train fits a stage-2 ranker of kind (cfg.Ranking.RankerKind when empty) on
// the namespace's examples and stores it in ranker_models with status
// 'shadow'; earlier shadow models are retired, a promoted model stays live
// until Promote decides. The most recent cfg.Ranking.HoldoutFraction of
// retrievals is held out: the model is fitted on the rest and its held-out
// AUC is stored as eval_score, next to the AUC of relevance alone as
// baseline_score. Fails with CodeUnavailable below MinTrainExamples or with
// a single class. Audited as ActRankAdjust.
func (r *Ranker) Train(ctx context.Context, ns, kind, actor string) (TrainResult, error) {
	var res TrainResult
	if err := requireNS(ns); err != nil {
		return res, err
	}
	if actor == "" {
		return res, core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	cfg := r.env.Cfg.Ranking
	if kind == "" {
		kind = cfg.RankerKind
	}
	if kind != KindLogReg && kind != KindMLP {
		return res, core.Errf(core.CodeInvalidArgument, "unknown ranker kind %q", kind)
	}
	exs, err := r.Examples(ctx, ns)
	if err != nil {
		return res, err
	}
	if len(exs) < cfg.MinTrainExamples || len(exs) == 0 {
		return res, core.Errf(core.CodeUnavailable, "%d training examples, need %d", len(exs), cfg.MinTrainExamples)
	}
	train, hold := splitHoldout(exs, cfg.HoldoutFraction)
	if !bothClasses(train) {
		return res, core.Errf(core.CodeUnavailable, "training examples have a single class")
	}
	X, y := make([][]float64, len(train)), make([]float64, len(train))
	for i, ex := range train {
		X[i], y[i] = ex.X, ex.Y
	}
	m, err := fit(kind, X, y, cfg)
	if err != nil {
		return res, err
	}
	evalSet := hold
	if len(evalSet) == 0 {
		evalSet = train
	}
	ps, rel, lab := make([]float64, len(evalSet)), make([]float64, len(evalSet)), make([]float64, len(evalSet))
	for i, ex := range evalSet {
		ps[i], rel[i], lab[i] = m.Predict(ex.X), ex.Relevance, ex.Y
	}
	res = TrainResult{Kind: kind, NExamples: len(exs), NHoldout: len(hold), EvalScore: AUC(ps, lab), BaselineScore: AUC(rel, lab)}

	weights, err := json.Marshal(m)
	if err != nil {
		return TrainResult{}, err
	}
	names, _ := json.Marshal(m.Features)
	now := r.now()
	err = r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		res.ModelID = r.env.IDs.New(core.PrefixModel)
		if _, err := tx.ExecContext(ctx, `UPDATE ranker_models SET status = 'retired' WHERE namespace = ? AND status = 'shadow'`, ns); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO ranker_models(id, namespace, kind, status, weights, feature_names, n_examples, eval_score, baseline_score, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?)`, res.ModelID, ns, kind, "shadow", string(weights), string(names), len(exs), res.EvalScore, res.BaselineScore, now); err != nil {
			return err
		}
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActRankAdjust, Target: res.ModelID,
			Details: map[string]any{"op": "train", "kind": kind, "n_examples": len(exs), "n_holdout": len(hold),
				"eval_score": res.EvalScore, "baseline_score": res.BaselineScore},
		})
		return err
	})
	if err != nil {
		return TrainResult{}, err
	}
	return res, nil
}

// splitHoldout keeps the last fraction of retrievals (whole groups) apart.
func splitHoldout(exs []Example, frac float64) (train, hold []Example) {
	var groups []string
	for i, ex := range exs {
		if i == 0 || ex.RetrievalID != exs[i-1].RetrievalID {
			groups = append(groups, ex.RetrievalID)
		}
	}
	k := int(math.Ceil(frac * float64(len(groups))))
	if frac <= 0 || len(groups) < 2 {
		k = 0
	}
	if k >= len(groups) {
		k = len(groups) - 1
	}
	held := map[string]bool{}
	for _, g := range groups[len(groups)-k:] {
		held[g] = true
	}
	for _, ex := range exs {
		if held[ex.RetrievalID] {
			hold = append(hold, ex)
		} else {
			train = append(train, ex)
		}
	}
	return train, hold
}

func bothClasses(exs []Example) bool {
	var pos, neg bool
	for _, ex := range exs {
		if ex.Y > 0 {
			pos = true
		} else {
			neg = true
		}
	}
	return pos && neg
}
