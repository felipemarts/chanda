package sleep

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/forget"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

// turn inserts a session turn with its role, session, ordinal and valid_from
// ("" leaves role and valid_from NULL).
func (f *fixture) turn(id, zone, session string, ordinal int, role, validFrom, content string) {
	f.t.Helper()
	var r, vf any
	if role != "" {
		r = role
	}
	if validFrom != "" {
		vf = validFrom
	}
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,valid_from,session_id,ordinal,role)
		VALUES(?,?,?,'turn',?,?,?,'agent',?,?,?,?,?)`, id, ns, "src_"+zone, content, "h-"+id, zone, f.now(), vf, session, ordinal, r)
}

// retrieval logs a retrieval as logRetrieval does: candidates in final
// order (id and type), the delivered ids, query text and params.
func (f *fixture) retrieval(id, query string, cands, delivered []string) {
	f.t.Helper()
	type lc struct {
		ID       string             `json:"id"`
		Type     string             `json:"type"`
		Final    float64            `json:"final"`
		Features map[string]float64 `json:"features"`
	}
	dset := map[string]bool{}
	for _, d := range delivered {
		dset[d] = true
	}
	var cs []lc
	for i, c := range cands {
		typ := map[string]string{"evt": "event", "fct": "fact", "pg": "page"}[c[:strings.IndexByte(c, '_')]]
		d := 0.0
		if dset[c] {
			d = 1
		}
		cs = append(cs, lc{ID: c, Type: typ, Final: 1 - float64(i)/100, Features: map[string]float64{"delivered": d}})
	}
	cj, _ := json.Marshal(cs)
	dj, _ := json.Marshal(delivered)
	params := fmt.Sprintf(`{"query_echo":%q}`, query)
	f.exec(`INSERT INTO retrievals(id,namespace,session_id,mode,query_text,params,candidates,delivered,reason,ts)
		VALUES(?,?,NULL,'context',?,?,?,?,'',?)`, id, ns, query, params, string(cj), string(dj), f.now())
}

func (f *fixture) outcome(id, retrieval string, value float64) {
	f.exec(`INSERT INTO outcomes(id,namespace,retrieval_id,signal,value,used_ids,actor,ts) VALUES(?,?,?,'task_outcome',?,'[]','agent',?)`,
		id, ns, retrieval, value, f.now())
}

func (f *fixture) guidedSleeper(fake *llm.Fake, mut func(*config.GuidedConfig)) *Sleeper {
	f.e.Cfg.Sleep.Mode = config.SleepModeGuided
	if mut != nil {
		mut(&f.e.Cfg.Sleep.Guided)
	}
	fl := &fakeFlush{}
	d := Deps{Integrity: &fakeIntegrity{}, Embeddings: fl, Utility: fl, Evaluator: &fakeEval{}}
	if fake != nil {
		d.LLM = fake
	}
	return New(f.e, d)
}

// guidedCorpus: three turns about a guitar returned by two retrievals (one
// of them failed), and one turn no retrieval returned.
func guidedCorpus(f *fixture, marker string) {
	f.turn("evt_a1", "internal", "s1", 1, "user", "2023-05-22", "I bought a Fender guitar for 450 dollars in Lisbon.")
	f.turn("evt_a2", "internal", "s1", 2, "assistant", "", "A Fender guitar is a great choice for blues.")
	f.turn("evt_b1", "external", "s2", 1, "user", "", "My guitar lessons with Marta start on Monday.")
	f.turn("evt_z", "internal", "s3", 1, "user", "", "The cafeteria menu changes on Fridays.")
	f.retrieval("ret_1", "which guitar did I buy "+marker, []string{"evt_a1", "fct_x", "evt_b1", "evt_a2"}, []string{"evt_a1", "evt_b1"})
	f.retrieval("ret_2", "guitar lessons "+marker, []string{"evt_a2", "evt_a1", "evt_b1"}, []string{"evt_a2"})
	f.outcome("out_1", "ret_1", -1)
}

func (f *fixture) items(page string) []string {
	f.t.Helper()
	rows, err := f.e.Store.DB().Query(`SELECT text FROM page_items WHERE page_id=? AND status='active' ORDER BY ordinal`, page)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (f *fixture) cited(item string) []string {
	f.t.Helper()
	rows, err := f.e.Store.DB().Query(`SELECT source_id FROM provenance WHERE derived_type='page_item' AND derived_id=? AND relation='cites' ORDER BY source_id`, item)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestGuidedExtractiveEndToEnd(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	day := f.str(`SELECT substr(recorded_at,1,10) FROM events WHERE id='evt_a2'`)
	s := f.guidedSleeper(nil, nil) // no LLM provider: the extractive digest needs none
	res := f.run(s, RunInput{})
	g := res.Counts.Guided
	if res.Status != StatusCompleted || g == nil || !res.Gate.Ran || res.Gate.Reverted {
		t.Fatalf("run: %s %+v %v gate %+v", res.Status, g, res.Notices, res.Gate)
	}
	if g.Digest != "extractive" || g.CandidateGroups != 2 || g.Groups != 1 || g.GroupsDigested != 1 || g.GroupsSkipped != 0 ||
		g.LinesWritten != 3 || len(g.Fichas) != 1 || res.Cost.LLMCalls != 0 {
		t.Fatalf("counts: %+v cost %+v", g, res.Cost)
	}
	page := g.Fichas[0]
	if f.str(`SELECT kind||'/'||status||'/'||trust_zone||'/'||title FROM pages WHERE id=?`, page) != "summary/active/external/Digest: guitar, fender" {
		t.Fatalf("page: %s", f.str(`SELECT kind||'/'||status||'/'||trust_zone||'/'||title FROM pages WHERE id=?`, page))
	}
	want := []string{
		"2023-05-22 | user | evt_a1 | I bought a Fender guitar for 450 dollars in Lisbon.",
		day + " | assistant | evt_a2 | A Fender guitar is a great choice for blues.",
		day + " | user | evt_b1 | My guitar lessons with Marta start on Monday.",
	}
	if got := f.items(page); !reflect.DeepEqual(got, want) {
		t.Fatalf("items:\n%q\nwant\n%q", got, want)
	}
	if body := f.str(`SELECT body FROM pages WHERE id=?`, page); body != strings.Join(want, "\n\n") {
		t.Fatalf("body %q", body)
	}
	for ord, w := range []struct{ evt, zone string }{{"evt_a1", "internal"}, {"evt_a2", "internal"}, {"evt_b1", "external"}} {
		item := f.str(`SELECT id FROM page_items WHERE page_id=? AND ordinal=?`, page, ord+1)
		if got := f.cited(item); !reflect.DeepEqual(got, []string{w.evt}) {
			t.Errorf("item %d cites %v, want %s", ord+1, got, w.evt)
		}
		if z := f.str(`SELECT trust_zone FROM page_items WHERE id=?`, item); z != w.zone {
			t.Errorf("item %d zone %s want %s", ord+1, z, w.zone)
		}
	}
	if n := f.count(`SELECT count(*) FROM provenance WHERE derived_type='page' AND derived_id=? AND relation='cites'`, page); n != 3 {
		t.Fatalf("page-level cites: %d", n)
	}
	if got := f.str(`SELECT group_concat(event_id) FROM (SELECT event_id FROM event_processing WHERE step=? ORDER BY event_id)`, StepGuidedDigest); got != "evt_a1,evt_a2,evt_b1" {
		t.Fatalf("digested members: %s", got)
	}
	if n := f.count(`SELECT count(*) FROM event_processing WHERE step<>?`, StepGuidedDigest); n != 0 {
		t.Fatal("guided mode runs no uniform extraction or lesson review")
	}
	if n := f.count(`SELECT count(*) FROM embed_queue WHERE record_type='page' AND record_id=?`, page); n != 1 {
		t.Fatal("the ficha page must be queued for embedding")
	}
	if !contains(res.Report, "Phase 2 (guided digest)") || !contains(res.Report, "Lines written: 3") || !contains(res.Report, page) {
		t.Fatalf("report:\n%s", res.Report)
	}
	f.verifyAudit()

	// Second run: every member already digested, the group is skipped.
	res = f.run(s, RunInput{})
	g = res.Counts.Guided
	if g.GroupsSkipped != 1 || g.GroupsDigested != 0 || len(g.Fichas) != 0 || f.count(`SELECT count(*) FROM pages`) != 1 {
		t.Fatalf("second run: %+v", g)
	}
	if !contains(strings.Join(res.Notices, "|"), "nothing to digest") {
		t.Fatalf("notices: %v", res.Notices)
	}
}

func TestGuidedLLMDigestVerifiesLines(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	var shown []string
	fake := &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
		if req.System != systemDigest {
			return llm.FakeResponse{Err: core.Errf(core.CodeInternal, "unexpected prompt")}
		}
		shown = ids(reEventID, req.Prompt)
		return llm.FakeResponse{JSON: `{"lines":[
			{"kind":"line","event_ids":["evt_a1"],"text":"Bought a Fender guitar for 450 dollars in Lisbon."},
			{"kind":"line","event_ids":["evt_a1"],"text":"The guitar cost 500 dollars."},
			{"kind":"line","event_ids":["evt_nope"],"text":"Something else happened."}]}`}
	}}
	s := f.guidedSleeper(fake, func(g *config.GuidedConfig) { g.Digest = config.DigestLLM })
	res := f.run(s, RunInput{})
	g := res.Counts.Guided
	if res.Status != StatusCompleted || res.Cost.LLMCalls != 1 || len(fake.Calls()) != 1 {
		t.Fatalf("run: %s %+v %v", res.Status, res.Cost, res.Notices)
	}
	if !reflect.DeepEqual(shown, []string{"evt_a1", "evt_a2", "evt_b1"}) {
		t.Fatalf("the model sees only the group's turns: %v", shown)
	}
	if g.LinesWritten != 1 || g.GroupsDigested != 1 || len(g.Fichas) != 1 ||
		!reflect.DeepEqual(g.LinesDiscarded, map[string]int{DiscardUnsupportedNumber: 1, DiscardUnknownOrigin: 1}) {
		t.Fatalf("counts: %+v", g)
	}
	if got := f.items(g.Fichas[0]); !reflect.DeepEqual(got, []string{"2023-05-22 | user | evt_a1 | Bought a Fender guitar for 450 dollars in Lisbon."}) {
		t.Fatalf("items %q", got)
	}
	if !contains(res.Report, "Lines discarded: unknown_origin 1, unsupported_number 1") {
		t.Fatalf("report:\n%s", res.Report)
	}
	// Every member is digested, also those no kept line cites.
	if n := f.count(`SELECT count(*) FROM event_processing WHERE step=?`, StepGuidedDigest); n != 3 {
		t.Fatalf("digested: %d", n)
	}
}

func TestGuidedLLMDigestNeedsProvider(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	s := f.guidedSleeper(nil, func(g *config.GuidedConfig) { g.Digest = config.DigestLLM })
	res := f.run(s, RunInput{})
	if res.Gate.Ran || f.count(`SELECT count(*) FROM pages`) != 0 || !contains(strings.Join(res.Notices, "|"), "needs an LLM provider") {
		t.Fatalf("run: %+v %v", res.Gate, res.Notices)
	}
}

// Decision 0142: the guided sleep never reads retrievals.query_text or
// params; a marker planted there reaches no prompt and no ficha.
func TestGuidedNeverLeaksQueryText(t *testing.T) {
	const marker = "QXMARKER7731"
	for _, digest := range []string{config.DigestExtractive, config.DigestLLM} {
		t.Run(digest, func(t *testing.T) {
			f := newFixture(t)
			guidedCorpus(f, marker)
			fake := &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
				ids := ids(reEventID, req.Prompt)
				return llm.FakeResponse{JSON: fmt.Sprintf(`{"lines":[{"kind":"line","event_ids":[%q],"text":"A Fender guitar."}]}`, ids[0])}
			}}
			s := f.guidedSleeper(fake, func(g *config.GuidedConfig) { g.Digest = digest })
			res := f.run(s, RunInput{})
			if len(res.Counts.Guided.Fichas) != 1 {
				t.Fatalf("no ficha: %+v %v", res.Counts.Guided, res.Notices)
			}
			for _, c := range fake.Calls() {
				if contains(c.System, marker) || contains(c.Prompt, marker) {
					t.Fatal("a prompt holds the query text")
				}
			}
			if digest == config.DigestLLM && len(fake.Calls()) != 1 {
				t.Fatalf("llm calls: %d", len(fake.Calls()))
			}
			for _, q := range []string{`SELECT count(*) FROM pages WHERE instr(title||body, ?) > 0`, `SELECT count(*) FROM page_items WHERE instr(text, ?) > 0`,
				`SELECT count(*) FROM sleep_runs WHERE instr(COALESCE(report,'')||COALESCE(outputs,''), ?) > 0`} {
				if n := f.count(q, marker); n != 0 {
					t.Fatalf("%s: the query text leaked", q)
				}
			}
		})
	}
}

// Forgetting an origin event of a ficha removes its item, and the page when
// no item survives (the existing cascade).
func TestGuidedFichaForgetCascade(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	s := f.guidedSleeper(nil, nil)
	res := f.run(s, RunInput{})
	page := res.Counts.Guided.Fichas[0]
	forgetEvent := func(id string) forget.Report {
		t.Helper()
		rep, err := forget.Forget(f.ctx, f.e, ns, forget.Scope{Kind: forget.ScopeRecord, Ref: id}, false, "admin")
		if err != nil {
			t.Fatalf("forget %s: %v %+v", id, err, rep.Verification)
		}
		return rep
	}
	rep := forgetEvent("evt_b1")
	if len(rep.Removed.PageItems) != 1 || !reflect.DeepEqual(rep.PagesRebuild, []string{page}) {
		t.Fatalf("forget evt_b1: items %v rebuild %v", rep.Removed.PageItems, rep.PagesRebuild)
	}
	if f.str(`SELECT status FROM pages WHERE id=?`, page) != "needs_rebuild" {
		t.Fatal("the page leaves reads until rebuilt")
	}
	f.run(s, RunInput{Phases: []int{1}}) // phase 1 rebuilds the page from its surviving items
	got := f.items(page)
	if f.str(`SELECT status FROM pages WHERE id=?`, page) != "active" || len(got) != 2 || contains(strings.Join(got, "\n"), "evt_b1") {
		t.Fatalf("rebuilt page: %q", got)
	}
	forgetEvent("evt_a1")
	rep = forgetEvent("evt_a2")
	if !reflect.DeepEqual(rep.Removed.Pages, []string{page}) {
		t.Fatalf("the page goes with its last item: %+v", rep.Removed)
	}
	if f.count(`SELECT count(*) FROM pages WHERE id=?`, page) != 0 || f.count(`SELECT count(*) FROM page_items WHERE page_id=?`, page) != 0 {
		t.Fatal("page or items survived")
	}
}

// max_groups caps the groups digested per run; the rest waits for the next.
func TestGuidedMaxGroupsLeavesTheRestForTheNextRun(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	f.turn("evt_c1", "internal", "s4", 1, "user", "", "The dentist appointment is on Thursday.")
	f.retrieval("ret_3", "dentist", []string{"evt_c1", "evt_z"}, []string{"evt_c1", "evt_z"})
	s := f.guidedSleeper(nil, func(g *config.GuidedConfig) { g.MaxGroups = 1 })
	res := f.run(s, RunInput{})
	g := res.Counts.Guided
	if res.Status != StatusPartial || g.Groups != 2 || g.GroupsDigested != 1 || g.GroupsLeft != 1 {
		t.Fatalf("first run: %s %+v", res.Status, g)
	}
	// The guitar group has the higher priority (a failure, near misses).
	if f.count(`SELECT count(*) FROM event_processing WHERE step=? AND event_id='evt_a1'`, StepGuidedDigest) != 1 {
		t.Fatal("the higher-priority group goes first")
	}
	res = f.run(s, RunInput{})
	g = res.Counts.Guided
	if res.Status != StatusCompleted || g.GroupsSkipped != 1 || g.GroupsDigested != 1 || g.GroupsLeft != 0 {
		t.Fatalf("second run: %s %+v", res.Status, g)
	}
	if f.count(`SELECT count(*) FROM pages WHERE kind='summary'`) != 2 {
		t.Fatal("two fichas")
	}
}

// The event budget stops the digest; a group never digests partially.
func TestGuidedEventBudget(t *testing.T) {
	f := newFixture(t)
	guidedCorpus(f, "")
	s := f.guidedSleeper(nil, nil)
	res := f.run(s, RunInput{Budget: Budget{Events: 2}})
	g := res.Counts.Guided
	if res.Status != StatusPartial || g.GroupsDigested != 0 || g.GroupsLeft != 1 || f.count(`SELECT count(*) FROM pages`) != 0 {
		t.Fatalf("budget: %s %+v %v", res.Status, g, res.Notices)
	}
}
