package rank

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

func feat(name string, x []float64) float64 {
	for i, n := range FeatureNames {
		if n == name {
			return x[i]
		}
	}
	panic(name)
}

func TestStage2Features(t *testing.T) {
	now := env.TestEpoch
	yes := true
	used := &urow{Alpha: 3, Beta: 1, NShown: 4, LastUsedAt: core.FormatTime(now.Add(-48 * time.Hour))}
	tests := []struct {
		name string
		c    Candidate
		u    *urow
		want map[string]float64
	}{
		{"scores copied", Candidate{ID: "evt_1", BM25: 2, Cosine: 0.5, Rerank: 0.7, RRF: 0.03}, nil,
			map[string]float64{"bm25": 2, "cosine": 0.5, "rerank": 0.7, "rrf": 0.03}},
		{"record type one-hot from id", Candidate{ID: "fct_1"}, nil, map[string]float64{"type_fact": 1, "type_event": 0}},
		{"zone one-hot, missing zone is external", Candidate{ID: "pg_1"}, nil, map[string]float64{"zone_external": 1, "zone_user": 0, "type_page": 1}},
		{"user zone", Candidate{ID: "evt_1", Zone: core.ZoneUser}, nil, map[string]float64{"zone_user": 1, "zone_external": 0}},
		{"no utility is neutral and never used", Candidate{ID: "evt_1"}, nil,
			map[string]float64{"utility_mean": 0.5, "log_utility_count": 0, "never_used": 1, "log_days_since_use": 0}},
		{"utility mean, count and time since last use", Candidate{ID: "evt_1"}, used,
			map[string]float64{"utility_mean": 0.75, "log_utility_count": math.Log1p(4), "never_used": 0, "log_days_since_use": math.Log1p(2)}},
		{"age in days", Candidate{ID: "evt_1", AgeSeconds: 86400}, nil, map[string]float64{"log_age_days": math.Log1p(1)}},
		{"entity match when present", Candidate{ID: "evt_1", EntityMatch: &yes}, nil, map[string]float64{"entity_match": 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := features(tc.c, tc.u, now)
			if len(x) != len(FeatureNames) {
				t.Fatalf("len %d", len(x))
			}
			for k, v := range tc.want {
				if got := feat(k, x); math.Abs(got-v) > 1e-12 {
					t.Errorf("%s = %v want %v", k, got, v)
				}
			}
		})
	}
}

func TestAUC(t *testing.T) {
	tests := []struct {
		name           string
		scores, labels []float64
		want           float64
	}{
		{"perfect", []float64{0.1, 0.2, 0.8, 0.9}, []float64{0, 0, 1, 1}, 1},
		{"inverted", []float64{0.9, 0.8, 0.2, 0.1}, []float64{0, 0, 1, 1}, 0},
		{"all tied", []float64{0.5, 0.5, 0.5, 0.5}, []float64{0, 1, 0, 1}, 0.5},
		{"single class", []float64{0.1, 0.2}, []float64{1, 1}, 0.5},
	}
	for _, tc := range tests {
		if got := AUC(tc.scores, tc.labels); got != tc.want {
			t.Errorf("%s: %v want %v", tc.name, got, tc.want)
		}
	}
}

// separable is a deterministic dataset where feature 14 (utility_mean) decides.
func separable(n int) ([][]float64, []float64) {
	var X [][]float64
	var y []float64
	for i := 0; i < n; i++ {
		x := make([]float64, len(FeatureNames))
		x[0] = hash01("x0", i)
		x[3] = hash01("x3", i)
		x[14] = hash01("u", i)
		X = append(X, x)
		if x[14] > 0.6 {
			y = append(y, 1)
		} else {
			y = append(y, 0)
		}
	}
	return X, y
}

func TestRankersAreDeterministicAndLearn(t *testing.T) {
	cfg := config.Default().Ranking
	X, y := separable(200)
	for _, kind := range []string{KindLogReg, KindMLP} {
		t.Run(kind, func(t *testing.T) {
			a, err := fit(kind, X, y, cfg)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := fit(kind, X, y, cfg)
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if string(ja) != string(jb) {
				t.Fatal("two trainings on the same data differ")
			}
			ps := make([]float64, len(X))
			for i, x := range X {
				ps[i] = a.Predict(x)
			}
			if auc := AUC(ps, y); auc < 0.95 {
				t.Fatalf("auc %v", auc)
			}
			// The standardisation travels with the model: a JSON round trip predicts the same.
			var back Model
			if err := json.Unmarshal(ja, &back); err != nil {
				t.Fatal(err)
			}
			if back.Predict(X[7]) != a.Predict(X[7]) || len(back.Mean) != len(FeatureNames) {
				t.Fatal("round trip changed predictions")
			}
		})
	}
}

