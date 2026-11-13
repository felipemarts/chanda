// Package clock provides the real and the test Clock implementations.
package clock

import (
	"sync"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// System is the wall clock, truncated to microseconds for stable storage.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Fake is a deterministic clock. Each Now() returns the current instant and
// then advances it by Step (zero Step means frozen). Safe for concurrent use.
type Fake struct {
	mu   sync.Mutex
	t    time.Time
	Step time.Duration
}

// NewFake returns a clock starting at start that advances by step per call.
func NewFake(start time.Time, step time.Duration) *Fake {
	return &Fake{t: start.UTC(), Step: step}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.t
	f.t = f.t.Add(f.Step)
	return t
}

// Set moves the clock to t.
func (f *Fake) Set(t time.Time) { f.mu.Lock(); f.t = t.UTC(); f.mu.Unlock() }

// Advance moves the clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.mu.Lock(); f.t = f.t.Add(d); f.mu.Unlock() }

var _ core.Clock = System{}
var _ core.Clock = (*Fake)(nil)
