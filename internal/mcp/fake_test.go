package mcp

import (
	"context"
	"reflect"
	"sync"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// call is one invocation seen by fakeAPI.
type call struct {
	tool     string
	in       any
	actor    chanda.Actor
	hasActor bool
}

// fakeAPI records calls and answers with canned outputs. Writes carry an
// AuditID equal to their 1-based call number.
type fakeAPI struct {
	mu      sync.Mutex
	calls   []call
	fail    map[string]error // tool -> error to return
	panicOn string           // tool that panics
}

func (f *fakeAPI) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return call{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func record[O any](f *fakeAPI, ctx context.Context, tool string, in any, out O, audited bool) (O, error) {
	f.mu.Lock()
	a, ok := chanda.ActorFrom(ctx)
	f.calls = append(f.calls, call{tool: tool, in: in, actor: a, hasActor: ok})
	n := int64(len(f.calls))
	err := f.fail[tool]
	panicking := f.panicOn == tool
	f.mu.Unlock()
	if panicking {
		panic("fake panic in " + tool)
	}
	if audited {
		reflect.ValueOf(&out).Elem().FieldByName("Meta").FieldByName("AuditID").SetInt(n)
	}
	return out, err // on error the envelope keeps only Meta
}

var sampleItem = chanda.Item{
	ID: "fct_01", Type: chanda.RecFact, Text: "Ana is the owner of Billing", ValidFrom: "2026-01-01T00:00:00.000000000Z",
	Status: "active", Origin: chanda.Origin{SourceURI: "file:///docs/a.md", EventIDs: []string{"evt_01"}},
	Zone: chanda.ZoneInternal, Score: 0.9,
}

var sampleWrite = chanda.WriteResult{ID: "evt_02", Action: chanda.ActionCreated, Reason: "new_event", Flags: []string{}}

func (f *fakeAPI) Context(ctx context.Context, in chanda.ContextInput) (chanda.ContextOutput, error) {
	return record(f, ctx, chanda.ToolContext, in, chanda.ContextOutput{
		RetrievalID: "ret_01", Text: "<memory>Ana is the owner of Billing</memory>", Items: []chanda.Item{sampleItem}, TokensUsed: 9,
	}, false)
}
func (f *fakeAPI) Search(ctx context.Context, in chanda.SearchInput) (chanda.SearchOutput, error) {
	return record(f, ctx, chanda.ToolSearch, in, chanda.SearchOutput{RetrievalID: "ret_02", Items: []chanda.Item{sampleItem}}, false)
}
func (f *fakeAPI) Get(ctx context.Context, in chanda.GetInput) (chanda.GetOutput, error) {
	return record(f, ctx, chanda.ToolGet, in, chanda.GetOutput{
		Item: sampleItem, RecordedAt: "2026-01-01T00:00:00.000000000Z",
		Fact:       &chanda.FactDetail{Subject: "ent_01", Predicate: "owner", Value: "Ana", Confidence: 0.5},
		Provenance: []chanda.Edge{{Relation: "derived_from", SourceType: chanda.RecEvent, SourceID: "evt_01"}},
	}, false)
}
func (f *fakeAPI) Timeline(ctx context.Context, in chanda.TimelineInput) (chanda.TimelineOutput, error) {
	return record(f, ctx, chanda.ToolTimeline, in, chanda.TimelineOutput{Subject: "ent_01", Entries: []chanda.TimelineEntry{}}, false)
}
func (f *fakeAPI) Explain(ctx context.Context, in chanda.ExplainInput) (chanda.ExplainOutput, error) {
	final := 0.5
	return record(f, ctx, chanda.ToolExplain, in, chanda.ExplainOutput{
		RetrievalID: in.RetrievalID, RecordID: in.RecordID, Mode: "search", Candidate: true, Delivered: true, Rank: 1,
		Scores: chanda.StageScores{Final: &final}, Features: map[string]float64{"bm25": 1.5}, Reason: "delivered",
	}, false)
}
func (f *fakeAPI) Stats(ctx context.Context, in chanda.StatsInput) (chanda.StatsOutput, error) {
	return record(f, ctx, chanda.ToolStats, in, chanda.StatsOutput{Namespace: in.Namespace}, false)
}
func (f *fakeAPI) Remember(ctx context.Context, in chanda.RememberInput) (chanda.RememberOutput, error) {
	return record(f, ctx, chanda.ToolRemember, in, chanda.RememberOutput{WriteResult: sampleWrite}, true)
}
func (f *fakeAPI) AssertFact(ctx context.Context, in chanda.AssertFactInput) (chanda.AssertFactOutput, error) {
	return record(f, ctx, chanda.ToolAssertFact, in, chanda.AssertFactOutput{WriteResult: sampleWrite}, true)
}
func (f *fakeAPI) Ingest(ctx context.Context, in chanda.IngestInput) (chanda.IngestOutput, error) {
	return record(f, ctx, chanda.ToolIngest, in, chanda.IngestOutput{Documents: []chanda.DocumentResult{{URI: in.Path, Version: 1, Action: "created", ChunksAdded: 2}}}, true)
}
func (f *fakeAPI) LogSession(ctx context.Context, in chanda.LogSessionInput) (chanda.LogSessionOutput, error) {
	return record(f, ctx, chanda.ToolLogSession, in, chanda.LogSessionOutput{SessionID: in.SessionID, Turns: []chanda.TurnResult{}}, true)
}
func (f *fakeAPI) Outcome(ctx context.Context, in chanda.OutcomeInput) (chanda.OutcomeOutput, error) {
	return record(f, ctx, chanda.ToolOutcome, in, chanda.OutcomeOutput{OutcomeID: "out_01", Credited: len(in.UsedIDs)}, true)
}
func (f *fakeAPI) Flag(ctx context.Context, in chanda.FlagInput) (chanda.FlagOutput, error) {
	return record(f, ctx, chanda.ToolFlag, in, chanda.FlagOutput{FlagID: "flg_01", ID: in.ID, Status: "flagged"}, true)
}
func (f *fakeAPI) Flush(ctx context.Context, in chanda.FlushInput) (chanda.FlushOutput, error) {
	return record(f, ctx, chanda.ToolFlush, in, chanda.FlushOutput{Complete: true}, false)
}
func (f *fakeAPI) Forget(ctx context.Context, in chanda.ForgetInput) (chanda.ForgetOutput, error) {
	return record(f, ctx, chanda.ToolForget, in, chanda.ForgetOutput{
		DryRun: in.DryRun, RootEvents: 1, Removed: chanda.RemovedCounts{Events: 1},
		Verification: chanda.Verification{Passed: true, Probes: []chanda.Probe{{Name: "fts", Passed: true}}},
	}, true)
}
func (f *fakeAPI) Promote(ctx context.Context, in chanda.PromoteInput) (chanda.PromoteOutput, error) {
	return record(f, ctx, chanda.ToolPromote, in, chanda.PromoteOutput{ID: in.ID, From: chanda.ZoneExternal, To: in.Zone}, true)
}
func (f *fakeAPI) SleepRun(ctx context.Context, in chanda.SleepRunInput) (chanda.SleepRunOutput, error) {
	return record(f, ctx, chanda.ToolSleepRun, in, chanda.SleepRunOutput{RunID: "run_01", Status: "completed", Phases: []int{1}}, true)
}
func (f *fakeAPI) Verify(ctx context.Context, in chanda.VerifyInput) (chanda.VerifyOutput, error) {
	return record(f, ctx, chanda.ToolVerify, in, chanda.VerifyOutput{OK: true, Audit: chanda.AuditCheck{OK: true}, Provenance: chanda.ProvenanceCheck{OK: true, Orphans: []string{}}}, false)
}
func (f *fakeAPI) RankingReset(ctx context.Context, in chanda.RankingResetInput) (chanda.RankingResetOutput, error) {
	return record(f, ctx, chanda.ToolRankingReset, in, chanda.RankingResetOutput{}, true)
}
func (f *fakeAPI) LearnedKeys(ctx context.Context, in chanda.LearnedKeysInput) (chanda.LearnedKeysOutput, error) {
	return record(f, ctx, chanda.ToolLearnedKeys, in, chanda.LearnedKeysOutput{Action: in.Action}, true)
}
