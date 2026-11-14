package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/rank"
	"github.com/felipemarts/chanda-mcp/internal/read"
	"github.com/felipemarts/chanda-mcp/internal/sleep"
	"github.com/felipemarts/chanda-mcp/internal/store"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// The gate set (decisions 0061 and 0092): approved gate_questions scored
// with the metric of internal/eval.GateScore, through the read path's
// read-only Score. The formula is copied here (internal/eval belongs to the
// evaluation harness and must not be a dependency of the server); a test
// checks both agree on a fixture.

// GateQuestion is one row of the gate set.
type GateQuestion struct {
	ID        string             `json:"id"`
	Namespace string             `json:"namespace"`
	Question  string             `json:"question"`
	Expected  sleep.GateExpected `json:"expected"`
	Status    string             `json:"status"` // proposed, approved or retired
	CreatedAt string             `json:"created_at"`
}

// Gate question statuses.
const (
	GateProposed = "proposed"
	GateApproved = "approved"
	GateRetired  = "retired"
)

// parseGateExpected decodes gate_questions.expected strictly (as
// eval.ParseGateExpected does).
func parseGateExpected(s string) (sleep.GateExpected, error) {
	var e sleep.GateExpected
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

// loadGate reads the gate questions of ns with status (all when empty), by id.
func loadGate(ctx context.Context, q store.Queryer, ns, status string) ([]GateQuestion, error) {
	query := `SELECT id, namespace, question, expected, status, created_at FROM gate_questions WHERE namespace = ?`
	args := []any{ns}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	var out []GateQuestion
	var raw []string
	for rows.Next() {
		var g GateQuestion
		var exp string
		if err := rows.Scan(&g.ID, &g.Namespace, &g.Question, &exp, &g.Status, &g.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out, raw = append(out, g), append(raw, exp)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Expected, err = parseGateExpected(raw[i]); err != nil {
			return nil, fmt.Errorf("gate question %s: %w", out[i].ID, err)
		}
	}
	return out, nil
}

// gateScore is eval.GateScoreBudget over the read path: the mean over
// questions, in points (0-100), of
//
//   - with record ids and answers: mean of nDCG@10 over the ids and
//     answer-in-context (1 or 0);
//   - with only one of them: that component;
//   - with neither (abstention): 1 if the result is empty, else 0;
//
// on the context-mode delivery at read.context_default_tokens, after the
// harness budget enforcement. An empty set scores 0 (eval refuses it; the
// sleep gate reports it through GateSize instead).
func (a *App) gateScore(ctx context.Context, ns string, qs []GateQuestion, o read.ScoreOptions) (float64, error) {
	if len(qs) == 0 {
		return 0, nil
	}
	budget := a.e.Cfg.Read.ContextDefaultTokens
	var total float64
	for _, gq := range qs {
		in := read.ScoreInput{Namespace: ns, Query: gq.Question, Mode: read.ModeContext, BudgetTokens: budget}
		if gq.Expected.AsOf != "" {
			t, err := core.ParseTime(gq.Expected.AsOf)
			if err != nil {
				return 0, err
			}
			in.AsOf = t
		}
		res, err := a.reader.Score(ctx, in, o)
		if err != nil {
			return 0, fmt.Errorf("gate question %s: %w", gq.ID, err)
		}
		var got []gateItem
		for _, it := range res.Items {
			got = append(got, gateItem{id: it.ID, text: item(it).Text})
		}
		total += gateQuestionScore(gq.Expected, enforceBudget(got, budget))
	}
	return 100 * total / float64(len(qs)), nil
}

type gateItem struct{ id, text string }

// enforceBudget is eval.EnforceBudget: keep, in rank order, the items whose
// tokens (eval.CountTokens) still fit.
func enforceBudget(xs []gateItem, budget int) []gateItem {
	var out []gateItem
	used := 0
	for _, x := range xs {
		t := (utf8.RuneCountInString(x.text) + 3) / 4
		if used+t > budget {
			continue
		}
		used += t
		out = append(out, x)
	}
	return out
}

// gateQuestionScore is eval's gateQuestionScore with Judge specialised to
// gold record ids (each id its own relevance unit, hit by identity).
func gateQuestionScore(e sleep.GateExpected, xs []gateItem) float64 {
	if len(e.RecordIDs) == 0 && len(e.Answers) == 0 {
		if len(xs) == 0 {
			return 1
		}
		return 0
	}
	var all strings.Builder
	for _, x := range xs {
		all.WriteString(normalizeText(x.text) + "\n")
	}
	var sum float64
	n := 0
	if len(e.RecordIDs) > 0 {
		ranks := make([]int, len(e.RecordIDs))
		for i, x := range xs {
			for j, id := range e.RecordIDs {
				if ranks[j] == 0 && x.id == id {
					ranks[j] = i + 1
				}
			}
		}
		sum += ndcgAt(ranks, 10)
		n++
	}
	if len(e.Answers) > 0 {
		hit := false
		for _, ans := range e.Answers {
			hit = hit || strings.Contains(all.String(), normalizeText(ans))
		}
		if hit {
			sum++
		}
		n++
	}
	return sum / float64(n)
}

func ndcgAt(firstHit []int, k int) float64 {
	var dcg, idcg float64
	for _, r := range firstHit {
		if r > 0 && r <= k {
			dcg += 1 / math.Log2(float64(r)+1)
		}
	}
	for i := 1; i <= min(len(firstHit), k); i++ {
		idcg += 1 / math.Log2(float64(i)+1)
	}
	return min(1, dcg/idcg)
}

var foldAccents = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// normalizeText is eval.Normalize: lowercase, no diacritics, whitespace
// collapsed.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		r = unicode.ToLower(r)
		if f, ok := foldAccents[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// gateEvaluator is sleep's Evaluator: the approved gate set scored inside
// the gate transaction. Through a transaction the read path is keyword only
// with λ = 0 (read.ScoreOptions.Q): the vector index, utility and learned
// ranker read the store on their own and would deadlock on the single
// connection (decision 0092).
type gateEvaluator struct{ a *App }

func (g gateEvaluator) Score(ctx context.Context, q store.Queryer, ns string) (float64, error) {
	qs, err := loadGate(ctx, q, ns, GateApproved)
	if err != nil {
		return 0, err
	}
	return g.a.gateScore(ctx, ns, qs, read.ScoreOptions{Q: q})
}

func (g gateEvaluator) GateSize(ctx context.Context, q store.Queryer, ns string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM gate_questions WHERE namespace = ? AND status = ?`, ns, GateApproved).Scan(&n)
	return n, err
}

// promotionEvaluator is rank.Promote's Evaluator: the approved gate set
// scored outside any transaction with the full read path, at λ = 0 for the
// baseline and at the candidate λ with the newest model's term otherwise.
// With no approved question both score 0 and the model never passes.
func (a *App) promotionEvaluator(ctx context.Context, ns string) (rank.Evaluator, error) {
	qs, err := loadGate(ctx, a.e.Store.DB(), ns, GateApproved)
	if err != nil {
		return nil, err
	}
	return func(useLearned bool, lambda float64) (float64, error) {
		zero := 0.0
		o := read.ScoreOptions{Lambda: &zero}
		if useLearned {
			o = read.ScoreOptions{Lambda: &lambda, Term: modelTerm{a.ranker.Learned()}}
		}
		return a.gateScore(ctx, ns, qs, o)
	}, nil
}

// ---------------------------------------------------------------------------
// Reviewing the gate set (BASE.md open question "who writes the gate set";
// decision 0092): sleep proposes, only a human approves.

// GateList returns the gate questions of a namespace with the given status
// (all when empty), by id.
func (a *App) GateList(ctx context.Context, namespace, status string) ([]GateQuestion, error) {
	c := a.begin(ctx, namespace)
	switch status {
	case "", GateProposed, GateApproved, GateRetired:
	default:
		return nil, core.Errf(core.CodeInvalidArgument, "unknown gate status %q", status)
	}
	qs, err := loadGate(ctx, a.e.Store.DB(), c.ns, status)
	if qs == nil {
		qs = []GateQuestion{}
	}
	return qs, coded(err)
}

// GateReviewInput approves or retires one gate question.
type GateReviewInput struct {
	Namespace string `json:"namespace,omitempty"`
	ID        string `json:"id"`
	Status    string `json:"status"` // approved or retired
}

// GateReviewOutput reports the change.
type GateReviewOutput struct {
	chanda.Meta
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// GateReview moves a gate question to approved or retired. Only a human
// actor may review (CodePermissionDenied otherwise); a question already in
// that status is a conflict. Audited as gate_review.
func (a *App) GateReview(ctx context.Context, in GateReviewInput) (GateReviewOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := GateReviewOutput{Meta: c.meta(0), ID: in.ID, To: in.Status}
	if !c.actor.Human {
		return out, core.Errf(core.CodePermissionDenied, "only a human may review the gate set")
	}
	if in.Status != GateApproved && in.Status != GateRetired {
		return out, core.Errf(core.CodeInvalidArgument, "status must be approved or retired")
	}
	err := a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT status FROM gate_questions WHERE namespace = ? AND id = ?`, c.ns, in.ID).
			Scan(&out.From); err == sql.ErrNoRows {
			return core.Errf(core.CodeNotFound, "gate question %s not found", in.ID)
		} else if err != nil {
			return err
		}
		if out.From == in.Status {
			return core.Errf(core.CodeConflict, "gate question %s is already %s", in.ID, in.Status)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE gate_questions SET status = ? WHERE id = ?`, in.Status, in.ID); err != nil {
			return err
		}
		id, err := a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: c.ns, Actor: c.actor.Name, Action: audit.ActGateReview,
			Target: in.ID, Details: map[string]any{"from": out.From, "to": in.Status}})
		out.Meta = c.meta(id)
		return err
	})
	return out, coded(err)
}

// GateAddInput is a question written by a person.
type GateAddInput struct {
	Namespace string             `json:"namespace,omitempty"`
	Question  string             `json:"question"`
	Expected  sleep.GateExpected `json:"expected"`
}

// GateAdd stores a question written by a human, approved at once. Only a
// human actor may add (CodePermissionDenied otherwise). Audited as
// gate_review (to approved).
func (a *App) GateAdd(ctx context.Context, in GateAddInput) (GateQuestion, chanda.Meta, error) {
	c := a.begin(ctx, in.Namespace)
	q := GateQuestion{Namespace: c.ns, Question: strings.TrimSpace(in.Question), Expected: in.Expected, Status: GateApproved}
	if !c.actor.Human {
		return q, c.meta(0), core.Errf(core.CodePermissionDenied, "only a human may write the gate set")
	}
	if q.Question == "" {
		return q, c.meta(0), core.Errf(core.CodeInvalidArgument, "question is required")
	}
	exp, err := json.Marshal(in.Expected)
	if err != nil {
		return q, c.meta(0), coded(err)
	}
	if _, err := parseGateExpected(string(exp)); err != nil {
		return q, c.meta(0), err
	}
	var auditID int64
	err = a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		q.ID, q.CreatedAt = a.e.IDs.New(sleep.PrefixGateQuestion), core.FormatTime(a.e.Clock.Now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO gate_questions(id, namespace, question, expected, status, created_at)
			VALUES (?,?,?,?,?,?)`, q.ID, c.ns, q.Question, string(exp), GateApproved, q.CreatedAt); err != nil {
			return err
		}
		var err error
		auditID, err = a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: c.ns, Actor: c.actor.Name, Action: audit.ActGateReview,
			Target: q.ID, Details: map[string]any{"from": "", "to": GateApproved}})
		return err
	})
	return q, c.meta(auditID), coded(err)
}
