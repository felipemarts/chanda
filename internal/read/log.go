package read

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// ActRetrieve is the audit action for a logged query (the retrievals row is
// a mutation of the learning layer). Details hold counts only, never the
// query text or record content.
const ActRetrieve = "retrieve"

// LoggedCandidate is one entry of retrievals.candidates: the per-stage
// scores and features of a candidate, whether it was delivered and, if not,
// why it left the list.
type LoggedCandidate struct {
	ID      string          `json:"id"`
	Type    core.RecordType `json:"type"`
	Zone    core.Zone       `json:"zone,omitempty"`
	AgeDays int             `json:"age_days"`
	Scores
	Delivered bool   `json:"delivered,omitempty"`
	Position  int    `json:"position,omitempty"` // 1-based position in the delivered list
	Dropped   string `json:"dropped,omitempty"`
}

// logRetrieval writes the retrievals row and its audit entry in one
// transaction. Candidates are logged in a total order: candidates that
// reached fusion by final score (as ranked), then the rest by id.
// Candidates unknown in this namespace are not logged, so no id from
// another namespace is ever written here.
func (r *Reader) logRetrieval(ctx context.Context, q request, res *result, all []*cand, now time.Time, extra map[string]any) error {
	pos := map[string]int{}
	delivered := make([]string, len(res.items))
	for i, it := range res.items {
		delivered[i] = it.ID
		pos[it.ID] = i + 1
	}
	var scored, rest []*cand
	for _, c := range all {
		switch {
		case c.dropped == dropNotFound || c.dropped == dropNamespace:
			continue
		case c.scored:
			scored = append(scored, c)
		default:
			rest = append(rest, c)
		}
	}
	sortByFinal(scored)
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].id() < rest[j].id() })
	logged := make([]LoggedCandidate, 0, len(scored)+len(rest))
	for _, c := range append(scored, rest...) {
		lc := LoggedCandidate{ID: c.id(), Type: c.rec.typ, Zone: c.rec.zone, AgeDays: c.ageDays, Scores: c.scores(), Dropped: c.dropped}
		if p, ok := pos[c.id()]; ok {
			lc.Delivered, lc.Position, lc.Dropped = true, p, ""
		}
		logged = append(logged, lc)
	}
	params := map[string]any{
		"types": q.types, "zones": q.zones, "include_history": q.includeHistory,
		"limit": q.limit, "budget_tokens": q.budget, "config_hash": r.cfgHash,
	}
	for k, v := range extra {
		params[k] = v
	}
	pj, err := json.Marshal(params)
	if err != nil {
		return err
	}
	cj, err := json.Marshal(logged)
	if err != nil {
		return err
	}
	dj, err := json.Marshal(delivered)
	if err != nil {
		return err
	}
	var sess any
	if q.sessionID != "" {
		sess = q.sessionID
	}
	return r.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO retrievals(id, namespace, session_id, mode, query_text, params, candidates, delivered, reason, ts)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, res.retrievalID, q.ns, sess, string(q.mode), q.text, string(pj), string(cj), string(dj),
			res.reason, core.FormatTime(now)); err != nil {
			return core.Wrap(core.CodeInternal, err, "log retrieval")
		}
		_, err := r.e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: q.ns, Actor: r.e.Cfg.MCP.Actor, Action: ActRetrieve, Target: res.retrievalID,
			Details: map[string]any{"mode": string(q.mode), "candidates": len(logged), "delivered": len(delivered), "reason": res.reason},
		})
		return err
	})
}
