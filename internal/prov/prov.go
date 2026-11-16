// Package prov owns the provenance graph (BASE.md "Provenance, forgetting and
// security"): writing edges, walking a derived record back to the events that
// produced it, deriving zones from those events, walking forward from events
// to everything derived from them, the integrity checker and promote.
//
// Graph conventions (shared with every writer of the provenance table):
//
//   - An edge points from a derived record to one of its sources
//     (derived_type, derived_id) -> (source_type, source_id).
//   - Origin (derivation) edges are exactly derived_from, confirmed_by and
//     cites; only they are followed when computing origins, zones and
//     descendants. supersedes (new fact -> old fact), duplicate_of (new ->
//     older record) and mentions (event/fact -> entity) relate records
//     without carrying content, so forgetting an old fact's origin never
//     deletes its successor. Any other relation is not a path to an event.
//   - A page reaches events through its items (page_items.page_id) and through
//     any edge whose derived side is the page itself.
//   - Embeddings carry no edges: a vector belongs to (record_type, record_id)
//     and reaches events through that record.
//   - Entities are shared vocabulary: they are not derived records, carry no
//     zone and need no origin. Descendants never collects them. forget removes
//     an entity only when it is the scope or when no live record refers to it
//     any more (see internal/forget).
package prov

import (
	"context"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/store"
)

// originRelations are the derivation relations: the content of the derived
// record came from the source.
var originRelations = map[string]bool{
	core.RelDerivedFrom: true,
	core.RelConfirmedBy: true,
	core.RelCites:       true,
}

// IsOriginRelation reports whether an edge with relation rel is followed when
// computing origins and descendants.
func IsOriginRelation(rel string) bool { return originRelations[rel] }

// originSQL is the SQL list matching originRelations, for "relation IN (...)".
const originSQL = `('derived_from','confirmed_by','cites')`

// Ref names one record.
type Ref struct {
	Type core.RecordType `json:"type"`
	ID   string          `json:"id"`
}

func (r Ref) key() string { return string(r.Type) + "\x00" + r.ID }

// EventRef is an origin event of a record.
type EventRef struct {
	ID      string    `json:"id"`
	Zone    core.Zone `json:"zone"`
	Retired bool      `json:"retired"`
}

// EmbeddingKey identifies one embeddings row.
type EmbeddingKey struct {
	RecordType core.RecordType `json:"record_type"`
	RecordID   string          `json:"record_id"`
	ModelID    string          `json:"model_id"`
}

