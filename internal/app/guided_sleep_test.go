package app

import (
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/sleep"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Decision 0142 on the wired App: retrievals logged by the real read path
// guide the sleep; with no LLM provider the extractive digest writes a
// ficha that passes the provenance checker and is embedded before the run
// ends. The query text reaches no ficha.
func TestGuidedSleepThroughTheApp(t *testing.T) {
	const marker = "zqmarker"
	a := newAppWith(t, Options{}, func(c *config.Config) { c.Sleep.Mode = config.SleepModeGuided })
	fc := a.e.Clock.(interface{ Advance(time.Duration) })
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "s1", Turns: []chanda.Turn{
		{Role: "user", Content: "I bought a Fender guitar for 450 dollars in Lisbon."},
		{Role: "assistant", Content: "A Fender guitar is a great choice for blues."},
	}}))
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "s2", Turns: []chanda.Turn{
		{Role: "user", Content: "The cafeteria menu changes on Fridays."},
	}}))
	fc.Advance(time.Minute)
	must(a.Context(agent, chanda.ContextInput{Query: "Fender guitar price " + marker}))
	fc.Advance(time.Minute)

	out := must(a.SleepRun(cli, chanda.SleepRunInput{}))
	if out.Status != sleep.StatusCompleted || out.Cost.LLMCalls != 0 || !strings.Contains(out.Report, "Phase 2 (guided digest)") {
		t.Fatalf("sleep_run %+v\n%s", out, out.Report)
	}
	if n := count(t, a, `SELECT count(*) FROM pages WHERE kind='summary' AND status='active'`); n != 1 {
		t.Fatalf("%d fichas\n%s", n, out.Report)
	}
	if n := count(t, a, `SELECT count(*) FROM page_items i JOIN pages p ON p.id=i.page_id WHERE p.kind='summary' AND i.text LIKE '% | user | evt_% | I bought a Fender guitar for 450 dollars in Lisbon.'`); n != 1 {
		t.Fatal("the user's turn must be a ficha line")
	}
	if n := count(t, a, `SELECT count(*) FROM embeddings e JOIN pages p ON p.id=e.record_id WHERE p.kind='summary'`); n != 1 {
		t.Fatal("the ficha must be embedded by the end of the run")
	}
	if n := count(t, a, `SELECT count(*) FROM pages WHERE instr(lower(title||body), ?) > 0`, marker); n != 0 {
		t.Fatal("the query text leaked into a ficha")
	}
	verifyOK(t, a)
}
