package rank

import (
	"context"
	"sort"
)

// Scores returns the stage-1 utility (posterior mean alpha/(alpha+beta)) of
// the given ids in ns. Records with fewer than cfg.Ranking.MinExposures
// exposures (n_shown) are absent from the map: the read path treats them as
// neutral (Neutral). It is what the read path logs in the candidate's
// "utility" field; it changes no order on its own (see Lambda).
func (r *Ranker) Scores(ctx context.Context, ns string, ids []string) (map[string]float64, error) {
	out := map[string]float64{}
	if err := requireNS(ns); err != nil {
		return out, err
	}
	rows, err := loadURows(ctx, r.env.Store.DB(), ns, ids)
	if err != nil {
		return nil, err
	}
	for id, u := range rows {
		if u.NShown >= int64(r.env.Cfg.Ranking.MinExposures) {
			out[id] = u.Score
		}
	}
	return out, nil
}

// Neutral is the utility of a record without enough exposures (the Beta(1,1) mean).
const Neutral = 0.5

// Lambda returns the weight λ of the learned term in read step 7. It is 0
// when learning is off (configuration or runtime toggle) or when no model is
// promoted, and never above cfg.Ranking.LambdaMax.
func (r *Ranker) Lambda(ctx context.Context, ns string) (float64, error) {
	if err := requireNS(ns); err != nil {
		return 0, err
	}
	st, err := loadState(ctx, r.env.Store.DB(), ns)
	if err != nil {
		return 0, err
	}
	if !r.learningOn(st) {
		return 0, nil
	}
	if u := r.env.Cfg.Ranking.UtilityLambda; u > 0 { // decision 0146
		return min(u, r.env.Cfg.Ranking.LambdaMax), nil
	}
	if st.ActiveModel == "" || st.Lambda <= 0 {
		return 0, nil
	}
	return min(st.Lambda, r.env.Cfg.Ranking.LambdaMax), nil
}

// Final is read step 7: (1−λ)·relevance + λ·term. With λ = 0 it returns
// relevance unchanged, bit for bit, so turning learning off restores the
// pre-learning order exactly.
func Final(relevance, term, lambda float64) float64 {
	if lambda <= 0 {
		return relevance
	}
	return (1-lambda)*relevance + lambda*term
}

// Scored is a candidate as step 7 sees it.
type Scored struct {
	ID        string
	Relevance float64
	Term      float64 // learned term (Terms); ignored when λ = 0
}

// Order returns ids sorted by Final descending, ties by id ascending.
// The relevance cut (step 8) stays with the read path and happens on
// relevance, before or regardless of this order.
func Order(items []Scored, lambda float64) []string {
	s := append([]Scored(nil), items...)
	sort.SliceStable(s, func(i, j int) bool {
		fi, fj := Final(s[i].Relevance, s[i].Term, lambda), Final(s[j].Relevance, s[j].Term, lambda)
		if fi != fj {
			return fi > fj
		}
		return s[i].ID < s[j].ID
	})
	out := make([]string, len(s))
	for i, it := range s {
		out[i] = it.ID
	}
	return out
}
