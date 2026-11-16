package app

import (
	"context"
	"database/sql"
	"os"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/facts"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/rank"
	"github.com/felipemarts/chanda/internal/sleep"
	"github.com/felipemarts/chanda/internal/store"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// newSleeper wires sleep (decision 0092): the LLM from Options or config,
// facts through the write path's assert, the provenance checker, the
// embedding indexer, utility recompute, the gate set, entity vectors and,
// unless learning is off, training and promotion of the ranker.
func (a *App) newSleeper() (*sleep.Sleeper, error) {
	provider := a.opt.LLM
	if provider == nil {
		p, err := llm.FromConfig(a.e.Cfg.LLM, os.Getenv)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, "llm")
		}
		provider = p
	}
	d := sleep.Deps{
		LLM: provider, Integrity: integrity{}, Embeddings: embeddings{a}, Utility: utility{a.ranker},
		Evaluator: gateEvaluator{a}, Logf: a.opt.Logf,
	}
	if !a.opt.DisableFacts {
		d.Facts = factSink{a}
	}
	if !a.opt.DisableVectors {
		d.EntityVectors = entityVectors{a}
	}
	if !a.opt.DisableLearning {
		d.Learning = learning{a}
	}
	return sleep.New(a.e, d), nil
}

// factSink runs write steps 8 to 10 for a sleep candidate inside the gate
// transaction, as AssertFact does: reconcile (facts.Assert, derived_from
// edges to the origins, zone = lowest origin zone), queue the new fact for
// embedding and run the per-write integrity check.
type factSink struct{ a *App }

func (s factSink) AssertCandidate(ctx context.Context, tx *sql.Tx, ns string, c sleep.Candidate) (string, error) {
	r, err := s.a.facts.Assert(ctx, tx, facts.AssertInput{
		Namespace: ns, Subject: c.Subject, SubjectType: c.SubjectType, Predicate: c.Predicate, Value: c.Value,
		ValidFrom: c.ValidFrom, OriginEventIDs: c.OriginEventIDs, Actor: sleep.Actor,
	})
	if err != nil {
		return "", err
	}
	if newFact(r) {
		if err := s.a.enqueueFacts(ctx, tx, ns, []string{r.FactID}); err != nil {
			return "", err
		}
		if err := s.a.checkDerived(ctx, tx, ns, []string{r.FactID}); err != nil {
			return "", err
		}
	}
	return string(r.Action), nil
}

// integrity is the provenance checker over a queryer (the gate tx or the store).
type integrity struct{}

func (integrity) Check(ctx context.Context, q store.Queryer, ns string) (int, error) {
	r, err := prov.Check(ctx, q, ns)
	return len(r.Violations), err
}

// embeddings drains the queue in the caller's goroutine.
type embeddings struct{ a *App }

func (e embeddings) Flush(ctx context.Context, ns string) error {
	for {
		n, err := e.a.indexer.RunOnce(ctx)
		if err != nil {
			return err
		}
		pending, err := e.a.indexer.Pending(ctx, ns)
		if err != nil || pending == 0 || n == 0 {
			return err
		}
	}
}

type utility struct{ r *rank.Ranker }

func (u utility) RecomputeUtility(ctx context.Context, ns string) error {
	_, err := u.r.RecomputeUtility(ctx, ns)
	return err
}

// entityVectors embeds canonical names in memory with the configured
// embedder, for merge proposals by vector.
type entityVectors struct{ a *App }

func (v entityVectors) EntityVectors(ctx context.Context, ns string, ids []string) (map[string][]float32, error) {
	out := map[string][]float32{}
	var names, have []string
	for _, id := range ids {
		var name string
		err := v.a.e.Store.DB().QueryRowContext(ctx, `SELECT canonical_name FROM entities WHERE namespace = ? AND id = ?`, ns, id).Scan(&name)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		names, have = append(names, name), append(have, id)
	}
	const batch = 64
	for i := 0; i < len(names); i += batch {
		j := min(i+batch, len(names))
		vecs, err := v.a.emb.Embed(ctx, names[i:j])
		if err != nil {
			return nil, err
		}
		for k, vec := range vecs {
			out[have[i+k]] = vec
		}
	}
	return out, nil
}

