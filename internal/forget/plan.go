package forget

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/prov"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Removed lists the ids of every row forget deletes, by kind. Ids are not
// content; the lists are returned to the caller, never stored.
type Removed struct {
	Events     []string `json:"events"`
	Facts      []string `json:"facts"`
	PageItems  []string `json:"page_items"`
	Pages      []string `json:"pages"`
	Entities   []string `json:"entities"`
	Documents  []string `json:"documents"`
	Sources    []string `json:"sources"`
	Retrievals []string `json:"retrievals"`
	Outcomes   []string `json:"outcomes"`
	Keys       []string `json:"keys"` // learned query keys (decision 0145)
}

// plan is everything one forget will do, computed before any deletion.
type plan struct {
	scope     *resolved
	removed   Removed
	factsKept []string // facts with another live origin: lose only the edges
	rebuild   []string // pages that keep some items: needs_rebuild, body recomputed
	other     []prov.Ref
	embs      []prov.EmbeddingKey
	snapshots []string
	sleepRuns []string // sleep_runs rows whose snapshot path is cleared
	// Probe material, captured before deletion.
	terms   map[string][]string // removed event id -> its rarest tokens
	distinc map[string]bool     // tokens found only in removed events
	needles []needle
	vectors []savedVector
	hashes  []string // content_hash of removed events (tombstone_keeps_hash only)
	// gone is every removed record id (events, facts, items, pages, entities,
	// documents, sources, retrievals, outcomes, learned keys).
	gone map[string]bool
}

type needle struct {
	owner string // id of the removed record the bytes come from
	b     string
}

type savedVector struct {
	ns, owner, model string
	vec              []float32
}

func set(ids ...[]string) map[string]bool {
	m := map[string]bool{}
	for _, l := range ids {
		for _, id := range l {
			m[id] = true
		}
	}
	return m
}

