package sleep

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// PrefixGateQuestion is the id prefix of gate_questions rows.
const PrefixGateQuestion = "gq"

// GateExpected is the JSON stored in gate_questions.expected (the same shape
// internal/eval reads): record ids that should be delivered, answer
// substrings that should be in the context, and the reference date. With
// neither ids nor answers the question expects an empty result.
type GateExpected struct {
	RecordIDs []string `json:"record_ids,omitempty"`
	Answers   []string `json:"answers,omitempty"`
	AsOf      string   `json:"as_of,omitempty"`
}

// proposeGate authors gate questions from the corpus (decision 0092): one
// question per current fact, "What is <subject> <predicate>?", expecting the
// fact id and its value as of the fact's valid_from. The date keeps the
// question true after a later, legitimate supersede: at that date the old
// fact held. Only facts of the internal and user zones are used (the gate
// must not be built on untrusted content), in zone, confidence and id
// order; a question whose text already exists in the namespace, in any
// status, is not proposed again. At most sleep.gate_proposals per run.
// Proposals have status 'proposed': they score nothing until a human
// approves them (App.GateReview).
func (s *Sleeper) proposeGate(ctx context.Context, r *run) error {
	limit := s.env.Cfg.Sleep.GateProposals
	if limit <= 0 {
		return nil
	}
	type cand struct{ factID, question, value, validFrom string }
	var cands []cand
	err := func() error {
		rows, err := s.db().QueryContext(ctx, `SELECT f.id, e.canonical_name, f.predicate, f.value, f.valid_from
			FROM facts f JOIN entities e ON e.id = f.subject
			WHERE f.namespace = ? AND f.status = 'active' AND f.trust_zone IN ('internal','user')
			ORDER BY CASE f.trust_zone WHEN 'user' THEN 0 ELSE 1 END, f.confidence DESC, f.id`, r.ns)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c cand
			var name, pred string
			if err := rows.Scan(&c.factID, &name, &pred, &c.value, &c.validFrom); err != nil {
				return err
			}
			c.question = "What is " + strings.TrimSpace(name) + " " + strings.ReplaceAll(pred, "_", " ") + "?"
			cands = append(cands, c)
		}
		return rows.Err()
	}()
	if err != nil || len(cands) == 0 {
		return err
	}
	existing, err := queryStrings(ctx, s.db(), `SELECT question FROM gate_questions WHERE namespace = ? ORDER BY question`, r.ns)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, q := range existing {
		seen[q] = true
	}
	return s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		for _, c := range cands {
			if len(r.res.Counts.GateProposed) >= limit {
				break
			}
			if seen[c.question] || strings.TrimSpace(c.value) == "" {
				continue
			}
			seen[c.question] = true
			exp, err := json.Marshal(GateExpected{RecordIDs: []string{c.factID}, Answers: []string{c.value}, AsOf: c.validFrom})
			if err != nil {
				return err
			}
			id := s.env.IDs.New(PrefixGateQuestion)
			if _, err := tx.ExecContext(ctx, `INSERT INTO gate_questions(id, namespace, question, expected, status, created_at)
				VALUES (?,?,?,?,'proposed',?)`, id, r.ns, c.question, string(exp), s.now()); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, ActGatePropose, id, map[string]any{"fact": c.factID, "run": r.res.RunID}); err != nil {
				return err
			}
			r.res.Counts.GateProposed = append(r.res.Counts.GateProposed, id)
		}
		return nil
	})
}
