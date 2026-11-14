package facts

import (
	"context"
	"database/sql"
	"errors"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// maxMergeHops bounds the merged_into chain walk.
const maxMergeHops = 16

// Resolve maps a name to an entity (write step 8). The name is normalized
// (norm.Alias) and looked up in entity_aliases; with an exact match the
// entity is returned (following merged_into), otherwise a new entity is
// created with the name as canonical_name and the normalized alias. No
// similarity merging happens here: sleep proposes merges.
func (s *Service) Resolve(ctx context.Context, tx *sql.Tx, ns, name, typ string) (entityID string, created bool, err error) {
	return s.resolve(ctx, tx, ns, name, typ, "system")
}

func (s *Service) resolve(ctx context.Context, tx *sql.Tx, ns, name, typ, actor string) (string, bool, error) {
	alias := norm.Alias(name)
	if ns == "" || alias == "" {
		return "", false, core.Errf(core.CodeInvalidArgument, "entity name and namespace are required")
	}
	id, ok, err := lookupAlias(ctx, tx, ns, alias)
	if err != nil || ok {
		return id, false, err
	}
	id = s.e.IDs.For(core.RecEntity)
	canonical := norm.CollapseSpace(norm.NFC(name))
	if _, err := tx.ExecContext(ctx, `INSERT INTO entities(id, namespace, canonical_name, type, created_at) VALUES (?,?,?,?,?)`,
		id, ns, canonical, norm.CollapseSpace(typ), s.now()); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entity_aliases(namespace, alias_norm, entity_id) VALUES (?,?,?)`, ns, alias, id); err != nil {
		return "", false, err
	}
	if _, err := s.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: actor, Action: audit.ActEntityCreate, Target: id,
		Details: map[string]any{"aliases": 1}}); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func lookupAlias(ctx context.Context, q store.Queryer, ns, alias string) (string, bool, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT entity_id FROM entity_aliases WHERE namespace = ? AND alias_norm = ?`, ns, alias).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	id, err = followMerges(ctx, q, ns, id)
	return id, err == nil, err
}

// followMerges returns the surviving entity of a merged_into chain.
func followMerges(ctx context.Context, q store.Queryer, ns, id string) (string, error) {
	for i := 0; i < maxMergeHops; i++ {
		var into sql.NullString
		err := q.QueryRowContext(ctx, `SELECT merged_into FROM entities WHERE namespace = ? AND id = ?`, ns, id).Scan(&into)
		if err != nil {
			return "", err
		}
		if !into.Valid || into.String == "" {
			return id, nil
		}
		id = into.String
	}
	return "", core.Errf(core.CodeIntegrity, "entity merge chain too long at %s", id)
}

// LookupEntity resolves a subject given as an entity id or as a name,
// without creating anything. ok is false when nothing matches.
func (s *Service) LookupEntity(ctx context.Context, q store.Queryer, ns, nameOrID string) (id string, ok bool, err error) {
	if core.RecordTypeOf(nameOrID) == core.RecEntity {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM entities WHERE namespace = ? AND id = ?`, ns, nameOrID).Scan(&n); err != nil {
			return "", false, err
		}
		if n > 0 {
			id, err := followMerges(ctx, q, ns, nameOrID)
			return id, err == nil, err
		}
	}
	alias := norm.Alias(nameOrID)
	if alias == "" {
		return "", false, nil
	}
	return lookupAlias(ctx, q, ns, alias)
}

// subjectFor resolves a subject for a write: an existing entity id is used
// as is, anything else is a name resolved (and created if needed).
func (s *Service) subjectFor(ctx context.Context, tx *sql.Tx, ns, subject, typ, actor string) (string, error) {
	if core.RecordTypeOf(subject) == core.RecEntity {
		if id, ok, err := s.LookupEntity(ctx, tx, ns, subject); err != nil || ok {
			return id, err
		}
	}
	id, _, err := s.resolve(ctx, tx, ns, subject, typ, actor)
	return id, err
}

func canonicalName(ctx context.Context, q store.Queryer, ns, id string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, `SELECT canonical_name FROM entities WHERE namespace = ? AND id = ?`, ns, id).Scan(&name)
	return name, err
}
