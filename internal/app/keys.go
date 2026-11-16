package app

import (
	"context"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/keys"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// LearnedKeys is the admin control of the learned query keys (decision
// 0145): enable and disable the namespace's runtime switch, reset (delete
// every key), rebuild (create the missing keys of every positive outcome
// already recorded, deterministic, no LLM) and stats. Every mutating action
// is audited. The output always carries the counts after the action.
//
// New keys (rebuild) are queued for embedding; flush (or the background
// indexer) gives them their vectors before the vector side of a read can
// find them.
func (a *App) LearnedKeys(ctx context.Context, in chanda.LearnedKeysInput) (chanda.LearnedKeysOutput, error) {
	c := a.begin(ctx, in.Namespace)
	action := strings.TrimSpace(in.Action)
	out := chanda.LearnedKeysOutput{Meta: c.meta(0), Action: action}
	var auditID int64
	var err error
	switch action {
	case chanda.KeysEnable, chanda.KeysDisable:
		auditID, err = keys.SetEnabled(ctx, a.e, c.ns, action == chanda.KeysEnable, c.actor.Name)
	case chanda.KeysReset:
		var r keys.ResetResult
		r, err = keys.Reset(ctx, a.e, c.ns, c.actor.Name)
		auditID, out.Removed = r.AuditID, r.Keys
	case chanda.KeysRebuild:
		var r keys.RebuildResult
		r, err = keys.Rebuild(ctx, a.e, c.ns, c.actor.Name)
		auditID, out.Outcomes, out.Created, out.Existing = r.AuditID, int64(r.Outcomes), int64(r.Created.Created), int64(r.Existing)
		if err == nil && r.Created.Created > 0 {
			a.indexer.Notify()
		}
	case chanda.KeysStats:
	default:
		return out, core.Errf(core.CodeInvalidArgument, "learned_keys: action must be one of %s, not %q",
			strings.Join(chanda.KeysActions(), ", "), in.Action)
	}
	if err != nil {
		return out, coded(err)
	}
	st, err := keys.StatsOf(ctx, a.e.Store.DB(), c.ns)
	if err != nil {
		return out, coded(err)
	}
	out.Enabled, out.Keys, out.Records, out.Embedded, out.Pending = st.Enabled, st.Keys, st.Records, st.Embedded, st.Pending
	if !a.e.Cfg.Read.LearnedKeys {
		c.notice("learned_keys_read_off: read.learned_keys is off, so reads ignore keys whatever the switch says")
	}
	out.Meta = c.meta(auditID)
	return out, nil
}
