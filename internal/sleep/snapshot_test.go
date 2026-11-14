package sleep

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// evalHook lets a test act in the middle of the gate.
type evalHook struct {
	n     int
	after func()
	score func(n int) float64
}

func (h *evalHook) Score(ctx context.Context, q store.Queryer, ns string) (float64, error) {
	h.n++
	if h.n == 2 && h.after != nil {
		h.after()
	}
	return h.score(h.n), nil
}

// forget may delete every snapshot file, also during a run. A missing
// snapshot must never crash sleep; the gate still discards the staged
// changes (it rolls back the transaction, it does not read the file).
func TestSnapshotDeletedDuringRunStillReverts(t *testing.T) {
	for _, regress := range []bool{true, false} {
		t.Run(fmt.Sprintf("regress=%v", regress), func(t *testing.T) {
			f := newFixture(t)
			f.event("evt_1", "internal", "Carl likes tea.")
			fake := script{extract: func(ids []string) string {
				return `{"facts":[` + factJSON([]string{"evt_1"}, "Carl", "likes", "tea", "") + `]}`
			}}.fake()
			dir := filepath.Join(filepath.Dir(f.e.Store.Path()), f.e.Cfg.Sleep.SnapshotDir)
			hook := &evalHook{
				after: func() { // what forget does to snapshot files
					entries, _ := os.ReadDir(dir)
					for _, e := range entries {
						os.Remove(filepath.Join(dir, e.Name()))
					}
				},
				score: func(n int) float64 {
					if n == 2 && regress {
						return 50
					}
					return 100
				},
			}
			s := New(f.e, Deps{LLM: fake, Facts: &fakeSink{f: f}, Evaluator: hook})
			res := f.run(s, RunInput{})
			want, facts := StatusCompleted, 1
			if regress {
				want, facts = StatusReverted, 0
			}
			if res.Status != want || f.count(`SELECT count(*) FROM facts`) != facts {
				t.Fatalf("status=%s facts=%d notices=%v", res.Status, f.count(`SELECT count(*) FROM facts`), res.Notices)
			}
			if res.Snapshot != "" || f.str(`SELECT snapshot FROM sleep_runs WHERE id=?`, res.RunID) != "" || !hasNotice(res, "snapshot file removed") {
				t.Fatalf("a deleted snapshot must not stay recorded: %q %v", res.Snapshot, res.Notices)
			}
			if regress && f.auditCount(audit.ActSleepRevert) != 1 {
				t.Fatal("revert must be audited")
			}
			f.verifyAudit()
		})
	}
}