// buildPlan resolves the scope, walks the graph and decides what is removed,
// what survives without the edge and which pages are rebuilt.
func buildPlan(ctx context.Context, q store.Queryer, cfg *config.Config, dbPath, ns string, s Scope) (*plan, error) {
	rs, err := resolve(ctx, q, ns, s)
	if err != nil {
		return nil, err
	}
	p := &plan{scope: rs}
	p.removed.Events = rs.roots
	gone := set(rs.roots)
	desc, err := prov.Descendants(ctx, q, rs.roots)
	if err != nil {
		return nil, err
	}
	p.other = desc.Other

	// Facts: removed when every origin is forgotten (or the scope names it);
	// otherwise the fact survives and loses only the edges.
	forced := set(rs.facts)
	for _, f := range prov.SortedUnique(append(append([]string{}, desc.Facts...), rs.facts...)) {
		origins, err := prov.Origins(ctx, q, core.RecFact, f)
		if err != nil {
			return nil, err
		}
		if forced[f] || allIn(origins, gone) {
			p.removed.Facts = append(p.removed.Facts, f)
		} else {
			p.factsKept = append(p.factsKept, f)
		}
	}
	// Page items: removed when any origin is forgotten, since their text
	// cannot be split by origin.
	forced = set(rs.items)
	for _, it := range prov.SortedUnique(append(append([]string{}, desc.PageItems...), rs.items...)) {
		origins, err := prov.Origins(ctx, q, core.RecPageItem, it)
		if err != nil {
			return nil, err
		}
		if forced[it] || anyIn(origins, gone) {
			p.removed.PageItems = append(p.removed.PageItems, it)
		}
	}
	// Pages: any forgotten origin takes the page out of reads. A page left
	// without items (or named by the scope) is removed with its items.
	goneItems := set(p.removed.PageItems)
	forced = set(rs.pages)
	pages := append(append([]string{}, desc.Pages...), rs.pages...)
	for _, it := range p.removed.PageItems {
		pg, err := ids(ctx, q, `SELECT page_id FROM page_items WHERE id = ?`, it)
		if err != nil {
			return nil, err
		}
		pages = append(pages, pg...)
	}
	for _, pg := range prov.SortedUnique(pages) {
		items, err := ids(ctx, q, `SELECT id FROM page_items WHERE page_id = ? ORDER BY id`, pg)
		if err != nil {
			return nil, err
		}
		left := 0
		for _, it := range items {
			if !goneItems[it] {
				left++
			}
		}
		if forced[pg] || left == 0 {
			p.removed.Pages = append(p.removed.Pages, pg)
			for _, it := range items {
				if !goneItems[it] {
					goneItems[it] = true
					p.removed.PageItems = append(p.removed.PageItems, it)
				}
			}
		} else {
			p.rebuild = append(p.rebuild, pg)
		}
	}
	p.removed.PageItems = prov.SortedUnique(p.removed.PageItems)
	for k := range set(p.removed.Facts, p.removed.PageItems, p.removed.Pages) {
		gone[k] = true
	}

	if err := p.planEntities(ctx, q, ns, gone); err != nil {
		return nil, err
	}
	if err := p.planContainers(ctx, q, gone); err != nil {
		return nil, err
	}

	// Vectors of every removed record and of rebuilt pages (their old body
	// held the forgotten text).
	var refs []prov.Ref
	add := func(rt core.RecordType, l []string) {
		for _, id := range l {
			refs = append(refs, prov.Ref{Type: rt, ID: id})
		}
	}
	add(core.RecEvent, p.removed.Events)
	add(core.RecFact, p.removed.Facts)
	add(core.RecPageItem, p.removed.PageItems)
	add(core.RecPage, p.removed.Pages)
	add(core.RecPage, p.rebuild)
	add(core.RecEntity, p.removed.Entities)
	if p.embs, err = prov.EmbeddingsOf(ctx, q, refs); err != nil {
		return nil, err
	}

	if err := p.captureProbes(ctx, q, cfg, ns); err != nil {
		return nil, err
	}
	if err := p.planUsage(ctx, q, ns, gone); err != nil {
		return nil, err
	}
	if err := p.planKeys(ctx, q, ns, gone); err != nil {
		return nil, err
	}
	p.gone = gone
	p.snapshots, p.sleepRuns, err = listSnapshots(ctx, q, cfg, dbPath)
	return p, err
}

func allIn(evs []prov.EventRef, m map[string]bool) bool {
	for _, e := range evs {
		if !m[e.ID] {
			return false
		}
	}
	return true
}

func anyIn(evs []prov.EventRef, m map[string]bool) bool {
	for _, e := range evs {
		if m[e.ID] {
			return true
		}
	}
	return false
}

