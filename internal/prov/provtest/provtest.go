// Package provtest seeds the tables prov and forget read, with direct SQL,
// for tests. It never goes through the write path (owned by another area).
package provtest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"

	"github.com/felipemarts/chanda/internal/env"
)

// T is the fixed timestamp used for created_at columns.
const T = "2026-01-01T00:00:00.000000000Z"

// Seeder writes rows into one test database.
type Seeder struct {
	t  testing.TB
	e  *env.Env
	NS string
}

// New returns a Seeder for namespace ns.
func New(t testing.TB, e *env.Env, ns string) *Seeder { return &Seeder{t: t, e: e, NS: ns} }

// Exec runs one statement and fails the test on error.
func (s *Seeder) Exec(q string, args ...any) {
	s.t.Helper()
	if _, err := s.e.Store.DB().Exec(q, args...); err != nil {
		s.t.Fatalf("%s: %v", q, err)
	}
}

// Source inserts a source.
func (s *Seeder) Source(id, kind, uri, zone string) {
	s.t.Helper()
	s.Exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES(?,?,?,?,?,?)`, id, s.NS, kind, uri, zone, T)
}

// Document inserts a document at version 1.
func (s *Seeder) Document(id, sourceID, uri string) {
	s.t.Helper()
	s.Exec(`INSERT INTO documents(id,namespace,source_id,uri,current_version,content_hash,updated_at) VALUES(?,?,?,?,1,'h',?)`, id, s.NS, sourceID, uri, T)
}

// Event inserts an event; docID may be "" (no document). recordedAt "" means T.
func (s *Seeder) Event(id, sourceID, docID, zone, content, recordedAt string) {
	s.t.Helper()
	if recordedAt == "" {
		recordedAt = T
	}
	var doc any
	kind := "statement"
	if docID != "" {
		doc, kind = docID, "chunk"
	}
	h := sha256.Sum256([]byte(content))
	s.Exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,doc_id,doc_version,ordinal)
		VALUES(?,?,?,?,?,?,?,'tester',?,?,?,0)`, id, s.NS, sourceID, kind, content, hex.EncodeToString(h[:]), zone, recordedAt, doc, docVersion(docID))
	if docID != "" {
		var n int
		_ = s.e.Store.DB().QueryRow(`SELECT count(*) FROM document_chunks WHERE doc_id=?`, docID).Scan(&n)
		s.Exec(`INSERT INTO document_chunks(namespace,doc_id,version,ordinal,event_id) VALUES(?,?,1,?,?)`, s.NS, docID, n, id)
	}
}

func docVersion(docID string) any {
	if docID == "" {
		return nil
	}
	return 1
}

// Entity inserts an entity and its normalized alias.
func (s *Seeder) Entity(id, name, alias string) {
	s.t.Helper()
	s.Exec(`INSERT INTO entities(id,namespace,canonical_name,created_at) VALUES(?,?,?,?)`, id, s.NS, name, T)
	s.Exec(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,?)`, s.NS, alias, id)
}

// Fact inserts an active multi-valued fact.
func (s *Seeder) Fact(id, subject, predicate, value, zone, text string) {
	s.t.Helper()
	s.Exec(`INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES(?,?,?,?,?,?,?,?,'active',0.5,?,0,?)`, id, s.NS, subject, predicate, value, value, T, T, zone, text)
}

// Page inserts an active page.
func (s *Seeder) Page(id, title, body, zone string) {
	s.t.Helper()
	s.Exec(`INSERT INTO pages(id,namespace,kind,title,body,version,status,trust_zone,created_at,updated_at)
		VALUES(?,?,'summary',?,?,1,'active',?,?,?)`, id, s.NS, title, body, zone, T, T)
}

// Item inserts an active page item.
func (s *Seeder) Item(id, pageID string, ordinal int, text, zone string) {
	s.t.Helper()
	s.Exec(`INSERT INTO page_items(id,namespace,page_id,ordinal,text,status,trust_zone,created_at)
		VALUES(?,?,?,?,?,'active',?,?)`, id, s.NS, pageID, ordinal, text, zone, T)
}

// Edge inserts a provenance edge.
func (s *Seeder) Edge(dt, did, st, sid, rel string) {
	s.t.Helper()
	s.Exec(`INSERT INTO provenance(namespace,derived_type,derived_id,source_type,source_id,relation,created_at)
		VALUES(?,?,?,?,?,?,?)`, s.NS, dt, did, st, sid, rel, T)
}

// Embedding inserts a deterministic unit vector for a record (model "m", dim 8)
// derived from seed, and returns it.
func (s *Seeder) Embedding(rt, id, seed string) []float32 {
	s.t.Helper()
	v := Vector(seed, 8)
	s.Exec(`INSERT INTO embeddings(namespace,record_type,record_id,model_id,dim,vector) VALUES(?,?,?,'m',8,?)`, s.NS, rt, id, Blob(v))
	return v
}

// Vector returns a deterministic L2-normalized vector derived from seed.
func Vector(seed string, dim int) []float32 {
	h := sha256.Sum256([]byte(seed))
	v := make([]float32, dim)
	var n float64
	for i := range v {
		x := float64(int(h[i%32])-128) + float64(i)
		v[i] = float32(x)
		n += x * x
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// Blob encodes v as little-endian float32.
func Blob(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}
