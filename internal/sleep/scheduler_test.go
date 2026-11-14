package sleep

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

// startLoop runs the scheduler loop driven by ticks and returns a function
// that cancels it and waits for it to return.
func startLoop(t *testing.T, s *Sleeper) (ctx context.Context, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	return ctx, func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Start: %v", err)
		}
	}
}

// BASE.md "When it runs", through the scheduler loop: each tick asks the
// policy with the configured Clock; idleness with new events starts a run,
// a tick right after does not, the interval elapsing starts the next.
func TestSchedulerLoopRunsByPolicyOnTicks(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "the deploy runs on Fridays")
	ticks := make(chan time.Time)
	fl := &fakeFlush{}
	s := New(f.e, Deps{Integrity: &fakeIntegrity{}, Embeddings: fl, Utility: fl, Ticks: ticks})
	_, stop := startLoop(t, s)

	f.advance(time.Hour) // idle, never ran, one new event
	ticks <- time.Time{}
	ticks <- time.Time{} // blocks until the first tick's run is done; nothing new since
	f.advance(25 * time.Hour)
	ticks <- time.Time{} // interval elapsed
	ticks <- time.Time{} // and again nothing to do
	stop()

	if n := f.count(`SELECT count(*) FROM sleep_runs WHERE status = ?`, StatusCompleted); n != 2 {
		t.Fatalf("completed runs %d, want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM sleep_runs WHERE inputs LIKE '%"actor":"scheduler"%'`); n != 2 {
		t.Fatalf("runs by the scheduler %d", n)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND actor = 'scheduler'`, audit.ActSleepStart); n != 2 {
		t.Fatalf("sleep_start audits %d", n)
	}
	f.verifyAudit()
}

// A run cancelled in the middle (App.Close cancels the scheduler and waits)
// ends as failed, audited, and never stays "running".
func TestCancelledRunNeverStaysRunning(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "Ana became CTO.")
	ticks := make(chan time.Time)
	fl := &fakeFlush{}
	var cancelLoop func()
	fake := &llm.Fake{Handler: func(core.LLMRequest) llm.FakeResponse {
		cancelLoop() // shutdown while the provider is answering
		return llm.FakeResponse{Err: context.Canceled}
	}}
	s := New(f.e, Deps{LLM: fake, Facts: &fakeSink{f: f}, Integrity: &fakeIntegrity{}, Embeddings: fl, Utility: fl,
		Evaluator: &fakeEval{}, Ticks: ticks})
	ctx, cancel := context.WithCancel(context.Background())
	cancelLoop = cancel
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	f.advance(time.Hour)
	ticks <- time.Time{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) == 0 {
		t.Fatal("fixture: the provider was never called")
	}
	if n := f.count(`SELECT count(*) FROM sleep_runs WHERE status = ?`, StatusRunning); n != 0 {
		t.Fatalf("%d runs left running", n)
	}
	if got := f.str(`SELECT status FROM sleep_runs`); got != StatusFailed {
		t.Fatalf("status %q, want failed", got)
	}
	if f.str(`SELECT ended_at FROM sleep_runs`) == "" || !strings.Contains(f.str(`SELECT report FROM sleep_runs`), "cancelled") {
		t.Fatal("run not closed with the cancellation in its report")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND details LIKE '%"status":"failed"%'`, audit.ActSleepEnd); n != 1 {
		t.Fatalf("sleep_end audits %d", n)
	}
	f.verifyAudit()
}

// A row a crashed process left "running" is recovered as failed, audited,
// once it is older than sleep.stale_run_minutes; a recent one is left alone.
func TestStaleRunningRowsRecoveredAsFailed(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "something to consolidate")
	at := func(d time.Duration) string { return core.FormatTime(f.clk.Now().Add(-d)) }
	f.exec(`INSERT INTO sleep_runs(id, namespace, started_at, phases, budget, status) VALUES
		('run_old', ?, ?, '[1,2]', '{}', 'running'), ('run_new', ?, ?, '[1,2]', '{}', 'running'),
		('run_done', ?, ?, '[1]', '{}', 'completed')`, ns, at(2*time.Hour), ns, at(10*time.Minute), ns, at(3*time.Hour))
	s := New(f.e, Deps{})
	ids, err := s.RecoverStale(f.ctx)
	if err != nil || len(ids) != 1 || ids[0] != "run_old" {
		t.Fatalf("recovered %v %v", ids, err)
	}
	for id, want := range map[string]string{"run_old": StatusFailed, "run_new": StatusRunning, "run_done": StatusCompleted} {
		if got := f.str(`SELECT status FROM sleep_runs WHERE id = ?`, id); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
	if f.str(`SELECT ended_at FROM sleep_runs WHERE id = 'run_old'`) == "" {
		t.Error("recovered run has no ended_at")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND target = 'run_old'`, audit.ActSleepRecover); n != 1 {
		t.Fatalf("sleep_recover audits %d", n)
	}

	// Startup (the scheduler loop) recovers the other once it is stale too.
	f.advance(2 * time.Hour)
	ticks := make(chan time.Time)
	s = New(f.e, Deps{Ticks: ticks})
	_, stop := startLoop(t, s)
	ticks <- time.Time{} // received after the startup recovery
	stop()
	if got := f.str(`SELECT status FROM sleep_runs WHERE id = 'run_new'`); got != StatusFailed {
		t.Fatalf("run_new: %s after startup", got)
	}
	// stale_run_minutes: 0 turns recovery off.
	f.e.Cfg.Sleep.StaleRunMinutes = 0
	f.exec(`INSERT INTO sleep_runs(id, namespace, started_at, phases, budget, status) VALUES ('run_x', ?, ?, '[1]', '{}', 'running')`, ns, at(48*time.Hour))
	if ids, err := s.RecoverStale(f.ctx); err != nil || len(ids) != 0 {
		t.Fatalf("disabled recovery: %v %v", ids, err)
	}
	f.verifyAudit()
}
