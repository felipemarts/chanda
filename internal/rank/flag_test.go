package rank

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

// seedRecords inserts one event, two facts (active, superseded) and a page in ns.
func seedRecords(t *testing.T, e *env.Env, ns string) {
	t.Helper()
	ts := core.FormatTime(env.TestEpoch)
	for _, q := range []string{
		`INSERT INTO sources(id, namespace, kind, uri, trust_zone, created_at) VALUES ('src_` + ns + `','` + ns + `','session','s','internal','` + ts + `')`,
		`INSERT INTO events(id, namespace, source_id, kind, content, content_hash, trust_zone, actor, recorded_at) VALUES ('evt_` + ns + `','` + ns + `','src_` + ns + `','turn','c','h','internal','a','` + ts + `')`,
		`INSERT INTO facts(id, namespace, subject, predicate, value, value_norm, valid_from, recorded_at, status, confidence, trust_zone, is_single, text)
		 VALUES ('fct_` + ns + `','` + ns + `','ent_1','owner','x','x','` + ts + `','` + ts + `','active',0.5,'internal',1,'t')`,
		`INSERT INTO facts(id, namespace, subject, predicate, value, value_norm, valid_from, recorded_at, status, confidence, trust_zone, is_single, text)
		 VALUES ('fct_old` + ns + `','` + ns + `','ent_1','owner','y','y','` + ts + `','` + ts + `','superseded',0.5,'internal',1,'t')`,
		`INSERT INTO pages(id, namespace, kind, title, body, version, status, trust_zone, created_at, updated_at) VALUES ('pg_` + ns + `','` + ns + `','summary','t','b',1,'active','internal','` + ts + `','` + ts + `')`,
	} {
		if _, err := e.Store.DB().ExecContext(bg, q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFlagIsStrongNegativeAndRemovesFromReads(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		wantStatus string // facts.status after the flag; "" for non-facts
		wantResult string // FlagResult.FactStatus
	}{
		{"event: flags row only (raw log is immutable)", "evt_ns", "", ""},
		{"active fact becomes flagged", "fct_ns", "flagged", "flagged"},
		{"superseded fact keeps its status", "fct_oldns", "superseded", ""},
		{"page: flags row only", "pg_ns", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, e, _ := newTest(t, nil)
			seedRecords(t, e, "ns")
			res, err := r.Flag(bg, "ns", tc.id, "outdated", "user")
			if err != nil {
				t.Fatal(err)
			}
			if res.FactStatus != tc.wantResult {
				t.Fatalf("fact status in result %q", res.FactStatus)
			}
			if tc.wantStatus != "" {
				var st string
				e.Store.DB().QueryRowContext(bg, `SELECT status FROM facts WHERE id = ?`, tc.id).Scan(&st)
				if st != tc.wantStatus {
					t.Fatalf("status %q", st)
				}
			}
			u := utilRow(t, e, tc.id)
			if u == nil || u.Beta != 1+r.env.Cfg.Ranking.FlagPenalty || u.NShown != 0 {
				t.Fatalf("utility %+v", u)
			}
			if ok, _ := r.IsFlagged(bg, "ns", tc.id); !ok {
				t.Fatal("IsFlagged false")
			}
			if ok, _ := r.IsFlagged(bg, "other", tc.id); ok {
				t.Fatal("flag leaked to another namespace")
			}
			// The read path's filter excludes it; nothing was deleted.
			table := flaggable[core.RecordTypeOf(tc.id)]
			if n := countRows(t, e, `SELECT COUNT(*) FROM `+table+` t WHERE t.id = ? AND `+ExcludeFlaggedSQL("t.id"), tc.id); n != 0 {
				t.Fatal("ExcludeFlaggedSQL kept a flagged record")
			}
			if n := countRows(t, e, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, tc.id); n != 1 {
				t.Fatal("flag deleted the record")
			}
			if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = ? AND target = ?`, audit.ActFlag, tc.id); n != 1 {
				t.Fatal("flag not audited")
			}
		})
	}
}

func TestFlagValidation(t *testing.T) {
	r, e, _ := newTest(t, nil)
	seedRecords(t, e, "ns")
	tests := []struct {
		name, ns, id, reason, actor string
		code                        core.Code
	}{
		{"namespace required", "", "evt_ns", "r", "a", core.CodeInvalidArgument},
		{"reason required", "ns", "evt_ns", " ", "a", core.CodeInvalidArgument},
		{"actor required", "ns", "evt_ns", "r", "", core.CodeInvalidArgument},
		{"entity cannot be flagged", "ns", "ent_1", "r", "a", core.CodeInvalidArgument},
		{"unknown record", "ns", "evt_nope", "r", "a", core.CodeNotFound},
		{"record of another namespace", "other", "evt_ns", "r", "a", core.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.Flag(bg, tc.ns, tc.id, tc.reason, tc.actor); core.CodeOf(err) != tc.code {
				t.Fatalf("want %s got %v", tc.code, err)
			}
		})
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM flags`); n != 0 {
		t.Fatal("invalid flag stored")
	}
}

func TestFlaggedIDs(t *testing.T) {
	r, e, _ := newTest(t, nil)
	seedRecords(t, e, "ns")
	seedRecords(t, e, "ot")
	for _, id := range []string{"pg_ns", "evt_ns", "evt_ns"} {
		if _, err := r.Flag(bg, "ns", id, "wrong", "user"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Flag(bg, "ot", "evt_ot", "wrong", "user"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FlaggedIDs(bg, "ns", nil)
	if !reflect.DeepEqual(got, []string{"evt_ns", "pg_ns"}) {
		t.Fatalf("all: %v", got)
	}
	got, _ = r.FlaggedIDs(bg, "ns", []string{"pg_ns", "fct_ns", "evt_ot"})
	if !reflect.DeepEqual(got, []string{"pg_ns"}) {
		t.Fatalf("subset: %v", got)
	}
	if u := utilRow(t, e, "evt_ns"); u.Beta != 1+2*r.env.Cfg.Ranking.FlagPenalty {
		t.Fatalf("repeated flag adds a penalty each time: %+v", u)
	}
}
