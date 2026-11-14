package prov

import (
	"context"
	"database/sql"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// PrefixPromotion is the id prefix of promotions rows.
const PrefixPromotion = "prm"

// promotable maps the record types whose zone promote may raise to their table.
// Events are raw and immutable; entities carry no zone.
var promotable = map[core.RecordType]string{
	core.RecFact:     "facts",
	core.RecPage:     "pages",
	core.RecPageItem: "page_items",
}

// PromoteResult is the answer of the promote tool.
type PromoteResult struct {
	ID          string          `json:"id"`
	RecordType  core.RecordType `json:"record_type"`
	From        core.Zone       `json:"from"`
	To          core.Zone       `json:"to"`
	Changed     bool            `json:"changed"`
	PromotionID string          `json:"promotion_id,omitempty"`
	AuditID     int64           `json:"audit_id,omitempty"`
}

// Promote raises the zone of a derived record (fact, page or page item) to
// zone. BASE.md "Trust zones": only promote, called by a human actor, raises a
// record's zone, and the action is audited. A non-human caller gets
// core.CodePermissionDenied; a lower zone gets core.CodeConflict (promote
// never lowers; docs/errors.md); the same zone is a no-op. The raise is stored in
// promotions so the integrity checker accepts it.
func Promote(ctx context.Context, e *env.Env, ns, id string, zone core.Zone, actor string, human bool) (PromoteResult, error) {
	if !human {
		return PromoteResult{}, core.Errf(core.CodePermissionDenied, "promote requires a human actor")
	}
	if !zone.Valid() {
		return PromoteResult{}, core.Errf(core.CodeInvalidArgument, "unknown zone %q", zone)
	}
	if actor == "" {
		return PromoteResult{}, core.Errf(core.CodeInvalidArgument, "promote requires an actor")
	}
	rt := core.RecordTypeOf(id)
	table, ok := promotable[rt]
	if !ok {
		return PromoteResult{}, core.Errf(core.CodeInvalidArgument, "only facts, pages and page items can be promoted, not %q", id)
	}
	res := PromoteResult{ID: id, RecordType: rt, To: zone}
	err := e.Store.Tx(ctx, func(tx *sql.Tx) error {
		var cur string
		err := tx.QueryRowContext(ctx, `SELECT trust_zone FROM `+table+` WHERE id = ? AND namespace = ?`, id, ns).Scan(&cur)
		if err == sql.ErrNoRows {
			return core.Errf(core.CodeNotFound, "record %s not found", id)
		}
		if err != nil {
			return err
		}
		res.From = core.Zone(cur)
		switch {
		case zone.Rank() < res.From.Rank():
			return core.Errf(core.CodeConflict, "promote never lowers a zone (%s -> %s)", res.From, zone)
		case zone == res.From:
			return nil
		}
		res.Changed = true
		res.PromotionID = e.IDs.New(PrefixPromotion)
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET trust_zone = ? WHERE id = ?`, string(zone), id); err != nil {
			return err
		}
		aid, err := e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActPromote, Target: id,
			Details: map[string]string{"record_type": string(rt), "from": cur, "to": string(zone), "promotion_id": res.PromotionID},
		})
		if err != nil {
			return err
		}
		res.AuditID = aid
		_, err = tx.ExecContext(ctx, `INSERT INTO promotions(id, namespace, record_type, record_id, from_zone, to_zone, actor, audit_id, ts)
			VALUES (?,?,?,?,?,?,?,?,?)`, res.PromotionID, ns, string(rt), id, cur, string(zone), actor, aid, core.FormatTime(e.Clock.Now()))
		return err
	})
	if err != nil {
		return PromoteResult{}, err
	}
	return res, nil
}
