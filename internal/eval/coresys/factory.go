package coresys

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/app"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/eval"
	"github.com/felipemarts/chanda-mcp/internal/sleep"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Factories returns the C1..C5 factories (app.Options per decision 0094).
// C2 needs a reranker: without one (FactoryEnv.Reranker nil) it returns a
// CodeUnavailable error and the report lists it with the reason. C3..C5
// build on C2: without a reranker they run without it and say so in their
// description (C3 is then C1 + facts).
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
		if v.Reranker && fe.Reranker == nil {
			if v.Name == "C2" {
				return nil, core.Errf(core.CodeUnavailable, "C2 not run: %s", NoRerankerReason)
			}
			v.Reranker = false
			notes = append(notes, "Runs WITHOUT the reranker (none configured), so it is C1 + the stages above.")
		} else if v.Reranker {
			notes = append(notes, "Reranker "+fe.Reranker.ModelID()+".")
		}
		s, err := Open(ctx, v, fe)
		if err != nil {
			return nil, err
		}
		if v.Sleep && s.cfg.LLM.Endpoint == "" {
			notes = append(notes, "No LLM provider is configured: sleep runs phase 1 (maintenance) only.")
		}
		if v.Learning {
			notes = append(notes, fmt.Sprintf("Learning: the gate split is approved as the gate set, then %d passes over the training split "+
				"(outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none), then a sleep run trains and may promote a ranker.", TrainPasses))
		}
		for _, n := range notes {
			s.Declare(n)
		}
		return s, nil
	}
}

// TrainPasses is how many times the training split is replayed as tasks.
const TrainPasses = 3

// Train simulates a sequence of tasks (C5). First a person (the operator,
// a human actor) adds the gate split as approved gate questions: question,
// expected answers, as_of and the ids of the records that are evidence for
// the gold units (GateEvidence), as the person writing the answer key would
// look them up in the namespace; the ids make the gate score rank-sensitive
// (decision 0110). Then each training question is one task: the agent calls context
// with the question, and the task outcome is derived from the gold labels:
// the delivered records that are evidence for a gold unit (same corpus item,
// span contained) are reported as used_ids with value +1; when none is, the
// outcome is -1 with no used ids (every delivered record was unused). The
// split is replayed TrainPasses times in dataset order, then a sleep run
// trains a ranker and promotes it only if the gate allows. Evaluated
// questions never reach this method; gate questions proposed by sleep are
// left unreviewed.
func (s *System) Train(ctx context.Context, train, gate []eval.Question) error {
	if !s.v.Learning {
		return nil
	}
	if len(train) == 0 || len(gate) == 0 {
		return fmt.Errorf("%s: the dataset needs a training and a gate split", s.v.Name)
	}
	for _, q := range gate {
		ids, err := s.GateEvidence(ctx, q)
		if err != nil {
			return err
		}
		if _, _, err := s.app.GateAdd(op(ctx), app.GateAddInput{Namespace: Namespace, Question: q.Question,
			Expected: sleep.GateExpected{RecordIDs: ids, Answers: q.Answers, AsOf: q.AsOf}}); err != nil {
			return fmt.Errorf("gate question %s: %w", q.ID, err)
		}
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
			// One task takes a while: space the tasks so outcomes stay under
			// ranking.outcome_rate_per_minute, as real tasks would.
			if r := s.cfg.Ranking.OutcomeRatePerMinute; r > 0 {
				s.fc.Advance(time.Minute/time.Duration(r) + time.Second)
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
	run, err := s.sleepReport(ctx)
	if err != nil {
		return err
	}
	st, err := s.app.Stats(agent(ctx), chanda.StatsInput{Namespace: Namespace})
	if err != nil {
		return err
	}
	if st.Ranking.ActiveModel == "" && st.Ranking.Lambda == 0 {
		s.Declare(fmt.Sprintf("After %d tasks (%d positive) and %d gate questions, no ranker model was promoted (lambda 0; %s): ranking equals C4.",
			tasks, positive, len(gate), run))
	} else {
		s.Declare(fmt.Sprintf("After %d tasks (%d positive) and %d gate questions: active model %s, lambda %.2f (%s).",
			tasks, positive, len(gate), st.Ranking.ActiveModel, st.Ranking.Lambda, run))
	}
	return nil
}

// GateEvidence returns, sorted, the ids of the records of the namespace that
// are evidence for a gold unit of q, by the same test the metrics and the
// training outcomes use (eval.Hits: same corpus item, span contained in the
// record's text as get returns it). Every event, fact, page and page item is
// considered, whatever its status, so the key does not depend on any
// ranking. A question without gold (abstention) has none.
func (s *System) GateEvidence(ctx context.Context, q eval.Question) ([]string, error) {
	if len(q.Gold) == 0 {
		return nil, nil
	}
	if s.records == nil {
		recs, err := s.allRecords(ctx)
		if err != nil {
			return nil, err
		}
		s.records = recs
	}
	var ids []string
	for _, r := range s.records {
		if slices.ContainsFunc(q.Gold, func(g eval.Gold) bool { return eval.Hits(r, g) }) {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

// allRecords reads every record of the namespace through get, as the
// harness sees a delivered item (id, corpus item, text), ordered by id.
func (s *System) allRecords(ctx context.Context) ([]eval.Retrieved, error) {
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT id FROM events WHERE namespace = ?1 UNION ALL SELECT id FROM facts WHERE namespace = ?1
		UNION ALL SELECT id FROM pages WHERE namespace = ?1 UNION ALL SELECT id FROM page_items WHERE namespace = ?1 ORDER BY 1`, Namespace)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]eval.Retrieved, 0, len(ids))
	for _, id := range ids {
		g, err := s.app.Get(op(ctx), chanda.GetInput{Namespace: Namespace, ID: id})
		if core.CodeOf(err) == core.CodeNotFound {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get %s: %w", id, err)
		}
		out = append(out, eval.Retrieved{ID: id, DocID: s.docOf(g.Item), Text: g.Item.Text})
	}
	return out, nil
}
