package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/forget"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/rank"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Outcome reports how a task that used a retrieval went. The caller's zone
// is the channel ceiling: outcomes from an external channel are ignored.
func (a *App) Outcome(ctx context.Context, in chanda.OutcomeInput) (chanda.OutcomeOutput, error) {
	c := a.begin(ctx, in.Namespace)
	r, err := a.ranker.Outcome(ctx, rank.OutcomeInput{
		Namespace: c.ns, RetrievalID: in.RetrievalID, Value: in.Value, UsedIDs: in.UsedIDs,
		Actor: c.actor.Name, ActorZone: c.ceiling,
	})
	for _, n := range r.Notices {
		c.notice(n)
	}
	out := chanda.OutcomeOutput{Meta: c.meta(r.AuditID), OutcomeID: r.ID, Credited: len(r.Credited), KeysCreated: r.KeysCreated}
	if err == nil && r.KeysCreated > 0 {
		a.indexer.Notify() // the new keys wait in the embedding queue
	}
	return out, coded(err)
}

// Flag marks a record as wrong or outdated (strong negative; out of reads).
func (a *App) Flag(ctx context.Context, in chanda.FlagInput) (chanda.FlagOutput, error) {
	c := a.begin(ctx, in.Namespace)
	r, err := a.ranker.Flag(ctx, c.ns, strings.TrimSpace(in.ID), in.Reason, c.actor.Name)
	out := chanda.FlagOutput{Meta: c.meta(r.AuditID), FlagID: r.ID, ID: in.ID, Status: "flagged"}
	if err != nil {
		return out, coded(err)
	}
	if r.RecordType == core.RecFact && r.FactStatus == "" {
		// The fact was already out of current reads; it keeps its status.
		var st string
		if err := a.e.Store.DB().QueryRowContext(ctx, `SELECT status FROM facts WHERE id = ?`, in.ID).Scan(&st); err != nil {
			return out, coded(err)
		}
		out.Status = st
	}
	return out, nil
}

// Flush drains the embedding queue in the caller's goroutine until it is
// empty or timeout_ms passes. Without a timeout it waits until done.
func (a *App) Flush(ctx context.Context, in chanda.FlushInput) (chanda.FlushOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.FlushOutput{Meta: c.meta(0)}
	if in.TimeoutMS < 0 {
		return out, core.Errf(core.CodeInvalidArgument, "timeout_ms must be >= 0")
	}
	if in.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	for {
		n, err := a.indexer.RunOnce(ctx)
		out.Indexed += n
		if err != nil && ctx.Err() == nil {
			return out, coded(err)
		}
		pending, perr := a.indexer.Pending(context.WithoutCancel(ctx), c.ns)
		if perr != nil {
			return out, coded(perr)
		}
		out.Pending = pending
		if pending == 0 || ctx.Err() != nil || n == 0 {
			break
		}
	}
	out.Complete = out.Pending == 0
	if !out.Complete {
		c.notice("flush_incomplete: records are still waiting in the embedding queue")
		out.Meta = c.meta(0)
	}
	return out, nil
}

// Forget deletes in cascade and verifies. A failed probe is data
// (verification.passed false), not an error (docs/errors.md).
func (a *App) Forget(ctx context.Context, in chanda.ForgetInput) (chanda.ForgetOutput, error) {
	c := a.begin(ctx, in.Namespace)
	s := in.Scope
	rep, err := forget.Forget(ctx, a.e, c.ns, forget.Scope{Kind: s.Kind, Ref: s.Value, From: s.From, To: s.To, Query: s.Query},
		in.DryRun, c.actor.Name)
	out := chanda.ForgetOutput{Meta: c.meta(rep.AuditID), DryRun: in.DryRun, TombstoneID: rep.TombstoneID,
		Verification: chanda.Verification{Probes: []chanda.Probe{}}}
	if err != nil && !(core.CodeOf(err) == core.CodeIntegrity && rep.Verification != nil) {
		return out, coded(err)
	}
	r := rep.Removed
	out.RootEvents = int64(len(r.Events))
	out.Removed = chanda.RemovedCounts{
		Events: int64(len(r.Events)), Documents: int64(len(r.Documents)), Sources: int64(len(r.Sources)),
		Facts: int64(len(r.Facts)), FactsKept: int64(len(rep.FactsKept)), PagesRebuild: int64(len(rep.PagesRebuild)),
		PageItems: int64(len(r.PageItems)), Embeddings: int64(rep.Counts["embeddings"]), FTSEntries: int64(rep.Counts["fts_entries"]),
		Provenance: int64(rep.Counts["provenance_edges"]), Utility: int64(rep.Counts["utility"]),
		Retrievals: int64(len(r.Retrievals)), Outcomes: int64(len(r.Outcomes)), Snapshots: int64(rep.Snapshots),
		EntityLinks: int64(len(r.Entities)), Keys: int64(len(r.Keys)),
	}
	if v := rep.Verification; v != nil {
		out.Verification.Passed = v.OK
		for _, p := range v.Probes {
			d := fmt.Sprintf("checked %d", p.Checked)
			if len(p.Failures) > 0 {
				d += "; failures: " + strings.Join(p.Failures, ", ")
			}
			if p.Note != "" {
				d += "; " + p.Note
			}
			out.Verification.Probes = append(out.Verification.Probes, chanda.Probe{Name: p.Name, Passed: p.Pass, Detail: d})
		}
		if !v.OK {
			c.notice("forget_verification_failed: run verify and investigate")
			out.Meta = c.meta(rep.AuditID)
		}
	}
	return out, nil
}

