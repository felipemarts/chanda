package forget

import (
	"context"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/prov"
	"github.com/felipemarts/chanda/internal/store"
)

// Scope kinds (BASE.md "Forgetting": record id, document or source, entity,
// date range and search match).
const (
	ScopeRecord    = "record"     // Ref: any record id (evt_, fct_, pg_, pgi_, src_, doc_, ent_)
	ScopeDocument  = "document"   // Ref: document id or uri
	ScopeSource    = "source"     // Ref: source id or uri
	ScopeEntity    = "entity"     // Ref: entity id, canonical name or alias
	ScopeDateRange = "date_range" // From (inclusive) and To (exclusive) over events.recorded_at
	ScopeSearch    = "search"     // Query: every token must match (FTS5 over events)
)

// Scope selects what forget removes.
type Scope struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref,omitempty"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Query string `json:"query,omitempty"`
}

// resolved is a scope turned into root events plus records the scope names
// directly (removed even when they reach no root event).
type resolved struct {
	roots     []string
	facts     []string
	pages     []string
	items     []string
	entities  []string
	documents []string
	sources   []string
	// desc is the content-free scope description stored in the tombstone.
	desc map[string]any
	// Entity scope only (decision 0130): the names of the entities with the
	// mention rule applied, the terms used, and the records they matched.
	entityScope bool
	terms       []EntityTerm
	used        []mentionTerm
	mentions    []Mention
	// Search scope only: its FTS5 expression (every term), also matched
	// against learned keys. Content: never stored.
	searchExpr string
}

func resolve(ctx context.Context, q store.Queryer, ns string, s Scope) (*resolved, error) {
	r := &resolved{desc: map[string]any{"kind": s.Kind}}
	var err error
	switch s.Kind {
	case ScopeRecord:
		err = r.record(ctx, q, ns, s.Ref)
	case ScopeDocument:
		err = r.document(ctx, q, ns, s.Ref)
	case ScopeSource:
		err = r.source(ctx, q, ns, s.Ref)
	case ScopeEntity:
		err = r.entity(ctx, q, ns, s.Ref)
	case ScopeDateRange:
		err = r.dateRange(ctx, q, ns, s.From, s.To)
	case ScopeSearch:
		err = r.search(ctx, q, ns, s.Query)
	default:
		err = core.Errf(core.CodeInvalidArgument, "unknown forget scope %q", s.Kind)
	}
	if err != nil {
		return nil, err
	}
	r.roots = prov.SortedUnique(r.roots)
	return r, nil
}

func (r *resolved) record(ctx context.Context, q store.Queryer, ns, id string) error {
	rt := core.RecordTypeOf(id)
	switch rt {
	case core.RecSource:
		return r.source(ctx, q, ns, id)
	case core.RecDocument:
		return r.document(ctx, q, ns, id)
	case core.RecEntity:
		return r.entity(ctx, q, ns, id)
	}
	table := map[core.RecordType]string{core.RecEvent: "events", core.RecFact: "facts", core.RecPage: "pages", core.RecPageItem: "page_items"}[rt]
	if table == "" {
		return core.Errf(core.CodeInvalidArgument, "forget: %q is not a record id", id)
	}
	if err := mustExist(ctx, q, table, ns, id); err != nil {
		return err
	}
	r.desc["record_type"], r.desc["id"] = string(rt), id
	switch rt {
	case core.RecEvent:
		r.roots = append(r.roots, id)
		return nil
	case core.RecFact:
		r.facts = append(r.facts, id)
	case core.RecPage:
		r.pages = append(r.pages, id)
	case core.RecPageItem:
		r.items = append(r.items, id)
	}
	// A derived record would be rebuilt from its events (principle 1), so
	// forgetting it forgets the events it came from.
	evs, err := prov.Origins(ctx, q, rt, id)
	if err != nil {
		return err
	}
	for _, e := range evs {
		r.roots = append(r.roots, e.ID)
	}
	return nil
}

func mustExist(ctx context.Context, q store.Queryer, table, ns, id string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id = ? AND namespace = ?`, id, ns).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return core.Errf(core.CodeNotFound, "forget: %s not found", id)
	}
	return nil
}

func (r *resolved) document(ctx context.Context, q store.Queryer, ns, ref string) error {
	docs, err := ids(ctx, q, `SELECT id FROM documents WHERE namespace = ? AND (id = ? OR uri = ?) ORDER BY id`, ns, ref, ref)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return core.Errf(core.CodeNotFound, "forget: document not found")
	}
	r.desc["documents"] = docs
	return r.addDocuments(ctx, q, docs)
}

func (r *resolved) addDocuments(ctx context.Context, q store.Queryer, docs []string) error {
	r.documents = append(r.documents, docs...)
	for _, d := range docs {
		evs, err := ids(ctx, q, `SELECT id FROM events WHERE doc_id = ?1
			UNION SELECT event_id FROM document_chunks WHERE doc_id = ?1 ORDER BY 1`, d)
		if err != nil {
			return err
		}
		r.roots = append(r.roots, evs...)
	}
	return nil
}

func (r *resolved) source(ctx context.Context, q store.Queryer, ns, ref string) error {
	srcs, err := ids(ctx, q, `SELECT id FROM sources WHERE namespace = ? AND (id = ? OR uri = ?) ORDER BY id`, ns, ref, ref)
	if err != nil {
		return err
	}
	if len(srcs) == 0 {
		return core.Errf(core.CodeNotFound, "forget: source not found")
	}
	r.desc["sources"] = srcs
	r.sources = append(r.sources, srcs...)
	for _, s := range srcs {
		evs, err := ids(ctx, q, `SELECT id FROM events WHERE source_id = ? ORDER BY id`, s)
		if err != nil {
			return err
		}
		r.roots = append(r.roots, evs...)
		docs, err := ids(ctx, q, `SELECT id FROM documents WHERE source_id = ? ORDER BY id`, s)
		if err != nil {
			return err
		}
		if err := r.addDocuments(ctx, q, docs); err != nil {
			return err
		}
	}
	return nil
}

func (r *resolved) dateRange(ctx context.Context, q store.Queryer, ns, from, to string) error {
	if from == "" || to == "" {
		return core.Errf(core.CodeInvalidArgument, "forget: date range needs from and to")
	}
	f, err := core.ParseTime(from)
	if err != nil {
		return err
	}
	t, err := core.ParseTime(to)
	if err != nil {
		return err
	}
	if !f.Before(t) {
		return core.Errf(core.CodeInvalidArgument, "forget: from must be before to")
	}
	r.desc["from"], r.desc["to"] = core.FormatTime(f), core.FormatTime(t)
	evs, err := ids(ctx, q, `SELECT id FROM events WHERE namespace = ? AND recorded_at >= ? AND recorded_at < ? ORDER BY id`,
		ns, core.FormatTime(f), core.FormatTime(t))
	r.roots = append(r.roots, evs...)
	return err
}

func (r *resolved) search(ctx context.Context, q store.Queryer, ns, query string) error {
	m := ftsAllTerms(query)
	if m == "" {
		return core.Errf(core.CodeInvalidArgument, "forget: search query has no terms")
	}
	// The query text is content: the tombstone keeps only its size.
	r.desc["terms"] = len(tokens(query, 1))
	r.searchExpr = m
	evs, err := ids(ctx, q, `SELECT record_id FROM events_fts WHERE events_fts MATCH ? AND namespace = ? ORDER BY record_id`, m, ns)
	r.roots = append(r.roots, evs...)
	return err
}

func ids(ctx context.Context, q store.Queryer, query string, args ...any) ([]string, error) {
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
