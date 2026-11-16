package rank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
)

func TestUtilityOnlyEntersAfterMinExposures(t *testing.T) {
	r, e, _ := newTest(t, nil) // MinExposures 5
	for i := 1; i <= 5; i++ {
		ret := seedRetrieval(t, e, "ns", cands("evt_a", "evt_b"), []string{"evt_a", "evt_b"})
		mustOutcome(t, r, "ns", ret, 1, "evt_a")
		got, err := r.Scores(bg, "ns", []string{"evt_a", "evt_b", "evt_never"})
		if err != nil {
			t.Fatal(err)
		}
		if i < 5 && len(got) != 0 {
			t.Fatalf("exposure %d: scores before min exposures: %v", i, got)
		}
		if i == 5 {
			if len(got) != 2 || got["evt_a"] <= got["evt_b"] {
				t.Fatalf("exposure 5: %v", got)
			}
			if _, ok := got["evt_never"]; ok {
				t.Fatal("never-shown record must be neutral (absent)")
			}
		}
	}
	if got, _ := r.Scores(bg, "other", []string{"evt_a"}); len(got) != 0 {
		t.Fatal("scores leaked across namespaces")
	}
}

func TestLambdaIsZeroUnlessLearningOnAndModelPromoted(t *testing.T) {
	tests := []struct {
		name     string
		cfgOn    bool
		st       rankState
		want     float64
		lambdaMx float64
	}{
		{"promoted model and learning on", true, rankState{Enabled: true, Lambda: 0.2, ActiveModel: "mdl_1"}, 0.2, 0.3},
		{"configuration option off", false, rankState{Enabled: true, Lambda: 0.2, ActiveModel: "mdl_1"}, 0, 0.3},
		{"runtime toggle off", true, rankState{Enabled: false, Lambda: 0.2, ActiveModel: "mdl_1"}, 0, 0.3},
		{"no promoted model", true, rankState{Enabled: true, Lambda: 0.2}, 0, 0.3},
		{"capped at lambda max", true, rankState{Enabled: true, Lambda: 0.9, ActiveModel: "mdl_1"}, 0.3, 0.3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, e, _ := newTest(t, func(c *config.RankingConfig) { c.Enabled = tc.cfgOn; c.LambdaMax = tc.lambdaMx })
			if err := saveState(bg, e.Store.DB(), "ns", tc.st, "x"); err != nil {
				t.Fatal(err)
			}
			if got, _ := r.Lambda(bg, "ns"); got != tc.want {
				t.Fatalf("lambda %v want %v", got, tc.want)
			}
		})
	}
	r, _, _ := newTest(t, nil)
	if got, _ := r.Lambda(bg, "fresh"); got != 0 {
		t.Fatal("a new namespace starts at 0")
	}
}

// fakeTerm is a deterministic scorer standing in for the learned model.
func fakeTerm(id string) float64 {
	h := sha256.Sum256([]byte("term:" + id))
	return float64(binary.BigEndian.Uint64(h[:8])>>11) / (1 << 53)
}

// c4Order is the pre-learning order: relevance descending, id ascending.
func c4Order(items []Scored) []string {
	s := append([]Scored(nil), items...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].Relevance != s[j].Relevance {
			return s[i].Relevance > s[j].Relevance
		}
		return s[i].ID < s[j].ID
	})
	out := make([]string, len(s))
	for i := range s {
		out[i] = s[i].ID
	}
	return out
}

