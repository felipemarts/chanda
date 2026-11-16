package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/store"
)

// GateExpected is the JSON stored in gate_questions.expected. Record ids are
// the ids a system returns (evt_, fct_, pg_, ...). With neither ids nor
// answers, the question expects an empty result (abstention).
type GateExpected struct {
	RecordIDs []string `json:"record_ids,omitempty"`
	Answers   []string `json:"answers,omitempty"` // any of these substrings in the context answers it
	AsOf      string   `json:"as_of,omitempty"`
}

// GateQuestion is one approved row of gate_questions.
type GateQuestion struct {
	ID        string
	Namespace string
	Question  string
	Expected  GateExpected
}

// ParseGateExpected decodes gate_questions.expected strictly.
func ParseGateExpected(s string) (GateExpected, error) {
	var e GateExpected
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return e, core.Wrap(core.CodeInvalidArgument, err, "gate_questions.expected")
	}
	if e.AsOf != "" {
		if _, err := core.ParseTime(e.AsOf); err != nil {
			return e, err
		}
	}
	return e, nil
}

// LoadGateQuestions reads the approved gate questions of a namespace, by id.
func LoadGateQuestions(ctx context.Context, q store.Queryer, namespace string) ([]GateQuestion, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, namespace, question, expected FROM gate_questions
		 WHERE namespace = ? AND status = 'approved' ORDER BY id`, namespace)
	if err != nil {
		return nil, err
	}
	type raw struct{ id, ns, question, expected string }
	var rs []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.id, &r.ns, &r.question, &r.expected); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]GateQuestion, 0, len(rs))
	for _, r := range rs {
		e, err := ParseGateExpected(r.expected)
		if err != nil {
			return nil, fmt.Errorf("gate question %s: %w", r.id, err)
		}
		out = append(out, GateQuestion{ID: r.id, Namespace: r.ns, Question: r.question, Expected: e})
	}
	return out, nil
}

// GateScore scores sys on a namespace's gate set at the default context
// budget (read.context_default_tokens). See GateScoreBudget.
func GateScore(ctx context.Context, sys System, questions []GateQuestion) (float64, error) {
	return GateScoreBudget(ctx, sys, questions, config.Default().Read.ContextDefaultTokens)
}

// GateScoreBudget returns a score in [0, 100] (so the sleep gate tolerance
// of "2 points" applies directly): the mean over questions of
//
//   - with record ids and answers: mean of nDCG@10 over the ids and
//     answer-in-context (1 or 0);
//   - with only one of them: that component;
//   - with neither (abstention): 1 if the result is empty, else 0.
//
// It uses the same budget enforcement and metrics as the report, is
// deterministic for a deterministic System, and fails on an empty set.
func GateScoreBudget(ctx context.Context, sys System, questions []GateQuestion, budget int) (float64, error) {
	if len(questions) == 0 {
		return 0, core.Errf(core.CodeInvalidArgument, "gate: empty question set")
	}
	var total float64
	for _, gq := range questions {
		q := Query{ID: gq.ID, Text: gq.Question}
		if gq.Expected.AsOf != "" {
			t, err := core.ParseTime(gq.Expected.AsOf)
			if err != nil {
				return 0, err
			}
			q.AsOf = t
		}
		rs, err := sys.Retrieve(ctx, q, budget)
		if err != nil {
			return 0, fmt.Errorf("gate question %s: %w", gq.ID, err)
		}
		kept, _ := EnforceBudget(rs, budget)
		total += gateQuestionScore(gq.Expected, kept)
	}
	return 100 * total / float64(len(questions)), nil
}

func gateQuestionScore(e GateExpected, rs []Retrieved) float64 {
	if len(e.RecordIDs) == 0 && len(e.Answers) == 0 {
		if len(rs) == 0 {
			return 1
		}
		return 0
	}
	// Gold is keyed by record id: judge with DocID := the record's own id.
	byID := make([]Retrieved, len(rs))
	for i, r := range rs {
		r.DocID = r.ID
		byID[i] = r
	}
	q := Question{Answers: e.Answers}
	for _, id := range e.RecordIDs {
		q.Gold = append(q.Gold, Gold{DocID: id})
	}
	j := Judge(q, byID)
	var sum float64
	n := 0
	if len(e.RecordIDs) > 0 {
		sum += j.NDCGAt10
		n++
	}
	if len(e.Answers) > 0 {
		if j.AnswerHit {
			sum++
		}
		n++
	}
	return sum / float64(n)
}
