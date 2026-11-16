package forget_test

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/forget"
	"github.com/felipemarts/chanda/internal/prov/provtest"
)

// mentionCorpus seeds one entity, "Ysolde Karvenni", created after every
// event that names it, with no `mentions` edge anywhere, and one event per
// boundary of the mention rule (decision 0130). want lists the events the
// entity scope must remove.
func mentionCorpus(t *testing.T) (*env.Env, *provtest.Seeder, map[string]bool) {
	t.Helper()
	e, _ := env.NewTest(t)
	s := provtest.New(t, e, ns)
	s.Source("src_01", "user_statement", "user://owner", "user")
	events := []struct {
		id, content string
		want        bool
	}{
		{"evt_01", "Ysolde Karvenni prefere reunioes as tercas; o apelido dela no time e drovanquiz.", true},
		{"evt_02", "YSOLDE KARVENNI aprovou o orçamento.", true},                          // case
		{"evt_03", "Ysoldé Kárvenni ligou de manhã.", true},                               // diacritics
		{"evt_04", "Reunião com ysolde-karvenni@exemplo.com às 10h.", true},               // punctuation between tokens
		{"evt_05", "Ysolde chegou cedo hoje.", false},                                     // partial name
		{"evt_06", "A Karvenni Logística entregou as caixas.", false},                     // partial name
		{"evt_07", "Ysolde Karvenniana é outra pessoa.", false},                           // name inside another word
		{"evt_08", "Karvenni, Ysolde: ficha antiga do cadastro.", false},                  // tokens in another order
		{"evt_09", "Ysolde e Karvenni são duas pessoas diferentes.", false},               // tokens not consecutive
		{"evt_10", "A sala controlbasaltrino tem projetor novo.", false},                  // control
		{"evt_11", "Owner of the bakery asked for a new oven and owner approval.", false}, // short alias "owner"
	}
	want := map[string]bool{}
	for _, ev := range events {
		s.Event(ev.id, "src_01", "", "user", ev.content, "2026-01-01T00:00:00.000000000Z")
		s.Embedding("event", ev.id, ev.content)
		want[ev.id] = ev.want
	}
	// Created after the text; the alias "ysolde" and "owner" are short single tokens.
	s.Exec(`INSERT INTO entities(id,namespace,canonical_name,created_at) VALUES('ent_01',?,'Ysolde Karvenni','2026-06-01T00:00:00.000000000Z')`, ns)
	for _, a := range []string{"ysolde karvenni", "ysolde", "owner"} {
		s.Exec(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,'ent_01')`, ns, a)
	}
	// The same name in another namespace is never touched.
	o := provtest.New(t, e, "other")
	o.Source("src_91", "user_statement", "user://other", "user")
	o.Event("evt_91", "src_91", "", "user", "Ysolde Karvenni também aparece em outro namespace.", "")
	return e, s, want
}

func TestEntityMentionRuleTerms(t *testing.T) {
	cases := []struct {
		name, term string
		used       bool
		skip       string
	}{
		{"full name, two tokens", "ysolde karvenni", true, ""},
		{"single distinctive token of 9 runes", "zyntharix", true, ""},
		{"single token of exactly 8 runes", "drovanqu", true, ""},
		{"single token of 7 runes", "ysoldex", false, forget.SkipShortToken},
		{"common short word", "owner", false, forget.SkipShortToken},
		{"two tokens, 5 letters", "ana li", false, forget.SkipShortPhrase},
		{"two tokens, exactly 6 letters", "ana lim", true, ""},
		{"acronym", "ti", false, forget.SkipShortToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := env.NewTest(t)
			s := provtest.New(t, e, ns)
			s.Entity("ent_01", "Someone Else Entirely", "someone else entirely")
			s.Exec(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,'ent_01')`, ns, c.term)
			rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "ent_01"}, true, "admin")
			if err != nil {
				t.Fatal(err)
			}
			for _, et := range rep.EntityTerms {
				if et.Term == c.term {
					if et.Used != c.used || et.Skip != c.skip {
						t.Fatalf("%q: used %v skip %q, want %v %q", c.term, et.Used, et.Skip, c.used, c.skip)
					}
					return
				}
			}
			t.Fatalf("term %q not reported: %+v", c.term, rep.EntityTerms)
		})
	}
}

