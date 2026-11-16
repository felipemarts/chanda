package rank

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Learned is the stage-2 scorer seen by the read path.
type Learned struct{ r *Ranker }

// Learned returns the stage-2 scorer.
func (r *Ranker) Learned() *Learned { return &Learned{r: r} }

// Score returns the shadow score of one candidate: the prediction of the
// newest non-retired model of ns (the one Promote would evaluate). ok is false
// when no model exists. In shadow mode it is only logged (the candidate's
// "learned" field) and does not change the order (BASE.md "Shadow mode
// first"); only Terms feeds the order, and only through a promoted model.
func (l *Learned) Score(ctx context.Context, ns string, c Candidate) (score float64, ok bool, err error) {
	m, err := l.ScoreAll(ctx, ns, []Candidate{c})
	if err != nil || m == nil {
		return 0, false, err
	}
	return m[c.ID], true, nil
}

// ScoreAll is Score for a whole candidate list (one utility query). It
// returns nil when no model exists.
func (l *Learned) ScoreAll(ctx context.Context, ns string, cands []Candidate) (map[string]float64, error) {
	if err := requireNS(ns); err != nil {
		return nil, err
	}
	db := l.r.env.Store.DB()
	id, err := newestModel(ctx, db, ns)
	if err != nil || id == "" {
		return nil, err
	}
	m, err := l.r.model(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return l.r.predict(ctx, db, ns, m, cands)
}

// Terms returns the learned term of read step 7 for each candidate: the
// prediction of the promoted model of ns. Without a promoted model (λ is then
// 0) it returns Neutral for every candidate, so the term never matters.
func (r *Ranker) Terms(ctx context.Context, ns string, cands []Candidate) (map[string]float64, error) {
	if err := requireNS(ns); err != nil {
		return nil, err
	}
	if r.env.Cfg.Ranking.UtilityLambda > 0 {
		// Decision 0146: the term is the stage-1 utility, neutral below
		// ranking.min_exposures; no learned model is used.
		ids := make([]string, len(cands))
		for i, c := range cands {
			ids[i] = c.ID
		}
		us, err := r.Scores(ctx, ns, ids)
		if err != nil {
			return nil, err
		}
		out := make(map[string]float64, len(cands))
		for _, c := range cands {
			out[c.ID] = Neutral
			if u, ok := us[c.ID]; ok {
				out[c.ID] = u
			}
		}
		return out, nil
	}
	db := r.env.Store.DB()
	st, err := loadState(ctx, db, ns)
	if err != nil {
		return nil, err
	}
	if st.ActiveModel == "" {
		out := make(map[string]float64, len(cands))
		for _, c := range cands {
			out[c.ID] = Neutral
		}
		return out, nil
	}
	m, err := r.model(ctx, db, st.ActiveModel)
	if err != nil {
		return nil, err
	}
	return r.predict(ctx, db, ns, m, cands)
}

func (r *Ranker) predict(ctx context.Context, q store.Queryer, ns string, m *Model, cands []Candidate) (map[string]float64, error) {
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.ID
	}
	rows, err := loadURows(ctx, q, ns, ids)
	if err != nil {
		return nil, err
	}
	now := r.env.Clock.Now()
	out := make(map[string]float64, len(cands))
	for _, c := range cands {
		out[c.ID] = m.Predict(features(c, rows[c.ID], now))
	}
	return out, nil
}

// model loads (and caches) a model by id.
func (r *Ranker) model(ctx context.Context, q store.Queryer, id string) (*Model, error) {
	r.mu.Lock()
	m, ok := r.models[id]
	r.mu.Unlock()
	if ok {
		return m, nil
	}
	var kind, weights, names string
	err := q.QueryRowContext(ctx, `SELECT kind, weights, feature_names FROM ranker_models WHERE id = ?`, id).Scan(&kind, &weights, &names)
	if err == sql.ErrNoRows {
		return nil, core.Errf(core.CodeNotFound, "model %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	m = &Model{}
	if err := json.Unmarshal([]byte(weights), m); err != nil {
		return nil, core.Wrap(core.CodeIntegrity, err, "model weights")
	}
	if err := json.Unmarshal([]byte(names), &m.Features); err != nil {
		return nil, core.Wrap(core.CodeIntegrity, err, "model feature names")
	}
	if len(m.Features) != len(FeatureNames) || len(m.Mean) != len(FeatureNames) || len(m.Std) != len(FeatureNames) {
		return nil, core.Errf(core.CodeIntegrity, "model %s has %d features, this version uses %d", id, len(m.Features), len(FeatureNames))
	}
	for i, n := range m.Features {
		if n != FeatureNames[i] {
			return nil, core.Errf(core.CodeIntegrity, "model %s feature %d is %q, want %q", id, i, n, FeatureNames[i])
		}
	}
	m.ID, m.Kind = id, kind
	r.mu.Lock()
	r.models[id] = m
	r.mu.Unlock()
	return m, nil
}
