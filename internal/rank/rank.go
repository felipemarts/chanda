// Package rank implements "Ranking that learns from use" (BASE.md, principle 9,
// build phase 8): outcome and flag signals, per-record utility (stage 1), a
// small learned ranker (stage 2) in shadow mode, promotion by evaluation and
// reversibility. Everything is deterministic: no randomness, fixed
// iteration counts, zero or hash-seeded initialisation, total SQL ordering.
//
// How the read path uses this package (read step 7):
//
//	λ := Lambda(ns)                         // 0 unless a model passed promotion
//	term := Terms(ns, candidates)[id]       // learned score of the promoted model
//	final := Final(relevance, term, λ)      // (1−λ)·relevance + λ·term
//
// Relevance is expected in [0,1]. With λ = 0 Final returns relevance exactly,
// so with learning off the order equals the pre-learning (C4) order. Utility
// (Scores) and the shadow score (Learned.Score) are logged in the candidate's
// "utility" and "learned" fields. Flagged records must be excluded from reads
// (see ExcludeFlaggedSQL).
package rank

import (
	"context"
	"database/sql"
	"sync"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Ranker owns the learning tables: outcomes, utility, flags, rank_state and
// ranker_models.
type Ranker struct {
	env *env.Env

	mu     sync.Mutex
	models map[string]*Model // decoded models by id; rows never change once written
}

// New returns a Ranker over e.
func New(e *env.Env) *Ranker { return &Ranker{env: e, models: map[string]*Model{}} }

// rankState mirrors one rank_state row.
type rankState struct {
	Enabled     bool
	Lambda      float64
	Mean        float64
	N           int64
	ActiveModel string
	ResetAt     string
}

func defaultState() rankState { return rankState{Enabled: true} }

func loadState(ctx context.Context, q store.Queryer, ns string) (rankState, error) {
	st := defaultState()
	var enabled int
	var active, reset sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT enabled, lambda, outcome_mean, outcome_n, active_model, reset_at FROM rank_state WHERE namespace = ?`, ns).
		Scan(&enabled, &st.Lambda, &st.Mean, &st.N, &active, &reset)
	if err == sql.ErrNoRows {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Enabled, st.ActiveModel, st.ResetAt = enabled != 0, active.String, reset.String
	return st, nil
}

func saveState(ctx context.Context, q store.Queryer, ns string, st rankState, now string) error {
	_, err := q.ExecContext(ctx, `
INSERT INTO rank_state(namespace, enabled, lambda, outcome_mean, outcome_n, active_model, updated_at, reset_at)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(namespace) DO UPDATE SET enabled = excluded.enabled, lambda = excluded.lambda,
  outcome_mean = excluded.outcome_mean, outcome_n = excluded.outcome_n,
  active_model = excluded.active_model, updated_at = excluded.updated_at, reset_at = excluded.reset_at`,
		ns, boolInt(st.Enabled), st.Lambda, st.Mean, st.N, nullStr(st.ActiveModel), now, nullStr(st.ResetAt))
	return err
}

// learningOn reports whether learning is on for the namespace: the
// configuration option and the runtime toggle must both be on.
func (r *Ranker) learningOn(st rankState) bool { return r.env.Cfg.Ranking.Enabled && st.Enabled }

func (r *Ranker) now() string { return core.FormatTime(r.env.Clock.Now()) }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func requireNS(ns string) error {
	if ns == "" {
		return core.Errf(core.CodeInvalidArgument, "namespace is required")
	}
	return nil
}
