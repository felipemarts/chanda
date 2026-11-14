package rank

import (
	"errors"
	"math"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

func insertModel(t *testing.T, e *env.Env, id, status, created string) {
	t.Helper()
	if _, err := e.Store.DB().ExecContext(bg, `INSERT INTO ranker_models(id, namespace, kind, status, weights, feature_names, n_examples, created_at)
		VALUES (?,'ns','logreg',?,'{}','[]',100,?)`, id, status, created); err != nil {
		t.Fatal(err)
	}
}

func modelStatus(t *testing.T, e *env.Env, id string) string {
	var s string
	e.Store.DB().QueryRowContext(bg, `SELECT status FROM ranker_models WHERE id = ?`, id).Scan(&s)
	return s
}

// fixedEval returns base for the baseline and learned for the learned ranking.
func fixedEval(base, learned float64, calls *int) Evaluator {
	return func(useLearned bool, lambda float64) (float64, error) {
		*calls++
		if useLearned {
			return learned, nil
		}
		return base, nil
	}
}

// BASE.md: λ only leaves 0 if the learned ranking beats the baseline on the
// fixed set by the configured margin; it rises by steps up to the ceiling.
func TestPromotionByEvaluation(t *testing.T) {
	tests := []struct {
		name         string
		base, learnd float64
		times        int
		wantLambda   float64
		wantStatus   string
		wantNotice   string
	}{
		{"beats baseline by the margin: promoted at one step", 70, 71, 1, 0.05, "promoted", ""},
		{"beats baseline by less than the margin: stays 0", 70, 70.5, 1, 0, "shadow", NoticeGateFailed},
		{"equal to baseline: stays 0", 70, 70, 1, 0, "shadow", NoticeGateFailed},
		{"worse than baseline: stays 0", 70, 60, 1, 0, "shadow", NoticeGateFailed},
		{"repeated passes rise by step", 70, 75, 3, 0.15, "promoted", ""},
		{"capped at lambda max", 70, 75, 10, 0.3, "promoted", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, e, _ := newTest(t, nil) // margin 1, step 0.05, max 0.3
			insertModel(t, e, "mdl_a", "shadow", "2026-01-01")
			var res PromoteResult
			var err error
			calls := 0
			for i := 0; i < tc.times; i++ {
				if res, err = r.Promote(bg, "ns", "eval", fixedEval(tc.base, tc.learnd, &calls)); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2*tc.times {
				t.Fatalf("evaluator calls %d", calls)
			}
			got, _ := r.Lambda(bg, "ns")
			if math.Abs(got-tc.wantLambda) > 1e-9 || math.Abs(res.LambdaTo-tc.wantLambda) > 1e-9 {
				t.Fatalf("lambda %v (result %v) want %v", got, res.LambdaTo, tc.wantLambda)
			}
			if s := modelStatus(t, e, "mdl_a"); s != tc.wantStatus {
				t.Fatalf("model status %s", s)
			}
			if tc.wantNotice != "" && (len(res.Notices) != 1 || res.Notices[0] != tc.wantNotice) {
				t.Fatalf("notices %v", res.Notices)
			}
		})
	}
}

func TestFailedGateReturnsLambdaToZero(t *testing.T) {
	r, e, _ := newTest(t, nil)
	insertModel(t, e, "mdl_a", "shadow", "2026-01-01")
	calls := 0
	for i := 0; i < 3; i++ {
		r.Promote(bg, "ns", "eval", fixedEval(70, 75, &calls))
	}
	res, err := r.Promote(bg, "ns", "eval", fixedEval(70, 69, &calls))
	if err != nil || res.Passed || res.LambdaFrom == 0 || res.LambdaTo != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _ := r.Lambda(bg, "ns"); got != 0 {
		t.Fatalf("lambda %v after failed gate", got)
	}
	if s := modelStatus(t, e, "mdl_a"); s != "shadow" {
		t.Fatalf("failed model goes back to shadow, got %s", s)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActRankPromote); n != 1 {
		t.Fatalf("promote audit rows %d", n)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActRankAdjust); n != 3 {
		t.Fatalf("adjust audit rows %d", n)
	}
}

func TestNewModelReplacesPromotedOneFromFirstStep(t *testing.T) {
	r, e, _ := newTest(t, nil)
	insertModel(t, e, "mdl_a", "shadow", "2026-01-01")
	calls := 0
	r.Promote(bg, "ns", "eval", fixedEval(70, 75, &calls))
	r.Promote(bg, "ns", "eval", fixedEval(70, 75, &calls))
	insertModel(t, e, "mdl_b", "shadow", "2026-02-01")
	res, err := r.Promote(bg, "ns", "eval", fixedEval(70, 75, &calls))
	if err != nil || !res.Promoted || res.ModelID != "mdl_b" || math.Abs(res.LambdaTo-0.05) > 1e-12 {
		t.Fatalf("%+v %v", res, err)
	}
	if modelStatus(t, e, "mdl_a") != "retired" || modelStatus(t, e, "mdl_b") != "promoted" {
		t.Fatal("statuses after replacement")
	}
}

func TestPromoteDoesNothingWhenLearningIsOffOrNoModel(t *testing.T) {
	calls := 0
	r, e, _ := newTest(t, func(c *config.RankingConfig) { c.Enabled = false })
	insertModel(t, e, "mdl_a", "shadow", "2026-01-01")
	res, err := r.Promote(bg, "ns", "eval", fixedEval(0, 100, &calls))
	if err != nil || calls != 0 || res.Notices[0] != NoticeLearningDisabled {
		t.Fatalf("%+v %v calls=%d", res, err, calls)
	}
	r2, _, _ := newTest(t, nil)
	res, err = r2.Promote(bg, "ns", "eval", fixedEval(0, 100, &calls))
	if err != nil || calls != 0 || res.Notices[0] != NoticeNoModel {
		t.Fatalf("%+v %v", res, err)
	}
	r3, e3, _ := newTest(t, nil)
	insertModel(t, e3, "mdl_a", "shadow", "2026-01-01")
	if _, err := r3.Promote(bg, "ns", "eval", func(bool, float64) (float64, error) { return 0, errors.New("boom") }); err == nil {
		t.Fatal("evaluator error must propagate")
	}
	if got, _ := r3.Lambda(bg, "ns"); got != 0 || modelStatus(t, e3, "mdl_a") != "shadow" {
		t.Fatal("evaluator error changed state")
	}
}
