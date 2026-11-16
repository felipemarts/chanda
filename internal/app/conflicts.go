package app

import (
	"context"
	"database/sql"

	"github.com/felipemarts/chanda/internal/conflicts"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/dedup"
	"github.com/felipemarts/chanda/internal/write"
)

// conflictParams are the detection settings of decision 0148 for the
// running embedder: sleep.conflicts.min_cosine (or minCosine when > 0) and
// max_per_record, and the model's near-duplicate threshold and lexical path.
func (a *App) conflictParams(minCosine float64) conflicts.Params {
	k := a.e.Cfg.Sleep.Conflicts
	model := a.emb.ModelID()
	l := a.e.Cfg.Write.Lexical(model)
	p := conflicts.Params{
		MinCosine: k.Cosine(), MaxPerRecord: k.PerRecord(), Threshold: a.e.Cfg.Write.NearDup(model),
		Lexical: dedup.Lexical{MaxAddedWords: l.MaxAddedWords, MinCosine: l.MinCosine},
	}
	if minCosine > 0 {
		p.MinCosine = minCosine
	}
	return p
}

// detectConflicts is conflict detection on the write path (decision 0148),
// run right after the near-duplicate guard over the guard's neighbours
// (ids, texts) and their cosines to the new event. Candidate pairs go to
// the conflict queue as pending. Nothing else changes. Fact assertions are
// never part of a pair (the read path never shows them).
func (a *App) detectConflicts(ctx context.Context, tx *sql.Tx, ev write.Event, ids, texts []string, cosines []float64) error {
	if ev.Kind == core.EventFact {
		return nil
	}
	nb := make([]conflicts.Neighbour, 0, len(ids))
	for i, id := range ids {
		var kind, validFrom, recordedAt string
		if err := tx.QueryRowContext(ctx, `SELECT kind, COALESCE(valid_from,''), recorded_at FROM events WHERE id = ?`, id).
			Scan(&kind, &validFrom, &recordedAt); err != nil {
			return err
		}
		if kind == string(core.EventFact) {
			continue
		}
		nb = append(nb, conflicts.Neighbour{
			Record: conflicts.Record{ID: id, Content: texts[i], ValidFrom: validFrom, RecordedAt: recordedAt}, Cosine: cosines[i],
		})
	}
	pairs, _ := conflicts.Detect(conflicts.Record{ID: ev.ID, Content: ev.Content, ValidFrom: ev.ValidFrom, RecordedAt: ev.RecordedAt},
		nb, a.conflictParams(0))
	if len(pairs) == 0 {
		return nil
	}
	_, err := conflicts.Enqueue(ctx, tx, a.e.IDs, ev.Namespace, core.FormatTime(a.e.Clock.Now()), pairs)
	return err
}
