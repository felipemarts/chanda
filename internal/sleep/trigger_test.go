package sleep

import (
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

func TestSleepConfigDefaults(t *testing.T) {
	c := config.Default()
	if c.Sleep.IdleMinutes != 30 || c.Sleep.VolumeEvents != 200 || c.Sleep.IntervalHours != 24 ||
		c.Sleep.GateTolerance != 2 || c.Sleep.LessonMinEvents != 3 || c.Sleep.BatchEvents <= 0 ||
		c.Sleep.CheckEveryMinutes <= 0 || c.LLM.ResponseFormat != "auto" || c.LLM.MaxTokens <= 0 {
		t.Fatalf("sleep/llm defaults: %+v %+v", c.Sleep, c.LLM)
	}
}

// BASE.md "When it runs": on idleness combined with volume or interval;
// never during a write burst.
func TestWhenItRunsIdleAndVolumeOrInterval(t *testing.T) {
	p := PolicyFrom(config.Default().Sleep)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	var never time.Time
	cases := []struct {
		name      string
		lastWrite time.Time
		newEvents int
		lastRun   time.Time
		want      bool
	}{
		{"idle and volume", ago(31 * time.Minute), 200, ago(time.Hour), true},
		{"idle and interval", ago(31 * time.Minute), 1, ago(25 * time.Hour), true},
		{"idle, neither volume nor interval", ago(31 * time.Minute), 199, ago(23 * time.Hour), false},
		{"volume but not idle", ago(29 * time.Minute), 500, ago(48 * time.Hour), false},
		{"write burst", ago(time.Second), 500, ago(48 * time.Hour), false},
		{"never ran, idle with new events", ago(time.Hour), 1, never, true},
		{"never ran, nothing written", never, 0, never, false},
		{"no writes, interval elapsed", never, 0, ago(25 * time.Hour), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := p.ShouldRun(now, c.lastWrite, c.newEvents, c.lastRun); got != c.want {
				t.Fatalf("ShouldRun = %v, want %v", got, c.want)
			}
		})
	}
}

// BASE.md: "A run never starts during a write burst" — also on demand.
func TestRunNeverStartsDuringWriteBurst(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "user", "hello there")
	s := New(f.e, Deps{})
	if _, err := s.Run(f.ctx, RunInput{}); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("expected conflict during burst, got %v", err)
	}
	if n := f.count(`SELECT count(*) FROM sleep_runs`); n != 0 {
		t.Fatalf("no run row may be created during a burst, got %d", n)
	}
	f.advance(time.Minute)
	if _, err := s.Run(f.ctx, RunInput{}); err != nil {
		t.Fatal(err)
	}
}

func TestTickRunsWhenPolicySays(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "user", "hello there")
	s := New(f.e, Deps{})
	f.advance(10 * time.Minute)
	if res, err := s.Tick(f.ctx); err != nil || len(res) != 0 {
		t.Fatalf("not idle yet: %v %v", res, err)
	}
	f.advance(30 * time.Minute)
	res, err := s.Tick(f.ctx)
	if err != nil || len(res) != 1 || res[0].Namespace != ns {
		t.Fatalf("idle with new events must run: %v %v", res, err)
	}
	if res, err := s.Tick(f.ctx); err != nil || len(res) != 0 {
		t.Fatalf("no new events and interval not elapsed: %v %v", res, err)
	}
	f.advance(25 * time.Hour)
	if res, err := s.Tick(f.ctx); err != nil || len(res) != 1 {
		t.Fatalf("interval elapsed: %v %v", res, err)
	}
}

func TestRunRejectsUnknownPhaseAndNegativeBudget(t *testing.T) {
	f := newFixture(t)
	s := New(f.e, Deps{})
	if _, err := s.Run(f.ctx, RunInput{Phases: []int{3}}); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("phase 3: %v", err)
	}
	if _, err := s.Run(f.ctx, RunInput{Budget: Budget{Tokens: -1}}); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("negative budget: %v", err)
	}
}
