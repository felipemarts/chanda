// Package keys owns the learned query keys (decision 0145): the
// learned_keys table, its FTS index (trigger-maintained), the key vectors
// (embeddings rows of record type "key", queued like every other record)
// and the per-namespace runtime switch.
//
// A key is created when the session agent reports a positive outcome
// (value > 0) for a retrieval: the retrieval's query text, normalised like
// an event, becomes a key of every used record the retrieval delivered.
// Creation runs inside the outcome transaction (internal/rank calls Create);
// sleep never creates keys. Rebuild replays the outcomes already recorded
// by the same rule. The read path (internal/read) searches the keys.
//
// Everything is deterministic: records in id order, outcomes in id order,
// ids from the env generator, no LLM.
package keys

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
	"github.com/felipemarts/chanda-mcp/internal/write/normalize"
)

// Audit actions of this package (registered in internal/audit). Details
// hold ids and counts only, never key text.
const (
	ActCreate  = audit.ActKeysCreate  // keys made by one positive outcome
	ActToggle  = audit.ActKeysToggle  // runtime switch turned on or off
	ActReset   = audit.ActKeysReset   // every key of the namespace deleted
	ActRebuild = audit.ActKeysRebuild // missing keys of recorded outcomes created
)

// recordTables maps the record types a key may point to (the types the
// read path delivers) to their table.
var recordTables = map[core.RecordType]string{
	core.RecEvent: "events",
	core.RecFact:  "facts",
	core.RecPage:  "pages",
}

// Created counts what one creation did.
type Created struct {
	Created  int `json:"created"`  // new keys
	Existing int `json:"existing"` // same (record, text) already had a key
	Skipped  int `json:"skipped"`  // id of no deliverable record of the namespace
}

func (c *Created) add(o Created) {
	c.Created += o.Created
	c.Existing += o.Existing
	c.Skipped += o.Skipped
}

// NormalizeText is the key text of a query: write step 1 (normalize.Text).
func NormalizeText(q string) string { return normalize.Text(q) }

