package prov

import (
	"context"
	"fmt"
	"sort"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Violation kinds reported by Check.
const (
	VNoOrigin          = "no_origin"           // fact, page or page item reaches no event
	VZoneAboveOrigins  = "zone_above_origins"  // zone rose above its origins without a promotion
	VDanglingEdge      = "dangling_edge"       // edge endpoint does not exist (in the namespace)
	VOrphanEmbedding   = "orphan_embedding"    // vector of a record that does not exist
	VEmbeddingNoOrigin = "embedding_no_origin" // vector of a derived record that reaches no event
)

// Violation is one integrity problem. Detail never holds record content.
type Violation struct {
	Kind       string          `json:"kind"`
	RecordType core.RecordType `json:"record_type"`
	RecordID   string          `json:"record_id"`
	Detail     string          `json:"detail,omitempty"`
}

// Report is the outcome of Check.
type Report struct {
	OK         bool           `json:"ok"`
	Checked    map[string]int `json:"checked"`
	Violations []Violation    `json:"violations"`
}

// graphState is the provenance graph of one namespace (or all), in memory.
type graphState struct {
	exists  map[core.RecordType]map[string]bool
	zone    map[string]core.Zone // fact, page, page_item and event zones by id
	edges   map[string][]Ref     // derived key -> origin-edge sources
	items   map[string][]string  // page id -> item ids
	ceiling map[string]core.Zone // record key -> highest promoted zone
}

func nsClause(ns string) (string, []any) {
	if ns == "" {
		return "1=1", nil
	}
	return "namespace = ?", []any{ns}
}

func loadGraph(ctx context.Context, q store.Queryer, ns string) (*graphState, []edgeRow, []EmbeddingKey, error) {
	g := &graphState{
		exists:  map[core.RecordType]map[string]bool{},
		zone:    map[string]core.Zone{},
		edges:   map[string][]Ref{},
		items:   map[string][]string{},
		ceiling: map[string]core.Zone{},
	}
	where, args := nsClause(ns)
	tables := []struct {
		rt      core.RecordType
		table   string
		zoneCol bool
		pageCol bool
	}{
		{core.RecEvent, "events", true, false},
		{core.RecFact, "facts", true, false},
		{core.RecPage, "pages", true, false},
		{core.RecPageItem, "page_items", true, true},
		{core.RecEntity, "entities", false, false},
		{core.RecSource, "sources", true, false},
		{core.RecDocument, "documents", false, false},
	}
	for _, tb := range tables {
		cols := "id, '', ''"
		if tb.zoneCol {
			cols = "id, trust_zone, ''"
		}
		if tb.pageCol {
			cols = "id, trust_zone, page_id"
		}
		rows, err := q.QueryContext(ctx, `SELECT `+cols+` FROM `+tb.table+` WHERE `+where+` ORDER BY id`, args...)
		if err != nil {
			return nil, nil, nil, err
		}
		m := map[string]bool{}
		for rows.Next() {
			var id, z, page string
			if err := rows.Scan(&id, &z, &page); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			m[id] = true
			if z != "" && tb.rt != core.RecSource {
				g.zone[id] = core.Zone(z)
			}
			if page != "" {
				g.items[page] = append(g.items[page], id)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, nil, nil, err
		}
		g.exists[tb.rt] = m
	}
	rows, err := q.QueryContext(ctx, `SELECT derived_type, derived_id, source_type, source_id, relation FROM provenance
		WHERE `+where+` ORDER BY derived_type, derived_id, source_type, source_id, relation`, args...)
	if err != nil {
		return nil, nil, nil, err
	}
	var edges []edgeRow
	for rows.Next() {
		var r edgeRow
		var dt, st string
		if err := rows.Scan(&dt, &r.derived.ID, &st, &r.source.ID, &r.relation); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		r.derived.Type, r.source.Type = core.RecordType(dt), core.RecordType(st)
		edges = append(edges, r)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	for _, r := range edges {
		if IsOriginRelation(r.relation) {
			g.edges[r.derived.key()] = append(g.edges[r.derived.key()], r.source)
		}
	}
	rows, err = q.QueryContext(ctx, `SELECT record_type, record_id, to_zone FROM promotions WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var rt, id, z string
		if err := rows.Scan(&rt, &id, &z); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		k := Ref{core.RecordType(rt), id}.key()
		if core.Zone(z).Rank() > g.ceiling[k].Rank() {
			g.ceiling[k] = core.Zone(z)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT record_type, record_id, model_id FROM embeddings WHERE `+where+`
		ORDER BY record_type, record_id, model_id`, args...)
	if err != nil {
		return nil, nil, nil, err
	}
	var embs []EmbeddingKey
	for rows.Next() {
		var k EmbeddingKey
		var rt string
		if err := rows.Scan(&rt, &k.RecordID, &k.ModelID); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		k.RecordType = core.RecordType(rt)
		embs = append(embs, k)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	return g, edges, embs, nil
}

type edgeRow struct {
	derived, source Ref
	relation        string
}

// origins returns the existing origin events of r (cycle-safe).
func (g *graphState) origins(r Ref) []string {
	seen := map[string]bool{}
	var out []string
	stack := []Ref{r}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n.key()] {
			continue
		}
		seen[n.key()] = true
		if n.Type == core.RecEvent {
			if g.exists[core.RecEvent][n.ID] {
				out = append(out, n.ID)
			}
			continue
		}
		stack = append(stack, g.edges[n.key()]...)
		if n.Type == core.RecPage {
			for _, it := range g.items[n.ID] {
				stack = append(stack, Ref{core.RecPageItem, it})
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Check verifies the provenance graph of namespace ns ("" checks every
// namespace) against BASE.md "Integrity rules":
//
//   - every fact, page item and page reaches at least one event (pages
//     through their items); every vector belongs to an existing record and,
//     for derived records, reaches an event through it;
//   - every derived record's zone is at most core.MinZone of its origins,
//     unless a promotion (promote tool) raised it to at most that zone;
//   - every edge endpoint exists.
//
// It reads only; violations are returned in the report, not as an error.
func Check(ctx context.Context, q store.Queryer, ns string) (Report, error) {
	g, edges, embs, err := loadGraph(ctx, q, ns)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Checked: map[string]int{}}
	add := func(kind string, rt core.RecordType, id, detail string) {
		rep.Violations = append(rep.Violations, Violation{kind, rt, id, detail})
	}
	for _, rt := range []core.RecordType{core.RecFact, core.RecPageItem, core.RecPage} {
		ids := sortedKeys(g.exists[rt])
		rep.Checked[string(rt)+"s"] = len(ids)
		for _, id := range ids {
			r := Ref{rt, id}
			evs := g.origins(r)
			if len(evs) == 0 {
				add(VNoOrigin, rt, id, "")
				continue
			}
			zs := make([]core.Zone, len(evs))
			for i, ev := range evs {
				zs[i] = g.zone[ev]
			}
			allowed := core.MinZone(zs...)
			if c := g.ceiling[r.key()]; c.Rank() > allowed.Rank() {
				allowed = c
			}
			if z := g.zone[id]; z.Rank() > allowed.Rank() || !z.Valid() {
				add(VZoneAboveOrigins, rt, id, fmt.Sprintf("zone %s above %s", z, allowed))
			}
		}
	}
	rep.Checked["edges"] = len(edges)
	for _, e := range edges {
		for _, end := range []Ref{e.derived, e.source} {
			m, known := g.exists[end.Type]
			switch {
			case !known:
				add(VDanglingEdge, end.Type, end.ID, "unknown record type in edge "+e.relation)
			case !m[end.ID]:
				add(VDanglingEdge, end.Type, end.ID, fmt.Sprintf("edge %s %s -> %s missing endpoint", e.relation, e.derived.ID, e.source.ID))
			}
		}
	}
	rep.Checked["embeddings"] = len(embs)
	for _, k := range embs {
		m, known := g.exists[k.RecordType]
		if !known || !m[k.RecordID] {
			add(VOrphanEmbedding, k.RecordType, k.RecordID, "model "+k.ModelID)
			continue
		}
		switch k.RecordType {
		case core.RecFact, core.RecPage, core.RecPageItem:
			if len(g.origins(Ref{k.RecordType, k.RecordID})) == 0 {
				add(VEmbeddingNoOrigin, k.RecordType, k.RecordID, "model "+k.ModelID)
			}
		}
	}
	sort.SliceStable(rep.Violations, func(i, j int) bool {
		a, b := rep.Violations[i], rep.Violations[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.RecordType != b.RecordType {
			return a.RecordType < b.RecordType
		}
		if a.RecordID != b.RecordID {
			return a.RecordID < b.RecordID
		}
		return a.Detail < b.Detail
	})
	rep.OK = len(rep.Violations) == 0
	return rep, nil
}

// VerifyReport is the answer of the verify tool.
type VerifyReport struct {
	OK    bool               `json:"ok"`
	Audit audit.VerifyResult `json:"audit"`
	Graph Report             `json:"graph"`
}

// Verify checks the audit hash chain and the provenance graph of every
// namespace (BASE.md: "The verify tool checks the chain and the integrity of
// the graph").
func Verify(ctx context.Context, e *env.Env) (VerifyReport, error) {
	db := e.Store.DB()
	a, err := audit.Verify(ctx, db)
	if err != nil {
		return VerifyReport{}, err
	}
	g, err := Check(ctx, db, "")
	if err != nil {
		return VerifyReport{}, err
	}
	return VerifyReport{OK: a.OK && g.OK, Audit: a, Graph: g}, nil
}