// Promote raises a record's zone. Only a human actor may call it; promoting
// to a zone that is not higher than the current one is a conflict.
func (a *App) Promote(ctx context.Context, in chanda.PromoteInput) (chanda.PromoteOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.PromoteOutput{Meta: c.meta(0), ID: in.ID, To: in.Zone}
	r, err := prov.Promote(ctx, a.e, c.ns, strings.TrimSpace(in.ID), in.Zone, c.actor.Name, c.actor.Human)
	if err != nil {
		return out, coded(err)
	}
	out.From, out.To, out.Meta = r.From, r.To, c.meta(r.AuditID)
	if !r.Changed {
		return out, core.Errf(core.CodeConflict, "record %s is already in zone %s", in.ID, r.From)
	}
	return out, nil
}

// Verify checks the audit hash chain and the provenance graph of the
// namespace. A failed check is data (ok false), not an error.
func (a *App) Verify(ctx context.Context, in chanda.VerifyInput) (chanda.VerifyOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.VerifyOutput{Meta: c.meta(0), Provenance: chanda.ProvenanceCheck{Orphans: []string{}}}
	db := a.e.Store.DB()
	ar, err := audit.Verify(ctx, db)
	if err != nil {
		return out, coded(err)
	}
	g, err := prov.Check(ctx, db, c.ns)
	if err != nil {
		return out, coded(err)
	}
	out.Audit = chanda.AuditCheck{OK: ar.OK, Entries: ar.Entries, BrokenAt: ar.BrokenAt, Reason: ar.Reason}
	out.Provenance.OK = g.OK
	for _, n := range g.Checked {
		out.Provenance.Checked += int64(n)
	}
	seen := map[string]bool{}
	kinds := map[string]int{}
	for _, v := range g.Violations {
		kinds[v.Kind]++
		if !seen[v.RecordID] {
			seen[v.RecordID] = true
			out.Provenance.Orphans = append(out.Provenance.Orphans, v.RecordID)
		}
	}
	sort.Strings(out.Provenance.Orphans)
	if len(out.Provenance.Orphans) > 100 {
		out.Provenance.Orphans = out.Provenance.Orphans[:100]
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		c.notice(fmt.Sprintf("provenance_violation: %s x%d", k, kinds[k]))
	}
	out.OK = ar.OK && g.OK
	out.Meta = c.meta(0)
	return out, nil
}

// RankingReset clears the learning state of the namespace.
func (a *App) RankingReset(ctx context.Context, in chanda.RankingResetInput) (chanda.RankingResetOutput, error) {
	c := a.begin(ctx, in.Namespace)
	r, err := a.ranker.Reset(ctx, c.ns, c.actor.Name)
	out := chanda.RankingResetOutput{Meta: c.meta(r.AuditID), UtilityRows: r.UtilityRows, Models: r.Models}
	return out, coded(err)
}

// RankingToggle turns learned ranking on or off for a namespace (the runtime
// toggle of decision 0070; ranking.enabled is the global switch). Off sets
// λ to 0 at once, so every read returns the pre-learning (C4) order; on
// again needs a new promotion. Only a human actor may toggle.
func (a *App) RankingToggle(ctx context.Context, namespace string, enabled bool) (chanda.Meta, error) {
	c := a.begin(ctx, namespace)
	if !c.actor.Human {
		return c.meta(0), core.Errf(core.CodePermissionDenied, "only a human may toggle learned ranking")
	}
	id, err := a.ranker.SetEnabled(ctx, c.ns, enabled, c.actor.Name)
	return c.meta(id), coded(err)
}
