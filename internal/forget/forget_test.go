package forget_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/forget"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/prov/provtest"
)

const ns = "acme"

// contents of every event of the corpus, by id.
var contents = map[string]string{
	"evt_01": "Project Quetzalcoatl launches in March with a budget of 4.2 million reais.",
	"evt_02": "The onboarding checklist covers laptops, badges and payroll enrollment.",
	"evt_03": "Confidential: Zyntharix Holdings will acquire Pelmorra Logistics for 870 million.",
	"evt_04": "The integration team led by Ortensia Valquíria reports weekly to the board.",
	"evt_05": "User asked about the Zyntharix closing timeline; answer: closing expected in Q3.",
	"evt_06": "Discussed cafeteria menu rotation and vegetarian options on Fridays.",
	"evt_07": "My personal phone number is 5511987654321 and I prefer calls after lunch.",
	"evt_08": "Reminder: Ortensia Valquíria prefers written status updates over meetings.",
	"evt_09": "Rumor: Zyntharix Holdings eyeing logistics acquisitions in Brazil.",
	"evt_10": "Earlier draft: the Quetzalcoatl budget was 3.9 million reais.",
}

// corpus builds a small realistic namespace: two documents, a session, user
// statements and a web result; entities, facts with provenance (including a
// fact confirmed by two events and a superseded fact), two pages with items
// citing events, vectors for everything, retrievals with outcomes, utility,
// flags, the embedding queue, a sleep run with a snapshot file holding the
// content and a gate question. A second namespace holds similar text.
func corpus(t *testing.T) (*env.Env, string) {
	t.Helper()
	e, _ := env.NewTest(t)
	s := provtest.New(t, e, ns)
	s.Source("src_01", "document", "file:///docs/handbook.md", "internal")
	s.Source("src_02", "document", "file:///docs/merger-plan.md", "internal")
	s.Source("src_03", "session", "session://s1", "internal")
	s.Source("src_04", "user_statement", "user://owner", "user")
	s.Source("src_05", "web", "https://news.example/zyn", "external")
	s.Document("doc_01", "src_01", "file:///docs/handbook.md")
	s.Document("doc_02", "src_02", "file:///docs/merger-plan.md")
	ev := func(id, src, doc, zone, at string) { s.Event(id, src, doc, zone, contents[id], at) }
	ev("evt_01", "src_01", "doc_01", "internal", "2026-02-01T10:00:00.000000000Z")
	ev("evt_02", "src_01", "doc_01", "internal", "2026-02-01T10:00:00.000000000Z")
	ev("evt_10", "src_01", "doc_01", "internal", "2026-01-15T10:00:00.000000000Z")
	ev("evt_03", "src_02", "doc_02", "internal", "2026-02-03T10:00:00.000000000Z")
	ev("evt_04", "src_02", "doc_02", "internal", "2026-02-03T10:00:00.000000000Z")
	ev("evt_05", "src_03", "", "internal", "2026-02-05T09:00:00.000000000Z")
	ev("evt_06", "src_03", "", "internal", "2026-02-05T09:01:00.000000000Z")
	ev("evt_07", "src_04", "", "user", "2026-03-10T12:00:00.000000000Z")
	ev("evt_08", "src_04", "", "user", "2026-03-15T12:00:00.000000000Z")
	ev("evt_09", "src_05", "", "external", "2026-02-20T08:00:00.000000000Z")

	s.Entity("ent_01", "Zyntharix Holdings", "zyntharix holdings")
	s.Entity("ent_02", "Pelmorra Logistics", "pelmorra logistics")
	s.Entity("ent_03", "Ortensia Valquíria", "ortensia valquiria")
	s.Entity("ent_04", "Project Quetzalcoatl", "project quetzalcoatl")
	s.Entity("ent_05", "Owner", "owner")
	for ev, ents := range map[string][]string{
		"evt_01": {"ent_04"}, "evt_03": {"ent_01", "ent_02"}, "evt_04": {"ent_03"}, "evt_05": {"ent_01"},
		"evt_07": {"ent_05"}, "evt_08": {"ent_03"}, "evt_09": {"ent_01"}, "evt_10": {"ent_04"},
	} {
		for _, ent := range ents {
			s.Edge("event", ev, "entity", ent, core.RelMentions)
		}
	}
	fact := func(id, subj, pred, val, zone, text string, origins ...string) {
		s.Fact(id, subj, pred, val, zone, text)
		for i, o := range origins {
			rel := core.RelDerivedFrom
			if i > 0 {
				rel = core.RelConfirmedBy
			}
			s.Edge("fact", id, "event", o, rel)
		}
		s.Embedding("fact", id, id)
	}
	fact("fct_01", "ent_01", "acquires", "Pelmorra Logistics", "internal", "Zyntharix Holdings acquires Pelmorra Logistics", "evt_03", "evt_05")
	s.Edge("fact", "fct_01", "entity", "ent_02", core.RelMentions)
	fact("fct_02", "ent_03", "role", "integration lead", "internal", "Ortensia Valquíria: integration lead", "evt_04", "evt_08")
	fact("fct_03", "ent_04", "budget", "4.2 million reais", "internal", "Quetzalcoatl budget: 4.2 million reais", "evt_01")
	fact("fct_04", "ent_01", "deal_value", "870 million", "internal", "Zyntharix deal value: 870 million", "evt_03")
	fact("fct_05", "ent_05", "phone", "5511987654321", "user", "owner phone: 5511987654321", "evt_07")
	fact("fct_06", "ent_04", "budget", "3.9 million reais", "internal", "Quetzalcoatl budget: 3.9 million reais", "evt_10")
	s.Exec(`UPDATE facts SET status='superseded', superseded_by='fct_03', valid_to=? WHERE id='fct_06'`, provtest.T)
	s.Edge("fact", "fct_03", "fact", "fct_06", core.RelSupersedes)

	s.Page("pg_01", "Merger summary", "Zyntharix will buy Pelmorra for 870 million.\nOrtensia Valquíria leads the integration.\nOrtensia wants written updates.", "internal")
	s.Item("pgi_01", "pg_01", 0, "Zyntharix will buy Pelmorra for 870 million.", "internal")
	s.Item("pgi_02", "pg_01", 1, "Ortensia Valquíria leads the integration.", "internal")
	s.Item("pgi_03", "pg_01", 2, "Ortensia wants written updates.", "internal")
	s.Edge("page_item", "pgi_01", "event", "evt_03", core.RelCites)
	s.Edge("page_item", "pgi_02", "event", "evt_04", core.RelCites)
	s.Edge("page_item", "pgi_03", "event", "evt_08", core.RelCites)
	s.Page("pg_02", "Onboarding", "Checklist: laptops, badges, payroll.", "internal")
	s.Item("pgi_04", "pg_02", 0, "Checklist: laptops, badges, payroll.", "internal")
	s.Edge("page_item", "pgi_04", "event", "evt_02", core.RelCites)
	for id := range contents {
		s.Embedding("event", id, contents[id])
	}
	for _, id := range []string{"pg_01", "pg_02", "pgi_01", "pgi_02", "pgi_03", "pgi_04"} {
		s.Embedding(map[bool]string{true: "page", false: "page_item"}[strings.HasPrefix(id, "pg_")], id, id)
	}

	ret := `INSERT INTO retrievals(id,namespace,session_id,mode,query_text,params,candidates,delivered,ts) VALUES(?,?,'s1','search',?,'{}',?,?,?)`
	s.Exec(ret, "ret_01", ns, "Zyntharix acquisition", `[{"id":"evt_03","final":0.9},{"id":"evt_09","final":0.5}]`, `["evt_03"]`, provtest.T)
	s.Exec(ret, "ret_02", ns, "what is on the cafeteria menu", `[{"id":"evt_06","final":0.8}]`, `["evt_06"]`, provtest.T)
	s.Exec(ret, "ret_03", ns, "onboarding laptops", `[{"id":"evt_02","final":0.8}]`, `["evt_02"]`, provtest.T)
	out := `INSERT INTO outcomes(id,namespace,retrieval_id,signal,value,used_ids,actor,ts) VALUES(?,?,?,'task_outcome',1,?,'agent',?)`
	s.Exec(out, "out_01", ns, "ret_01", `["evt_03"]`, provtest.T)
	s.Exec(out, "out_02", ns, "ret_02", `["evt_06"]`, provtest.T)
	s.Exec(out, "out_03", ns, "ret_03", `["evt_02"]`, provtest.T)
	for _, r := range [][2]string{{"event", "evt_03"}, {"event", "evt_06"}, {"event", "evt_07"}, {"fact", "fct_01"}, {"event", "evt_02"}} {
		s.Exec(`INSERT INTO utility(namespace,record_type,record_id,n_shown,updated_at) VALUES(?,?,?,3,?)`, ns, r[0], r[1], provtest.T)
	}
	s.Exec(`INSERT INTO embed_queue(namespace,record_type,record_id,enqueued_at) VALUES(?,'event','evt_09',?)`, ns, provtest.T)
	s.Exec(`INSERT INTO flags(id,namespace,record_type,record_id,reason,actor,ts) VALUES('flg_01',?,'fact','fct_04','outdated','agent',?)`, ns, provtest.T)
	s.Exec(`INSERT INTO event_processing(namespace,event_id,step,sleep_run_id,processed_at) VALUES(?,'evt_03','extract_facts','run_01',?)`, ns, provtest.T)
	s.Exec(`INSERT INTO gate_questions(id,namespace,question,expected,status,created_at) VALUES('gq_01',?,'Who acquires Pelmorra?','{"ids":["evt_03"]}','approved',?)`, ns, provtest.T)

	// A snapshot taken by a sleep run, holding the raw content.
	dir := filepath.Join(filepath.Dir(e.Store.Path()), "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, id := range sortedIDs() {
		all.WriteString(contents[id] + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "run_01.db"), []byte(all.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Exec(`INSERT INTO sleep_runs(id,namespace,started_at,phases,budget,inputs,snapshot,status) VALUES('run_01',?,?,'[1]','{}',?,'snapshots/run_01.db','completed')`,
		ns, provtest.T, `{"events":["evt_03","evt_06"]}`)

	// Another namespace with overlapping vocabulary must be untouched.
	o := provtest.New(t, e, "other")
	o.Source("src_91", "session", "session://o", "internal")
	o.Event("evt_91", "src_91", "", "internal", "Zyntharix Holdings is also discussed in another namespace.", "2026-03-12T00:00:00.000000000Z")
	o.Embedding("event", "evt_91", contents["evt_03"]) // identical vector, other namespace

	// Write a few audit entries so the chain has history.
	for _, id := range sortedIDs() {
		if _, err := e.Audit.Append(context.Background(), e.Store.DB(), audit.Entry{Namespace: ns, Actor: "tester", Action: audit.ActWrite, Target: id}); err != nil {
			t.Fatal(err)
		}
	}
	return e, dir
}

func sortedIDs() []string {
	return prov.SortedUnique(func() []string {
		var out []string
		for id := range contents {
			out = append(out, id)
		}
		return out
	}())
}

func count(t *testing.T, e *env.Env, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// windows returns every 12-byte substring of s (the minimum the spec's byte
// scan uses), to check that no fragment of forgotten content was stored.
func windows(s string) []string {
	var out []string
	for i := 0; i+12 <= len(s); i++ {
		out = append(out, s[i:i+12])
	}
	return out
}

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return b
}

// BASE.md "Forgetting" and the deletion suite: after forget, no search, fact,
// vector or summary returns the content; every probe passes.
func TestDeletionSuite(t *testing.T) {
	cases := []struct {
		name        string
		scope       forget.Scope
		events      []string // removed events
		facts       []string // removed facts
		kept        []string // facts that survive with another origin
		items       []string // removed page items
		pages       []string // removed pages
		rebuild     []string // pages left needing a rebuild
		entities    []string
		sources     []string
		documents   []string
		retrievals  []string
		absentBytes []string // distinctive strings that must not remain anywhere
	}{
		{
			name:   "record id",
			scope:  forget.Scope{Kind: forget.ScopeRecord, Ref: "evt_07"},
			events: []string{"evt_07"}, facts: []string{"fct_05"}, entities: []string{"ent_05"},
			absentBytes: []string{"5511987654321", "prefer calls after lunch"},
		},
		{
			name:   "document",
			scope:  forget.Scope{Kind: forget.ScopeDocument, Ref: "file:///docs/merger-plan.md"},
			events: []string{"evt_03", "evt_04"}, facts: []string{"fct_04"}, kept: []string{"fct_01", "fct_02"},
			items: []string{"pgi_01", "pgi_02"}, rebuild: []string{"pg_01"},
			sources: []string{"src_02"}, documents: []string{"doc_02"}, retrievals: []string{"ret_01"},
			absentBytes: []string{"Confidential: Zyntharix", "reports weekly to the board", "file:///docs/merger-plan.md", "Zyntharix will buy Pelmorra"},
		},
		{
			name:   "entity",
			scope:  forget.Scope{Kind: forget.ScopeEntity, Ref: "Zyntharix Holdings"},
			events: []string{"evt_03", "evt_05", "evt_09"}, facts: []string{"fct_01", "fct_04"},
			items: []string{"pgi_01"}, rebuild: []string{"pg_01"},
			entities: []string{"ent_01", "ent_02"}, sources: []string{"src_05"}, retrievals: []string{"ret_01"},
			absentBytes: []string{"Zyntharix Holdings will", "Pelmorra Logistics", "eyeing logistics acquisitions", "Zyntharix will buy"},
		},
		{
			name:   "date range",
			scope:  forget.Scope{Kind: forget.ScopeDateRange, From: "2026-03-01", To: "2026-04-01"},
			events: []string{"evt_07", "evt_08"}, facts: []string{"fct_05"}, kept: []string{"fct_02"},
			items: []string{"pgi_03"}, rebuild: []string{"pg_01"}, entities: []string{"ent_05"}, sources: []string{"src_04"},
			absentBytes: []string{"5511987654321", "prefers written status updates", "user://owner", "wants written updates"},
		},
		{
			name:   "search match",
			scope:  forget.Scope{Kind: forget.ScopeSearch, Query: "cafeteria vegetarian"},
			events: []string{"evt_06"}, retrievals: []string{"ret_02"},
			absentBytes: []string{"cafeteria menu rotation", "vegetarian options on Fridays"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, snapDir := corpus(t)
			ctx := context.Background()
			db := e.Store.DB()

			// dry_run reports the same plan and changes nothing.
			auditBefore := count(t, e, `SELECT count(*) FROM audit_log`)
			dry, err := forget.Forget(ctx, e, ns, c.scope, true, "admin")
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			if count(t, e, `SELECT count(*) FROM events`) != len(contents)+1 || count(t, e, `SELECT count(*) FROM audit_log`) != auditBefore {
				t.Fatal("dry run changed the database")
			}

			rep, err := forget.Forget(ctx, e, ns, c.scope, false, "admin")
			if err != nil {
				t.Fatalf("forget: %v (verification %+v)", err, rep.Verification)
			}
			if !reflect.DeepEqual(dry.Removed, rep.Removed) {
				t.Errorf("dry run %+v differs from run %+v", dry.Removed, rep.Removed)
			}
			eq := func(what string, got, want []string) {
				t.Helper()
				if len(got) == 0 && len(want) == 0 {
					return
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %v, want %v", what, got, want)
				}
			}
			eq("events", rep.Removed.Events, c.events)
			eq("facts", rep.Removed.Facts, c.facts)
			eq("facts kept", rep.FactsKept, c.kept)
			eq("page items", rep.Removed.PageItems, c.items)
			eq("pages", rep.Removed.Pages, c.pages)
			eq("pages rebuild", rep.PagesRebuild, c.rebuild)
			eq("entities", rep.Removed.Entities, c.entities)
			eq("sources", rep.Removed.Sources, c.sources)
			eq("documents", rep.Removed.Documents, c.documents)
			eq("retrievals", rep.Removed.Retrievals, c.retrievals)

			// 100% of probes pass, and each probe actually checked something.
			if rep.Verification == nil || !rep.Verification.OK {
				t.Fatalf("verification failed: %+v", rep.Verification)
			}
			for _, p := range rep.Verification.Probes {
				if !p.Pass {
					t.Errorf("probe %s failed: %v", p.Name, p.Failures)
				}
				if p.Checked == 0 {
					t.Errorf("probe %s checked nothing (%s)", p.Name, p.Note)
				}
			}

			// Survivors keep the facts with other origins, minus the forgotten edge.
			for _, f := range c.kept {
				origins, err := prov.Origins(ctx, db, core.RecFact, f)
				if err != nil || len(origins) == 0 {
					t.Errorf("kept fact %s lost every origin: %v", f, err)
				}
				for _, o := range origins {
					for _, gone := range c.events {
						if o.ID == gone {
							t.Errorf("kept fact %s still points to %s", f, gone)
						}
					}
				}
			}
			// A page with a forgotten origin leaves reads, without the removed text.
			for _, pg := range c.rebuild {
				var status string
				db.QueryRow(`SELECT status FROM pages WHERE id = ?`, pg).Scan(&status)
				if status != "needs_rebuild" {
					t.Errorf("page %s status = %s", pg, status)
				}
			}
			// Nothing derived is left, in the graph or in usage tables.
			for _, ev := range c.events {
				if n := count(t, e, `SELECT count(*) FROM events_fts WHERE record_id = ?`, ev); n != 0 {
					t.Errorf("fts entry of %s survived", ev)
				}
			}
			if n := count(t, e, `SELECT count(*) FROM events WHERE namespace = 'other'`); n != 1 {
				t.Errorf("other namespace touched")
			}
			// The snapshot that held the content is gone.
			if _, err := os.Stat(filepath.Join(snapDir, "run_01.db")); !os.IsNotExist(err) {
				t.Errorf("snapshot survived: %v", err)
			}
			if n := count(t, e, `SELECT count(*) FROM sleep_runs WHERE snapshot IS NOT NULL`); n != 0 {
				t.Errorf("sleep run still points to a deleted snapshot")
			}

			// Independent byte check of the database and WAL files.
			raw := append(fileBytes(t, e.Store.Path()), fileBytes(t, e.Store.Path()+"-wal")...)
			for _, s := range c.absentBytes {
				if bytes.Contains(raw, []byte(s)) {
					t.Errorf("file still holds %q", s)
				}
			}

			// The tombstone and the audit log hold no content.
			var proof strings.Builder
			rows, err := db.Query(`SELECT scope || requested_by || removed_counts || coalesce(content_hashes,'') FROM tombstones
				UNION ALL SELECT actor || action || target || details FROM audit_log`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var s string
				rows.Scan(&s)
				proof.WriteString(s)
			}
			rows.Close()
			for _, ev := range c.events {
				for _, w := range windows(contents[ev]) {
					if strings.Contains(proof.String(), w) {
						t.Fatalf("tombstone or audit holds content of %s: %q", ev, w)
					}
				}
			}
			for _, s := range append([]string{c.scope.Query, c.scope.Ref}, c.absentBytes...) {
				if s != "" && !strings.HasPrefix(s, "evt_") && strings.Contains(proof.String(), s) {
					t.Errorf("tombstone or audit holds %q", s)
				}
			}
			if count(t, e, `SELECT count(*) FROM tombstones WHERE id = ? AND requested_by = 'admin' AND content_hashes IS NULL`, rep.TombstoneID) != 1 {
				t.Error("tombstone missing")
			}
			if count(t, e, `SELECT count(*) FROM audit_log WHERE action = ? AND target = ?`, audit.ActForget, rep.TombstoneID) != 1 {
				t.Error("forget audit entry missing")
			}

			// The audit chain and the provenance graph still verify.
			v, err := prov.Verify(ctx, e)
			if err != nil || !v.OK {
				t.Fatalf("verify after forget: %+v %v", v, err)
			}
		})
	}
}
