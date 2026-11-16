package app

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/eval"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/internal/read"
	"github.com/felipemarts/chanda/internal/sleep"
	"github.com/felipemarts/chanda/pkg/chanda"
)

var (
	reEvent = regexp.MustCompile(`(?s)<event id="([^"]+)"[^>]*>\n(.*?)\n</event>`)
	reClaim = regexp.MustCompile(`^(.+?)'s (\w+) is (.+?)\.?$`)
)

// fakeLLM extracts "<Subject>'s <attribute> is <value>." from each event,
// keeps every judged item pending and skips every lesson. It records what
// it was shown.
func fakeLLM(shown *[]string) *llm.Fake {
	return &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
		switch {
		case strings.Contains(req.System, "You extract facts"):
			type fact struct {
				EventIDs    []string `json:"event_ids"`
				Subject     string   `json:"subject"`
				SubjectType string   `json:"subject_type"`
				Predicate   string   `json:"predicate"`
				Value       string   `json:"value"`
				ValidFrom   string   `json:"valid_from"`
			}
			out := struct {
				Facts []fact `json:"facts"`
			}{Facts: []fact{}}
			for _, m := range reEvent.FindAllStringSubmatch(req.Prompt, -1) {
				if shown != nil {
					*shown = append(*shown, m[2])
				}
				if c := reClaim.FindStringSubmatch(strings.TrimSpace(m[2])); c != nil {
					out.Facts = append(out.Facts, fact{EventIDs: []string{m[1]}, Subject: c[1], Predicate: c[2], Value: c[3]})
				}
			}
			b, _ := json.Marshal(out)
			return llm.FakeResponse{JSON: string(b)}
		case strings.Contains(req.System, "lessons learned"):
			return llm.FakeResponse{JSON: `{"skip":true,"page_id":"","title":"","item":""}`}
		default:
			return llm.FakeResponse{JSON: `{"verdicts":[]}`}
		}
	}}
}

// BASE.md "Sleep": sleep_run runs both phases on the wired App: facts
// extracted by the LLM go through the write path's assert with provenance,
// the gate measures the approved gate set, the report and counts come back.
// fact_assertion events are never sent for extraction.
func TestSleepRunThroughTheApp(t *testing.T) {
	var shown []string
	a := newAppWith(t, Options{LLM: fakeLLM(&shown)}, nil)
	fc := a.e.Clock.(interface{ Advance(time.Duration) })
	must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Atlas", Predicate: "status", Value: "green"}))
	r := must(a.Remember(agent, chanda.RememberInput{Content: "Marina's role is tech lead."}))
	must(a.Remember(agent, chanda.RememberInput{Content: "The deploy runs every night."}))
	fc.Advance(time.Minute)

	out, err := a.SleepRun(cli, chanda.SleepRunInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != sleep.StatusCompleted || out.Created != 1 || out.AuditID == 0 || !slices.Equal(out.Phases, []int{1, 2}) {
		t.Fatalf("sleep_run %+v", out)
	}
	if !strings.Contains(out.Report, "Events extracted: 2") || out.Cost.LLMCalls == 0 {
		t.Fatalf("report %s", out.Report)
	}
	if !hasNotice(out.Notices, "gate: no approved gate questions") {
		t.Fatalf("an empty gate set must be reported: %v", out.Notices)
	}
	for _, s := range shown {
		if strings.Contains(s, "status: green") {
			t.Fatal("a fact_assertion event was sent for extraction")
		}
	}
	tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Marina", Predicate: "role"}))
	if len(tl.Entries) != 1 || tl.Entries[0].Value != "tech lead" || !slices.Equal(tl.Entries[0].Origin.EventIDs, []string{r.ID}) {
		t.Fatalf("timeline %+v", tl)
	}
	if n := count(t, a, `SELECT count(*) FROM gate_questions WHERE status = 'proposed'`); n != 1 {
		t.Fatalf("%d gate questions proposed, want 1 (the asserted fact of the internal zone... or user)", n)
	}
	if n := count(t, a, `SELECT count(*) FROM embed_queue`); n != 0 {
		t.Fatalf("%d records still queued after sleep", n)
	}
	verifyOK(t, a)

	a2 := newAppWith(t, Options{DisableSleep: true}, nil)
	if _, err := a2.SleepRun(cli, chanda.SleepRunInput{}); core.CodeOf(err) != core.CodeUnavailable {
		t.Fatalf("disabled sleep: %v", err)
	}
}

// Decision 0061: the gate measures the real gate set inside the
// transaction; a change that loses an approved answer is reverted.
func TestSleepGateRevertsARegression(t *testing.T) {
	a := newAppWith(t, Options{LLM: fakeLLM(nil)}, nil)
	fc := a.e.Clock.(interface{ Advance(time.Duration) })
	f := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Atlas", Predicate: "owner", Value: "Marina Duarte"}))
	_, _, err := a.GateAdd(cli, GateAddInput{Question: "Atlas owner", Expected: sleep.GateExpected{RecordIDs: []string{f.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	must(a.Remember(agent, chanda.RememberInput{Content: "Atlas's owner is Bruno Lima."}))
	fc.Advance(time.Minute)
	out := must(a.SleepRun(cli, chanda.SleepRunInput{}))
	if out.Status != sleep.StatusReverted || out.Created != 0 {
		t.Fatalf("a superseded gate answer must revert the run: %+v\n%s", out, out.Report)
	}
	if v := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Atlas", Predicate: "owner"})); len(v.Entries) != 1 {
		t.Fatalf("staged fact survived the revert: %+v", v.Entries)
	}
	verifyOK(t, a)
}

// BASE.md open question "who writes the gate set" (decision 0092): sleep
// proposes, only a human approves or retires.
func TestGateReviewNeedsAHuman(t *testing.T) {
	a := newAppWith(t, Options{}, nil)
	fc := a.e.Clock.(interface{ Advance(time.Duration) })
	must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Atlas", Predicate: "owner", Value: "Marina"}))
	fc.Advance(time.Minute)
	must(a.SleepRun(cli, chanda.SleepRunInput{Phases: []int{1}}))
	qs := must(a.GateList(cli, "", GateProposed))
	if len(qs) != 1 || qs[0].Question != "What is Atlas owner?" {
		t.Fatalf("proposed %+v", qs)
	}
	if _, err := a.GateReview(agent, GateReviewInput{ID: qs[0].ID, Status: GateApproved}); core.CodeOf(err) != core.CodePermissionDenied {
		t.Fatalf("agent approved the gate set: %v", err)
	}
	r := must(a.GateReview(cli, GateReviewInput{ID: qs[0].ID, Status: GateApproved}))
	if r.From != GateProposed || r.To != GateApproved || r.AuditID == 0 {
		t.Fatalf("review %+v", r)
	}
	if _, err := a.GateReview(cli, GateReviewInput{ID: qs[0].ID, Status: GateApproved}); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("approving twice: %v", err)
	}
	if _, _, err := a.GateAdd(agent, GateAddInput{Question: "x"}); core.CodeOf(err) != core.CodePermissionDenied {
		t.Fatalf("agent wrote the gate set: %v", err)
	}
	if n := len(must(a.GateList(cli, "", GateApproved))); n != 1 {
		t.Fatalf("%d approved", n)
	}
	// The approved question scores 100 on the current memory.
	if s := must(gateEvaluator{a}.Score(bg, a.e.Store.DB(), "ns")); s != 100 {
		t.Fatalf("gate score %v", s)
	}
	verifyOK(t, a)
}

// evalSystem shows the App's read-only scoring path to internal/eval.
type evalSystem struct{ a *App }

func (evalSystem) Name() string                            { return "app" }
func (evalSystem) Ingest(context.Context, eval.Item) error { return nil }
func (evalSystem) Close() error                            { return nil }
func (s evalSystem) Retrieve(ctx context.Context, q eval.Query, budget int) ([]eval.Retrieved, error) {
	r, err := s.a.reader.Score(ctx, read.ScoreInput{Namespace: "ns", Query: q.Text, AsOf: q.AsOf, BudgetTokens: budget}, read.ScoreOptions{})
	var out []eval.Retrieved
	for _, it := range r.Items {
		out = append(out, eval.Retrieved{ID: it.ID, Text: item(it).Text})
	}
	return out, err
}

// The sleep gate and the evaluation harness use the same metric: on one
// fixture, gateScore equals eval.GateScoreBudget.
func TestGateScoreAgreesWithEval(t *testing.T) {
	a := newAppWith(t, Options{}, nil)
	f1 := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Atlas", Predicate: "owner", Value: "Marina Duarte", ValidFrom: "2026-01-01"}))
	must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Atlas", Predicate: "owner", Value: "Bruno Lima", ValidFrom: "2026-03-01"}))
	e1 := must(a.Remember(agent, chanda.RememberInput{Content: "The Atlas deploy runs every night at two in the morning."}))
	must(a.Remember(agent, chanda.RememberInput{Content: "Lunch at the Recife office is served at noon."}))
	must(a.Flush(cli, chanda.FlushInput{}))
	qs := []GateQuestion{
		{ID: "g1", Question: "Atlas owner", Expected: sleep.GateExpected{RecordIDs: []string{f1.ID}, Answers: []string{"Marina Duarte"}, AsOf: "2026-02-01T00:00:00Z"}},
		{ID: "g2", Question: "Atlas owner", Expected: sleep.GateExpected{Answers: []string{"Bruno"}}},
		{ID: "g3", Question: "when does the Atlas deploy run", Expected: sleep.GateExpected{RecordIDs: []string{e1.ID, "evt_missing"}}},
		{ID: "g4", Question: "quantum chromodynamics lattice", Expected: sleep.GateExpected{}},
		{ID: "g5", Question: "Atlas", Expected: sleep.GateExpected{Answers: []string{"Not There"}}},
	}
	var eqs []eval.GateQuestion
	for _, q := range qs {
		eqs = append(eqs, eval.GateQuestion{ID: q.ID, Question: q.Question, Expected: eval.GateExpected{
			RecordIDs: q.Expected.RecordIDs, Answers: q.Expected.Answers, AsOf: q.Expected.AsOf}})
	}
	want, err := eval.GateScoreBudget(bg, evalSystem{a}, eqs, a.e.Cfg.Read.ContextDefaultTokens)
	if err != nil {
		t.Fatal(err)
	}
	got := must(a.gateScore(bg, "ns", qs, read.ScoreOptions{}))
	if got != want || got <= 0 || got >= 100 {
		t.Fatalf("gateScore %v, eval.GateScore %v", got, want)
	}
	for i := range qs {
		w := must(eval.GateScoreBudget(bg, evalSystem{a}, eqs[i:i+1], a.e.Cfg.Read.ContextDefaultTokens))
		g := must(a.gateScore(bg, "ns", qs[i:i+1], read.ScoreOptions{}))
		if g != w {
			t.Errorf("%s: gateScore %v, eval %v", qs[i].ID, g, w)
		}
	}

}
