package app

import (
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// conflictFake answers conflict verdicts by topic: the Acme office moved
// (applied), the Zenith price names the older record as current (the
// later-date rule makes it uncertain), the dog's name gets an answer off
// the schema (the pair stays pending). Other prompts get empty answers.
func conflictFake() *llm.Fake {
	return &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
		if !strings.HasPrefix(req.System, "You judge pairs") {
			return llm.FakeResponse{JSON: `{"facts":[]}`}
		}
		switch {
		case strings.Contains(req.Prompt, "Acme"):
			return llm.FakeResponse{JSON: `{"verdict":"supersedes","current":"B","confidence":0.9}`}
		case strings.Contains(req.Prompt, "Zenith"):
			return llm.FakeResponse{JSON: `{"verdict":"supersedes","current":"A","confidence":0.9}`}
		case strings.Contains(req.Prompt, "dog"):
			return llm.FakeResponse{JSON: `{"verdict":"replaces","current":"B","confidence":0.9}`}
		}
		return llm.FakeResponse{JSON: `{"verdict":"unrelated","current":"A","confidence":0.5}`}
	}}
}

type conflictWorld struct {
	a                     *App
	fake                  *llm.Fake
	porto, lisbon         string
	price10, price12, dog string
	sleep                 chanda.SleepRunOutput
}

// conflictScenario writes the three updates through the API (write-time
// detection queues them), then runs a maintenance sleep with the fake LLM.
func conflictScenario(t *testing.T, mut func(*config.Config)) *conflictWorld {
	t.Helper()
	fake := conflictFake()
	a := newAppWith(t, Options{LLM: fake}, func(c *config.Config) {
		conflictsOn(c)
		c.Sleep.Mode = config.SleepModeMaintenance
		if mut != nil {
			mut(c)
		}
	})
	w := &conflictWorld{a: a, fake: fake}
	w.porto = remember(t, a, "The office of Acme is in Porto.", "2024-01-01")
	w.price10 = remember(t, a, "The Zenith plan costs 10 dollars.", "2024-01-01")
	w.dog = remember(t, a, "Our dog is called Rex.", "2024-01-01")
	w.lisbon = remember(t, a, "The office of Acme is in Lisbon now.", "2024-06-01")
	w.price12 = remember(t, a, "The Zenith plan costs 12 dollars.", "2024-06-01")
	remember(t, a, "Our dog is called Bolt.", "2024-06-01")
	must(a.Flush(cli, chanda.FlushInput{}))
	a.e.Clock.(interface{ Advance(time.Duration) }).Advance(time.Hour)
	w.sleep = must(a.SleepRun(cli, chanda.SleepRunInput{}))
	return w
}

