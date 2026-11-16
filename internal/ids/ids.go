// Package ids generates record ids "<prefix>_<ULID>" deterministically.
//
// The time part comes from the Clock; the entropy part comes from a SHA-256
// counter stream seeded by the configured seed. Two runs with the same
// Clock, seed and sequence of calls produce the same ids (principle 11).
// Ids are monotonic within a process.
package ids

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/felipemarts/chanda/internal/core"
)

type Generator struct {
	mu      sync.Mutex
	clock   core.Clock
	seed    []byte
	counter uint64
	last    ulid.ULID
}

func New(clock core.Clock, seed string) *Generator {
	return &Generator{clock: clock, seed: []byte(seed)}
}

func (g *Generator) entropy() [10]byte {
	g.counter++
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], g.counter)
	h := sha256.Sum256(append(append([]byte{}, g.seed...), b[:]...))
	var e [10]byte
	copy(e[:], h[:10])
	return e
}

// ULID returns the next raw ULID string.
func (g *Generator) ULID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := ulid.Timestamp(g.clock.Now())
	var id ulid.ULID
	if err := id.SetTime(ms); err != nil {
		panic(err)
	}
	if ms == g.last.Time() {
		// Monotonic: increment previous entropy so ids sort by creation order.
		id = g.last
		ent := id.Entropy()
		for i := len(ent) - 1; i >= 0; i-- {
			ent[i]++
			if ent[i] != 0 {
				break
			}
		}
		_ = id.SetEntropy(ent)
	} else {
		e := g.entropy()
		_ = id.SetEntropy(e[:])
	}
	g.last = id
	return id.String()
}

// New returns "<prefix>_<ULID>".
func (g *Generator) New(prefix string) string { return prefix + "_" + g.ULID() }

// For returns a new id for a record type.
func (g *Generator) For(t core.RecordType) string { return g.New(core.IDPrefix(t)) }