// Create makes the keys of one positive outcome inside q (the outcome's
// transaction): one key per record id of ids (the used ids the retrieval
// delivered), in id order, with text = NormalizeText(queryText). A key of
// the same text for the same record is not created twice. Each new key
// gets its provenance edges (key -> retrieval derived_from, key -> record
// points_to) and is queued for embedding. now is the creation time.
//
// Create writes no audit entry; the caller does (CreateForOutcome).
func Create(ctx context.Context, q store.Queryer, e *env.Env, ns, retrievalID, queryText string, ids []string, now string) (Created, error) {
	var res Created
	text := NormalizeText(queryText)
	if ns == "" || retrievalID == "" || strings.TrimSpace(text) == "" {
		return res, nil
	}
	for _, id := range sortedUnique(ids) {
		rt := core.RecordTypeOf(id)
		table := recordTables[rt]
		if table == "" {
			res.Skipped++
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ? AND namespace = ?`, id, ns).Scan(&n); err != nil {
			return res, err
		}
		if n == 0 {
			res.Skipped++
			continue
		}
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM learned_keys
			WHERE namespace = ? AND record_type = ? AND record_id = ? AND text = ?`, ns, string(rt), id, text).Scan(&n); err != nil {
			return res, err
		}
		if n > 0 {
			res.Existing++
			continue
		}
		key := e.IDs.New(core.PrefixKey)
		if _, err := q.ExecContext(ctx, `INSERT INTO learned_keys(id, namespace, record_type, record_id, retrieval_id, text, created_at)
			VALUES (?,?,?,?,?,?,?)`, key, ns, string(rt), id, retrievalID, text, now); err != nil {
			return res, err
		}
		for _, edge := range []struct {
			st core.RecordType
			sid, rel string
		}{
			{core.RecRetrieval, retrievalID, core.RelDerivedFrom},
			{rt, id, core.RelPointsTo},
		} {
			if _, err := q.ExecContext(ctx, `INSERT INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
				VALUES (?,?,?,?,?,?,?)`, ns, string(core.RecKey), key, string(edge.st), edge.sid, edge.rel, now); err != nil {
				return res, err
			}
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at) VALUES (?,?,?,?)
			ON CONFLICT(record_type, record_id) DO NOTHING`, ns, string(core.RecKey), key, now); err != nil {
			return res, err
		}
		res.Created++
	}
	return res, nil
}

// CreateForOutcome is Create for the outcome tool, with its audit entry
// (action ActCreate, target the outcome id, counts only). It reads the
// retrieval's query text inside q.
func CreateForOutcome(ctx context.Context, q store.Queryer, e *env.Env, ns, outcomeID, retrievalID, actor string, ids []string, now string) (Created, error) {
	var query string
	if err := q.QueryRowContext(ctx, `SELECT query_text FROM retrievals WHERE id = ? AND namespace = ?`, retrievalID, ns).Scan(&query); err != nil {
		return Created{}, err
	}
	c, err := Create(ctx, q, e, ns, retrievalID, query, ids, now)
	if err != nil {
		return c, err
	}
	_, err = e.Audit.Append(ctx, q, audit.Entry{
		Namespace: ns, Actor: actor, Action: ActCreate, Target: outcomeID,
		Details: map[string]any{"retrieval_id": retrievalID, "created": c.Created, "existing": c.Existing, "skipped": c.Skipped},
	})
	return c, err
}

// Enabled reports the runtime switch of namespace ns (on when never set).
func Enabled(ctx context.Context, q store.Queryer, ns string) (bool, error) {
	var on int
	err := q.QueryRowContext(ctx, `SELECT enabled FROM learned_keys_state WHERE namespace = ?`, ns).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return on != 0, err
}

func requireNS(ns, actor string) error {
	if ns == "" {
		return core.Errf(core.CodeInvalidArgument, "namespace is required")
	}
	if actor == "" {
		return core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	return nil
}

// SetEnabled stores the runtime switch of ns. Audited (ActToggle, with the
// value before and after). Returns the audit id.
func SetEnabled(ctx context.Context, e *env.Env, ns string, enabled bool, actor string) (int64, error) {
	if err := requireNS(ns, actor); err != nil {
		return 0, err
	}
	var auditID int64
	err := e.Store.Tx(ctx, func(tx *sql.Tx) error {
		was, err := Enabled(ctx, tx, ns)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO learned_keys_state(namespace, enabled, updated_at) VALUES (?,?,?)
			ON CONFLICT(namespace) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
			ns, boolInt(enabled), core.FormatTime(e.Clock.Now())); err != nil {
			return err
		}
		auditID, err = e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: ActToggle, Target: ns,
			Details: map[string]any{"enabled_from": was, "enabled_to": enabled},
		})
		return err
	})
	return auditID, err
}

// ResetResult reports what Reset deleted.
type ResetResult struct {
	AuditID    int64 `json:"audit_id"`
	Keys       int64 `json:"keys"`
	Embeddings int64 `json:"embeddings"`
	Provenance int64 `json:"provenance_edges"`
}

// Reset deletes every key of ns with its vectors, queue rows, FTS rows (by
// trigger) and provenance edges. The switch keeps its value. Audited
// (ActReset, counts only).
func Reset(ctx context.Context, e *env.Env, ns, actor string) (ResetResult, error) {
	var res ResetResult
	if err := requireNS(ns, actor); err != nil {
		return res, err
	}
	err := e.Store.Tx(ctx, func(tx *sql.Tx) error {
		r, err := DeleteWhere(ctx, tx, `namespace = ?`, ns)
		if err != nil {
			return err
		}
		res.Keys, res.Embeddings, res.Provenance = r.Keys, r.Embeddings, r.Provenance
		res.AuditID, err = e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: ActReset, Target: ns,
			Details: map[string]any{"keys": res.Keys, "embeddings": res.Embeddings, "provenance_edges": res.Provenance},
		})
		return err
	})
	if err != nil {
		return ResetResult{}, err
	}
	return res, nil
}

// Deleted counts the rows DeleteWhere removed.
type Deleted struct {
	Keys, Embeddings, Queue, Provenance int64
}

// DeleteWhere deletes the keys matching cond (SQL over learned_keys, with
// args) and everything derived from them: vectors, queue rows, provenance
// edges; FTS rows go by trigger. It runs inside q (a transaction).
func DeleteWhere(ctx context.Context, q store.Queryer, cond string, args ...any) (Deleted, error) {
	var d Deleted
	sub := `SELECT id FROM learned_keys WHERE ` + cond
	steps := []struct {
		n     *int64
		query string
	}{
		{&d.Embeddings, `DELETE FROM embeddings WHERE record_type = 'key' AND record_id IN (` + sub + `)`},
		{&d.Queue, `DELETE FROM embed_queue WHERE record_type = 'key' AND record_id IN (` + sub + `)`},
		{&d.Provenance, `DELETE FROM provenance WHERE derived_type = 'key' AND derived_id IN (` + sub + `)`},
		{&d.Keys, `DELETE FROM learned_keys WHERE ` + cond},
	}
	for _, s := range steps {
		r, err := q.ExecContext(ctx, s.query, args...)
		if err != nil {
			return d, err
		}
		*s.n, _ = r.RowsAffected()
	}
	return d, nil
}

// RebuildResult reports what Rebuild did.
type RebuildResult struct {
	AuditID  int64 `json:"audit_id"`
	Outcomes int   `json:"outcomes"` // positive outcomes replayed
	Created
}

// Rebuild creates the missing keys of every positive outcome already
// recorded in ns, by the creation rule (value > 0; used ids the retrieval
// delivered), processing outcomes in id order. Existing keys are kept, so
// running it twice creates nothing the second time. Audited (ActRebuild,
// counts only). It runs whatever write.learned_keys says: it is the
// explicit admin path that gives a copied database its keys.
func Rebuild(ctx context.Context, e *env.Env, ns, actor string) (RebuildResult, error) {
	var res RebuildResult
	if err := requireNS(ns, actor); err != nil {
		return res, err
	}
	now := core.FormatTime(e.Clock.Now())
	err := e.Store.Tx(ctx, func(tx *sql.Tx) error {
		type outcome struct{ retrieval, query, used, delivered string }
		rows, err := tx.QueryContext(ctx, `SELECT o.retrieval_id, r.query_text, o.used_ids, r.delivered
			FROM outcomes o JOIN retrievals r ON r.id = o.retrieval_id AND r.namespace = o.namespace
			WHERE o.namespace = ? AND o.value > 0 ORDER BY o.id`, ns)
		if err != nil {
			return err
		}
		var outs []outcome
		for rows.Next() {
			var o outcome
			if err := rows.Scan(&o.retrieval, &o.query, &o.used, &o.delivered); err != nil {
				rows.Close()
				return err
			}
			outs = append(outs, o)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, o := range outs {
			c, err := Create(ctx, tx, e, ns, o.retrieval, o.query, intersect(parseIDs(o.used), parseIDs(o.delivered)), now)
			if err != nil {
				return err
			}
			res.Outcomes++
			res.add(c)
		}
		res.AuditID, err = e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: ActRebuild, Target: ns,
			Details: map[string]any{"outcomes": res.Outcomes, "created": res.Created.Created, "existing": res.Existing, "skipped": res.Skipped},
		})
		return err
	})
	if err != nil {
		return RebuildResult{}, err
	}
	return res, nil
}

// Stats describes the keys of a namespace.
type Stats struct {
	Enabled  bool  `json:"enabled"`  // runtime switch
	Keys     int64 `json:"keys"`     // learned_keys rows
	Records  int64 `json:"records"`  // distinct records with at least one key
	Embedded int64 `json:"embedded"` // keys with a vector (any model)
	Pending  int64 `json:"pending"`  // keys waiting in the embedding queue
}

// StatsOf reads the key counts of ns.
func StatsOf(ctx context.Context, q store.Queryer, ns string) (Stats, error) {
	var s Stats
	var err error
	if s.Enabled, err = Enabled(ctx, q, ns); err != nil {
		return s, err
	}
	err = q.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM learned_keys WHERE namespace = ?1),
		(SELECT COUNT(DISTINCT record_id) FROM learned_keys WHERE namespace = ?1),
		(SELECT COUNT(DISTINCT record_id) FROM embeddings WHERE namespace = ?1 AND record_type = 'key'),
		(SELECT COUNT(*) FROM embed_queue WHERE namespace = ?1 AND record_type = 'key')`, ns).
		Scan(&s.Keys, &s.Records, &s.Embedded, &s.Pending)
	return s, err
}

func parseIDs(s string) []string {
	var raw []any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &raw); err != nil {
		return nil
	}
	var out []string
	for _, v := range raw {
		if id, ok := v.(string); ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// intersect returns the ids of a that are also in b.
func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, id := range b {
		in[id] = true
	}
	var out []string
	for _, id := range a {
		if in[id] {
			out = append(out, id)
		}
	}
	return out
}

func sortedUnique(ids []string) []string {
	m := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			m[id] = true
		}
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
