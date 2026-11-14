package sleep

import (
	"context"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Policy decides when sleep runs on its own (BASE.md "When it runs").
type Policy struct {
	Idle     time.Duration // no write for at least this long
	Volume   int           // at least this many new events, or
	Interval time.Duration // at least this long since the last run
	Burst    time.Duration // a write this recent is a burst: never start
}

// PolicyFrom builds the policy from configuration.
func PolicyFrom(c config.SleepConfig) Policy {
	return Policy{
		Idle:     time.Duration(c.IdleMinutes) * time.Minute,
		Volume:   c.VolumeEvents,
		Interval: time.Duration(c.IntervalHours) * time.Hour,
		Burst:    time.Duration(c.BurstSeconds) * time.Second,
	}
}

// InBurst reports whether a write happened within the burst window.
func (p Policy) InBurst(now, lastWriteAt time.Time) bool {
	return p.Burst > 0 && !lastWriteAt.IsZero() && now.Sub(lastWriteAt) < p.Burst
}

// ShouldRun is the automatic trigger: idleness combined with volume or
// interval, and never during a write burst. A namespace that never ran
// meets the interval condition as soon as it has new events.
func (p Policy) ShouldRun(now, lastWriteAt time.Time, newEventsSinceLastRun int, lastRunAt time.Time) bool {
	if p.InBurst(now, lastWriteAt) {
		return false
	}
	if !lastWriteAt.IsZero() && now.Sub(lastWriteAt) < p.Idle {
		return false
	}
	volume := p.Volume > 0 && newEventsSinceLastRun >= p.Volume
	interval := false
	if lastRunAt.IsZero() {
		interval = newEventsSinceLastRun > 0
	} else {
		interval = now.Sub(lastRunAt) >= p.Interval
	}
	return volume || interval
}

// Tick checks every namespace once and runs sleep (both phases, default
// budget, actor "scheduler") where the policy says so. Namespaces are
// visited in order; a namespace whose run is refused with CodeConflict is
// skipped. It returns the results of the runs it started.
func (s *Sleeper) Tick(ctx context.Context) ([]*Result, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT DISTINCT namespace FROM events ORDER BY namespace`)
	if err != nil {
		return nil, err
	}
	var nss []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			rows.Close()
			return nil, err
		}
		nss = append(nss, ns)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	p := PolicyFrom(s.env.Cfg.Sleep)
	var out []*Result
	for _, ns := range nss {
		st, err := s.state(ctx, ns)
		if err != nil {
			return out, err
		}
		if !p.ShouldRun(s.env.Clock.Now(), st.LastWriteAt, st.NewEvents, st.LastRunAt) {
			continue
		}
		res, err := s.Run(ctx, RunInput{Namespace: ns, Actor: "scheduler"})
		if res != nil {
			out = append(out, res)
		}
		if err != nil && core.CodeOf(err) != core.CodeConflict {
			return out, err
		}
	}
	return out, nil
}

// Start runs the scheduler loop until ctx is done, calling Tick every
// sleep.check_every_minutes. The ticker only paces the checks; every
// decision uses the configured Clock. Errors go to Deps.Logf.
//
// On entry it recovers runs a previous process left "running"
// (RecoverStale). A run cancelled by ctx ends as failed, never "running".
func (s *Sleeper) Start(ctx context.Context) error {
	if ids, err := s.RecoverStale(ctx); err != nil && s.d.Logf != nil {
		s.d.Logf("sleep scheduler: recover stale runs: %v", err)
	} else if len(ids) > 0 && s.d.Logf != nil {
		s.d.Logf("sleep scheduler: recovered %d stale run(s) as failed", len(ids))
	}
	every := time.Duration(s.env.Cfg.Sleep.CheckEveryMinutes) * time.Minute
	if every <= 0 {
		every = 5 * time.Minute
	}
	ticks := s.d.Ticks
	if ticks == nil {
		t := time.NewTicker(every)
		defer t.Stop()
		ticks = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ticks:
			if !ok {
				return nil
			}
			if _, err := s.Tick(ctx); err != nil && s.d.Logf != nil && ctx.Err() == nil {
				s.d.Logf("sleep scheduler: %v", err)
			}
		}
	}
}
