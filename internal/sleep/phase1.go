package sleep

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// phase1 is deterministic maintenance (BASE.md "Phase 1"). The snapshot is
// taken at the start of the phase-2 apply step, immediately before the first
// phase-2 change (see snapshotAndBegin).
func (s *Sleeper) phase1(ctx context.Context, r *run) error {
	if s.d.Integrity != nil {
		v, err := s.d.Integrity.Check(ctx, s.db(), r.ns)
		if err != nil {
			return err
		}
		r.res.Counts.IntegrityViolations = v
		if v > 0 {
			r.res.notice("integrity: derived records without a path to an event were found")
		}
	} else {
		r.res.notice("integrity checker not configured; check skipped")
	}
	if s.d.Embeddings != nil {
		if err := s.d.Embeddings.Flush(ctx, r.ns); err != nil {
			return err
		}
	} else {
		r.res.notice("embedding indexer not configured; pending embeddings left")
	}
	if s.d.Utility != nil {
		if err := s.d.Utility.RecomputeUtility(ctx, r.ns); err != nil {
			return err
		}
	} else {
		r.res.notice("utility not configured; recompute skipped")
	}
	if err := s.proposeMerges(ctx, r); err != nil {
		return err
	}
	if err := s.markStale(ctx, r); err != nil {
		return err
	}
	return s.rebuildPages(ctx, r)
}

// ---------------------------------------------------------------------------
// Entity merge proposals.

type entityNames struct {
	id, typ string
	names   []string // normalized canonical name and aliases, sorted unique
}

func (s *Sleeper) loadEntities(ctx context.Context, q queryer, ns string) ([]entityNames, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, canonical_name, type FROM entities
		WHERE namespace=? AND merged_into IS NULL ORDER BY id`, ns)
	if err != nil {
		return nil, err
	}
	var ents []entityNames
	idx := map[string]int{}
	for rows.Next() {
		var e entityNames
		var name string
		if err := rows.Scan(&e.id, &name, &e.typ); err != nil {
			rows.Close()
			return nil, err
		}
		if n := normalize(name); n != "" {
			e.names = []string{n}
		}
		idx[e.id] = len(ents)
		ents = append(ents, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT entity_id, alias_norm FROM entity_aliases WHERE namespace=?
		ORDER BY entity_id, alias_norm`, ns)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, alias string
		if err := rows.Scan(&id, &alias); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[id]; ok {
			if n := normalize(alias); n != "" {
				ents[i].names = append(ents[i].names, n)
			}
		}
	}
	rows.Close()
	for i := range ents {
		ents[i].names = uniqueSorted(ents[i].names)
	}
	return ents, rows.Err()
}

type proposal struct {
	a, b   string
	score  float64
	reason string
}