// BASE.md "Forgetting", scope entity; decision 0050: the entity scope
// resolves to the events that mention the entity. Decision 0130: they are
// found by a whole-token phrase match of the entity's names, case and
// diacritics folded, in every event of the namespace, with no mentions edge
// and regardless of when the entity was created.
func TestEntityScopeRemovesEventsThatNameTheEntity(t *testing.T) {
	e, _, want := mentionCorpus(t)
	ctx := context.Background()
	scope := forget.Scope{Kind: forget.ScopeEntity, Ref: "Ysolde Karvenni"}
	dry, err := forget.Forget(ctx, e, ns, scope, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var wantIDs []string
	for id, w := range want {
		if w {
			wantIDs = append(wantIDs, id)
		}
	}
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(dry.Removed.Events, wantIDs) {
		t.Fatalf("events = %v, want %v", dry.Removed.Events, wantIDs)
	}
	// dry_run lists every matched event with the term that matched.
	var listed []string
	for _, m := range dry.Mentions {
		if m.Term != "ysolde karvenni" {
			t.Errorf("mention %s matched by %q", m.ID, m.Term)
		}
		listed = append(listed, m.ID)
	}
	if !reflect.DeepEqual(listed, wantIDs) {
		t.Fatalf("mentions = %v, want %v", listed, wantIDs)
	}
	terms := map[string]forget.EntityTerm{}
	for _, et := range dry.EntityTerms {
		terms[et.Term] = et
	}
	if !terms["ysolde karvenni"].Used || terms["ysolde"].Used || terms["owner"].Skip != forget.SkipShortToken || len(terms) != 3 {
		t.Fatalf("entity terms = %+v", dry.EntityTerms)
	}

	rep, err := forget.Forget(ctx, e, ns, scope, false, "admin")
	if err != nil {
		t.Fatalf("%v %+v", err, rep.Verification)
	}
	if !reflect.DeepEqual(rep.Removed, dry.Removed) {
		t.Fatalf("run %+v differs from dry run %+v", rep.Removed, dry.Removed)
	}
	if p := probe(rep.Verification, forget.ProbeEntity); !p.Pass || p.Checked == 0 {
		t.Fatalf("entity probe: %+v", p)
	}
	for id, w := range want {
		if n := count(t, e, `SELECT count(*) FROM events WHERE id = ?`, id); (n == 0) != w {
			t.Errorf("%s: present %v, want removed %v", id, n == 1, w)
		}
	}
	for _, q := range []string{
		`SELECT count(*) FROM events_fts WHERE events_fts MATCH 'drovanquiz'`,
		`SELECT count(*) FROM embeddings WHERE record_id IN ('evt_01','evt_02','evt_03','evt_04')`,
		`SELECT count(*) FROM entities WHERE id = 'ent_01'`,
	} {
		if n := count(t, e, q); n != 0 {
			t.Errorf("%s = %d", q, n)
		}
	}
	if count(t, e, `SELECT count(*) FROM events WHERE id = 'evt_91'`) != 1 {
		t.Fatal("an event of another namespace was removed")
	}
	// The tombstone keeps counts, never the names.
	var scopeDesc string
	e.Store.DB().QueryRow(`SELECT scope FROM tombstones WHERE id = ?`, rep.TombstoneID).Scan(&scopeDesc)
	if strings.Contains(strings.ToLower(scopeDesc), "ysolde") || !strings.Contains(scopeDesc, `"terms_used":1`) {
		t.Fatalf("tombstone scope = %s", scopeDesc)
	}
}

// Decision 0130: the names of every entity in the merge set count. Forgetting
// by the name of an absorbed entity forgets the entity that absorbed it and
// everything merged into it (the same real-world entity).
func TestEntityScopeFollowsMergeChains(t *testing.T) {
	e, _ := env.NewTest(t)
	s := provtest.New(t, e, ns)
	s.Source("src_01", "user_statement", "user://owner", "user")
	s.Entity("ent_01", "Robert Smithfield", "robert smithfield")
	s.Entity("ent_02", "Bob Smithfield", "bob smithfield")
	s.Entity("ent_03", "Roberto Smithfield", "roberto smithfield")
	s.Entity("ent_04", "Alice Smithfield", "alice smithfield")
	s.Exec(`UPDATE entities SET merged_into = 'ent_01' WHERE id = 'ent_02'`)
	s.Exec(`UPDATE entities SET merged_into = 'ent_02' WHERE id = 'ent_03'`)
	for id, c := range map[string]string{
		"evt_01": "Robert Smithfield signed the lease.",
		"evt_02": "Bob Smithfield called about the lease.",
		"evt_03": "Roberto Smithfield paid the deposit.",
		"evt_04": "Alice Smithfield is his sister.",
		"evt_05": "The Smithfield family owns the bakery.",
	} {
		s.Event(id, "src_01", "", "user", c, "")
	}
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "bob smithfield"}, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rep.Removed.Events, ","); got != "evt_01,evt_02,evt_03" {
		t.Fatalf("events = %s", got)
	}
	if got := strings.Join(rep.Removed.Entities, ","); got != "ent_01,ent_02,ent_03" {
		t.Fatalf("entities = %s", got)
	}
}

