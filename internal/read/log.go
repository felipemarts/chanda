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
const ActRetrieve = audit.ActRetrieve

// LoggedCandidate is one entry of retrievals.candidates, in the shape
// internal/rank reads (its Candidate): the stage scores as numbers (null when
// the stage gave none), zone and age_seconds; every other feature is a
// number under "features" (rank decodes features as map[string]float64).
// "dropped" is the reason a candidate left the list; rank ignores it.
//
// utility is the term used in the final score: the record's utility, or
// NeutralUtility when it has none (features.has_utility = 0).
type LoggedCandidate struct {
	ID         string             `json:"id"`
	Type       core.RecordType    `json:"type"`
	BM25       *float64           `json:"bm25"`
	Cosine     *float64           `json:"cosine"`
	Rerank     *float64           `json:"rerank"`
	RRF        *float64           `json:"rrf"`
	Utility    *float64           `json:"utility"`
	Learned    *float64           `json:"learned"`
	Final      *float64           `json:"final"`
	Zone       core.Zone          `json:"zone"`
	AgeSeconds float64            `json:"age_seconds"`
	Features   map[string]float64 `json:"features"`
	Dropped    string             `json:"dropped,omitempty"`
}

// Delivered reports whether the candidate was delivered.
func (lc LoggedCandidate) Delivered() bool { return lc.Features["delivered"] == 1 }

func loggedCandidate(c *cand, position int) LoggedCandidate {
	s := c.scores()
	lc := LoggedCandidate{
		ID: c.id(), Type: c.rec.typ, BM25: s.BM25, Cosine: s.Cosine, Rerank: s.Rerank, RRF: s.RRF,
		Learned: s.Learned, Final: s.Final, Zone: c.rec.zone, AgeSeconds: c.ageSec, Dropped: c.dropped,
		Features: map[string]float64{"kw_rank": float64(c.kwRank), "vec_rank": float64(c.vecRank), "has_utility": 0, "delivered": 0},
	}
	if c.scored {
		u := NeutralUtility
		if c.hasUtil {
			u, lc.Features["has_utility"] = c.utility, 1
		}
		lc.Utility = ptr(u)
		lc.Features["coverage"], lc.Features["evidence"], lc.Features["relevance"] = c.coverage, c.evidence, c.relevance
	}
	if position > 0 {
		lc.Features["delivered"], lc.Features["position"], lc.Dropped = 1, float64(position), ""
	}
	return lc
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
		logged = append(logged, loggedCandidate(c, pos[c.id()]))
	}
	params := map[string]any{
		"types": q.types, "zones": q.zones, "include_history": q.includeHistory,
		"limit": q.limit, "budget_tokens": q.budget, "config_hash": r.cfgHash, "gate": q.gate,
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
		actor := q.actor
		if actor == "" {
			actor = r.e.Cfg.MCP.Actor
		}
		var err error
		res.auditID, err = r.e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: q.ns, Actor: actor, Action: ActRetrieve, Target: res.retrievalID,
			Details: map[string]any{"mode": string(q.mode), "candidates": len(logged), "delivered": len(delivered), "reason": res.reason},
		})
		return err
	})
}