// AddEdge writes one provenance edge (derived -> source) inside q. Writing an
// existing edge again is a no-op. It rejects self-edges and empty fields.
func AddEdge(ctx context.Context, q store.Queryer, clk core.Clock, ns string, derivedType core.RecordType, derivedID string, sourceType core.RecordType, sourceID, relation string) error {
	switch {
	case ns == "" || derivedType == "" || derivedID == "" || sourceType == "" || sourceID == "" || relation == "":
		return core.Errf(core.CodeInvalidArgument, "provenance edge: empty field")
	case derivedType == sourceType && derivedID == sourceID:
		return core.Errf(core.CodeInvalidArgument, "provenance edge: %s cannot derive from itself", derivedID)
	case derivedType == core.RecEvent && IsOriginRelation(relation):
		return core.Errf(core.CodeInvalidArgument, "provenance edge: events are raw and have no origin edges (relation %s)", relation)
	}
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO provenance
		(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
		VALUES (?,?,?,?,?,?,?)`, ns, string(derivedType), derivedID, string(sourceType), sourceID, relation, core.FormatTime(clk.Now()))
	return err
}

// sources returns the origin-edge sources of a record, plus the items of a page.
func sources(ctx context.Context, q store.Queryer, r Ref) ([]Ref, error) {
	out, err := scanRefs(ctx, q, `SELECT source_type, source_id FROM provenance
		WHERE derived_type = ? AND derived_id = ? AND relation IN `+originSQL+`
		ORDER BY source_type, source_id`, string(r.Type), r.ID)
	if err != nil {
		return nil, err
	}
	if r.Type == core.RecPage {
		items, err := scanIDs(ctx, q, `SELECT id FROM page_items WHERE page_id = ? ORDER BY id`, r.ID)
		if err != nil {
			return nil, err
		}
		for _, id := range items {
			out = append(out, Ref{core.RecPageItem, id})
		}
	}
	return out, nil
}

// Origins walks origin edges from (recordType, id) back to events. It is
// cycle-safe and returns existing events only, sorted by id. An event is its
// own origin.
func Origins(ctx context.Context, q store.Queryer, recordType core.RecordType, id string) ([]EventRef, error) {
	seen := map[string]bool{}
	var eventIDs []string
	queue := []Ref{{recordType, id}}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		if seen[r.key()] {
			continue
		}
		seen[r.key()] = true
		if r.Type == core.RecEvent {
			eventIDs = append(eventIDs, r.ID)
			continue
		}
		next, err := sources(ctx, q, r)
		if err != nil {
			return nil, err
		}
		queue = append(queue, next...)
	}
	return loadEvents(ctx, q, eventIDs)
}

// loadEvents returns the existing events among ids, sorted by id.
func loadEvents(ctx context.Context, q store.Queryer, ids []string) ([]EventRef, error) {
	ids = SortedUnique(ids)
	var out []EventRef
	for _, chunk := range chunks(ids, 500) {
		rows, err := q.QueryContext(ctx, `SELECT id, trust_zone, retired_at IS NOT NULL FROM events
			WHERE id IN (`+placeholders(len(chunk))+`) ORDER BY id`, anys(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e EventRef
			var z string
			if err := rows.Scan(&e.ID, &z, &e.Retired); err != nil {
				rows.Close()
				return nil, err
			}
			e.Zone = core.Zone(z)
			out = append(out, e)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ZoneOf is core.MinZone of the zones of the record's origin events (principle
// 5). A record with no origin gets core.ZoneExternal, the safe default; the
// checker reports it separately.
func ZoneOf(ctx context.Context, q store.Queryer, recordType core.RecordType, id string) (core.Zone, error) {
	origins, err := Origins(ctx, q, recordType, id)
	if err != nil {
		return "", err
	}
	return MinOriginZone(origins), nil
}

// MinOriginZone is core.MinZone over the zones of evs.
func MinOriginZone(evs []EventRef) core.Zone {
	zs := make([]core.Zone, len(evs))
	for i, e := range evs {
		zs[i] = e.Zone
	}
	return core.MinZone(zs...)
}

// DescendantSet is every derived record reachable from a set of events by
// following origin edges backwards (source -> derived), plus the embeddings
// rows of those records and of the root events.
type DescendantSet struct {
	Facts      []string       `json:"facts"`
	PageItems  []string       `json:"page_items"`
	Pages      []string       `json:"pages"`
	Other      []Ref          `json:"other"` // derived records of any other type
	Embeddings []EmbeddingKey `json:"embeddings"`
}

// Descendants collects every record derived, directly or transitively, from
// rootEventIDs. Pages are reached through their items and through edges that
// name the page itself. Entities are never collected (shared vocabulary).
// Every list is sorted.
func Descendants(ctx context.Context, q store.Queryer, rootEventIDs []string) (DescendantSet, error) {
	var set DescendantSet
	seen := map[string]bool{}
	var queue []Ref
	for _, id := range SortedUnique(rootEventIDs) {
		r := Ref{core.RecEvent, id}
		seen[r.key()] = true
		queue = append(queue, r)
	}
	var collected []Ref
	push := func(r Ref) {
		if r.Type == core.RecEntity || seen[r.key()] {
			return
		}
		seen[r.key()] = true
		collected = append(collected, r)
		queue = append(queue, r)
	}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		derived, err := scanRefs(ctx, q, `SELECT derived_type, derived_id FROM provenance
			WHERE source_type = ? AND source_id = ? AND relation IN `+originSQL+`
			ORDER BY derived_type, derived_id`, string(r.Type), r.ID)
		if err != nil {
			return set, err
		}
		for _, d := range derived {
			push(d)
		}
		if r.Type == core.RecPageItem {
			pages, err := scanIDs(ctx, q, `SELECT page_id FROM page_items WHERE id = ?`, r.ID)
			if err != nil {
				return set, err
			}
			for _, p := range pages {
				push(Ref{core.RecPage, p})
			}
		}
	}
	all := append([]Ref{}, collected...)
	for _, id := range SortedUnique(rootEventIDs) {
		all = append(all, Ref{core.RecEvent, id})
	}
	for _, r := range collected {
		switch r.Type {
		case core.RecFact:
			set.Facts = append(set.Facts, r.ID)
		case core.RecPageItem:
			set.PageItems = append(set.PageItems, r.ID)
		case core.RecPage:
			set.Pages = append(set.Pages, r.ID)
		default:
			set.Other = append(set.Other, r)
		}
	}
	sort.Strings(set.Facts)
	sort.Strings(set.PageItems)
	sort.Strings(set.Pages)
	SortRefs(set.Other)
	embs, err := EmbeddingsOf(ctx, q, all)
	if err != nil {
		return set, err
	}
	set.Embeddings = embs
	return set, nil
}

// EmbeddingsOf returns the embeddings rows of the given records, every model,
// sorted by (record_type, record_id, model_id).
func EmbeddingsOf(ctx context.Context, q store.Queryer, recs []Ref) ([]EmbeddingKey, error) {
	byType := map[core.RecordType][]string{}
	for _, r := range recs {
		byType[r.Type] = append(byType[r.Type], r.ID)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, string(t))
	}
	sort.Strings(types)
	var out []EmbeddingKey
	for _, t := range types {
		for _, chunk := range chunks(SortedUnique(byType[core.RecordType(t)]), 500) {
			rows, err := q.QueryContext(ctx, `SELECT record_type, record_id, model_id FROM embeddings
				WHERE record_type = ? AND record_id IN (`+placeholders(len(chunk))+`)
				ORDER BY record_type, record_id, model_id`, append([]any{t}, anys(chunk)...)...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var k EmbeddingKey
				var rt string
				if err := rows.Scan(&rt, &k.RecordID, &k.ModelID); err != nil {
					rows.Close()
					return nil, err
				}
				k.RecordType = core.RecordType(rt)
				out = append(out, k)
			}
			if err := rows.Close(); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.RecordType != b.RecordType {
			return a.RecordType < b.RecordType
		}
		if a.RecordID != b.RecordID {
			return a.RecordID < b.RecordID
		}
		return a.ModelID < b.ModelID
	})
	return out, nil
}

// ---------------------------------------------------------------------------
// Small SQL helpers, exported where internal/forget reuses them.

// SortedUnique returns ids sorted, without duplicates or empty strings.
func SortedUnique(ids []string) []string {
	m := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !m[id] {
			m[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// SortRefs sorts refs by (type, id).
func SortRefs(rs []Ref) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Type != rs[j].Type {
			return rs[i].Type < rs[j].Type
		}
		return rs[i].ID < rs[j].ID
	})
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func chunks(ss []string, n int) [][]string {
	var out [][]string
	for len(ss) > n {
		out = append(out, ss[:n])
		ss = ss[n:]
	}
	if len(ss) > 0 {
		out = append(out, ss)
	}
	return out
}

func scanRefs(ctx context.Context, q store.Queryer, query string, args ...any) ([]Ref, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ref
	for rows.Next() {
		var t, id string
		if err := rows.Scan(&t, &id); err != nil {
			return nil, err
		}
		out = append(out, Ref{core.RecordType(t), id})
	}
	return out, rows.Err()
}

func scanIDs(ctx context.Context, q store.Queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
