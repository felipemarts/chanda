package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedEvent(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_1','ns','user_statement','u','user','t')`)
	_, err := s.DB().ExecContext(ctx, `INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at)
		VALUES('evt_1','ns','src_1','statement','hello','h','user','a','t')`)
	if err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, s *store.Store, q string, args ...any) {
	t.Helper()
	if _, err := s.DB().Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestMigrationsApplyToEmptyDB(t *testing.T) {
	s := open(t)
	v, err := s.Version(context.Background())
	if err != nil || v < 1 {
		t.Fatalf("version=%d err=%v", v, err)
	}
	// Re-running is a no-op.
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEventContentUpdateRejected(t *testing.T) {
	s := open(t)
	seedEvent(t, s)
	for _, q := range []string{
		`UPDATE events SET content='changed' WHERE id='evt_1'`,
		`UPDATE events SET content_hash='x' WHERE id='evt_1'`,
		`UPDATE events SET trust_zone='external' WHERE id='evt_1'`,
	} {
		_, err := s.DB().Exec(q)
		if !store.IsImmutableViolation(err) {
			t.Errorf("%s: expected immutable violation, got %v", q, err)
		}
	}
	if _, err := s.DB().Exec(`UPDATE events SET retired_at='t2' WHERE id='evt_1'`); err != nil {
		t.Fatalf("retired_at must be updatable: %v", err)
	}
}

func TestFTSTriggersAndDiacritics(t *testing.T) {
	s := open(t)
	mustExec(t, s, `INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_1','ns','document','u','internal','t')`)
	mustExec(t, s, `INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at)
		VALUES('evt_1','ns','src_1','chunk','A ação de preço subiu','h','internal','a','t')`)
	var id string
	if err := s.DB().QueryRow(`SELECT record_id FROM events_fts WHERE events_fts MATCH 'acao'`).Scan(&id); err != nil || id != "evt_1" {
		t.Fatalf("fts lookup: id=%q err=%v", id, err)
	}
	mustExec(t, s, `DELETE FROM events WHERE id='evt_1'`)
	var n int
	s.DB().QueryRow(`SELECT count(*) FROM events_fts WHERE events_fts MATCH 'acao'`).Scan(&n)
	if n != 0 {
		t.Fatalf("fts entry survived delete")
	}
}

func TestSingleActiveFactUnique(t *testing.T) {
	s := open(t)
	ins := `INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES(?,'ns','ent_1','job_title',?,?,'t','t','active',0.5,'user',?,'x')`
	mustExec(t, s, ins, "fct_1", "a", "a", 1)
	if _, err := s.DB().Exec(ins, "fct_2", "b", "b", 1); err == nil {
		t.Fatal("second active single fact must be rejected")
	}
	mustExec(t, s, ins, "fct_3", "c", "c", 0) // multi coexists
}

func TestAuditLogAppendOnly(t *testing.T) {
	s := open(t)
	mustExec(t, s, `INSERT INTO audit_log(id,ts,namespace,actor,action,target,details,prev_hash,hash) VALUES(1,'t','ns','a','x','y','{}','0','h')`)
	for _, q := range []string{`UPDATE audit_log SET actor='b'`, `DELETE FROM audit_log`} {
		if _, err := s.DB().Exec(q); !store.IsImmutableViolation(err) {
			t.Errorf("%s: expected immutable violation, got %v", q, err)
		}
	}
}
