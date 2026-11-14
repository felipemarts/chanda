package chanda

import "context"

// API is Chanda's public interface: one method per MCP tool.
//
// Implementations must:
//   - default an empty Namespace to the configured one, and filter by
//     namespace again before responding;
//   - read the caller from the context with [ActorFrom] and record its Name in
//     the audit log of every mutation; Promote must refuse a non-human actor
//     with CodePermissionDenied;
//   - return errors that carry a stable [Code];
//   - set Meta.AuditID on outputs of calls that wrote an audit entry.
type API interface {
	// Read profile.
	Context(ctx context.Context, in ContextInput) (ContextOutput, error)
	Search(ctx context.Context, in SearchInput) (SearchOutput, error)
	Get(ctx context.Context, in GetInput) (GetOutput, error)
	Timeline(ctx context.Context, in TimelineInput) (TimelineOutput, error)
	Explain(ctx context.Context, in ExplainInput) (ExplainOutput, error)
	Stats(ctx context.Context, in StatsInput) (StatsOutput, error)

	// Standard profile.
	Remember(ctx context.Context, in RememberInput) (RememberOutput, error)
	AssertFact(ctx context.Context, in AssertFactInput) (AssertFactOutput, error)
	Ingest(ctx context.Context, in IngestInput) (IngestOutput, error)
	LogSession(ctx context.Context, in LogSessionInput) (LogSessionOutput, error)
	Outcome(ctx context.Context, in OutcomeInput) (OutcomeOutput, error)
	Flag(ctx context.Context, in FlagInput) (FlagOutput, error)
	Flush(ctx context.Context, in FlushInput) (FlushOutput, error)

	// Administration profile.
	Forget(ctx context.Context, in ForgetInput) (ForgetOutput, error)
	Promote(ctx context.Context, in PromoteInput) (PromoteOutput, error)
	SleepRun(ctx context.Context, in SleepRunInput) (SleepRunOutput, error)
	Verify(ctx context.Context, in VerifyInput) (VerifyOutput, error)
	RankingReset(ctx context.Context, in RankingResetInput) (RankingResetOutput, error)
}

// Unwired is an API whose every method fails with Err, or with a
// CodeUnavailable error when Err is nil. It lets a shell (the MCP server, the
// CLI) start before the core is wired, and is a convenient base to embed in
// test fakes that override only some methods.
type Unwired struct{ Err error }

var _ API = Unwired{}

func (u Unwired) err(tool string) error {
	if u.Err != nil {
		return u.Err
	}
	return Errf(CodeUnavailable, "%s: chanda core is not wired into this build", tool)
}

func (u Unwired) Context(context.Context, ContextInput) (ContextOutput, error) {
	return ContextOutput{}, u.err(ToolContext)
}
func (u Unwired) Search(context.Context, SearchInput) (SearchOutput, error) {
	return SearchOutput{}, u.err(ToolSearch)
}
func (u Unwired) Get(context.Context, GetInput) (GetOutput, error) {
	return GetOutput{}, u.err(ToolGet)
}
func (u Unwired) Timeline(context.Context, TimelineInput) (TimelineOutput, error) {
	return TimelineOutput{}, u.err(ToolTimeline)
}
func (u Unwired) Explain(context.Context, ExplainInput) (ExplainOutput, error) {
	return ExplainOutput{}, u.err(ToolExplain)
}
func (u Unwired) Stats(context.Context, StatsInput) (StatsOutput, error) {
	return StatsOutput{}, u.err(ToolStats)
}
func (u Unwired) Remember(context.Context, RememberInput) (RememberOutput, error) {
	return RememberOutput{}, u.err(ToolRemember)
}
func (u Unwired) AssertFact(context.Context, AssertFactInput) (AssertFactOutput, error) {
	return AssertFactOutput{}, u.err(ToolAssertFact)
}
func (u Unwired) Ingest(context.Context, IngestInput) (IngestOutput, error) {
	return IngestOutput{}, u.err(ToolIngest)
}
func (u Unwired) LogSession(context.Context, LogSessionInput) (LogSessionOutput, error) {
	return LogSessionOutput{}, u.err(ToolLogSession)
}
func (u Unwired) Outcome(context.Context, OutcomeInput) (OutcomeOutput, error) {
	return OutcomeOutput{}, u.err(ToolOutcome)
}
func (u Unwired) Flag(context.Context, FlagInput) (FlagOutput, error) {
	return FlagOutput{}, u.err(ToolFlag)
}
func (u Unwired) Flush(context.Context, FlushInput) (FlushOutput, error) {
	return FlushOutput{}, u.err(ToolFlush)
}
func (u Unwired) Forget(context.Context, ForgetInput) (ForgetOutput, error) {
	return ForgetOutput{}, u.err(ToolForget)
}
func (u Unwired) Promote(context.Context, PromoteInput) (PromoteOutput, error) {
	return PromoteOutput{}, u.err(ToolPromote)
}
func (u Unwired) SleepRun(context.Context, SleepRunInput) (SleepRunOutput, error) {
	return SleepRunOutput{}, u.err(ToolSleepRun)
}
func (u Unwired) Verify(context.Context, VerifyInput) (VerifyOutput, error) {
	return VerifyOutput{}, u.err(ToolVerify)
}
func (u Unwired) RankingReset(context.Context, RankingResetInput) (RankingResetOutput, error) {
	return RankingResetOutput{}, u.err(ToolRankingReset)
}
