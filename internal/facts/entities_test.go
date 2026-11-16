package facts

import (
	"database/sql"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
)

func TestResolveEntities_ExactAliasMatchOrCreate(t *testing.T) {
	f := newFixture(t)
	var first string
	cases := []struct {
		name        string
		wantCreated bool
		wantSame    bool
	}{
		{"José Silva", true, true},
		{"jose silva", false, true},
		{"JOSÉ   SILVA.", false, true},
		{"José-Silva", false, true},
		{"José Silvã", false, true}, // accents stripped
		{"José Silveira", true, false},
	}
	for _, c := range cases {
		f.tx(func(tx *sql.Tx) error {
			id, created, err := f.s.Resolve(f.ctx, tx, testNS, c.name, "person")
			if err != nil {
				return err
			}
			if created != c.wantCreated {
				t.Errorf("%q: created=%v want %v", c.name, created, c.wantCreated)
			}
			if first == "" {
				first = id
			}
			if (id == first) != c.wantSame {
				t.Errorf("%q: id=%s first=%s wantSame=%v", c.name, id, first, c.wantSame)
			}
			if core.RecordTypeOf(id) != core.RecEntity {
				t.Errorf("bad id %q", id)
			}
			return nil
		})
	}
	if n := f.count(`SELECT count(*) FROM entities`); n != 2 {
		t.Fatalf("entities=%d want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action='entity_create'`); n != 2 {
		t.Fatalf("entity_create audits=%d want 2", n)
	}
	var canon string
	f.e.Store.DB().QueryRow(`SELECT canonical_name FROM entities WHERE id=?`, first).Scan(&canon)
	if canon != "José Silva" {
		t.Fatalf("canonical=%q", canon)
	}
	f.verifyAudit()
}

func TestResolveEntities_NamespaceIsolationAndMerges(t *testing.T) {
	f := newFixture(t)
	var a, b, other string
	f.tx(func(tx *sql.Tx) (err error) {
		a, _, err = f.s.Resolve(f.ctx, tx, testNS, "Acme", "")
		if err != nil {
			return err
		}
		b, _, err = f.s.Resolve(f.ctx, tx, testNS, "Acme Corp", "")
		if err != nil {
			return err
		}
		other, _, err = f.s.Resolve(f.ctx, tx, "other", "Acme", "")
		return err
	})
	if other == a {
		t.Fatal("namespaces must not share entities")
	}
	f.exec(`UPDATE entities SET merged_into=? WHERE id=?`, a, b)
	got, ok, err := f.s.LookupEntity(f.ctx, f.e.Store.DB(), testNS, "ACME corp")
	if err != nil || !ok || got != a {
		t.Fatalf("merged lookup: %s %v %v want %s", got, ok, err, a)
	}
	got, ok, _ = f.s.LookupEntity(f.ctx, f.e.Store.DB(), testNS, b)
	if !ok || got != a {
		t.Fatalf("lookup by merged id: %s %v", got, ok)
	}
	if _, ok, _ := f.s.LookupEntity(f.ctx, f.e.Store.DB(), testNS, "Nobody"); ok {
		t.Fatal("lookup must not create")
	}
	f.tx(func(tx *sql.Tx) error {
		if _, _, err := f.s.Resolve(f.ctx, tx, testNS, " ?! ", ""); core.CodeOf(err) != core.CodeInvalidArgument {
			t.Errorf("empty alias: %v", err)
		}
		return nil
	})
}

func TestPredicateCatalog_SingleSeededUnknownIsMulti(t *testing.T) {
	f := newFixture(t)
	f.e.Cfg.Facts.SinglePredicates = []string{"Job Title", "owner", "job_title"}
	cases := []struct {
		name string
		want core.Cardinality
	}{
		{"skill", core.Multi},
		{"job_title", core.Single},
		{"owner", core.Single},
		{"skill", core.Multi},
	}
	for _, c := range cases {
		f.tx(func(tx *sql.Tx) error {
			got, err := f.s.Cardinality(f.ctx, tx, testNS, c.name, "test")
			if err != nil {
				return err
			}
			if got != c.want {
				t.Errorf("%s: %s want %s", c.name, got, c.want)
			}
			return nil
		})
	}
	// Seeded lazily, once per namespace: job_title, owner (singles) + skill.
	if n := f.count(`SELECT count(*) FROM predicates WHERE namespace=?`, testNS); n != 3 {
		t.Fatalf("predicates=%d want 3", n)
	}
	if n := f.count(`SELECT count(*) FROM predicates WHERE namespace='other'`); n != 0 {
		t.Fatalf("other namespace seeded early")
	}
	f.tx(func(tx *sql.Tx) error {
		if _, err := f.s.Cardinality(f.ctx, tx, testNS, "Job Title", "test"); core.CodeOf(err) != core.CodeInvalidArgument {
			t.Errorf("unnormalized predicate accepted: %v", err)
		}
		return nil
	})
	f.verifyAudit()
}
