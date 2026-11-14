package coresys

import (
	"context"
	"fmt"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/eval"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Factories returns the C1..C5 factories. A configuration that cannot run
// here returns a CodeUnavailable error (the report lists it with the reason):
//
//   - C2 needs a reranker; none is wired into the core yet.
//   - C3..C5 build on C2: without a reranker they run without it and say so
//     in their description (C3 is then C1 + facts).
//   - C4 needs sleep_run; C5 needs C4.
func Factories() map[string]eval.Factory {
	out := map[string]eval.Factory{}
	for _, v := range Variants {
		out[v.Name] = factory(v)
	}
	return out
}

func factory(v Variant) eval.Factory {
	return func(ctx context.Context, fe eval.FactoryEnv) (eval.System, error) {
		var notes []string
		if v.Reranker {
			if ok, why := RerankerAvailable(fe.Config); !ok {
				if v.Name == "C2" {
					return nil, core.Errf(core.CodeUnavailable, "C2 not run, %s", why)
				}
				v.Reranker = false
				notes = append(notes, "Runs WITHOUT the reranker (none is wired into the core), so it is C1 + the stages above.")
			}
		}
		s, err := Open(ctx, v, fe.Config, fe.Embedder)
		if err != nil {
			return nil, err
		}
		if v.Sleep {
			if err := s.probeSleep(ctx); err != nil {
				s.Close()
				return nil, err
			}
			if s.cfg.LLM.Endpoint == "" {
				notes = append(notes, "No LLM provider is configured: sleep runs phase 1 (maintenance) only.")
			}
		}
		if v.Learning {
			notes = append(notes, fmt.Sprintf("Training: %d passes over the training split, outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none; then a sleep run.", TrainPasses))
		}
		for _, n := range notes {
			s.Declare(n)
		}
		return s, nil
	}
}

// probeSleep checks that sleep_run is wired, on a namespace of its own so
// the evaluated namespace is untouched.
func (s *System) probeSleep(ctx context.Context) error {
	_, err := s.app.SleepRun(op(ctx), chanda.SleepRunInput{Namespace: Namespace + "-probe", Phases: []int{1}})
	if core.CodeOf(err) == core.CodeUnavailable {
		return core.Errf(core.CodeUnavailable, "%s not run, sleep_run is unavailable in the core: %v", s.v.Name, err)
	}
	return err
}

// TrainPasses is how many times the training split is replayed as tasks.
const TrainPasses = 3

// Train simulates a sequence of tasks (C5). Each training question is one
// task: the agent calls context with the question, and the task outcome is
// derived from the gold labels: the delivered records that are evidence for
// a gold unit (same corpus item, span contained) are reported as used_ids
// with value +1; when none is, the outcome is -1 with no used ids (every
// delivered record was unused). The split is replayed TrainPasses times in
// dataset order, then a sleep run trains and, if the gate allows, promotes
// the ranker. Evaluated (gate) questions never reach this method.
func (s *System) Train(ctx context.Context, train []eval.Question) error {
	if !s.v.Learning {
		return nil
	}
	if len(train) == 0 {
		return fmt.Errorf("%s: the dataset has no training split", s.v.Name)
	}
	var tasks, positive int
	for range TrainPasses {
		for _, q := range train {
			query := eval.Query{ID: q.ID, Text: q.Question}
			if q.AsOf != "" {
				t, err := eval.ItemTime(q.AsOf)
				if err != nil {
					return err
				}
				query.AsOf = t
			}
			rid, rs, err := s.Context(ctx, query, s.cfg.Read.ContextDefaultTokens)
			if err != nil {
				return err
			}
			if rid == "" {
				continue
			}
			used := []string{}
			for _, r := range rs {
				for _, g := range q.Gold {
					if eval.Hits(r, g) {
						used = append(used, r.ID)
						break
					}
				}
			}
			value := -1.0
			if len(used) > 0 {
				value = 1
				positive++
			}
			if _, err := s.app.Outcome(agent(ctx), chanda.OutcomeInput{Namespace: Namespace, RetrievalID: rid, Value: value, UsedIDs: used}); err != nil {
				return fmt.Errorf("outcome for %s: %w", q.ID, err)
			}
			tasks++
		}
	}
	if err := s.sleep(ctx); err != nil {
		return err
	}
	st, err := s.app.Stats(agent(ctx), chanda.StatsInput{Namespace: Namespace})
	if err != nil {
		return err
	}
	if st.Ranking.ActiveModel == "" && st.Ranking.Lambda == 0 {
		s.Declare(fmt.Sprintf("After %d tasks (%d positive) no ranker model was promoted (lambda 0): ranking equals C4.", tasks, positive))
	} else {
		s.Declare(fmt.Sprintf("After %d tasks (%d positive): active model %q, lambda %.2f.", tasks, positive, st.Ranking.ActiveModel, st.Ranking.Lambda))
	}
	return nil
}
