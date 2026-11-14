package sleep

import (
	"context"
	"database/sql"
	"sort"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// maxMergeHops bounds the merged_into chain walk (as internal/facts does).
const maxMergeHops = 16

// mergedFact is an active fact of a single predicate in a merge set.
type mergedFact struct {
	id, subject, predicate, validFrom, recordedAt string
	zone                                          core.Zone
}

// reconcileMerge applies the decision table to the active facts of single
// predicates across the merge set of into (into and every entity merged into
// it), right after a merge was accepted, so a single predicate has at most
// one active value per surviving entity (decision 0103):
//
//   - an active fact from a lower zone than the highest zone among them is
//     quarantined (it supersedes nothing until reviewed);
//   - the rest are ordered by valid_from, then recorded_at, then id; the last
//     one stays active and each earlier one is closed by the next
//     (valid_to = its valid_from, superseded_by, a supersedes edge).
//
// Every change is audited with the merged entity and the run.
func (s *Sleeper) reconcileMerge(ctx context.Context, tx *sql.Tx, r *run, into, from string) error {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE m(id, depth) AS (
			SELECT ?, 0
			UNION SELECT e.id, m.depth + 1 FROM entities e JOIN m ON e.merged_into = m.id
			WHERE e.namespace = ? AND m.depth < ?)
		SELECT f.id, f.subject, f.predicate, f.valid_from, f.recorded_at, f.trust_zone FROM facts f
		WHERE f.namespace = ? AND f.status = 'active' AND f.is_single = 1 AND f.subject IN (SELECT id FROM m)
		ORDER BY f.predicate, f.valid_from, f.recorded_at, f.id`, into, r.ns, maxMergeHops, r.ns)
	if err != nil {
		return err
	}
	byPred := map[string][]mergedFact{}
	var preds []string
	for rows.Next() {
		var f mergedFact
		var z string
		if err := rows.Scan(&f.id, &f.subject, &f.predicate, &f.validFrom, &f.recordedAt, &z); err != nil {
			rows.Close()
			return err
		}
		f.zone = core.Zone(z)
		if byPred[f.predicate] == nil {
			preds = append(preds, f.predicate)
		}
		byPred[f.predicate] = append(byPred[f.predicate], f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Strings(preds)
	c := &r.res.Counts
	now := s.now()
	for _, p := range preds {
		fs := byPred[p]
		if len(fs) < 2 {
			continue
		}
		top := 0
		for _, f := range fs {
			top = max(top, f.zone.Rank())
		}
		var keep []mergedFact
		var lower []mergedFact
		for _, f := range fs {
			if f.zone.Rank() < top {
				lower = append(lower, f)
			} else {
				keep = append(keep, f)
			}
		}
		// keep is already in (valid_from, recorded_at, id) order.
		winner := keep[len(keep)-1]
		for _, f := range lower {
			if _, err := tx.ExecContext(ctx, `UPDATE facts SET status = 'quarantined' WHERE id = ? AND status = 'active'`, f.id); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, audit.ActQuarantine, f.id, map[string]any{
				"active": winner.id, "reason": "entity_merge", "into": into, "from": from, "run": r.res.RunID,
			}); err != nil {
				return err
			}
			c.FactsQuarantined = append(c.FactsQuarantined, f.id)
		}
		for i := 0; i < len(keep)-1; i++ {
			old, next := keep[i], keep[i+1]
			if _, err := tx.ExecContext(ctx, `UPDATE facts SET status = 'superseded', valid_to = ?, superseded_by = ?
				WHERE id = ? AND status = 'active'`, next.validFrom, next.id, old.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
				VALUES (?,'fact',?,'fact',?,?,?)`, r.ns, next.id, old.id, core.RelSupersedes, now); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, audit.ActSupersede, old.id, map[string]any{
				"by": next.id, "reason": "entity_merge", "into": into, "from": from, "run": r.res.RunID,
			}); err != nil {
				return err
			}
			c.FactsSuperseded = append(c.FactsSuperseded, old.id)
		}
	}
	return nil
}
