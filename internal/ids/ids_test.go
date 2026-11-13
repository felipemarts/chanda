package ids_test

import (
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/ids"
)

func seq(step time.Duration) []string {
	g := ids.New(clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step), "seed")
	var out []string
	for i := 0; i < 50; i++ {
		out = append(out, g.For(core.RecEvent))
	}
	return out
}

func TestDeterministicAndMonotonic(t *testing.T) {
	for _, step := range []time.Duration{0, time.Millisecond} {
		a, b := seq(step), seq(step)
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("step %v: run differs at %d", step, i)
			}
			if i > 0 && a[i] <= a[i-1] {
				t.Fatalf("step %v: not monotonic at %d: %s <= %s", step, i, a[i], a[i-1])
			}
			if !strings.HasPrefix(a[i], "evt_") || core.RecordTypeOf(a[i]) != core.RecEvent {
				t.Fatalf("bad prefix %s", a[i])
			}
		}
	}
}
