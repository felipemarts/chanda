package facts

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
)

const inBatch = 500

// MarkStaleFromRetired marks as stale_source the facts derived from
// eventIDs whose origins (derived_from and confirmed_by edges to events) are
// all retired (write step 6, sleep phase 1). A fact with another live origin
// survives. Facts already rejected, flagged or stale are left as they are.
// A stale active fact does not reopen its predecessor: the next document
// version asserts its own value. Returns the ids marked, sorted.
func (s *Service) MarkStaleFromRetired(ctx context.Context, tx *sql.Tx, ns string, eventIDs []string) ([]string, error) {
	ids := append([]string(nil), eventIDs...)
	sort.Strings(ids)
	cand := map[string]bool{}
	for i := 0; i < len(ids); i += inBatch {
		batch := ids[i:min(i+inBatch, len(ids))]
		args := []any{ns, string(core.RecFact), string(core.RecEvent), core.RelDerivedFrom, core.RelConfirmedBy,
			string(core.FactActive), string(core.FactSuperseded), string(core.FactQuarantined)}
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT p.derived_id FROM provenance p JOIN facts f ON f.id = p.derived_id
			WHERE p.namespace = ? AND p.derived_type = ? AND p.source_type = ? AND p.relation IN (?, ?)
			AND f.status IN (?, ?, ?) AND p.source_id IN (?`+strings.Repeat(",?", len(batch)-1)+`)
			ORDER BY p.derived_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			cand[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	facts := make([]string, 0, len(cand))
	for id := range cand {
		facts = append(facts, id)
	}
	sort.Strings(facts)
	var marked []string
	for _, id := range facts {
		var live int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM provenance p JOIN events e ON e.id = p.source_id
			WHERE p.derived_type = ? AND p.derived_id = ? AND p.source_type = ? AND p.relation IN (?, ?) AND e.retired_at IS NULL`,
			string(core.RecFact), id, string(core.RecEvent), core.RelDerivedFrom, core.RelConfirmedBy).Scan(&live); err != nil {
			return nil, err
		}
		if live > 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE facts SET status = ? WHERE id = ?`, string(core.FactStaleSource), id); err != nil {
			return nil, err
		}
		if _, err := s.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: "system", Action: audit.ActStaleSource, Target: id,
			Details: map[string]any{"retired_events": len(ids)}}); err != nil {
			return nil, err
		}
		marked = append(marked, id)
	}
	return marked, nil
}
