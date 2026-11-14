package sleep

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

const ns = "ns"

type fixture struct {
	t   *testing.T
	e   *env.Env
	clk *clock.Fake
	ctx context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	e, clk := env.NewTest(t)
	e.Cfg.Namespace = ns
	f := &fixture{t: t, e: e, clk: clk, ctx: context.Background()}
	for _, z := range []string{"user", "internal", "external"} {
		f.exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES(?,?,?,?,?,?)`,
			"src_"+z, ns, map[string]string{"user": "user_statement", "internal": "session", "external": "web"}[z], "u-"+z, z, f.now())
	}
	return f
}

func (f *fixture) now() string { return core.FormatTime(f.clk.Now()) }

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.e.Store.DB().Exec(q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
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

func (f *fixture) str(q string, args ...any) string {
	f.t.Helper()
	var s sql.NullString
	if err := f.e.Store.DB().QueryRow(q, args...).Scan(&s); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

// event inserts a raw event in zone z ("user", "internal", "external").
func (f *fixture) event(id, z, content string) {
	f.t.Helper()
	kind := map[string]string{"user": "statement", "internal": "turn", "external": "web"}[z]
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, id, ns, "src_"+z, kind, content, "h-"+id, z, "agent", f.now())
}

func (f *fixture) entity(id, name, typ string, aliases ...string) {
	f.exec(`INSERT INTO entities(id,namespace,canonical_name,type,created_at) VALUES(?,?,?,?,?)`, id, ns, name, typ, f.now())
	for _, a := range append([]string{name}, aliases...) {
		f.exec(`INSERT OR IGNORE INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,?)`, ns, normalize(a), id)
	}
}

// fact inserts a fact with derived_from edges to origins.
func (f *fixture) fact(id, subject, pred, value, status, zone string, single bool, origins ...string) {
	f.t.Helper()
	s := 0
	if single {
		s = 1
	}
	f.exec(`INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, ns, subject, pred, value, normalize(value), f.now(), f.now(), status, 0.5, zone, s, pred+" "+value)
	for _, o := range origins {
		f.edge("fact", id, "event", o, core.RelDerivedFrom)
	}
}

func (f *fixture) edge(dt, did, st, sid, rel string) {
	f.exec(`INSERT INTO provenance(namespace,derived_type,derived_id,source_type,source_id,relation,created_at)
		VALUES(?,?,?,?,?,?,?)`, ns, dt, did, st, sid, rel, f.now())
}

func (f *fixture) advance(d time.Duration) { f.clk.Advance(d) }

func (f *fixture) verifyAudit() {
	f.t.Helper()
	res, err := audit.Verify(f.ctx, f.e.Store.DB())
	if err != nil || !res.OK {
		f.t.Fatalf("audit chain: %+v %v", res, err)
	}
}

func (f *fixture) auditCount(action string) int {
	return f.count(`SELECT count(*) FROM audit_log WHERE action=?`, action)
}

// ---------------------------------------------------------------------------
// Fakes for the consumer interfaces.

// fakeIntegrity counts facts, pages and page items with no direct edge to an event.
type fakeIntegrity struct{ calls int }

func (fi *fakeIntegrity) Check(ctx context.Context, q store.Queryer, ns string) (int, error) {
	fi.calls++
	var n int
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM facts f WHERE f.namespace=?1 AND NOT EXISTS
			(SELECT 1 FROM provenance p WHERE p.derived_type='fact' AND p.derived_id=f.id AND p.source_type='event' AND p.relation IN ('derived_from','confirmed_by','cites')))
		+ (SELECT count(*) FROM pages g WHERE g.namespace=?1 AND g.status<>'retired' AND NOT EXISTS
			(SELECT 1 FROM provenance p WHERE p.derived_type='page' AND p.derived_id=g.id AND p.source_type='event' AND p.relation IN ('derived_from','confirmed_by','cites')))
		+ (SELECT count(*) FROM page_items i WHERE i.namespace=?1 AND i.status<>'retired' AND NOT EXISTS
			(SELECT 1 FROM provenance p WHERE p.derived_type='page_item' AND p.derived_id=i.id AND p.source_type='event' AND p.relation IN ('derived_from','confirmed_by','cites')))`, ns).Scan(&n)
	return n, err
}

type fakeFlush struct{ embed, utility int }

func (ff *fakeFlush) Flush(ctx context.Context, ns string) error            { ff.embed++; return nil }
func (ff *fakeFlush) RecomputeUtility(ctx context.Context, ns string) error { ff.utility++; return nil }

// fakeSink is a minimal write path steps 8-10: resolve the entity by alias,
// confirm an identical active value, otherwise insert an active multi fact
// in the lowest origin zone with derived_from edges.
type fakeSink struct {
	f     *fixture
	calls int
}

func (fs *fakeSink) AssertCandidate(ctx context.Context, tx *sql.Tx, ns string, c Candidate) (string, error) {
	fs.calls++
	e := fs.f.e
	alias := normalize(c.Subject)
	var ent string
	err := tx.QueryRowContext(ctx, `SELECT entity_id FROM entity_aliases WHERE namespace=? AND alias_norm=?`, ns, alias).Scan(&ent)
	if err == sql.ErrNoRows {
		ent = e.IDs.For(core.RecEntity)
		now := core.FormatTime(e.Clock.Now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO entities(id,namespace,canonical_name,type,created_at) VALUES(?,?,?,?,?)`, ent, ns, c.Subject, c.SubjectType, now); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,?)`, ns, alias, ent); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM facts WHERE namespace=? AND subject=? AND predicate=? AND value_norm=? AND status='active'`,
		ns, ent, c.Predicate, normalize(c.Value)).Scan(&existing)
	if err == nil {
		return "confirmed", nil
	}
	var zones []core.Zone
	for _, o := range c.OriginEventIDs {
		var z string
		if err := tx.QueryRowContext(ctx, `SELECT trust_zone FROM events WHERE id=?`, o).Scan(&z); err != nil {
			return "", err
		}
		zones = append(zones, core.Zone(z))
	}
	id := e.IDs.For(core.RecFact)
	now := core.FormatTime(e.Clock.Now())
	vf := c.ValidFrom
	if vf == "" {
		vf = now
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES(?,?,?,?,?,?,?,?,'active',0.5,?,0,?)`, id, ns, ent, c.Predicate, c.Value, normalize(c.Value), vf, now,
		string(core.MinZone(zones...)), c.Subject+" "+c.Predicate+" "+c.Value); err != nil {
		return "", err
	}
	for _, o := range c.OriginEventIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO provenance(namespace,derived_type,derived_id,source_type,source_id,relation,created_at)
			VALUES(?,'fact',?,'event',?,'derived_from',?)`, ns, id, o, now); err != nil {
			return "", err
		}
	}
	_, err = e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: Actor, Action: audit.ActFactInsert, Target: id})
	return "inserted", err
}

// fakeEval scores 100 minus 10 points per active fact whose predicate is
// "harmful" (a planted regression), reading only through q.
type fakeEval struct{ calls int }

func (fe *fakeEval) Score(ctx context.Context, q store.Queryer, ns string) (float64, error) {
	fe.calls++
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM facts WHERE namespace=? AND status='active' AND predicate='harmful'`, ns).Scan(&n)
	return 100 - 10*float64(n), err
}

type fakeVectors map[string][]float32

func (fv fakeVectors) EntityVectors(ctx context.Context, ns string, ids []string) (map[string][]float32, error) {
	out := map[string][]float32{}
	for _, id := range ids {
		if v, ok := fv[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
