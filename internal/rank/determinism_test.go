package rank

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/clock"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/env"
)

// RankScenario runs every mutating operation of this package. Integration should
// port it into internal/determinism's Scenario (test files are not importable).
func RankScenario(t *testing.T, e *env.Env) {
	t.Helper()
	r := New(e)
	seedRecords(t, e, "ns")
	w := newWorld()
	w.play(t, r, e, e.Clock.(*clock.Fake), "ns", 0, 70)
	for _, id := range []string{"evt_ns", "fct_ns", "pg_ns"} {
		if _, err := r.Flag(bg, "ns", id, "outdated", "user"); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{KindLogReg, KindMLP} {
		if _, err := r.Train(bg, "ns", kind, "sleep"); err != nil {
			t.Fatal(err)
		}
	}
	eval := gateEvaluator(t, r, w, "ns", []int{9000, 9001, 9002, 9003, 9004, 9005, 9006, 9007})
	for i := 0; i < 2; i++ {
		if _, err := r.Promote(bg, "ns", "eval", eval); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.RecomputeUtility(bg, "ns"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetEnabled(bg, "ns", false, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reset(bg, "ns", "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestRankDatabasesAreByteIdentical(t *testing.T) {
	run := func(dir string) []byte {
		path := filepath.Join(dir, "chanda.db")
		cfg := config.Default()
		cfg.Ranking.OutcomeRatePerMinute = 0
		e, _ := env.NewTestAt(t, path, cfg)
		RankScenario(t, e)
		if err := e.Store.Checkpoint(bg); err != nil {
			t.Fatal(err)
		}
		if err := e.Store.Close(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, b := run(t.TempDir()), run(t.TempDir())
	if !bytes.Equal(a, b) {
		t.Fatalf("databases differ: %x vs %x", sha256.Sum256(a), sha256.Sum256(b))
	}
}