// planEntities removes the scope's entities, and entities that only removed
// records referred to (otherwise their names would outlive the content).
// Entities still referenced by a live record are shared vocabulary and stay.
func (p *plan) planEntities(ctx context.Context, q store.Queryer, ns string, gone map[string]bool) error {
	forced := set(p.scope.entities)
	cands := append([]string{}, p.scope.entities...)
	for _, id := range sortedKeys(gone) {
		ents, err := ids(ctx, q, `SELECT source_id FROM provenance WHERE derived_id = ? AND source_type = 'entity' ORDER BY source_id`, id)
		if err != nil {
			return err
		}
		cands = append(cands, ents...)
	}
	for _, f := range p.removed.Facts {
		subj, err := ids(ctx, q, `SELECT subject FROM facts WHERE id = ?`, f)
		if err != nil {
			return err
		}
		cands = append(cands, subj...)
	}
	for _, ent := range prov.SortedUnique(cands) {
		if forced[ent] {
			p.removed.Entities = append(p.removed.Entities, ent)
			continue
		}
		refs, err := ids(ctx, q, `SELECT derived_id FROM provenance WHERE source_type = 'entity' AND source_id = ?1
			UNION SELECT id FROM facts WHERE subject = ?1
			UNION SELECT id FROM entities WHERE merged_into = ?1 ORDER BY 1`, ent)
		if err != nil {
			return err
		}
		live := false
		for _, r := range refs {
			if !gone[r] && !forced[r] {
				live = true
				break
			}
		}
		var exists int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM entities WHERE id = ? AND namespace = ?`, ent, ns).Scan(&exists); err != nil {
			return err
		}
		if !live && exists > 0 {
			p.removed.Entities = append(p.removed.Entities, ent)
		}
	}
	for _, e := range p.removed.Entities {
		gone[e] = true
	}
	return nil
}

// planContainers removes documents and sources left without events.
func (p *plan) planContainers(ctx context.Context, q store.Queryer, gone map[string]bool) error {
	docs := append([]string{}, p.scope.documents...)
	srcs := append([]string{}, p.scope.sources...)
	for _, ev := range p.removed.Events {
		d, err := ids(ctx, q, `SELECT doc_id FROM events WHERE id = ?1 AND doc_id IS NOT NULL
			UNION SELECT doc_id FROM document_chunks WHERE event_id = ?1 ORDER BY 1`, ev)
		if err != nil {
			return err
		}
		docs = append(docs, d...)
		s, err := ids(ctx, q, `SELECT source_id FROM events WHERE id = ?`, ev)
		if err != nil {
			return err
		}
		srcs = append(srcs, s...)
	}
	forced := set(p.scope.documents, p.scope.sources)
	for _, d := range prov.SortedUnique(docs) {
		evs, err := ids(ctx, q, `SELECT id FROM events WHERE doc_id = ?1 UNION SELECT event_id FROM document_chunks WHERE doc_id = ?1 ORDER BY 1`, d)
		if err != nil {
			return err
		}
		if forced[d] || allGone(evs, gone) {
			p.removed.Documents = append(p.removed.Documents, d)
			gone[d] = true
			s, err := ids(ctx, q, `SELECT source_id FROM documents WHERE id = ?`, d)
			if err != nil {
				return err
			}
			srcs = append(srcs, s...)
		}
	}
	for _, s := range prov.SortedUnique(srcs) {
		refs, err := ids(ctx, q, `SELECT id FROM events WHERE source_id = ?1 UNION SELECT id FROM documents WHERE source_id = ?1 ORDER BY 1`, s)
		if err != nil {
			return err
		}
		if forced[s] || allGone(refs, gone) {
			p.removed.Sources = append(p.removed.Sources, s)
			gone[s] = true
		}
	}
	return nil
}

func allGone(ids []string, gone map[string]bool) bool {
	for _, id := range ids {
		if !gone[id] {
			return false
		}
	}
	return true
}

// planUsage selects the learning rows that cite the forgotten content:
// retrievals whose candidates, delivered list or params name a removed
// record, or whose query text holds a term found only in removed events or a
// probe byte string; their outcomes; and outcomes whose used_ids name a
// removed record.
func (p *plan) planUsage(ctx context.Context, q store.Queryer, ns string, gone map[string]bool) error {
	type ret struct{ id, query, cites string }
	rows, err := q.QueryContext(ctx, `SELECT id, query_text, candidates || delivered || params FROM retrievals WHERE namespace = ? ORDER BY id`, ns)
	if err != nil {
		return err
	}
	var rets []ret
	for rows.Next() {
		var r ret
		if err := rows.Scan(&r.id, &r.query, &r.cites); err != nil {
			rows.Close()
			return err
		}
		rets = append(rets, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	goneIDs := sortedKeys(gone)
	citesGone := func(s string) bool {
		for _, id := range goneIDs {
			if strings.Contains(s, `"`+id+`"`) {
				return true
			}
		}
		return false
	}
	goneRet := map[string]bool{}
	for _, r := range rets {
		hit := citesGone(r.cites)
		for _, t := range tokens(r.query, 1) {
			hit = hit || p.distinc[t]
		}
		for _, n := range p.needles {
			hit = hit || strings.Contains(r.query, n.b)
		}
		if hit {
			p.removed.Retrievals = append(p.removed.Retrievals, r.id)
			goneRet[r.id] = true
		}
	}
	rows, err = q.QueryContext(ctx, `SELECT id, retrieval_id, used_ids FROM outcomes WHERE namespace = ? ORDER BY id`, ns)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, rid, used string
		if err := rows.Scan(&id, &rid, &used); err != nil {
			rows.Close()
			return err
		}
		if goneRet[rid] || citesGone(used) {
			p.removed.Outcomes = append(p.removed.Outcomes, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for k := range set(p.removed.Retrievals, p.removed.Outcomes) {
		gone[k] = true
	}
	return nil
}

// planKeys selects the learned keys (decision 0145) that go: keys of a
// removed record, keys of a removed retrieval, and keys whose text the
// scope matches: a term found only in removed events or a probe byte
// string (the rule planUsage applies to query text), a used term of the
// entity scope (mention rule, over learned_keys_fts), or every term of the
// search scope. Their vectors join the removed vectors.
func (p *plan) planKeys(ctx context.Context, q store.Queryer, ns string, gone map[string]bool) error {
	byText := map[string]bool{}
	var exprs []string
	for _, t := range p.scope.used {
		exprs = append(exprs, t.ftsQuery())
	}
	if p.scope.searchExpr != "" {
		exprs = append(exprs, p.scope.searchExpr)
	}
	for _, x := range exprs {
		hits, err := ids(ctx, q, `SELECT record_id FROM learned_keys_fts WHERE learned_keys_fts MATCH ? AND namespace = ? ORDER BY record_id`, x, ns)
		if err != nil {
			return err
		}
		for _, h := range hits {
			byText[h] = true
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT id, record_id, retrieval_id, text FROM learned_keys WHERE namespace = ? ORDER BY id`, ns)
	if err != nil {
		return err
	}
	type key struct{ id, record, retrieval, text string }
	var ks []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.record, &k.retrieval, &k.text); err != nil {
			rows.Close()
			return err
		}
		ks = append(ks, k)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, k := range ks {
		hit := gone[k.record] || gone[k.retrieval] || byText[k.id]
		for _, t := range tokens(k.text, 1) {
			hit = hit || p.distinc[t]
		}
		for _, n := range p.needles {
			hit = hit || strings.Contains(k.text, n.b)
		}
		if hit {
			p.removed.Keys = append(p.removed.Keys, k.id)
		}
	}
	refs := make([]prov.Ref, len(p.removed.Keys))
	for i, id := range p.removed.Keys {
		refs[i] = prov.Ref{Type: core.RecKey, ID: id}
		gone[id] = true
	}
	embs, err := prov.EmbeddingsOf(ctx, q, refs)
	if err != nil {
		return err
	}
	p.embs = append(p.embs, embs...)
	return nil
}

