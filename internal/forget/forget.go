// Package forget implements cascade deletion with verification (BASE.md
// "Forgetting", differentiator "Verifiable forgetting").
//
// Forget resolves a scope into root events, walks the provenance graph to
// every derived record, deletes physically in one transaction, scrubs the
// file (FTS optimize, WAL truncation, VACUUM with secure_delete), deletes
// snapshots, writes a content-free tombstone and audit entry, and then runs
// its probes before answering: full-text search, vector search, a byte scan
// of the database and WAL files, a scan of every table for removed ids, the
// snapshot files and, for the entity scope, the entity's names in what
// remains (decision 0130).
package forget

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/prov"
)

// Report is the answer of forget: what was (or, with DryRun, would be)
// removed, and the verification report. It never holds content.
type Report struct {
	DryRun bool           `json:"dry_run"`
	Scope  map[string]any `json:"scope"`
	// Removed lists the ids deleted. FactsKept had another live origin and
	// only lost their edges; PagesRebuild left reads (needs_rebuild) and
	// lost the items that cited forgotten events.
	Removed      Removed        `json:"removed"`
	FactsKept    []string       `json:"facts_kept"`
	PagesRebuild []string       `json:"pages_rebuild"`
	Snapshots    int            `json:"snapshots"`
	Counts       map[string]int `json:"counts"`
	TombstoneID  string         `json:"tombstone_id,omitempty"`
	AuditID      int64          `json:"audit_id,omitempty"`
	Verification *Verification  `json:"verification,omitempty"`
	// Entity scope only (decision 0130): every name of the scope's entities
	// with the mention rule applied, and every record whose text names a
	// used term, with the term that matched. They hold entity names, so they
	// are returned to the caller and never stored.
	EntityTerms []EntityTerm `json:"entity_terms,omitempty"`
	Mentions    []Mention    `json:"mentions,omitempty"`
}

// Forget removes scope from namespace ns, or with dryRun only reports what
// would be removed. After a real run the verification report is filled; if a
// probe fails, the report is returned together with a core.CodeIntegrity error.
func Forget(ctx context.Context, e *env.Env, ns string, scope Scope, dryRun bool, actor string) (Report, error) {
	if ns == "" || actor == "" {
		return Report{}, core.Errf(core.CodeInvalidArgument, "forget needs a namespace and an actor")
	}
	db := e.Store.DB()
	dbPath := e.Store.Path()
	p, err := buildPlan(ctx, db, e.Cfg, dbPath, ns, scope)
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		DryRun: dryRun, Scope: p.scope.desc, Removed: p.removed,
		FactsKept: p.factsKept, PagesRebuild: p.rebuild, Snapshots: len(p.snapshots),
		Counts:      map[string]int{"embeddings": len(p.embs)},
		EntityTerms: p.scope.terms, Mentions: p.scope.mentions,
	}
	if dryRun {
		return rep, nil
	}
	if p.empty() {
		rep.Verification = &Verification{OK: true}
		return rep, nil
	}
	err = e.Store.Tx(ctx, func(tx *sql.Tx) error {
		if err := p.delete(ctx, tx, e, ns, rep.Counts); err != nil {
			return err
		}
		return p.tombstone(ctx, tx, e, ns, actor, &rep)
	})
	if err != nil {
		return Report{}, err
	}
	if err := scrubFile(ctx, db); err != nil {
		return rep, core.Wrap(core.CodeInternal, err, "forget: file scrub after commit")
	}
	for _, f := range p.snapshots {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return rep, core.Wrap(core.CodeInternal, err, "forget: deleting a snapshot")
		}
	}
	v, err := p.verify(ctx, e, ns, dbPath)
	if err != nil {
		return rep, err
	}
	rep.Verification = v
	if !v.OK {
		return rep, core.Errf(core.CodeIntegrity, "forget: verification failed")
	}
	return rep, nil
}

func (p *plan) empty() bool {
	r := p.removed
	return len(r.Events)+len(r.Facts)+len(r.PageItems)+len(r.Pages)+len(r.Entities)+len(r.Documents)+
		len(r.Sources)+len(r.Retrievals)+len(r.Outcomes)+len(r.Keys)+len(r.Conflicts)+len(r.Supersessions)+
		len(p.factsKept)+len(p.rebuild) == 0
}

// execIn runs query once per chunk of ids (bound to the single "(%s)" in
// query) and adds the affected rows to counts[key].
func execIn(ctx context.Context, tx *sql.Tx, counts map[string]int, key, query string, idList []string) error {
	for _, ch := range chunk(idList, 400) {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ch)), ",")
		args := make([]any, len(ch))
		for i, id := range ch {
			args[i] = id
		}
		// Some statements bind the list twice.
		n := strings.Count(query, "(%s)")
		all := make([]any, 0, n*len(args))
		for i := 0; i < n; i++ {
			all = append(all, args...)
		}
		res, err := tx.ExecContext(ctx, strings.ReplaceAll(query, "(%s)", "("+ph+")"), all...)
		if err != nil {
			return core.Wrap(core.CodeInternal, err, "forget: "+key)
		}
		if counts != nil && key != "" {
			k, _ := res.RowsAffected()
			counts[key] += int(k)
		}
	}
	return nil
}