// learning trains a new ranker when there are new examples and asks
// rank.Promote to evaluate the newest model on the gate set.
type learning struct{ a *App }

func (l learning) Learn(ctx context.Context, ns string) (sleep.LearnResult, error) {
	out := sleep.LearnResult{Notices: []string{}}
	r := l.a.ranker
	exs, err := r.Examples(ctx, ns)
	if err != nil {
		return out, err
	}
	var trainedOn int
	err = l.a.e.Store.DB().QueryRowContext(ctx, `SELECT n_examples FROM ranker_models WHERE namespace = ? AND status IN ('shadow','promoted')
		ORDER BY created_at DESC, id DESC LIMIT 1`, ns).Scan(&trainedOn)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	switch {
	case len(exs) < l.a.e.Cfg.Ranking.MinTrainExamples:
		out.Notices = append(out.Notices, "train_skipped_few_examples")
	case len(exs) <= trainedOn:
		out.Notices = append(out.Notices, "train_skipped_no_new_examples")
	default:
		tr, err := r.Train(ctx, ns, "", sleep.Actor)
		switch {
		case core.CodeOf(err) == core.CodeUnavailable:
			out.Notices = append(out.Notices, "train_skipped_single_class")
		case err != nil:
			return out, err
		default:
			out.Trained = tr.ModelID
		}
	}
	eval, err := l.a.promotionEvaluator(ctx, ns)
	if err != nil {
		return out, err
	}
	pr, err := r.Promote(ctx, ns, sleep.Actor, eval)
	if err != nil {
		return out, err
	}
	out.Evaluated, out.Passed, out.Promoted = pr.ModelID, pr.Passed, pr.Promoted
	out.Baseline, out.Learned, out.Lambda = pr.Baseline, pr.Learned, pr.LambdaTo
	out.Notices = append(out.Notices, pr.Notices...)
	return out, nil
}

// SleepRun runs sleep on demand (phases, budget) and returns the report and
// the counts of what phase 2 kept. A run refused during a write burst or
// while another runs is CodeConflict.
func (a *App) SleepRun(ctx context.Context, in chanda.SleepRunInput) (chanda.SleepRunOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.SleepRunOutput{Meta: c.meta(0), Phases: []int{}}
	if a.opt.DisableSleep || a.sleeper == nil {
		return out, core.Errf(core.CodeUnavailable, "sleep_run: sleep is disabled in this configuration")
	}
	res, err := a.sleeper.Run(ctx, sleep.RunInput{
		Namespace: c.ns, Phases: in.Phases, Budget: sleep.Budget{Tokens: in.Budget.Tokens, Events: in.Budget.Events}, Actor: c.actor.Name,
	})
	if res == nil {
		return out, coded(err)
	}
	for _, n := range res.Notices {
		c.notice(n)
	}
	var auditID int64
	if qerr := a.e.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM audit_log WHERE target = ? AND action = 'sleep_end'`,
		res.RunID).Scan(&auditID); qerr != nil && err == nil {
		err = qerr
	}
	out.Meta, out.RunID, out.Status, out.Phases, out.Report = c.meta(auditID), res.RunID, res.Status, res.Phases, res.Report
	out.Cost = chanda.SleepCost{InputTokens: res.Cost.InputTokens, OutputTokens: res.Cost.OutputTokens, LLMCalls: res.Cost.LLMCalls}
	if !res.Gate.Reverted {
		k := res.Counts
		for act, n := range k.FactActions {
			if act != string(facts.ActionConfirmed) {
				out.Created += n
			}
		}
		out.Created += len(k.FactsActivated)
		out.Superseded = k.FactActions[string(facts.ActionSuperseded)] + len(k.FactsSuperseded)
		out.Merged = len(k.MergesApplied)
		out.Rejected = len(k.FactsRejected) + len(k.MergesRejected)
	}
	return out, coded(err)
}
