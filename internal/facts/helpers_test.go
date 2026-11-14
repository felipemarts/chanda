package facts

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

const testNS = "ns"

type fixture struct {
	t   *testing.T
	e   *env.Env
	clk *clock.Fake
	s   *Service
	ctx context.Context
	n   int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	e, clk := env.NewTest(t)
	f := &fixture{t: t, e: e, clk: clk, s: New(e), ctx: context.Background()}
	for _, z := range []core.Zone{core.ZoneExternal, core.ZoneInternal, core.ZoneUser} {
		f.exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES(?,?,?,?,?,?)`,
			"src_"+string(z), testNS, map[core.Zone]string{core.ZoneExternal: "web", core.ZoneInternal: "document", core.ZoneUser: "user_statement"}[z],
			"uri:"+string(z), string(z), "t")
	}
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.e.Store.DB().Exec(q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

// event seeds a raw event in zone z and returns its id.
func (f *fixture) event(z core.Zone) string {
	f.t.Helper()
	f.n++
	id := fmt.Sprintf("evt_%04d", f.n)
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, id, testNS, "src_"+string(z), "fact_assertion", "c"+id, "h"+id, string(z), "test",
		core.FormatTime(f.e.Clock.Now()))
	return id
}

func (f *fixture) tx(fn func(tx *sql.Tx) error) {
	f.t.Helper()
	if err := f.e.Store.Tx(f.ctx, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.e.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (f *fixture) verifyAudit() {
	f.t.Helper()
	r, err := audit.Verify(f.ctx, f.e.Store.DB())
	if err != nil || !r.OK {
		f.t.Fatalf("audit chain: %+v %v", r, err)
	}
}
