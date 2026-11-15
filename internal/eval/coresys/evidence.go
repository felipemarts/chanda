package coresys

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// EntityNames lists the live entities of the namespace with their types,
// ordered by name (eval.EntityLister): the names sleep compares when it
// proposes merges by vector similarity.
func (s *System) EntityNames(ctx context.Context) ([]eval.EntityName, error) {
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT canonical_name, COALESCE(type, '') FROM entities
		WHERE namespace = ? AND merged_into IS NULL ORDER BY canonical_name, id`, Namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eval.EntityName
	for rows.Next() {
		var e eval.EntityName
		if err := rows.Scan(&e.Name, &e.Type); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Evidence runs one context query and returns every candidate the relevance
// cut judged (those that reached fusion: they passed the rule filter and the
// near-duplicate collapse), with the coverage and cosine logged in the
// retrieval, whatever the cut decided (eval.EvidenceProber). It is how the
// evidence floors are calibrated on the training split.
func (s *System) Evidence(ctx context.Context, q eval.Query, budget int) ([]eval.EvidenceCandidate, error) {
	rid, _, err := s.Context(ctx, q, budget)
	if err != nil {
		return nil, err
	}
	var raw string
	if err := s.e.Store.DB().QueryRowContext(ctx, `SELECT candidates FROM retrievals WHERE id = ?`, rid).Scan(&raw); err != nil {
		return nil, fmt.Errorf("retrieval %s: %w", rid, err)
	}
	var cands []struct {
		ID       string             `json:"id"`
		Cosine   *float64           `json:"cosine"`
		BM25     *float64           `json:"bm25"`
		Features map[string]float64 `json:"features"`
	}
	if err := json.Unmarshal([]byte(raw), &cands); err != nil {
		return nil, err
	}
	if s.records == nil {
		if s.records, err = s.allRecords(ctx); err != nil {
			return nil, err
		}
	}
	byID := make(map[string]eval.Retrieved, len(s.records))
	for _, r := range s.records {
		byID[r.ID] = r
	}
	var out []eval.EvidenceCandidate
	for _, c := range cands {
		cov, judged := c.Features["coverage"]
		r, known := byID[c.ID]
		if !judged || !known {
			continue
		}
		ec := eval.EvidenceCandidate{Retrieved: r, Coverage: cov, Features: c.Features}
		if c.BM25 != nil {
			ec.BM25, ec.HasBM25 = *c.BM25, true
		}
		if c.Cosine != nil {
			ec.Cosine, ec.HasCosine = *c.Cosine, true
		}
		out = append(out, ec)
	}
	return out, nil
}
