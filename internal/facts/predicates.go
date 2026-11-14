package facts

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
)

// singlePredicates returns the configured single predicates, normalized,
// sorted and deduplicated.
func (s *Service) singlePredicates() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range s.e.Cfg.Facts.SinglePredicates {
		if n := norm.Predicate(p); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Cardinality returns the cardinality of predicate in ns, creating the
// catalog entry if needed. The first touch of a namespace seeds every
// configured single predicate (cfg.Facts.SinglePredicates). An unknown
// predicate enters as single if it is configured so, otherwise as multi
// (BASE.md "Integrity rules"). An existing entry is never changed here.
// name must already be normalized (norm.Predicate).
func (s *Service) Cardinality(ctx context.Context, tx *sql.Tx, ns, name, actor string) (core.Cardinality, error) {
	if name == "" || name != norm.Predicate(name) {
		return "", core.Errf(core.CodeInvalidArgument, "predicate must be normalized snake_case")
	}
	var card string
	err := tx.QueryRowContext(ctx, `SELECT cardinality FROM predicates WHERE namespace = ? AND name = ?`, ns, name).Scan(&card)
	if err == nil {
		return core.Cardinality(card), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM predicates WHERE namespace = ?`, ns).Scan(&n); err != nil {
		return "", err
	}
	singles := s.singlePredicates()
	toAdd := map[string]core.Cardinality{}
	if n == 0 {
		for _, p := range singles {
			toAdd[p] = core.Single
		}
	}
	if _, ok := toAdd[name]; !ok {
		toAdd[name] = core.Multi
		if i := sort.SearchStrings(singles, name); i < len(singles) && singles[i] == name {
			toAdd[name] = core.Single
		}
	}
	names := make([]string, 0, len(toAdd))
	for p := range toAdd {
		names = append(names, p)
	}
	sort.Strings(names)
	now := s.now()
	for _, p := range names {
		if _, err := tx.ExecContext(ctx, `INSERT INTO predicates(namespace, name, cardinality, created_at) VALUES (?,?,?,?)`,
			ns, p, string(toAdd[p]), now); err != nil {
			return "", err
		}
	}
	if _, err := s.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: actor, Action: ActPredicateCreate, Target: name,
		Details: map[string]any{"created": len(names), "seeded": n == 0}}); err != nil {
		return "", err
	}
	return toAdd[name], nil
}