// proposeMerges records pending entity_merge_proposals for pairs of live
// entities whose names (canonical or alias) or vectors are similar at or
// above sleep.merge_similarity. Entities with two different non-empty types
// are never proposed. Pairs that already have a proposal, in any status, are
// not proposed again. Nothing is merged here.
func (s *Sleeper) proposeMerges(ctx context.Context, r *run) error {
	ents, err := s.loadEntities(ctx, s.db(), r.ns)
	if err != nil || len(ents) < 2 {
		return err
	}
	existing := map[[2]string]bool{}
	rows, err := s.db().QueryContext(ctx, `SELECT entity_a, entity_b FROM entity_merge_proposals WHERE namespace=?
		ORDER BY entity_a, entity_b`, r.ns)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			rows.Close()
			return err
		}
		existing[pairKey(a, b)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	threshold := s.env.Cfg.Sleep.MergeSimilarity
	compatible := func(i, j int) bool {
		ti, tj := ents[i].typ, ents[j].typ
		return ti == "" || tj == "" || ti == tj
	}
	found := map[[2]string]proposal{}
	consider := func(i, j int, score float64, reason string) {
		k := pairKey(ents[i].id, ents[j].id)
		if existing[k] || !compatible(i, j) || score < threshold {
			return
		}
		if p, ok := found[k]; ok && p.score >= score {
			return
		}
		found[k] = proposal{a: k[0], b: k[1], score: score, reason: reason}
	}

	// Name similarity over blocked pairs: entities sharing a token or the
	// first three letters of a name. Edit similarity at the default
	// threshold implies one of the two for any realistic name.
	blocks := map[string][]int{}
	for i, e := range ents {
		keys := map[string]bool{}
		for _, n := range e.names {
			for _, t := range strings.Fields(n) {
				keys["t:"+t] = true
			}
			rs := []rune(n)
			keys["p:"+string(rs[:min(3, len(rs))])] = true
		}
		for k := range keys {
			blocks[k] = append(blocks[k], i)
		}
	}
	pairs := map[[2]int]bool{}
	for _, members := range blocks {
		for x := 0; x < len(members); x++ {
			for y := x + 1; y < len(members); y++ {
				i, j := members[x], members[y]
				if i > j {
					i, j = j, i
				}
				pairs[[2]int{i, j}] = true
			}
		}
	}
	for p := range pairs {
		best := 0.0
		for _, a := range ents[p[0]].names {
			for _, b := range ents[p[1]].names {
				best = max(best, nameSimilarity(a, b))
			}
		}
		consider(p[0], p[1], best, "name_similarity")
	}

	if s.d.EntityVectors != nil {
		if len(ents) > s.env.Cfg.Sleep.MergeMaxEntities {
			r.res.notice("merge proposals: too many entities for vector comparison; names only")
		} else {
			ids := make([]string, len(ents))
			for i, e := range ents {
				ids[i] = e.id
			}
			vecs, err := s.d.EntityVectors.EntityVectors(ctx, r.ns, ids)
			if err != nil {
				return err
			}
			for i := range ents {
				vi, ok := vecs[ents[i].id]
				if !ok {
					continue
				}
				for j := i + 1; j < len(ents); j++ {
					if vj, ok := vecs[ents[j].id]; ok {
						consider(i, j, cosine(vi, vj), "vector_similarity")
					}
				}
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	list := make([]proposal, 0, len(found))
	for _, p := range found {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].a != list[j].a {
			return list[i].a < list[j].a
		}
		return list[i].b < list[j].b
	})
	return s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		for _, p := range list {
			id := s.env.IDs.New(core.PrefixProposal)
			if _, err := tx.ExecContext(ctx, `INSERT INTO entity_merge_proposals
				(id, namespace, entity_a, entity_b, score, reason, status, sleep_run_id, created_at)
				VALUES (?,?,?,?,?,?,'pending',?,?)`, id, r.ns, p.a, p.b, p.score, p.reason, r.res.RunID, s.now()); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, ActMergePropose, id, map[string]any{
				"entity_a": p.a, "entity_b": p.b, "score": p.score, "reason": p.reason, "run": r.res.RunID}); err != nil {
				return err
			}
			r.res.Counts.MergeProposals = append(r.res.Counts.MergeProposals, id)
		}
		return nil
	})
}

func pairKey(a, b string) [2]string {
	if a > b {
		a, b = b, a
	}
	return [2]string{a, b}
}

// ---------------------------------------------------------------------------
// stale_source.

// originRelations are the provenance relations that count as an origin path
// to an event (supersedes, duplicate_of and mentions do not).
const originRelations = `('derived_from','confirmed_by','cites')`