// Derived records of the matched events go in cascade; facts, page items and
// page titles that name the entity go too, with the events the facts came
// from (decision 0050: "origin events of facts ... that mention it").
func TestEntityScopeCascadesToDerivedRecords(t *testing.T) {
	e, s, _ := mentionCorpus(t)
	s.Entity("ent_02", "Lidia Marquez", "lidia marquez")
	s.Source("src_02", "session", "session://s1", "internal")
	s.Event("evt_20", "src_02", "", "internal", "Lidia trabalha com a arquiteta de dados.", "")
	s.Event("evt_21", "src_02", "", "internal", "Lidia mudou de equipe em maio.", "")
	// A fact derived from a matched event (subject another entity).
	s.Fact("fct_01", "ent_02", "nickname_of_colleague", "drovanquiz", "user", "Lidia Marquez: colleague nickname drovanquiz")
	s.Edge("fact", "fct_01", "event", "evt_01", core.RelDerivedFrom)
	// A fact naming the entity whose origin does not: removed with its origin.
	s.Fact("fct_02", "ent_02", "works_with", "Ysolde Karvenni", "internal", "Lidia Marquez works with Ysolde Karvenni")
	s.Edge("fact", "fct_02", "event", "evt_20", core.RelDerivedFrom)
	// A fact of another entity that does not name her survives.
	s.Fact("fct_03", "ent_02", "team", "dados", "internal", "Lidia Marquez team: dados")
	s.Edge("fact", "fct_03", "event", "evt_21", core.RelDerivedFrom)
	// A page titled after her, and a page with an item naming her.
	s.Page("pg_01", "Ysolde Karvenni", "Arquiteta de dados.", "internal")
	s.Item("pgi_01", "pg_01", 0, "Arquiteta de dados.", "internal")
	s.Edge("page_item", "pgi_01", "event", "evt_21", core.RelCites)
	s.Page("pg_02", "Equipe de dados", "Ysolde Karvenni lidera a revisão.\nLidia mudou de equipe.", "internal")
	s.Item("pgi_02", "pg_02", 0, "Ysolde Karvenni lidera a revisão.", "internal")
	s.Item("pgi_03", "pg_02", 1, "Lidia mudou de equipe.", "internal")
	s.Edge("page_item", "pgi_02", "event", "evt_21", core.RelCites)
	s.Edge("page_item", "pgi_03", "event", "evt_21", core.RelCites)

	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "Ysolde Karvenni"}, false, "admin")
	if err != nil {
		t.Fatalf("%v %+v", err, rep.Verification)
	}
	if got := strings.Join(rep.Removed.Facts, ","); got != "fct_01,fct_02" {
		t.Fatalf("facts = %s", got)
	}
	if !slices.Contains(rep.Removed.Events, "evt_20") || slices.Contains(rep.Removed.Events, "evt_21") {
		t.Fatalf("events = %v", rep.Removed.Events)
	}
	if got := strings.Join(rep.Removed.Pages, ","); got != "pg_01" {
		t.Fatalf("pages = %s", got)
	}
	if got := strings.Join(rep.Removed.PageItems, ","); got != "pgi_01,pgi_02" {
		t.Fatalf("page items = %s", got)
	}
	if got := strings.Join(rep.PagesRebuild, ","); got != "pg_02" {
		t.Fatalf("pages rebuild = %s", got)
	}
	var body string
	e.Store.DB().QueryRow(`SELECT body FROM pages WHERE id = 'pg_02'`).Scan(&body)
	if body != "Lidia mudou de equipe." {
		t.Fatalf("rebuilt body = %q", body)
	}
	if count(t, e, `SELECT count(*) FROM facts WHERE id = 'fct_03'`) != 1 {
		t.Fatal("a fact that does not name the entity was removed")
	}
	if !rep.Verification.OK {
		t.Fatalf("verification: %+v", rep.Verification)
	}
}

