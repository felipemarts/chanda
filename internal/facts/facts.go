// Package facts implements entities, the predicate catalog and temporal fact
// reconciliation (BASE.md write path steps 8 and 9, "Decision table for
// facts", "The two time axes", "Time queries"), plus the deterministic fact
// extractors that run on the synchronous write path.
//
// Every mutating method takes the caller's *sql.Tx so that the fact, its
// provenance edges and its audit entry commit atomically with the event that
// produced it. Read methods use the store's connection directly.
//
// Order on the write path: facts are reconciled (Assert) before the
// near-duplicate guard (internal/dedup) runs, so a similar record with a
// different value is treated as an update, never discarded as a copy
// (principle 7).
package facts

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Audit action names of this package (registered in internal/audit).
const (
	ActPredicateCreate = audit.ActPredicate
	ActFactAdjust      = audit.ActFactAdjust // a history insert trimmed an overlapping closed fact
)

// Service owns the entities, entity_aliases, predicates and facts tables.
type Service struct {
	e    *env.Env
	exts []extractor
	// extErr is the first invalid extractor configuration, reported by AfterEvent.
	extErr error
}

// New returns a Service bound to e. Extractors are compiled from
// e.Cfg.Write.Extractors; an invalid configuration is reported by
// AfterEvent (and by ValidateExtractors, for startup checks).
func New(e *env.Env) *Service {
	s := &Service{e: e}
	s.exts, s.extErr = compileExtractors(e.Cfg.Write.Extractors)
	return s
}

func (s *Service) now() string { return core.FormatTime(s.e.Clock.Now()) }

// addEdge inserts one provenance edge (idempotent). It reports whether the
// edge is new.
func (s *Service) addEdge(ctx context.Context, q store.Queryer, ns string, dt core.RecordType, did string, st core.RecordType, sid, rel string) (bool, error) {
	res, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
		VALUES (?,?,?,?,?,?,?)`, ns, string(dt), did, string(st), sid, rel, s.now())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