// markStale sets status stale_source on active or quarantined facts that
// have event origins and none of them is live (every origin event retired
// or gone).
func (s *Sleeper) markStale(ctx context.Context, r *run) error {
	return s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		ids, err := queryStrings(ctx, tx, `SELECT f.id FROM facts f
			WHERE f.namespace=? AND f.status IN ('active','quarantined')
			AND EXISTS (SELECT 1 FROM provenance p WHERE p.derived_type='fact' AND p.derived_id=f.id AND p.source_type='event'
				AND p.relation IN `+originRelations+`)
			AND NOT EXISTS (SELECT 1 FROM provenance p JOIN events e ON e.id=p.source_id
				WHERE p.derived_type='fact' AND p.derived_id=f.id AND p.source_type='event'
				AND p.relation IN `+originRelations+` AND e.retired_at IS NULL)
			ORDER BY f.id`, r.ns)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE facts SET status='stale_source' WHERE id=?`, id); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, audit.ActStaleSource, id, map[string]any{"reason": "origin_retired", "run": r.res.RunID}); err != nil {
				return err
			}
		}
		r.res.Counts.StaleFacts = append(r.res.Counts.StaleFacts, ids...)
		return nil
	})
}

// ---------------------------------------------------------------------------
// Page rebuild.

// rebuildPages rebuilds every page in needs_rebuild from its remaining
// items: an item none of whose cited events survives is retired; the body is
// the remaining active items in order; the zone never rises (MinZone of the
// current zone and the remaining items); a page with no item left is
// retired with an empty body. No LLM is involved.
func (s *Sleeper) rebuildPages(ctx context.Context, r *run) error {
	return s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		pages, err := queryStrings(ctx, tx, `SELECT id FROM pages WHERE namespace=? AND status='needs_rebuild' ORDER BY id`, r.ns)
		if err != nil {
			return err
		}
		for _, pid := range pages {
			items, err := loadItems(ctx, tx, pid)
			if err != nil {
				return err
			}
			retired := 0
			for i := range items {
				zones, err := citedZones(ctx, tx, items[i].id)
				if err != nil {
					return err
				}
				if len(zones) == 0 {
					if _, err := tx.ExecContext(ctx, `UPDATE page_items SET status='retired' WHERE id=?`, items[i].id); err != nil {
						return err
					}
					items[i].status = "retired"
					retired++
					continue
				}
				items[i].zone = core.MinZone(append(zones, items[i].zone)...)
				if _, err := tx.ExecContext(ctx, `UPDATE page_items SET trust_zone=? WHERE id=?`, string(items[i].zone), items[i].id); err != nil {
					return err
				}
			}
			if err := s.refreshPage(ctx, tx, pid, items); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, audit.ActPageUpdate, pid, map[string]any{
				"rebuild": true, "retired_items": retired, "run": r.res.RunID}); err != nil {
				return err
			}
			r.res.Counts.PagesRebuilt = append(r.res.Counts.PagesRebuilt, pid)
		}
		return nil
	})
}

type pageItem struct {
	id, text, status string
	ordinal          int
	zone             core.Zone
}

func loadItems(ctx context.Context, q queryer, pageID string) ([]pageItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, text, status, ordinal, trust_zone FROM page_items
		WHERE page_id=? AND status IN ('active','staged') ORDER BY ordinal, id`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pageItem
	for rows.Next() {
		var it pageItem
		var z string
		if err := rows.Scan(&it.id, &it.text, &it.status, &it.ordinal, &z); err != nil {
			return nil, err
		}
		it.zone = core.Zone(z)
		out = append(out, it)
	}
	return out, rows.Err()
}

// citedZones returns the zones of the live events an item cites, sorted by event id.
func citedZones(ctx context.Context, q queryer, itemID string) ([]core.Zone, error) {
	zs, err := queryStrings(ctx, q, `SELECT e.trust_zone FROM provenance p JOIN events e ON e.id=p.source_id
		WHERE p.derived_type='page_item' AND p.derived_id=? AND p.source_type='event' AND p.relation='cites'
		AND e.retired_at IS NULL ORDER BY e.id`, itemID)
	out := make([]core.Zone, len(zs))
	for i, z := range zs {
		out[i] = core.Zone(z)
	}
	return out, err
}

// refreshPage recomputes a page from its active items (staged items count
// as active: refreshPage runs when they are promoted): body, zone (never
// rising), version, status, and the page-level cites edges.
func (s *Sleeper) refreshPage(ctx context.Context, tx *sql.Tx, pageID string, items []pageItem) error {
	var zoneStr, ns string
	if err := tx.QueryRowContext(ctx, `SELECT trust_zone, namespace FROM pages WHERE id=?`, pageID).Scan(&zoneStr, &ns); err != nil {
		return err
	}
	zones := []core.Zone{core.Zone(zoneStr)}
	var texts []string
	for _, it := range items {
		if it.status == "retired" {
			continue
		}
		zones = append(zones, it.zone)
		texts = append(texts, it.text)
	}
	status := "active"
	if len(texts) == 0 {
		status = "retired"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pages SET body=?, trust_zone=?, status=?, version=version+1, updated_at=? WHERE id=?`,
		strings.Join(texts, "\n\n"), string(core.MinZone(zones...)), status, s.now(), pageID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM provenance WHERE derived_type='page' AND derived_id=? AND relation='cites'`, pageID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
		SELECT DISTINCT ?, 'page', ?, 'event', p.source_id, 'cites', ?
		FROM provenance p JOIN page_items i ON i.id=p.derived_id
		WHERE i.page_id=? AND i.status IN ('active','staged') AND p.derived_type='page_item' AND p.source_type='event' AND p.relation='cites'
		ORDER BY p.source_id`, ns, pageID, s.now(), pageID)
	return err
}

// ---------------------------------------------------------------------------

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func queryStrings(ctx context.Context, q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