// captureProbes saves, before deletion, what the probes look for afterwards:
// the rarest tokens of each removed event, byte strings of every removed
// text and the removed vectors.
func (p *plan) captureProbes(ctx context.Context, q store.Queryer, cfg *config.Config, ns string) error {
	fc := cfg.Forget
	goneEv := set(p.removed.Events)
	p.terms = map[string][]string{}
	p.distinc = map[string]bool{}
	type stat struct {
		df, live int
	}
	cache := map[string]stat{}
	for _, ev := range p.removed.Events {
		var content, hash string
		if err := q.QueryRowContext(ctx, `SELECT content, content_hash FROM events WHERE id = ?`, ev).Scan(&content, &hash); err != nil {
			return err
		}
		p.hashes = append(p.hashes, hash)
		for _, b := range needles(content, fc.ProbeNeedles, fc.ProbeNeedleBytes) {
			p.needles = append(p.needles, needle{ev, b})
		}
		toks := tokens(content, 3)
		// Bound the work on long chunks: the longest tokens are the likeliest rare ones.
		sort.SliceStable(toks, func(i, j int) bool { return len(toks[i]) > len(toks[j]) })
		if len(toks) > 64 {
			toks = toks[:64]
		}
		for _, t := range toks {
			if _, ok := cache[t]; ok {
				continue
			}
			hits, err := ids(ctx, q, `SELECT record_id FROM events_fts WHERE events_fts MATCH ? AND namespace = ? ORDER BY record_id`, ftsPhrase(t), ns)
			if err != nil {
				return err
			}
			st := stat{df: len(hits)}
			for _, h := range hits {
				if !goneEv[h] {
					st.live++
				}
			}
			cache[t] = st
			if st.df > 0 && st.live == 0 {
				p.distinc[t] = true
			}
		}
		sort.SliceStable(toks, func(i, j int) bool {
			a, b := cache[toks[i]], cache[toks[j]]
			if (a.live > 0) != (b.live > 0) {
				return a.live == 0
			}
			if a.df != b.df {
				return a.df < b.df
			}
			if len(toks[i]) != len(toks[j]) {
				return len(toks[i]) > len(toks[j])
			}
			return toks[i] < toks[j]
		})
		var pick []string
		for _, t := range toks {
			if len(pick) == fc.ProbeTerms {
				break
			}
			if cache[t].df > 0 {
				pick = append(pick, t)
			}
		}
		p.terms[ev] = pick
	}
	texts := []struct {
		table, col string
		ids        []string
	}{
		{"facts", "text", p.removed.Facts},
		{"page_items", "text", p.removed.PageItems},
	}
	for _, tx := range texts {
		for _, id := range tx.ids {
			var s string
			if err := q.QueryRowContext(ctx, `SELECT `+tx.col+` FROM `+tx.table+` WHERE id = ?`, id).Scan(&s); err != nil {
				return err
			}
			for _, b := range needles(s, fc.ProbeNeedles, fc.ProbeNeedleBytes) {
				p.needles = append(p.needles, needle{id, b})
			}
		}
	}
	for _, k := range p.embs {
		var ns2 string
		var blob []byte
		if err := q.QueryRowContext(ctx, `SELECT namespace, vector FROM embeddings WHERE record_type = ? AND record_id = ? AND model_id = ?`,
			string(k.RecordType), k.RecordID, k.ModelID).Scan(&ns2, &blob); err != nil {
			return err
		}
		p.vectors = append(p.vectors, savedVector{ns2, k.RecordID, k.ModelID, decodeVector(blob)})
	}
	return nil
}

