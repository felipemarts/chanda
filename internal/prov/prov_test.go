package prov_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/prov"
	"github.com/felipemarts/chanda-mcp/internal/prov/provtest"
)

// graph seeds: user event e1, internal event e2, external event e3.
// f1 <- e1 ; f2 <- e1, e3 (confirmed_by) ; f3 <- f2 (derived from a fact)
// page p1 with items i1 (cites e2) and i2 (cites f1) ; cycle f4 <-> f5 <- e2
// e1 mentions ent1 ; f6 supersedes f1 (non-origin edge only, plus e2).
func graph(t *testing.T) (*env.Env, *provtest.Seeder) {
	e, _ := env.NewTest(t)
	s := provtest.New(t, e, "ns")
	s.Source("src_u", "user_statement", "u", "user")
	s.Source("src_d", "document", "d", "internal")
	s.Source("src_w", "web", "w", "external")
	s.Event("evt_1", "src_u", "", "user", "alpha content", "")
	s.Event("evt_2", "src_d", "", "internal", "beta content", "")
	s.Event("evt_3", "src_w", "", "external", "gamma content", "")
	s.Entity("ent_1", "Acme", "acme")
	for _, f := range []string{"fct_1", "fct_2", "fct_3", "fct_4", "fct_5", "fct_6"} {
		s.Fact(f, "ent_1", "p", f, "external", f)
	}
	s.Page("pg_1", "title", "body", "internal")
	s.Item("pgi_1", "pg_1", 0, "item one", "internal")
	s.Item("pgi_2", "pg_1", 1, "item two", "user")
	s.Edge("fact", "fct_1", "event", "evt_1", core.RelDerivedFrom)
	s.Edge("fact", "fct_2", "event", "evt_1", core.RelDerivedFrom)
	s.Edge("fact", "fct_2", "event", "evt_3", core.RelConfirmedBy)
	s.Edge("fact", "fct_3", "fact", "fct_2", core.RelDerivedFrom)
	s.Edge("page_item", "pgi_1", "event", "evt_2", core.RelCites)
	s.Edge("page_item", "pgi_2", "fact", "fct_1", core.RelCites)
	s.Edge("fact", "fct_4", "fact", "fct_5", core.RelDerivedFrom)
	s.Edge("fact", "fct_5", "fact", "fct_4", core.RelDerivedFrom)
	s.Edge("fact", "fct_5", "event", "evt_2", core.RelDerivedFrom)
	s.Edge("event", "evt_1", "entity", "ent_1", core.RelMentions)
	s.Edge("fact", "fct_6", "fact", "fct_1", core.RelSupersedes)
	s.Edge("fact", "fct_6", "event", "evt_2", core.RelDerivedFrom)
	s.Embedding("event", "evt_1", "e1")
	s.Embedding("fact", "fct_1", "f1")
	s.Embedding("page", "pg_1", "p1")
	s.Embedding("fact", "fct_6", "f6")
	return e, s
}

func ids(evs []prov.EventRef) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.ID)
	}
	return out
}

// BASE.md principle 4: every derived record points to its origin.
func TestOriginsWalkToEvents(t *testing.T) {
	e, _ := graph(t)
	ctx := context.Background()
	cases := []struct {
		name string
		rt   core.RecordType
		id   string
		want []string
		zone core.Zone
	}{
		{"event is its own origin", core.RecEvent, "evt_1", []string{"evt_1"}, core.ZoneUser},
		{"fact derived from one event", core.RecFact, "fct_1", []string{"evt_1"}, core.ZoneUser},
		{"confirmed_by is an origin", core.RecFact, "fct_2", []string{"evt_1", "evt_3"}, core.ZoneExternal},
		{"transitive through a fact", core.RecFact, "fct_3", []string{"evt_1", "evt_3"}, core.ZoneExternal},
		{"page through its items", core.RecPage, "pg_1", []string{"evt_1", "evt_2"}, core.ZoneInternal},
		{"cycle is safe", core.RecFact, "fct_4", []string{"evt_2"}, core.ZoneInternal},
		{"supersedes is not an origin", core.RecFact, "fct_6", []string{"evt_2"}, core.ZoneInternal},
		{"no origin gives external", core.RecFact, "fct_missing", nil, core.ZoneExternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := prov.Origins(ctx, e.Store.DB(), c.rt, c.id)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ids(got), c.want) {
				t.Fatalf("origins = %v, want %v", ids(got), c.want)
			}
			z, err := prov.ZoneOf(ctx, e.Store.DB(), c.rt, c.id)
			if err != nil || z != c.zone {
				t.Fatalf("zone = %v (%v), want %v", z, err, c.zone)
			}
		})
	}
}

