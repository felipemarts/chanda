package rank

import (
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

func TestOutcomeValidation(t *testing.T) {
	r, e, _ := newTest(t, nil)
	ret := seedRetrieval(t, e, "ns", cands("evt_a"), []string{"evt_a"})
	ok := OutcomeInput{Namespace: "ns", RetrievalID: ret, Value: 1, Actor: "agent", ActorZone: core.ZoneInternal}
	tests := []struct {
		name string
		mut  func(*OutcomeInput)
		code core.Code
	}{
		{"namespace required", func(i *OutcomeInput) { i.Namespace = "" }, core.CodeInvalidArgument},
		{"retrieval required", func(i *OutcomeInput) { i.RetrievalID = "" }, core.CodeInvalidArgument},
		{"value above 1", func(i *OutcomeInput) { i.Value = 1.01 }, core.CodeInvalidArgument},
		{"value below -1", func(i *OutcomeInput) { i.Value = -2 }, core.CodeInvalidArgument},
		{"actor required", func(i *OutcomeInput) { i.Actor = "" }, core.CodeInvalidArgument},
		{"zone required", func(i *OutcomeInput) { i.ActorZone = "" }, core.CodeInvalidArgument},
		{"unknown retrieval", func(i *OutcomeInput) { i.RetrievalID = "ret_nope" }, core.CodeNotFound},
		{"retrieval of another namespace", func(i *OutcomeInput) { i.Namespace = "other" }, core.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := ok
			tc.mut(&in)
			if _, err := r.Outcome(bg, in); core.CodeOf(err) != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM outcomes`); n != 0 {
		t.Fatalf("invalid outcomes stored: %d", n)
	}
}

func TestSignalsFromExternalZoneAreIgnored(t *testing.T) {
	r, e, _ := newTest(t, nil)
	ret := seedRetrieval(t, e, "ns", cands("evt_a"), []string{"evt_a"})
	res, err := r.Outcome(bg, OutcomeInput{Namespace: "ns", RetrievalID: ret, Value: 1, UsedIDs: []string{"evt_a"}, Actor: "web", ActorZone: core.ZoneExternal})
	if err != nil {
		t.Fatalf("external signal must not be an error: %v", err)
	}
	if res.ID != "" || len(res.Notices) != 1 || res.Notices[0] != NoticeExternalIgnored {
		t.Fatalf("want ignored notice, got %+v", res)
	}
	for _, q := range []string{`SELECT COUNT(*) FROM outcomes`, `SELECT COUNT(*) FROM utility`, `SELECT COUNT(*) FROM rank_state`, `SELECT COUNT(*) FROM audit_log`} {
		if n := countRows(t, e, q); n != 0 {
			t.Fatalf("%s = %d after an external signal", q, n)
		}
	}
}

// BASE.md Stage 1: used with success +1 positive, used with failure +1
// negative, delivered and not used +0.25 negative; centred before counting.
func TestStage1UtilityRules(t *testing.T) {
	tests := []struct {
		name                   string
		priorMean              float64
		value                  float64
		id                     string
		wantAlpha, wantBeta    float64
		wantUsed, wantSuccess  int64
		wantLastUsed, credited bool
	}{
		{"used with success adds 1 to alpha", 0, 1, "evt_a", 2, 1, 1, 1, true, true},
		{"used with failure adds 1 to beta", 0, -1, "evt_a", 1, 2, 1, 0, true, true},
		{"delivered not used adds unused penalty to beta", 0, 1, "evt_b", 1, 1.25, 0, 0, false, false},
		{"easy task below the moving average counts as failure", 0.8, 0.5, "evt_a", 1, 2, 1, 0, true, true},
		{"hard task above the moving average counts as success", -0.6, -0.2, "evt_a", 2, 1, 1, 1, true, true},
		{"value equal to the average moves neither side", 0.5, 0.5, "evt_a", 1, 1, 1, 0, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, e, _ := newTest(t, nil)
			if tc.priorMean != 0 {
				if err := saveState(bg, e.Store.DB(), "ns", rankState{Enabled: true, Mean: tc.priorMean, N: 10}, "x"); err != nil {
					t.Fatal(err)
				}
			}
			ret := seedRetrieval(t, e, "ns", cands("evt_a", "evt_b"), []string{"evt_a", "evt_b"})
			res := mustOutcome(t, r, "ns", ret, tc.value, "evt_a")
			if want := tc.value - tc.priorMean; res.Centred != want {
				t.Fatalf("centred %v want %v", res.Centred, want)
			}
			u := utilRow(t, e, tc.id)
			if u == nil {
				t.Fatal("no utility row")
			}
			if u.Alpha != tc.wantAlpha || u.Beta != tc.wantBeta || u.NUsed != tc.wantUsed || u.NSuccess != tc.wantSuccess || u.NShown != 1 {
				t.Fatalf("row %+v", u)
			}
			if u.Score != u.Alpha/(u.Alpha+u.Beta) {
				t.Fatalf("score %v", u.Score)
			}
			if (u.LastUsedAt != "") != tc.wantLastUsed {
				t.Fatalf("last_used_at %q", u.LastUsedAt)
			}
			st, _ := loadState(bg, e.Store.DB(), "ns")
			wantMean := tc.priorMean + 0.1*(tc.value-tc.priorMean)
			if st.Mean != wantMean {
				t.Fatalf("moving average %v want %v", st.Mean, wantMean)
			}
		})
	}
}

func TestOnlyDeliveredUsedIDsGetCredit(t *testing.T) {
	r, e, _ := newTest(t, nil)
	ret := seedRetrieval(t, e, "ns", cands("evt_a", "evt_b", "evt_c"), []string{"evt_a", "evt_b"})
	res := mustOutcome(t, r, "ns", ret, 1, "evt_b", "evt_c", "fct_zz", "evt_b")
	if len(res.Credited) != 1 || res.Credited[0] != "evt_b" {
		t.Fatalf("credited %v", res.Credited)
	}
	if len(res.Ignored) != 2 || res.Ignored[0] != "evt_c" || res.Ignored[1] != "fct_zz" {
		t.Fatalf("ignored %v", res.Ignored)
	}
	if len(res.Notices) != 1 {
		t.Fatalf("notices %v", res.Notices)
	}
	// A candidate that was not delivered gets nothing, not even exposure.
	if utilRow(t, e, "evt_c") != nil || utilRow(t, e, "fct_zz") != nil {
		t.Fatal("undelivered ids must not get a utility row")
	}
	if u := utilRow(t, e, "evt_a"); u.Beta != 1.25 || u.NShown != 1 {
		t.Fatalf("delivered-not-used row %+v", u)
	}
}

func TestOutcomeRateLimitPerNamespace(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 2 })
	var rets []string
	for i := 0; i < 4; i++ {
		rets = append(rets, seedRetrieval(t, e, "ns", cands("evt_a"), []string{"evt_a"}))
	}
	other := seedRetrieval(t, e, "other", cands("evt_x"), []string{"evt_x"})
	mustOutcome(t, r, "ns", rets[0], 1)
	mustOutcome(t, r, "ns", rets[1], 1)
	_, err := r.Outcome(bg, OutcomeInput{Namespace: "ns", RetrievalID: rets[2], Value: 1, Actor: "agent", ActorZone: core.ZoneUser})
	if core.CodeOf(err) != core.CodeRateLimited {
		t.Fatalf("third outcome in a minute: %v", err)
	}
	mustOutcome(t, r, "other", other, 1) // other namespaces are unaffected
	fc.Advance(time.Minute)
	mustOutcome(t, r, "ns", rets[2], 1)
}

func TestOneOutcomePerRetrieval(t *testing.T) {
	r, e, _ := newTest(t, nil)
	ret := seedRetrieval(t, e, "ns", cands("evt_a"), []string{"evt_a"})
	mustOutcome(t, r, "ns", ret, 1, "evt_a")
	_, err := r.Outcome(bg, OutcomeInput{Namespace: "ns", RetrievalID: ret, Value: 1, UsedIDs: []string{"evt_a"}, Actor: "agent", ActorZone: core.ZoneUser})
	if core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("repeat outcome: %v", err)
	}
	if u := utilRow(t, e, "evt_a"); u.Alpha != 2 {
		t.Fatalf("repeat changed utility: %+v", u)
	}
}

func TestOutcomeIsAudited(t *testing.T) {
	r, e, _ := newTest(t, nil)
	ret := seedRetrieval(t, e, "ns", cands("evt_a"), []string{"evt_a"})
	res := mustOutcome(t, r, "ns", ret, 0.5, "evt_a")
	if res.AuditID == 0 {
		t.Fatal("no audit id")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ? AND target = ?`, audit.ActOutcome, res.ID); n != 1 {
		t.Fatalf("audit rows %d", n)
	}
	if v, err := audit.Verify(bg, e.Store.DB()); err != nil || !v.OK {
		t.Fatalf("verify %+v %v", v, err)
	}
}
