package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// splitmix is a tiny deterministic generator for test vectors (no math/rand).
type splitmix uint64

func (s *splitmix) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// unit returns a deterministic unit vector of dim d.
func (s *splitmix) unit(d int) []float32 {
	v := make([]float32, d)
	for i := range v {
		v[i] = float32(int64(s.next()>>11)-(1<<52)) / (1 << 52)
	}
	return Normalized(v)
}

func axis(d, i int) []float32 {
	v := make([]float32, d)
	v[i] = 1
	return v
}

func mix(a, b []float32, wa, wb float32) []float32 {
	v := make([]float32, len(a))
	for i := range v {
		v[i] = wa*a[i] + wb*b[i]
	}
	return Normalized(v)
}

// seed helpers write the tables this package reads with direct SQL (the
// write path is another package).

func seedSource(t testing.TB, e *env.Env, ns string) string {
	t.Helper()
	id := e.IDs.For(core.RecSource)
	_, err := e.Store.DB().Exec(`INSERT INTO sources(id, namespace, kind, uri, trust_zone, created_at) VALUES (?,?,?,?,?,?)`,
		id, ns, "user_statement", "test:"+id, "user", core.FormatTime(e.Clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedEvent(t testing.TB, e *env.Env, ns, src, content string) string {
	t.Helper()
	id := e.IDs.For(core.RecEvent)
	h := sha256.Sum256([]byte(content))
	_, err := e.Store.DB().Exec(`INSERT INTO events(id, namespace, source_id, kind, content, content_hash, trust_zone, actor, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, id, ns, src, "statement", content, hex.EncodeToString(h[:]), "user", "test", core.FormatTime(e.Clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedFact(t testing.TB, e *env.Env, ns, text string) string {
	t.Helper()
	id := e.IDs.For(core.RecFact)
	now := core.FormatTime(e.Clock.Now())
	_, err := e.Store.DB().Exec(`INSERT INTO facts(id, namespace, subject, predicate, value, value_norm, valid_from, recorded_at, status, confidence, trust_zone, is_single, text)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, ns, "ent_x", "p", "v", "v", now, now, "active", 0.5, "user", 0, text)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedPage(t testing.TB, e *env.Env, ns, title, body, status string) string {
	t.Helper()
	id := e.IDs.For(core.RecPage)
	now := core.FormatTime(e.Clock.Now())
	_, err := e.Store.DB().Exec(`INSERT INTO pages(id, namespace, kind, title, body, version, status, trust_zone, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, id, ns, "summary", title, body, 1, status, "user", now, now)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func enqueue(t testing.TB, e *env.Env, ns string, rt core.RecordType, id string) {
	t.Helper()
	_, err := e.Store.DB().Exec(`INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at) VALUES (?,?,?,?)`,
		ns, string(rt), id, core.FormatTime(e.Clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
}

func verifyAudit(t testing.TB, e *env.Env) {
	t.Helper()
	r, err := audit.Verify(context.Background(), e.Store.DB())
	if err != nil || !r.OK {
		t.Fatalf("audit chain: %+v %v", r, err)
	}
}

func countRows(t testing.TB, e *env.Env, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ids(hs []core.VectorHit) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.RecordID
	}
	return out
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }
