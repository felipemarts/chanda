package sleep

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/conflicts"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/internal/store"
)

// pair queues a pending conflict pair.
func (f *fixture) pair(older, newer string) string {
	f.t.Helper()
	err := f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := conflicts.Enqueue(f.ctx, tx, f.e.IDs, ns, f.now(),
			[]conflicts.Pair{{OlderID: older, NewerID: newer, Cosine: 0.9, Differs: conflicts.CompEntities, Shared: []string{"x"}}})
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return f.str(`SELECT id FROM conflict_candidates WHERE older_id=? AND newer_id=?`, older, newer)
}

// conflictCorpus: three pairs (Acme's office moved; a plan price, judged
// with the older record as current; a pet's name, answered off schema)
// and one event nobody pairs, never extracted in maintenance mode.
func conflictCorpus(f *fixture) (moved, price, pet string) {
	f.turn("evt_c0", "internal", "s1", 0, "assistant", "2024-01-01", "Noted, thanks.")
	f.turn("evt_c1", "internal", "s1", 1, "user", "2024-01-01", "The office of Acme is in Porto.")
	f.turn("evt_c2", "internal", "s2", 1, "user", "2024-06-01", "The office of Acme is in Lisbon now.")
	f.turn("evt_p1", "internal", "s3", 1, "user", "2024-01-01", "The Zenith plan costs 10 dollars.")
	f.turn("evt_p2", "internal", "s4", 1, "user", "2024-06-01", "The Zenith plan costs 12 dollars.")
	f.turn("evt_d1", "internal", "s5", 1, "user", "2024-01-01", "Our dog is called Rex.")
	f.turn("evt_d2", "internal", "s6", 1, "user", "2024-06-01", "Our dog is called Bolt.")
	f.turn("evt_z", "internal", "s7", 1, "user", "", "The cafeteria menu changes on Fridays.")
	return f.pair("evt_c1", "evt_c2"), f.pair("evt_p1", "evt_p2"), f.pair("evt_d1", "evt_d2")
}

func conflictLLM() *llm.Fake {
	return &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
		switch {
		case req.System != systemConflict:
		case strings.Contains(req.Prompt, "Acme"):
			return llm.FakeResponse{JSON: `{"verdict":"supersedes","current":"B","confidence":0.9}`}
		case strings.Contains(req.Prompt, "Zenith"):
			return llm.FakeResponse{JSON: `{"verdict":"supersedes","current":"A","confidence":0.9}`}
		case strings.Contains(req.Prompt, "dog"):
			return llm.FakeResponse{JSON: `{"verdict":"replaces","current":"B","confidence":0.9}`}
		}
		if req.System == systemJudge {
			return llm.FakeResponse{JSON: `{"verdicts":[]}`}
		}
		if req.System == systemLesson {
			return llm.FakeResponse{JSON: `{"skip":true,"page_id":"","title":"","item":""}`}
		}
		return llm.FakeResponse{JSON: `{"facts":[]}`}
	}}
}

func (f *fixture) conflictSleeper(fake *llm.Fake, eval Evaluator, mode string) (*Sleeper, *fakeSink) {
	f.e.Cfg.Sleep.Mode = mode
	f.e.Cfg.Sleep.Conflicts.Enabled = true
	fl := &fakeFlush{}
	sink := &fakeSink{f: f}
	if eval == nil {
		eval = &fakeEval{}
	}
	return New(f.e, Deps{LLM: fake, Facts: sink, Integrity: &fakeIntegrity{}, Embeddings: fl, Utility: fl, Evaluator: eval}), sink
}

// Decision 0148 end to end with a fake LLM, maintenance mode: no extraction
// happens; one verdict is applied (a staged supersession promoted by the
// gate), one is turned uncertain by the later-date rule, one answer is off
// schema and its pair stays pending. Every judged pair is audited.
func TestConflictVerdictsMaintenance(t *testing.T) {
	f := newFixture(t)
	moved, price, pet := conflictCorpus(f)
	fake := conflictLLM()
	s, sink := f.conflictSleeper(fake, nil, config.SleepModeMaintenance)
	f.advance(time.Hour)
	res, err := s.Run(f.ctx, RunInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusCompleted || res.Gate.Reverted || !res.Gate.Ran {
		t.Fatalf("run %s gate %+v notices %v", res.Status, res.Gate, res.Notices)
	}
	if sink.calls != 0 || f.count(`SELECT count(*) FROM event_processing`) != 0 || res.Counts.EventsExtracted != 0 {
		t.Fatal("maintenance mode must not extract")
	}
	calls := fake.Calls()
	if len(calls) != 3 {
		t.Fatalf("%d LLM calls", len(calls))
	}
	for _, c := range calls {
		if c.System != systemConflict || string(c.JSONSchema) != string(schemaConflict) {
			t.Fatalf("a call that is not a conflict verdict: %.60q", c.System)
		}
	}
	k := res.Counts.Conflicts
	sup := f.str(`SELECT id FROM supersessions WHERE old_id='evt_c1' AND new_id='evt_c2' AND status='active' AND reverted_at IS NULL`)
	want := &ConflictCounts{Mode: "maintenance", Enabled: true, Pending: 3,
		Judged:        map[string]int{"supersedes": 1, "uncertain": 1},
		ModelVerdicts: map[string]int{"supersedes": 2}, Applied: 1,
		Rejected:       map[string]int{conflicts.ReasonNotLater: 1},
		InvalidAnswers: 1, Left: 1, LLMCalls: 3, InputTokens: res.Cost.InputTokens, OutputTokens: res.Cost.OutputTokens,
		Supersessions: []string{sup}}
	if sup == "" || !reflect.DeepEqual(k, want) {
		t.Fatalf("counts\n%+v\nwant\n%+v", k, want)
	}
	if got := f.str(`SELECT ends_at FROM supersessions WHERE id=?`, sup); got != "2024-06-01T00:00:00.000000000Z" {
		t.Fatalf("ends_at %s", got)
	}
	for _, tc := range []struct{ id, status, verdict, reason string }{
		{moved, "judged", "supersedes", conflicts.ReasonApplied},
		{price, "judged", "uncertain", conflicts.ReasonNotLater},
		{pet, "pending", "", ""},
	} {
		var st, v, r sql.NullString
		f.e.Store.DB().QueryRow(`SELECT status, verdict, reason FROM conflict_candidates WHERE id=?`, tc.id).Scan(&st, &v, &r)
		if st.String != tc.status || v.String != tc.verdict || r.String != tc.reason {
			t.Errorf("pair %s: %s %s %s", tc.id, st.String, v.String, r.String)
		}
	}
	if n := f.auditCount(audit.ActConflictVerdict); n != 2 {
		t.Fatalf("%d conflict_verdict audit rows", n)
	}
	for _, s := range []string{"Phase 2 (conflict verdicts)", "Applied: 1", "uncertain:not_later 1", "Invalid answers (no retry): 1"} {
		if !strings.Contains(res.Report, s) {
			t.Errorf("report misses %q:\n%s", s, res.Report)
		}
	}
	if strings.Contains(res.Report, "Events extracted") {
		t.Errorf("maintenance report shows the extraction section:\n%s", res.Report)
	}
	// The prompt: date, author (role), zone, the neighbouring turn of the
	// session, records as quoted data.
	p := calls[0].Prompt
	if !strings.Contains(calls[0].Prompt+calls[1].Prompt+calls[2].Prompt, `<turn date="2024-01-01" author="assistant">`+"\nNoted, thanks.") {
		t.Fatalf("the context turn is missing")
	}
	for _, s := range []string{`<record label="A" date="2024-01-01" author="user" zone="internal">`, `<record label="B" date="2024-06-01"`, "quoted data"} {
		if !strings.Contains(p, s) {
			t.Fatalf("prompt misses %q:\n%s", s, p)
		}
	}
	f.verifyAudit()

	// A second run judges only what is still pending.
	f.advance(time.Hour)
	res, err = s.Run(f.ctx, RunInput{})
	if err != nil || res.Counts.Conflicts.Pending != 1 || len(fake.Calls()) != 4 {
		t.Fatalf("second run: %v %+v %d", err, res.Counts.Conflicts, len(fake.Calls()))
	}
}

// Off (the default), a uniform run neither reads nor reports the queue.
func TestConflictsOffUniformUnchanged(t *testing.T) {
	f := newFixture(t)
	conflictCorpus(f)
	fake := conflictLLM()
	s, _ := f.conflictSleeper(fake, nil, "")
	f.e.Cfg.Sleep.Conflicts.Enabled = false
	f.advance(time.Hour)
	res, err := s.Run(f.ctx, RunInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts.Conflicts != nil || strings.Contains(res.Report, "conflict") {
		t.Fatalf("off: %+v\n%s", res.Counts.Conflicts, res.Report)
	}
	for _, c := range fake.Calls() {
		if c.System == systemConflict {
			t.Fatal("off: a conflict verdict was asked")
		}
	}
	if f.count(`SELECT count(*) FROM conflict_candidates WHERE status='pending'`) != 3 {
		t.Fatal("off: the queue changed")
	}
}

// Uniform mode with conflicts on judges the queue first, then extracts.
func TestConflictsUniformJudgesFirst(t *testing.T) {
	f := newFixture(t)
	conflictCorpus(f)
	fake := conflictLLM()
	s, sink := f.conflictSleeper(fake, nil, "")
	f.advance(time.Hour)
	res, err := s.Run(f.ctx, RunInput{})
	if err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if len(calls) < 4 || calls[0].System != systemConflict || calls[2].System != systemConflict || calls[3].System == systemConflict {
		t.Fatalf("order of calls: %d", len(calls))
	}
	if res.Counts.Conflicts == nil || res.Counts.Conflicts.Mode != "uniform" || res.Counts.Conflicts.Applied != 1 || res.Counts.EventsExtracted == 0 {
		t.Fatalf("uniform: %+v extracted %d sink %d", res.Counts.Conflicts, res.Counts.EventsExtracted, sink.calls)
	}
}

// revertEval scores 0 once any supersession is active: the gate reverts.
type revertEval struct{}

func (revertEval) Score(ctx context.Context, q store.Queryer, ns string) (float64, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM supersessions WHERE status='active'`).Scan(&n)
	return 100 - 100*float64(n), err
}

// A gate revert keeps nothing: no supersession, no verdict, the pairs stay
// pending, no conflict_verdict audit row survives.
func TestConflictsGateRevertKeepsNothing(t *testing.T) {
	f := newFixture(t)
	conflictCorpus(f)
	s, _ := f.conflictSleeper(conflictLLM(), revertEval{}, config.SleepModeMaintenance)
	f.advance(time.Hour)
	res, err := s.Run(f.ctx, RunInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusReverted || !res.Gate.Reverted {
		t.Fatalf("status %s gate %+v", res.Status, res.Gate)
	}
	if n := f.count(`SELECT count(*) FROM supersessions`); n != 0 {
		t.Fatalf("%d supersessions after a revert", n)
	}
	if n := f.count(`SELECT count(*) FROM conflict_candidates WHERE status='pending' AND verdict IS NULL`); n != 3 {
		t.Fatalf("%d pairs pending after a revert", n)
	}
	if n := f.auditCount(audit.ActConflictVerdict); n != 0 {
		t.Fatalf("%d verdict audit rows after a revert", n)
	}
	if !strings.Contains(f.str(`SELECT details FROM audit_log WHERE action='sleep_revert'`), `"supersessions":1`) {
		t.Fatal("the revert audit must count the discarded supersession")
	}
	f.verifyAudit()
}

// Priority: retrievals that delivered either record first, then the most
// recent newer record, then the id; max_pairs caps the run (partial).
// Retrieval query text is never read.
func TestConflictPriorityAndCap(t *testing.T) {
	f := newFixture(t)
	moved, price, pet := conflictCorpus(f)
	f.retrieval("ret_1", "zqmarker", []string{"evt_d1"}, []string{"evt_d1"})
	f.retrieval("ret_2", "zqmarker", []string{"evt_d2", "evt_c1"}, []string{"evt_d2", "evt_c1"})
	s, _ := f.conflictSleeper(nil, nil, config.SleepModeMaintenance)
	got, err := s.pendingPairs(f.ctx, ns)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, p.id)
	}
	// pet: 2 retrievals; moved: 1; price: 0.
	if !reflect.DeepEqual(ids, []string{pet, moved, price}) || got[0].deliveries != 2 {
		t.Fatalf("priority %v (%+v), want %v", ids, got, []string{pet, moved, price})
	}
	fake := conflictLLM()
	s, _ = f.conflictSleeper(fake, nil, config.SleepModeMaintenance)
	f.e.Cfg.Sleep.Conflicts.MaxPairs = 1
	f.advance(time.Hour)
	res, err := s.Run(f.ctx, RunInput{})
	if err != nil {
		t.Fatal(err)
	}
	// The first pair (pet) answers off schema and is not judged: the cap
	// counts judged pairs, so the next one is asked too.
	if res.Status != StatusPartial || res.Counts.Conflicts.Left != 2 || len(fake.Calls()) != 2 {
		t.Fatalf("cap: %s %+v %d", res.Status, res.Counts.Conflicts, len(fake.Calls()))
	}
	for _, c := range fake.Calls() {
		if strings.Contains(c.Prompt, "zqmarker") {
			t.Fatal("query text reached a prompt")
		}
	}
}