func ids(items []chanda.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// Decision 0148 end to end through the App, maintenance mode: no
// extraction; one supersession applied, one verdict made uncertain by the
// later-date rule, one invalid answer; then the read effects: context
// drops the outdated record (logged as superseded), search hides it unless
// include_history (then it says by what), an as_of before the end date sees
// it as current, the timeline shows the chain.
func TestConflictsEndToEnd(t *testing.T) {
	w := conflictScenario(t, nil)
	a, out := w.a, w.sleep
	k := out.Conflicts
	if out.Status != "completed" || k == nil || k.Mode != "maintenance" || k.Applied != 1 || k.Rejected["uncertain:not_later"] != 1 ||
		k.InvalidAnswers != 1 || k.LLMCalls != out.Cost.LLMCalls || len(k.Supersessions) != 1 {
		t.Fatalf("sleep_run %+v %+v\n%s", out, k, out.Report)
	}
	for _, c := range w.fake.Calls() {
		if !strings.HasPrefix(c.System, "You judge pairs") {
			t.Fatal("maintenance mode made a call that is not a conflict verdict")
		}
	}
	if n := count(t, a, `SELECT count(*) FROM event_processing`); n != 0 || out.Created != 0 {
		t.Fatalf("maintenance mode extracted: %d processed, %d created", n, out.Created)
	}

	const q = "Where is the office of Acme?"
	ctx := must(a.Context(agent, chanda.ContextInput{Query: q}))
	got := strings.Join(ids(ctx.Items), ",")
	if strings.Contains(got, w.porto) || !strings.Contains(got, w.lisbon) {
		t.Fatalf("context items %s (porto %s, lisbon %s)", got, w.porto, w.lisbon)
	}
	if cands := str(t, a, `SELECT candidates FROM retrievals WHERE id = ?`, ctx.RetrievalID); !strings.Contains(cands,
		`"id":"`+w.porto+`"`) || !strings.Contains(cands, `"dropped":"superseded"`) {
		t.Fatalf("the drop must be logged as superseded: %s", cands)
	}
	s := must(a.Search(agent, chanda.SearchInput{Query: q, Limit: 10}))
	if find(s.Items, w.porto) != nil || find(s.Items, w.lisbon) == nil {
		t.Fatalf("search must hide the outdated record: %v", ids(s.Items))
	}
	h := must(a.Search(agent, chanda.SearchInput{Query: q, Limit: 10, IncludeHistory: true}))
	old := find(h.Items, w.porto)
	if old == nil || old.Status != "superseded" || old.SupersededBy != w.lisbon || old.ValidTo != "2024-06-01T00:00:00.000000000Z" {
		t.Fatalf("include_history: %+v", old)
	}
	if cur := find(h.Items, w.lisbon); cur == nil || cur.SupersededBy != "" || cur.Status != "" {
		t.Fatalf("the current record: %+v", cur)
	}
	past := must(a.Context(agent, chanda.ContextInput{Query: q, AsOf: "2024-03-01"}))
	if find(past.Items, w.porto) == nil || find(past.Items, w.lisbon) != nil {
		t.Fatalf("as_of before the end date: %v", ids(past.Items))
	}
	ps := must(a.Search(agent, chanda.SearchInput{Query: q, Limit: 10, IncludeHistory: true, Filters: chanda.SearchFilters{AsOf: "2024-03-01"}}))
	if p := find(ps.Items, w.porto); p == nil || p.SupersededBy != "" {
		t.Fatalf("as_of before the end date, the old record is current: %+v", p)
	}
	later := must(a.Context(agent, chanda.ContextInput{Query: q, AsOf: "2024-07-01"}))
	if find(later.Items, w.porto) != nil {
		t.Fatalf("as_of after the end date: %v", ids(later.Items))
	}

	// Timeline: the subject's chain.
	must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Acme", Predicate: "industry", Value: "software"}))
	tl := must(a.Timeline(agent, chanda.TimelineInput{Subject: "Acme"}))
	if len(tl.Supersessions) != 1 || tl.Supersessions[0].OldID != w.porto || tl.Supersessions[0].NewID != w.lisbon ||
		tl.Supersessions[0].EndsAt != "2024-06-01T00:00:00.000000000Z" || tl.Supersessions[0].SupersessionID != k.Supersessions[0] {
		t.Fatalf("timeline %+v", tl.Supersessions)
	}
	if tl := must(a.Timeline(agent, chanda.TimelineInput{Subject: "Acme", AsOf: "2024-03-01"})); len(tl.Supersessions) != 0 {
		t.Fatalf("timeline as_of before the end date: %+v", tl.Supersessions)
	}
	verifyOK(t, a)
}

// Off, reads of the same database ignore supersessions: the outdated
// record is back, and the item carries no supersession field.
func TestConflictsReadsOffAreUnchanged(t *testing.T) {
	w := conflictScenario(t, nil)
	a := w.a
	const q = "Where is the office of Acme?"
	a.e.Cfg.Sleep.Conflicts.Enabled = false
	off := must(a.Search(agent, chanda.SearchInput{Query: q, Limit: 10, IncludeHistory: true}))
	p := find(off.Items, w.porto)
	if p == nil || p.SupersededBy != "" || p.Status != "" {
		t.Fatalf("off: %+v", p)
	}
	tl := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Acme", Predicate: "industry", Value: "software"}))
	_ = tl
	if x := must(a.Timeline(agent, chanda.TimelineInput{Subject: "Acme"})); x.Supersessions != nil {
		t.Fatalf("off: timeline %+v", x.Supersessions)
	}
}
