package read

import (
	"context"
	"testing"

	"github.com/felipemarts/chanda/internal/env"
)

// fx seeds the tables the read path reads with direct SQL, so the tests do
// not depend on the write path (being built in parallel).
type fx struct {
	t   testing.TB
	e   *env.Env
	ctx context.Context
}

func newFx(t testing.TB) *fx {
	e, _ := env.NewTest(t)
	return &fx{t: t, e: e, ctx: context.Background()}
}

const t0 = "2026-01-01T00:00:00.000000000Z"

func (f *fx) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.e.Store.DB().ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("seed %q: %v", q, err)
	}
}

func (f *fx) source(ns, id, kind, uri, zone string) {
	f.t.Helper()
	f.exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES (?,?,?,?,?,?)`, id, ns, kind, uri, zone, t0)
}

func (f *fx) doc(ns, id, src, uri string, version int) {
	f.t.Helper()
	f.exec(`INSERT INTO documents(id,namespace,source_id,uri,current_version,content_hash,updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, ns, src, uri, version, "h", t0)
}

type ev struct {
	ID, NS, Source, Kind, Content, Zone, Flags, Retired, RecordedAt, DocID, SessionID, Role string
	DocVersion, Ordinal                                                                     int
}

func (f *fx) event(e ev) {
	f.t.Helper()
	if e.Kind == "" {
		e.Kind = "chunk"
	}
	if e.Zone == "" {
		e.Zone = "internal"
	}
	if e.RecordedAt == "" {
		e.RecordedAt = t0
	}
	var doc, docV, ord, retired, sess, role any
	if e.DocID != "" {
		doc, docV, ord = e.DocID, e.DocVersion, e.Ordinal
	}
	if e.Retired != "" {
		retired = e.Retired
	}
	if e.SessionID != "" {
		sess, ord = e.SessionID, e.Ordinal
	}
	if e.Role != "" {
		role = e.Role
	}
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,doc_id,doc_version,ordinal,session_id,flags,retired_at,role)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.NS, e.Source, e.Kind, e.Content, "h-"+e.ID, e.Zone, "test", e.RecordedAt, doc, docV, ord, sess, e.Flags, retired, role)
}

type fc struct {
	ID, NS, Subject, Predicate, Value, Text, Status, Zone, ValidFrom, ValidTo, RecordedAt string
}

func (f *fx) fact(x fc) {
	f.t.Helper()
	if x.Status == "" {
		x.Status = "active"
	}
	if x.Zone == "" {
		x.Zone = "internal"
	}
	if x.ValidFrom == "" {
		x.ValidFrom = t0
	}
	if x.RecordedAt == "" {
		x.RecordedAt = t0
	}
	if x.Subject == "" {
		x.Subject = "ent_X"
	}
	if x.Predicate == "" {
		x.Predicate = "note"
	}
	var vt any
	if x.ValidTo != "" {
		vt = x.ValidTo
	}
	f.exec(`INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,valid_to,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.NS, x.Subject, x.Predicate, x.Value, x.Value, x.ValidFrom, vt, x.RecordedAt, x.Status, 0.5, x.Zone, 0, x.Text)
}

type pg struct{ ID, NS, Kind, Title, Body, Status, Zone, UpdatedAt string }

func (f *fx) page(x pg) {
	f.t.Helper()
	if x.Kind == "" {
		x.Kind = "summary"
	}
	if x.Status == "" {
		x.Status = "active"
	}
	if x.Zone == "" {
		x.Zone = "internal"
	}
	if x.UpdatedAt == "" {
		x.UpdatedAt = t0
	}
	f.exec(`INSERT INTO pages(id,namespace,kind,title,body,version,status,trust_zone,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.NS, x.Kind, x.Title, x.Body, 1, x.Status, x.Zone, t0, x.UpdatedAt)
}

func (f *fx) prov(ns, dt, did, st, sid, rel string) {
	f.t.Helper()
	f.exec(`INSERT INTO provenance(namespace,derived_type,derived_id,source_type,source_id,relation,created_at) VALUES (?,?,?,?,?,?,?)`,
		ns, dt, did, st, sid, rel, t0)
}

func (f *fx) chunk(ns, doc string, version, ordinal int, event string) {
	f.t.Helper()
	f.exec(`INSERT INTO document_chunks(namespace,doc_id,version,ordinal,event_id) VALUES (?,?,?,?,?)`, ns, doc, version, ordinal, event)
}

func (f *fx) flag(ns, rt, id string) {
	f.t.Helper()
	f.exec(`INSERT INTO flags(id,namespace,record_type,record_id,reason,actor,ts) VALUES (?,?,?,?,?,?,?)`, "flg_"+id, ns, rt, id, "wrong", "user", t0)
}

// basic seeds namespace ns with an internal document source, a user source
// and an external web source.
func (f *fx) basic(ns string) {
	f.t.Helper()
	f.source(ns, "src_doc_"+ns, "document", "file:///handbook-"+ns+".md", "internal")
	f.source(ns, "src_user_"+ns, "user_statement", "user:"+ns, "user")
	f.source(ns, "src_web_"+ns, "web", "https://example.com/"+ns, "external")
}
