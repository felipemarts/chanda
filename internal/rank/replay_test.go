package rank

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

// dumpUtility returns the learning state of ns as comparable strings.
func dumpUtility(t *testing.T, e *env.Env, ns string) []string {
	t.Helper()
	rows, err := e.Store.DB().QueryContext(bg, `SELECT `+utilityCols+` FROM utility WHERE namespace = ? ORDER BY record_type, record_id`, ns)
	if err != nil {
		t.Fatal(err)
	}
	us, err := scanURows(rows)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, u := range us {
		out = append(out, fmt.Sprintf("%+v", *u))
	}
	st, _ := loadState(bg, e.Store.DB(), ns)
	return append(out, fmt.Sprintf("mean=%v n=%d", st.Mean, st.N))
}

// playSignals runs a deterministic mix of outcomes and flags.
func playSignals(t *testing.T, r *Ranker, e *env.Env, fc interface{ Advance(time.Duration) }, ns string, n, salt int) {
	t.Helper()
	ids := []string{"evt_ns", "fct_ns", "pg_ns", "evt_a", "evt_b", "evt_c", "fct_d"}
	for i := 0; i < n; i++ {
		k := (i*7 + salt) % len(ids)
		delivered := []string{ids[k], ids[(k+1)%len(ids)], ids[(k+3)%len(ids)]}
		ret := seedRetrieval(t, e, ns, cands(delivered...), delivered)
		value := float64((i*13+salt)%21-10) / 10 // -1 .. 1
		used := []string{delivered[i%3]}
		if i%4 == 0 {
			used = append(used, "evt_not_delivered")
		}
		zone := core.ZoneInternal
		if i%9 == 5 {
			zone = core.ZoneExternal // ignored, must not count in either path
		}
		if _, err := r.Outcome(bg, OutcomeInput{Namespace: ns, RetrievalID: ret, Value: value, UsedIDs: used, Actor: "agent", ActorZone: zone}); err != nil {
			t.Fatal(err)
		}
		if i%11 == 3 {
			if _, err := r.Flag(bg, ns, ids[i%3], "outdated", "user"); err != nil {
				t.Fatal(err)
			}
		}
		fc.Advance(time.Second)
	}
}

func TestRecomputeUtilityEqualsIncrementalUpdates(t *testing.T) {
	r, e, fc := newTest(t, func(c *config.RankingConfig) { c.OutcomeRatePerMinute = 0 })
	seedRecords(t, e, "ns")
	playSignals(t, r, e, fc, "ns", 60, 1)
	incremental := dumpUtility(t, e, "ns")
	if len(incremental) < 5 {
		t.Fatalf("scenario too small: %v", incremental)
	}
	res, err := r.RecomputeUtility(bg, "ns")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes == 0 || res.Flags == 0 {
		t.Fatalf("recompute saw %+v", res)
	}
	if got := dumpUtility(t, e, "ns"); !reflect.DeepEqual(got, incremental) {
		t.Fatalf("recompute differs from incremental:\n%v\n%v", got, incremental)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ? AND target = 'ns'`, audit.ActRankAdjust); n != 1 {
		t.Fatal("recompute not audited")
	}

	// After a reset, the rebuild ignores everything up to the watermark.
	if _, err := r.Reset(bg, "ns", "admin"); err != nil {
		t.Fatal(err)
	}
	fc.Advance(time.Second)
	playSignals(t, r, e, fc, "ns", 25, 4)
	incremental = dumpUtility(t, e, "ns")
	if _, err := r.RecomputeUtility(bg, "ns"); err != nil {
		t.Fatal(err)
	}
	if got := dumpUtility(t, e, "ns"); !reflect.DeepEqual(got, incremental) {
		t.Fatalf("recompute after reset differs:\n%v\n%v", got, incremental)
	}
}
