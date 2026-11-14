// Package lme is the LongMemEval harness of the Chanda evaluation battery:
// dataset loader, stratified sampler, retrieval metrics, the phase 1
// retrieval conditions (BM25, plain RAG, Chanda search), context generation
// for phases 3/4, the embedding cache and the OpenAI-compatible embedding
// server. See eval/lme/README.md.
package lme

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Turn is one message of a haystack session.
type Turn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer bool   `json:"has_answer,omitempty"`
}

// Question is one LongMemEval instance (fields of the official README).
type Question struct {
	QuestionID         string          `json:"question_id"`
	QuestionType       string          `json:"question_type"`
	Question           string          `json:"question"`
	Answer             json.RawMessage `json:"answer"` // a string, sometimes a number
	QuestionDate       string          `json:"question_date"`
	HaystackSessionIDs []string        `json:"haystack_session_ids"`
	HaystackDates      []string        `json:"haystack_dates"`
	HaystackSessions   [][]Turn        `json:"haystack_sessions"`
	AnswerSessionIDs   []string        `json:"answer_session_ids"`
}

// AnswerText returns the gold answer as text (numbers are printed as given).
func (q *Question) AnswerText() string {
	var s string
	if json.Unmarshal(q.Answer, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(q.Answer))
}

// IsAbstention reports whether q is an abstention question: per the official
// README, "If question_id ends with _abs, then the question is an abstention
// question" (the retrieval scripts test the substring "_abs").
func IsAbstention(id string) bool { return strings.HasSuffix(id, "_abs") }

// Abstention is the pseudo-type used when abstention questions are reported
// as their own group.
const Abstention = "abstention"

// Validate checks the structural invariants the harness relies on.
func (q *Question) Validate() error {
	n := len(q.HaystackSessionIDs)
	if q.QuestionID == "" || q.QuestionType == "" || q.Question == "" {
		return fmt.Errorf("question %q: missing id, type or text", q.QuestionID)
	}
	if len(q.HaystackDates) != n || len(q.HaystackSessions) != n {
		return fmt.Errorf("question %s: %d session ids, %d dates, %d sessions", q.QuestionID, n, len(q.HaystackDates), len(q.HaystackSessions))
	}
	if _, err := ParseDate(q.QuestionDate); err != nil {
		return fmt.Errorf("question %s: %w", q.QuestionID, err)
	}
	for i, d := range q.HaystackDates {
		if _, err := ParseDate(d); err != nil {
			return fmt.Errorf("question %s session %d: %w", q.QuestionID, i, err)
		}
	}
	return nil
}

// dateLayout is the dataset's date format, e.g. "2023/05/20 (Sat) 02:21".
const dateLayout = "2006/01/02 (Mon) 15:04"

// ParseDate parses a dataset date. The dataset carries no time zone; it is
// read as UTC, consistently for sessions and questions.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q: %w", s, err)
	}
	return t.UTC(), nil
}

// RFC3339 converts a dataset date to RFC 3339 (UTC), the form Chanda accepts.
func RFC3339(s string) (string, error) {
	t, err := ParseDate(s)
	if err != nil {
		return "", err
	}
	return t.Format(time.RFC3339), nil
}

// Each streams the questions of a dataset file (a JSON array) to fn, in file
// order, without holding the whole file in memory. fn may keep q.
func Each(path string, fn func(q *Question) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return EachReader(f, fn)
}

// EachReader is Each over a reader.
func EachReader(r io.Reader, fn func(q *Question) error) error {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("dataset: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return fmt.Errorf("dataset: want a JSON array, got %v", tok)
	}
	for dec.More() {
		q := new(Question)
		if err := dec.Decode(q); err != nil {
			return fmt.Errorf("dataset: %w", err)
		}
		if err := q.Validate(); err != nil {
			return err
		}
		if err := fn(q); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("dataset: %w", err)
	}
	return nil
}

// Select streams only the questions whose id is in ids (all when ids is nil).
func Select(path string, ids map[string]bool, fn func(q *Question) error) error {
	return Each(path, func(q *Question) error {
		if ids != nil && !ids[q.QuestionID] {
			return nil
		}
		return fn(q)
	})
}

// OfficialTargets returns the evidence sessions as the official retrieval
// code defines them at session granularity (src/retrieval/run_retrieval.py,
// process_item_flat_index): a haystack session whose id contains "answer"
// and that has at least one user turn with has_answer. A session with
// "answer" in its id but no such user turn is renamed "noans" there and does
// not count. A question with no target is skipped by the official averages.
func OfficialTargets(q *Question) []string {
	var out []string
	for i, id := range q.HaystackSessionIDs {
		if !strings.Contains(id, "answer") {
			continue
		}
		for _, t := range q.HaystackSessions[i] {
			if t.Role == "user" && t.HasAnswer {
				out = append(out, id)
				break
			}
		}
	}
	return out
}