// The entity probe checks the scope's own claim in what remains, so a
// mention that survives (here: a trigger copies a removed event under a new
// id, the shape of the defect in eval/deviations.md entry 1) is reported as
// a failed probe instead of passed:true. The failure never holds the name.
func TestEntityProbeReportsASurvivingMention(t *testing.T) {
	e, s, _ := mentionCorpus(t)
	s.Exec(`CREATE TRIGGER keep_mention AFTER DELETE ON events WHEN OLD.id = 'evt_02' BEGIN
		INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at)
		VALUES ('evt_99', OLD.namespace, OLD.source_id, OLD.kind, OLD.content, 'x', OLD.trust_zone, OLD.actor, OLD.recorded_at); END`)
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "Ysolde Karvenni"}, false, "admin")
	if core.CodeOf(err) != core.CodeIntegrity || rep.Verification == nil || rep.Verification.OK {
		t.Fatalf("expected an integrity failure, got err=%v verification=%+v", err, rep.Verification)
	}
	p := probe(rep.Verification, forget.ProbeEntity)
	if p.Pass || len(p.Failures) != 1 || !strings.Contains(p.Failures[0], "evt_99") {
		t.Fatalf("entity probe: %+v", p)
	}
	if strings.Contains(strings.ToLower(p.Failures[0]), "karvenni") {
		t.Fatalf("failure holds the name: %s", p.Failures[0])
	}
}

// Other scopes do not run the entity probe; an entity with no name long
// enough for the rule removes only its facts and edges, and says so.
func TestEntityProbeOnlyForEntityScope(t *testing.T) {
	e, _, _ := mentionCorpus(t)
	rep, err := forget.Forget(context.Background(), e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_10"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if p := probe(rep.Verification, forget.ProbeEntity); p.Name != "" {
		t.Fatalf("record scope ran the entity probe: %+v", p)
	}

	e2, s2, _ := mentionCorpus(t)
	s2.Entity("ent_05", "Owner", "owner2")
	rep, err = forget.Forget(context.Background(), e2, ns, forget.Scope{Kind: forget.ScopeEntity, Ref: "ent_05"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Removed.Events) != 0 || len(rep.Mentions) != 0 {
		t.Fatalf("a short name swept events: %v", rep.Removed.Events)
	}
	if p := probe(rep.Verification, forget.ProbeEntity); !p.Pass || p.Note == "" {
		t.Fatalf("entity probe: %+v", p)
	}
}
