package embed

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/store"
)

// RecordRef names one record.
type RecordRef struct {
	Type core.RecordType
	ID   string
}

// embeddable lists the record types that get vectors, where their text lives
// and when they are live. Retired records are not (re)indexed, but vectors
// they already have are kept: the read path filters retired records, and only
// forget deletes.
//
// query marks a type embedded with the query role (core.RoleEmbedder's
// EmbedQuery; Embed when the embedder has no roles): a learned key is a
// past query, so key and new query compare as two queries (decision 0145).
// Every other type is a passage.
var embeddable = []struct {
	rt    core.RecordType
	table string
	text  string // SQL expression of the embedded text
	live  string // SQL predicate: record is still indexable
	query bool   // embedded with the query role
}{
	{core.RecEvent, "events", "content", "retired_at IS NULL", false},
	{core.RecFact, "facts", "text", "1", false},
	{core.RecPage, "pages", "title || char(10) || char(10) || body", "status <> 'retired'", false},
	{core.RecPageItem, "page_items", "text", "status <> 'retired'", false},
	{core.RecKey, "learned_keys", "text", "1", true},
}

// queryRole reports whether records of type rt are embedded as queries.
func queryRole(rt core.RecordType) bool {
	for _, e := range embeddable {
		if e.rt == rt {
			return e.query
		}
	}
	return false
}

// recordText returns the text of a record and whether it is live in ns.
// A missing record, one from another namespace or of an unknown type is not live.
func recordText(ctx context.Context, q store.Queryer, ns string, rt core.RecordType, id string) (string, bool, error) {
	for _, e := range embeddable {
		if e.rt != rt {
			continue
		}
		var text string
		var live bool
		err := q.QueryRowContext(ctx,
			`SELECT `+e.text+`, (`+e.live+`) FROM `+e.table+` WHERE id = ? AND namespace = ?`, id, ns).Scan(&text, &live)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return text, live, nil
	}
	return "", false, nil
}

// MissingFor lists the live records of namespace that have no vector for
// modelID, ordered by record type then id. During a model migration it is
// what still needs backfilling; empty means the new model is complete.
func (x *ExactIndex) MissingFor(ctx context.Context, namespace, modelID string) ([]RecordRef, error) {
	return missingFor(ctx, x.env.Store.DB(), namespace, modelID)
}

func missingFor(ctx context.Context, q store.Queryer, ns, modelID string) ([]RecordRef, error) {
	var out []RecordRef
	for _, e := range embeddable { // fixed order; ids sorted within each type
		rows, err := q.QueryContext(ctx,
			`SELECT r.id FROM `+e.table+` r
			 WHERE r.namespace = ? AND (`+e.live+`)
			   AND NOT EXISTS (SELECT 1 FROM embeddings m
			                   WHERE m.record_type = ? AND m.record_id = r.id AND m.model_id = ?)
			 ORDER BY r.id`, ns, string(e.rt), modelID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, RecordRef{e.rt, id})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sortRefs(out)
	return out, nil
}

// sortRefs orders by (type, id): embeddable order is not alphabetical.
func sortRefs(rs []RecordRef) {
	sort.Slice(rs, func(i, j int) bool { return lessRef(rs[i], rs[j]) })
}

func lessRef(a, b RecordRef) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	return a.ID < b.ID
}
