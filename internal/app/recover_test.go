package app

import (
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// App.Start recovers a run a crashed process left "running", also when
// the scheduler is off (sleep on demand only).
func TestStartRecoversStaleRunningRuns(t *testing.T) {
	for _, scheduler := range []bool{false, true} {
		a, fc := newApp(t, func(c *config.Config) { c.Sleep.Scheduler = scheduler })
		started := core.FormatTime(fc.Now().Add(-3 * time.Hour))
		if _, err := a.e.Store.DB().Exec(`INSERT INTO sleep_runs(id, namespace, started_at, phases, budget, status)
			VALUES ('run_crashed', 'ns', ?, '[1,2]', '{}', 'running')`, started); err != nil {
			t.Fatal(err)
		}
		a.Start(bg)
		for i := 0; i < 1000 && str(t, a, `SELECT status FROM sleep_runs WHERE id = 'run_crashed'`) == "running"; i++ {
			time.Sleep(5 * time.Millisecond) // the scheduler recovers in its own goroutine
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if got := str(t, a, `SELECT status FROM sleep_runs WHERE id = 'run_crashed'`); got != "failed" {
			t.Fatalf("scheduler %v: status %s", scheduler, got)
		}
		if n := count(t, a, `SELECT count(*) FROM audit_log WHERE action = 'sleep_recover'`); n != 1 {
			t.Fatalf("scheduler %v: sleep_recover audits %d", scheduler, n)
		}
		verifyOK(t, a)
	}
}