func TestInitialisationIsZeroOrSeededHash(t *testing.T) {
	cfg := config.Default().Ranking
	cfg.TrainIterations = 0
	X, y := separable(10)
	lr, _ := fit(KindLogReg, X, y, cfg)
	for _, w := range lr.W {
		if w != 0 {
			t.Fatal("logreg must start from zero")
		}
	}
	m1, _ := fit(KindMLP, X, y, cfg)
	if m1.W1[0][0] == 0 || m1.W1[0][0] == m1.W1[0][1] {
		t.Fatal("mlp init must be non-zero and break symmetry")
	}
	cfg.ModelSeed = "other"
	m2, _ := fit(KindMLP, X, y, cfg)
	if m2.W1[0][0] == m1.W1[0][0] {
		t.Fatal("mlp init must depend on the seed")
	}
	if _, err := fit("tree", X, y, cfg); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatal("unknown kind")
	}
}

func TestTrainVersionsModelsInShadow(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 0; c.MinTrainExamples = 30 })
	w := newWorld()
	w.play(t, r, e, fc, "ns", 0, 5)
	if _, err := r.Train(bg, "ns", "", "sleep"); core.CodeOf(err) != core.CodeUnavailable {
		t.Fatalf("below min examples: %v", err)
	}
	w.play(t, r, e, fc, "ns", 5, 60)
	first, err := r.Train(bg, "ns", "", "sleep")
	if err != nil {
		t.Fatal(err)
	}
	if first.NExamples != 65*3 || first.NHoldout != 13*3 || first.Kind != KindLogReg {
		t.Fatalf("train %+v", first)
	}
	var status, names string
	var n int
	var eval, base float64
	e.Store.DB().QueryRowContext(bg, `SELECT status, feature_names, n_examples, eval_score, baseline_score FROM ranker_models WHERE id = ?`, first.ModelID).
		Scan(&status, &names, &n, &eval, &base)
	if status != "shadow" || n != first.NExamples || eval != first.EvalScore || base != first.BaselineScore {
		t.Fatalf("row %s %d %v %v", status, n, eval, base)
	}
	var fn []string
	json.Unmarshal([]byte(names), &fn)
	if len(fn) != len(FeatureNames) {
		t.Fatal("feature names not stored")
	}
	second, err := r.Train(bg, "ns", KindMLP, "sleep")
	if err != nil {
		t.Fatal(err)
	}
	e.Store.DB().QueryRowContext(bg, `SELECT status FROM ranker_models WHERE id = ?`, first.ModelID).Scan(&status)
	if status != "retired" || second.Kind != KindMLP {
		t.Fatalf("older shadow model should be retired, got %s", status)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ? AND target LIKE 'mdl_%'`, audit.ActRankAdjust); n != 2 {
		t.Fatal("training not audited")
	}
}

func TestExamplesUseOnlyPastSignalsAndSkipGateRetrievals(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 0 })
	w := newWorld()
	w.play(t, r, e, fc, "ns", 0, 8)
	gate := seedRetrieval(t, e, "ns", cands("evt_u00"), []string{"evt_u00"})
	e.Store.DB().ExecContext(bg, `UPDATE retrievals SET params = '{"gate":true}' WHERE id = ?`, gate)
	mustOutcome(t, r, "ns", gate, 1, "evt_u00")
	exs, err := r.Examples(bg, "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(exs) != 8*3 {
		t.Fatalf("examples %d", len(exs))
	}
	for _, ex := range exs[:3] { // first retrieval: nothing learned yet
		if feat("log_utility_count", ex.X) != 0 || feat("never_used", ex.X) != 1 {
			t.Fatalf("first retrieval leaked its own outcome: %v", ex.X)
		}
	}
	for _, ex := range exs {
		if ex.RetrievalID == gate {
			t.Fatal("gate retrieval entered training")
		}
		if (ex.Y == 1) != w.isUseful(ex.ID) {
			t.Fatalf("label of %s = %v", ex.ID, ex.Y)
		}
	}
}
