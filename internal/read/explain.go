package read

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/felipemarts/chanda/internal/core"
)

// Explanation gives the per-stage scores of one record in a logged query.
type Explanation struct {
	RetrievalID string           `json:"retrieval_id"`
	Mode        Mode             `json:"mode"`
	Query       string           `json:"query"`
	TS          string           `json:"ts"`
	Params      json.RawMessage  `json:"params"`
	Reason      string           `json:"reason,omitempty"`
	RecordID    string           `json:"record_id"`
	Found       bool             `json:"found"`               // the record was a candidate
	Rank        int              `json:"rank,omitempty"`      // 1-based position among scored candidates by final score
	Candidate   *LoggedCandidate `json:"candidate,omitempty"` // stages, features, delivered or drop reason
	Delivered   []string         `json:"delivered"`
}

// Explain reads the logged retrieval and returns the stage scores of
// recordID. A record that never became a candidate gives Found = false.
func (r *Reader) Explain(ctx context.Context, ns, retrievalID, recordID string) (Explanation, error) {
	if ns == "" {
		ns = r.e.Cfg.Namespace
	}
	var ex Explanation
	var mode, params, cands, delivered string
	err := r.e.Store.DB().QueryRowContext(ctx, `SELECT mode, query_text, params, candidates, delivered, reason, ts
		FROM retrievals WHERE namespace = ? AND id = ?`, ns, retrievalID).
		Scan(&mode, &ex.Query, &params, &cands, &delivered, &ex.Reason, &ex.TS)
	if err == sql.ErrNoRows {
		return Explanation{}, core.Errf(core.CodeNotFound, "retrieval %s not found", retrievalID)
	}
	if err != nil {
		return Explanation{}, core.Wrap(core.CodeInternal, err, "retrieval")
	}
	ex.RetrievalID, ex.Mode, ex.RecordID, ex.Params = retrievalID, Mode(mode), recordID, json.RawMessage(params)
	var lcs []LoggedCandidate
	if err := json.Unmarshal([]byte(cands), &lcs); err != nil {
		return Explanation{}, core.Wrap(core.CodeIntegrity, err, "retrieval candidates")
	}
	if err := json.Unmarshal([]byte(delivered), &ex.Delivered); err != nil {
		return Explanation{}, core.Wrap(core.CodeIntegrity, err, "retrieval delivered")
	}
	for i := range lcs {
		if lcs[i].ID == recordID {
			ex.Found, ex.Candidate = true, &lcs[i]
			if lcs[i].Final != nil {
				ex.Rank = i + 1 // scored candidates are logged first, by final score
			}
			break
		}
	}
	return ex, nil
}