func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// listSnapshots returns the snapshot files forget deletes (decision 0050):
// every path recorded in sleep_runs.snapshot and every regular file in
// cfg.Sleep.SnapshotDir. Relative paths resolve against the database's
// directory. The database, its WAL and shared-memory files are never listed,
// and the snapshot directory is ignored when it is the database's directory.
func listSnapshots(ctx context.Context, q store.Queryer, cfg *config.Config, dbPath string) ([]string, []string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return nil, nil, nil
	}
	dbAbs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, nil, err
	}
	base := filepath.Dir(dbAbs)
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		return filepath.Clean(p)
	}
	protected := map[string]bool{dbAbs: true, dbAbs + "-wal": true, dbAbs + "-shm": true, dbAbs + "-journal": true}
	var files []string
	rows, err := q.QueryContext(ctx, `SELECT id, snapshot FROM sleep_runs WHERE snapshot IS NOT NULL AND snapshot != '' ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	var runs []string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return nil, nil, err
		}
		runs = append(runs, id)
		files = append(files, abs(path))
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if cfg.Sleep.SnapshotDir != "" {
		dir := abs(cfg.Sleep.SnapshotDir)
		if dir != base {
			entries, err := os.ReadDir(dir)
			if err != nil && !os.IsNotExist(err) {
				return nil, nil, err
			}
			for _, en := range entries {
				if en.Type().IsRegular() {
					files = append(files, filepath.Join(dir, en.Name()))
				}
			}
		}
	}
	var out []string
	for _, f := range prov.SortedUnique(files) {
		if protected[f] {
			continue
		}
		if fi, err := os.Lstat(f); err == nil && fi.Mode().IsRegular() {
			out = append(out, f)
		}
	}
	return out, runs, nil
}
