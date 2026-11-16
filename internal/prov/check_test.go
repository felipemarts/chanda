package prov_test

import (
	"context"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/prov/provtest"
)

// BASE.md "Integrity rules": every fact, page and vector has at least one path
// in provenance to an event; the zone of a derived record is the lowest among
// its origins.
func TestCheckIntegrityRules(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *provtest.Seeder)
		kinds []string // expected violation kinds, in report order
	}{
		{"clean graph passes", func(s *provtest.Seeder) {}, nil},
		{"every fact reaches an event", func(s *provtest.Seeder) {
			s.Fact("fct_9", "ent_1", "p", "v", "external", "t")
		}, []string{prov.VNoOrigin}},
		{"every page reaches an event through its items", func(s *provtest.Seeder) {
			s.Page("pg_9", "t", "b", "external")
		}, []string{prov.VNoOrigin}},
		{"every vector reaches an event through its record", func(s *provtest.Seeder) {
			s.Fact("fct_9", "ent_1", "p", "v", "external", "t")
			s.Embedding("fact", "fct_9", "x")
		}, []string{prov.VEmbeddingNoOrigin, prov.VNoOrigin}},
		{"vector of a missing record", func(s *provtest.Seeder) {
			s.Embedding("fact", "fct_gone", "x")
		}, []string{prov.VOrphanEmbedding}},
		{"edge to a missing event", func(s *provtest.Seeder) {
			s.Edge("fact", "fct_1", "event", "evt_gone", "derived_from")
		}, []string{prov.VDanglingEdge}},
		{"zone never rises on its own", func(s *provtest.Seeder) {
			s.Exec(`UPDATE facts SET trust_zone='user' WHERE id='fct_6'`)
		}, []string{prov.VZoneAboveOrigins}},
		{"a confirmation from a lower zone does not lower the zone", func(s *provtest.Seeder) {
			// fct_2 is derived from a user event and confirmed by an external one.
			s.Exec(`UPDATE facts SET trust_zone='user' WHERE id='fct_2'`)
		}, nil},
		{"page zone never rises on its own", func(s *provtest.Seeder) {
			s.Exec(`UPDATE pages SET trust_zone='user' WHERE id='pg_1'`)
		}, []string{prov.VZoneAboveOrigins}},
		{"a promotion allows the raised zone", func(s *provtest.Seeder) {
			s.Exec(`UPDATE facts SET trust_zone='user' WHERE id='fct_6'`)
			s.Exec(`INSERT INTO promotions(id,namespace,record_type,record_id,from_zone,to_zone,actor,audit_id,ts)
				VALUES('prm_1','ns','fact','fct_6','internal','user','human',1,?)`, provtest.T)
		}, nil},
		{"a promotion does not allow more than it granted", func(s *provtest.Seeder) {
			s.Edge("fact", "fct_3", "event", "evt_3", "derived_from")
			s.Exec(`UPDATE facts SET trust_zone='user' WHERE id='fct_3'`)
			s.Exec(`INSERT INTO promotions(id,namespace,record_type,record_id,from_zone,to_zone,actor,audit_id,ts)
				VALUES('prm_1','ns','fact','fct_3','external','internal','human',1,?)`, provtest.T)
		}, []string{prov.VZoneAboveOrigins}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, s := graph(t)
			c.setup(s)
			rep, err := prov.Check(context.Background(), e.Store.DB(), "ns")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, v := range rep.Violations {
				got = append(got, v.Kind)
			}
			if len(got) != len(c.kinds) {
				t.Fatalf("violations = %+v, want kinds %v", rep.Violations, c.kinds)
			}
			for i := range got {
				if got[i] != c.kinds[i] {
					t.Fatalf("violations = %+v, want kinds %v", rep.Violations, c.kinds)
				}
			}
			if rep.OK != (len(c.kinds) == 0) {
				t.Fatalf("OK = %v", rep.OK)
			}
		})
	}
}

func TestCheckIsPerNamespace(t *testing.T) {
	e, _ := graph(t)
	other := provtest.New(t, e, "other")
	other.Fact("fct_o", "ent_x", "p", "v", "external", "t")
	ctx := context.Background()
	if rep, _ := prov.Check(ctx, e.Store.DB(), "ns"); !rep.OK {
		t.Fatalf("ns should be clean: %+v", rep.Violations)
	}
	if rep, _ := prov.Check(ctx, e.Store.DB(), "other"); rep.OK {
		t.Fatal("other should report the orphan fact")
	}
}

// BASE.md: "The verify tool checks the chain and the integrity of the graph."
func TestVerifyCombinesAuditAndGraph(t *testing.T) {
	e, s := graph(t)
	ctx := context.Background()
	e.Audit.Append(ctx, e.Store.DB(), audit.Entry{Namespace: "ns", Actor: "a", Action: audit.ActWrite, Target: "evt_1"})
	r, err := prov.Verify(ctx, e)
	if err != nil || !r.OK || r.Audit.Entries != 1 {
		t.Fatalf("verify = %+v %v", r, err)
	}
	s.Fact("fct_9", "ent_1", "p", "v", "external", "t")
	if r, _ := prov.Verify(ctx, e); r.OK || r.Graph.OK || !r.Audit.OK {
		t.Fatalf("verify must fail on the graph only: %+v", r)
	}
}