func chunk(ss []string, n int) [][]string {
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

func countIn(ctx context.Context, tx *sql.Tx, query string, idList []string) (int, error) {
	total := 0
	for _, ch := range chunk(idList, 400) {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ch)), ",")
		args := make([]any, len(ch))
		for i, id := range ch {
			args[i] = id
		}
		var n int
		if err := tx.QueryRowContext(ctx, strings.ReplaceAll(query, "(%s)", "("+ph+")"), args...).Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// ftsCount counts FTS entries that belong to removed records.
func (p *plan) ftsCount(ctx context.Context, tx *sql.Tx) (int, error) {
	n := 0
	for _, x := range []struct {
		table string
		ids   []string
	}{{"events_fts", p.removed.Events}, {"facts_fts", p.removed.Facts}, {"pages_fts", p.removed.Pages}, {"learned_keys_fts", p.removed.Keys}} {
		k, err := countIn(ctx, tx, `SELECT count(*) FROM `+x.table+` WHERE record_id IN (%s)`, x.ids)
		if err != nil {
			return 0, err
		}
		n += k
	}
	return n, nil
}

// delete removes every planned row in FK-safe order.
func (p *plan) delete(ctx context.Context, tx *sql.Tx, e *env.Env, ns string, c map[string]int) error {
	r := p.removed
	records := sortedKeys(set(r.Events, r.Facts, r.PageItems, r.Pages, r.Entities, r.Keys))
	all := sortedKeys(p.gone)
	fts, err := p.ftsCount(ctx, tx)
	if err != nil {
		return err
	}
	c["fts_entries"] = fts

	steps := []struct {
		key, query string
		ids        []string
	}{
		{"keys", `DELETE FROM learned_keys WHERE id IN (%s)`, r.Keys}, // before their retrievals (foreign key)
		{"conflict_candidates", `DELETE FROM conflict_candidates WHERE id IN (%s)`, r.Conflicts},
		{"supersessions", `DELETE FROM supersessions WHERE id IN (%s)`, r.Supersessions},
		{"outcomes", `DELETE FROM outcomes WHERE id IN (%s)`, r.Outcomes},
		{"outcomes", `DELETE FROM outcomes WHERE retrieval_id IN (%s)`, r.Retrievals},
		{"retrievals", `DELETE FROM retrievals WHERE id IN (%s)`, r.Retrievals},
		{"utility", `DELETE FROM utility WHERE record_id IN (%s)`, records},
		{"embed_queue", `DELETE FROM embed_queue WHERE record_id IN (%s)`, records},
		{"", `DELETE FROM embeddings WHERE record_id IN (%s)`, append(append([]string{}, records...), p.rebuild...)},
		{"flags", `DELETE FROM flags WHERE record_id IN (%s)`, records},
		{"promotions", `DELETE FROM promotions WHERE record_id IN (%s)`, records},
		{"provenance_edges", `DELETE FROM provenance WHERE derived_id IN (%s) OR source_id IN (%s)`, all},
		{"", `UPDATE facts SET superseded_by = NULL WHERE superseded_by IN (%s)`, r.Facts},
		{"facts", `DELETE FROM facts WHERE id IN (%s)`, r.Facts},
		{"page_items", `DELETE FROM page_items WHERE id IN (%s)`, r.PageItems},
		{"pages", `DELETE FROM pages WHERE id IN (%s)`, r.Pages},
		{"event_processing", `DELETE FROM event_processing WHERE event_id IN (%s)`, r.Events},
		{"document_chunks", `DELETE FROM document_chunks WHERE event_id IN (%s) OR doc_id IN (%s)`, append(append([]string{}, r.Events...), r.Documents...)},
		{"events", `DELETE FROM events WHERE id IN (%s)`, r.Events},
		{"documents", `DELETE FROM documents WHERE id IN (%s)`, r.Documents},
		{"sources", `DELETE FROM sources WHERE id IN (%s)`, r.Sources},
		{"entity_aliases", `DELETE FROM entity_aliases WHERE entity_id IN (%s)`, r.Entities},
		{"entity_merge_proposals", `DELETE FROM entity_merge_proposals WHERE entity_a IN (%s) OR entity_b IN (%s)`, r.Entities},
		{"", `UPDATE entities SET merged_into = NULL WHERE merged_into IN (%s)`, r.Entities},
		{"entities", `DELETE FROM entities WHERE id IN (%s)`, r.Entities},
	}
	for _, s := range steps {
		if err := execIn(ctx, tx, c, s.key, s.query, s.ids); err != nil {
			return err
		}
	}
	c["embeddings"] = len(p.embs)

	// Pages that keep items leave reads and lose the forgotten text now; the
	// next sleep rebuilds them.
	now := core.FormatTime(e.Clock.Now())
	for _, pg := range p.rebuild {
		texts, err := ids(ctx, tx, `SELECT text FROM page_items WHERE page_id = ? AND status = 'active' ORDER BY ordinal, id`, pg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pages SET body = ?, status = 'needs_rebuild', version = version + 1, updated_at = ? WHERE id = ?`,
			strings.Join(texts, "\n"), now, pg); err != nil {
			return err
		}
	}
	c["pages_rebuild"] = len(p.rebuild)
	c["facts_kept"] = len(p.factsKept)

	// Records outside the graph that cite removed ids. Gate questions that
	// expect a removed record are deleted; sleep run inputs, outputs and
	// reports lose the ids.
	for _, ch := range chunk(all, 200) {
		res, err := tx.ExecContext(ctx, `DELETE FROM gate_questions WHERE `+jsonCites("expected", len(ch)), quoted(ch)...)
		if err != nil {
			return err
		}
		k, _ := res.RowsAffected()
		c["gate_questions"] += int(k)
	}
	byLen := append([]string{}, all...)
	sort.SliceStable(byLen, func(i, j int) bool { return len(byLen[i]) > len(byLen[j]) })
	for _, id := range byLen {
		res, err := tx.ExecContext(ctx, `UPDATE sleep_runs SET inputs = replace(inputs, ?1, '[forgotten]'),
			outputs = replace(outputs, ?1, '[forgotten]'), report = replace(report, ?1, '[forgotten]')
			WHERE instr(inputs, ?1) > 0 OR instr(outputs, ?1) > 0 OR instr(report, ?1) > 0`, id)
		if err != nil {
			return err
		}
		k, _ := res.RowsAffected()
		c["sleep_runs_scrubbed"] += int(k)
	}
	if err := execIn(ctx, tx, nil, "", `UPDATE sleep_runs SET snapshot = NULL WHERE id IN (%s)`, p.sleepRuns); err != nil {
		return err
	}
	c["snapshots"] = len(p.snapshots)

	// FTS entries are trigger-maintained: confirm they went with their rows.
	left, err := p.ftsCount(ctx, tx)
	if err != nil {
		return err
	}
	if left != 0 {
		return core.Errf(core.CodeIntegrity, "forget: %d FTS entries survived their records", left)
	}
	return nil
}

// jsonCites is a WHERE fragment true when col contains any of n quoted ids.
func jsonCites(col string, n int) string {
	if n == 0 {
		return "0"
	}
	return "(" + strings.TrimSuffix(strings.Repeat("instr("+col+", ?) > 0 OR ", n), " OR ") + ")"
}

func quoted(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = `"` + id + `"`
	}
	return out
}

// tombstone writes the proof of deletion and the audit entry; neither holds
// content. Content hashes are kept only with forget.tombstone_keeps_hash.
func (p *plan) tombstone(ctx context.Context, tx *sql.Tx, e *env.Env, ns, actor string, rep *Report) error {
	r := p.removed
	counts := map[string]int{
		"events": len(r.Events), "facts": len(r.Facts), "page_items": len(r.PageItems), "pages": len(r.Pages),
		"entities": len(r.Entities), "documents": len(r.Documents), "sources": len(r.Sources),
		"retrievals": len(r.Retrievals), "outcomes": len(r.Outcomes), "keys": len(r.Keys),
	}
	for k, v := range rep.Counts {
		if _, ok := counts[k]; !ok {
			counts[k] = v
		}
	}
	rep.Counts = counts
	scope, err := jsonString(p.scope.desc)
	if err != nil {
		return err
	}
	cnt, err := jsonString(counts)
	if err != nil {
		return err
	}
	var hashes any
	if e.Cfg.Forget.TombstoneKeepsHash {
		s, err := jsonString(prov.SortedUnique(p.hashes))
		if err != nil {
			return err
		}
		hashes = s
	}
	rep.TombstoneID = e.IDs.New(core.PrefixTombstone)
	if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones(id, namespace, ts, scope, requested_by, removed_counts, content_hashes)
		VALUES (?,?,?,?,?,?,?)`, rep.TombstoneID, ns, core.FormatTime(e.Clock.Now()), scope, actor, cnt, hashes); err != nil {
		return err
	}
	rep.AuditID, err = e.Audit.Append(ctx, tx, audit.Entry{
		Namespace: ns, Actor: actor, Action: audit.ActForget, Target: rep.TombstoneID,
		Details: map[string]any{"scope_kind": p.scope.desc["kind"], "counts": counts},
	})
	return err
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