// BASE.md phase 8: turning learning off returns exactly the order of C4.
func TestLearningOffReturnsExactlyThePreLearningOrder(t *testing.T) {
	var items []Scored
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("evt_%03d", i)
		rel := float64((i*37)%50) / 50 // many ties to exercise the tiebreak
		items = append(items, Scored{ID: id, Relevance: rel, Term: fakeTerm(id)})
	}
	want := c4Order(items)
	off := map[string]func(*testing.T) *Ranker{
		"runtime toggle": func(t *testing.T) *Ranker {
			r, e, _ := newTest(t, nil)
			saveState(bg, e.Store.DB(), "ns", rankState{Enabled: true, Lambda: 0.3, ActiveModel: "mdl_1"}, "x")
			if _, err := r.SetEnabled(bg, "ns", false, "admin"); err != nil {
				t.Fatal(err)
			}
			return r
		},
		"configuration option": func(t *testing.T) *Ranker {
			r, e, _ := newTest(t, func(c *config.RankingConfig) { c.Enabled = false })
			saveState(bg, e.Store.DB(), "ns", rankState{Enabled: true, Lambda: 0.3, ActiveModel: "mdl_1"}, "x")
			return r
		},
		"reset": func(t *testing.T) *Ranker {
			r, e, _ := newTest(t, nil)
			saveState(bg, e.Store.DB(), "ns", rankState{Enabled: true, Lambda: 0.3, ActiveModel: "mdl_1"}, "x")
			if _, err := r.Reset(bg, "ns", "admin"); err != nil {
				t.Fatal(err)
			}
			return r
		},
	}
	for _, name := range []string{"configuration option", "reset", "runtime toggle"} {
		t.Run(name, func(t *testing.T) {
			r := off[name](t)
			lambda, err := r.Lambda(bg, "ns")
			if err != nil || lambda != 0 {
				t.Fatalf("lambda %v %v", lambda, err)
			}
			if got := Order(items, lambda); !reflect.DeepEqual(got, want) {
				t.Fatal("order with learning off differs from the pre-learning order")
			}
			for _, it := range items {
				if Final(it.Relevance, it.Term, lambda) != it.Relevance {
					t.Fatal("final score must equal relevance bit for bit")
				}
			}
		})
	}
	if reflect.DeepEqual(Order(items, 0.3), want) {
		t.Fatal("sanity: with λ>0 the fake scorer should change the order")
	}
}

func TestSetEnabledIsAuditedAndZeroesLambda(t *testing.T) {
	r, e, _ := newTest(t, nil)
	saveState(bg, e.Store.DB(), "ns", rankState{Enabled: true, Lambda: 0.2, ActiveModel: "mdl_1"}, "x")
	if _, err := r.SetEnabled(bg, "ns", false, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetEnabled(bg, "ns", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Lambda(bg, "ns"); got != 0 {
		t.Fatalf("re-enabling must not restore λ without promotion: %v", got)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActRankToggle); n != 2 {
		t.Fatalf("toggle audit rows %d", n)
	}
}

func TestResetClearsLearningState(t *testing.T) {
	r, e, _ := newTest(t, nil)
	seedRecords(t, e, "ns")
	ret := seedRetrieval(t, e, "ns", cands("evt_a", "evt_b"), []string{"evt_a", "evt_b"})
	mustOutcome(t, r, "ns", ret, 1, "evt_a")
	r.Flag(bg, "ns", "evt_ns", "wrong", "user")
	e.Store.DB().ExecContext(bg, `INSERT INTO ranker_models(id, namespace, kind, status, weights, feature_names, n_examples, created_at) VALUES ('mdl_1','ns','logreg','promoted','{}','[]',1,'x')`)
	e.Store.DB().ExecContext(bg, `UPDATE rank_state SET lambda = 0.2, active_model = 'mdl_1' WHERE namespace = 'ns'`)
	other := seedRetrieval(t, e, "ot", cands("evt_z"), []string{"evt_z"})
	mustOutcome(t, r, "ot", other, 1, "evt_z")

	res, err := r.Reset(bg, "ns", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if res.UtilityRows != 3 || res.Models != 1 {
		t.Fatalf("reset %+v", res)
	}
	for _, q := range []string{`SELECT COUNT(*) FROM utility WHERE namespace = 'ns'`, `SELECT COUNT(*) FROM ranker_models WHERE namespace = 'ns'`} {
		if n := countRows(t, e, q); n != 0 {
			t.Fatalf("%s = %d", q, n)
		}
	}
	st, _ := loadState(bg, e.Store.DB(), "ns")
	if st.Lambda != 0 || st.Mean != 0 || st.N != 0 || st.ActiveModel != "" || st.ResetAt == "" || !st.Enabled {
		t.Fatalf("state after reset %+v", st)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM utility WHERE namespace = 'ot'`); n != 1 {
		t.Fatal("reset touched another namespace")
	}
	if ok, _ := r.IsFlagged(bg, "ns", "evt_ns"); !ok {
		t.Fatal("reset must keep flagged records out of reads")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActRankReset); n != 1 {
		t.Fatal("reset not audited")
	}
}
