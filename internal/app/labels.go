package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

// Source labels (decision 0153). A write may name its source with
// source.label. The label is stored after the write, in its own audited
// transaction; it never changes the zone, and a label that cannot be stored
// leaves the write as it was and only adds a notice.

// Notices of source labels.
const (
	noticeLabelOff     = "source_label_ignored: sources.agent_labels is off, so only a human actor names sources"
	noticeLabelEmpty   = "source_label_ignored: nothing is left of the label once cleaned"
	noticeLabelRefused = "source_label_refused: the label holds a secret or reads as an instruction"
	noticeLabelNoWrite = "source_label_ignored: nothing was written, so there is no source to name"
	noticeLabelFailed  = "source_label_failed: the label could not be stored"
)

// labelSource stores label for the source that query (with args) selects,
// a single source id in c.ns. An empty label does nothing.
func (a *App) labelSource(ctx context.Context, c *call, label, query string, args ...any) {
	if label == "" {
		return
	}
	if !a.e.Cfg.Sources.AgentLabelsOn() && !c.actor.Human {
		c.notice(noticeLabelOff)
		return
	}
	clean := config.CleanLabel(label)
	if clean == "" {
		c.notice(noticeLabelEmpty)
		return
	}
	if len(a.writer.Admitter().Screen(clean, core.ZoneExternal).Flags) > 0 {
		c.notice(noticeLabelRefused)
		return
	}
	err := a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		var id, old string
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT label FROM source_labels WHERE source_id = ?`, id).Scan(&old)
		if err == nil && old == clean {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO source_labels(source_id, namespace, label, actor, updated_at) VALUES (?,?,?,?,?)
			ON CONFLICT(source_id) DO UPDATE SET label = excluded.label, actor = excluded.actor, updated_at = excluded.updated_at`,
			id, c.ns, clean, c.actor.Name, core.FormatTime(a.e.Clock.Now())); err != nil {
			return err
		}
		_, err = a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: c.ns, Actor: c.actor.Name, Action: audit.ActSourceLabel, Target: id})
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.notice(noticeLabelNoWrite)
	case err != nil:
		c.notice(noticeLabelFailed)
		if a.opt.Logf != nil {
			a.opt.Logf("source label: %v", err)
		}
	}
}

// sourceOfEvent selects the source of an event (remember, assert_fact).
const sourceOfEvent = `SELECT source_id FROM events WHERE namespace = ? AND id = ?`

// sourceByURI selects a source by kind and uri (ingest).
const sourceByURI = `SELECT id FROM sources WHERE namespace = ? AND kind = ? AND uri = ?`
