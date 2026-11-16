package rank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/clock"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

// hash01 is a deterministic pseudo-value in [0,1) for test data (no math/rand).
func hash01(parts ...any) float64 {
	h := sha256.Sum256([]byte(fmt.Sprint(parts...)))
	return float64(binary.BigEndian.Uint64(h[:8])>>11) / (1 << 53)
}

// world is a synthetic namespace: a few records are consistently useful,
// the rest never help, and relevance is unrelated to usefulness.
type world struct {
	useful, useless []string
}

func newWorld() world {
	var w world
	for i := 0; i < 4; i++ {
		w.useful = append(w.useful, fmt.Sprintf("evt_u%02d", i))
	}
	for i := 0; i < 16; i++ {
		w.useless = append(w.useless, fmt.Sprintf("evt_n%02d", i))
	}
	return w
}

func (w world) isUseful(id string) bool { return id[4] == 'u' }

// query returns the candidates of synthetic query q, ordered by relevance
// (the pre-learning order). One useful record and four useless ones compete.
func (w world) query(q int) []Candidate {
	ids := []string{w.useful[q%len(w.useful)]}
	for k := 0; k < 4; k++ {
		ids = append(ids, w.useless[(q*3+k*5)%len(w.useless)])
	}
	out := make([]Candidate, len(ids))
	for i, id := range ids {
		rel := hash01("rel", q, id)
		out[i] = Candidate{ID: id, Type: core.RecEvent, Zone: core.ZoneInternal, BM25: 10 * rel, Cosine: hash01("cos", q, id),
			RRF: rel, Final: rel, AgeSeconds: 3600 * float64(1+i)}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RRF > out[j].RRF || (out[i].RRF == out[j].RRF && out[i].ID < out[j].ID)
	})
	return out
}

// play logs n retrievals of queries [from, from+n) with outcomes: the agent
// uses a useful record when delivered (task succeeds), otherwise it uses the
// top record and the task fails. Only the top 3 by relevance are delivered.
func (w world) play(t *testing.T, r *Ranker, e *env.Env, fc *clock.Fake, ns string, from, n int) {
	t.Helper()
	for q := from; q < from+n; q++ {
		cs := w.query(q)
		delivered := []string{cs[0].ID, cs[1].ID, cs[2].ID}
		if q%2 == 0 { // half of the time the useful record is delivered regardless of relevance
			for _, c := range cs[2:] {
				if w.isUseful(c.ID) {
					delivered[2] = c.ID
				}
			}
		}
		ret := seedRetrieval(t, e, ns, cs, delivered)
		value, used := -1.0, []string{delivered[0]}
		for _, id := range delivered {
			if w.isUseful(id) {
				value, used = 1, []string{id}
			}
		}
		mustOutcome(t, r, ns, ret, value, used...)
		fc.Advance(time.Minute)
	}
}