func TestDescendantsCollectsDerivedRecordsAndVectors(t *testing.T) {
	e, _ := graph(t)
	d, err := prov.Descendants(context.Background(), e.Store.DB(), []string{"evt_1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fct_1", "fct_2", "fct_3"}; !reflect.DeepEqual(d.Facts, want) {
		t.Errorf("facts = %v, want %v (supersedes must not be followed)", d.Facts, want)
	}
	if want := []string{"pgi_2"}; !reflect.DeepEqual(d.PageItems, want) {
		t.Errorf("items = %v", d.PageItems)
	}
	if want := []string{"pg_1"}; !reflect.DeepEqual(d.Pages, want) {
		t.Errorf("pages = %v", d.Pages)
	}
	want := []prov.EmbeddingKey{{core.RecEvent, "evt_1", "m"}, {core.RecFact, "fct_1", "m"}, {core.RecPage, "pg_1", "m"}}
	if !reflect.DeepEqual(d.Embeddings, want) {
		t.Errorf("embeddings = %v", d.Embeddings)
	}
	if len(d.Other) != 0 {
		t.Errorf("entities must not be collected: %v", d.Other)
	}
}

func TestAddEdge(t *testing.T) {
	e, _ := graph(t)
	ctx := context.Background()
	db := e.Store.DB()
	if err := prov.AddEdge(ctx, db, e.Clock, "ns", core.RecFact, "fct_6", core.RecEvent, "evt_3", core.RelConfirmedBy); err != nil {
		t.Fatal(err)
	}
	// Idempotent.
	if err := prov.AddEdge(ctx, db, e.Clock, "ns", core.RecFact, "fct_6", core.RecEvent, "evt_3", core.RelConfirmedBy); err != nil {
		t.Fatal(err)
	}
	if z, _ := prov.ZoneOf(ctx, db, core.RecFact, "fct_6"); z != core.ZoneExternal {
		t.Fatalf("zone after external confirmation = %s", z)
	}
	bad := []struct {
		name         string
		dt           core.RecordType
		did, sid, rl string
		st           core.RecordType
	}{
		{"self edge", core.RecFact, "fct_1", "fct_1", core.RelDerivedFrom, core.RecFact},
		{"event with origin edge", core.RecEvent, "evt_1", "evt_2", core.RelDerivedFrom, core.RecEvent},
		{"empty relation", core.RecFact, "fct_1", "evt_2", "", core.RecEvent},
	}
	for _, b := range bad {
		err := prov.AddEdge(ctx, db, e.Clock, "ns", b.dt, b.did, b.st, b.sid, b.rl)
		if core.CodeOf(err) != core.CodeInvalidArgument {
			t.Errorf("%s: err = %v", b.name, err)
		}
	}
}

// Architect note: only derived_from, confirmed_by and cites are derivation
// edges. duplicate_of, supersedes and any other relation are not paths to
// events and are not followed by Descendants.
func TestOnlyDerivationRelationsAreOrigins(t *testing.T) {
	e, s := graph(t)
	ctx := context.Background()
	s.Edge("fact", "fct_6", "fact", "fct_1", core.RelDuplicateOf)
	s.Edge("fact", "fct_6", "event", "evt_3", "related_to")
	got, err := prov.Origins(ctx, e.Store.DB(), core.RecFact, "fct_6")
	if err != nil || !reflect.DeepEqual(ids(got), []string{"evt_2"}) {
		t.Fatalf("origins = %v %v", ids(got), err)
	}
	for _, root := range []string{"evt_1", "evt_3"} {
		d, _ := prov.Descendants(ctx, e.Store.DB(), []string{root})
		for _, f := range d.Facts {
			if f == "fct_6" {
				t.Errorf("descendants of %s followed a non-derivation edge", root)
			}
		}
	}
	for _, r := range []string{core.RelDerivedFrom, core.RelConfirmedBy, core.RelCites} {
		if !prov.IsOriginRelation(r) {
			t.Errorf("%s must be an origin relation", r)
		}
	}
	for _, r := range []string{core.RelMentions, core.RelDuplicateOf, core.RelSupersedes, "related_to"} {
		if prov.IsOriginRelation(r) {
			t.Errorf("%s must not be an origin relation", r)
		}
	}
}
