package embed

import (
	"context"
	"database/sql"
	"sort"
	"sync"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// Indexer drains embed_queue (write step 7): it loads each queued record's
// text, embeds it with the current Embedder and stores the vector through the
// ExactIndex. The queue is processed in a fixed order (enqueued_at,
// record_type, record_id), in batches of cfg.Embed.BatchSize.
//
// Each batch reads and embeds outside any transaction (a real model may take
// a while and the store has one connection), then commits in one transaction
// that re-checks every item: a queue row that vanished or was re-enqueued, a
// record that was forgotten, retired or edited in between is not written from
// the stale read. Embeddings get no provenance edges: they are keyed by their
// record, and the integrity checker follows the record's own origins.
type Indexer struct {
	env *env.Env
	emb core.Embedder
	idx *ExactIndex

	mu      sync.Mutex // one drain at a time
	wake    chan struct{}
	errMu   sync.Mutex
	lastErr error
}

// NewIndexer wires an Indexer. emb's ModelID is the "current" model.
func NewIndexer(e *env.Env, emb core.Embedder, idx *ExactIndex) *Indexer {
	return &Indexer{env: e, emb: emb, idx: idx, wake: make(chan struct{}, 1)}
}

// ModelID is the model the indexer writes vectors for.
func (ix *Indexer) ModelID() string { return ix.emb.ModelID() }

// Notify wakes the background loop; it never blocks. The write path calls it
// after committing rows to embed_queue.
func (ix *Indexer) Notify() {
	select {
	case ix.wake <- struct{}{}:
	default:
	}
}

// RunOnce drains every row currently in embed_queue and returns how many
// queue rows it consumed (indexed or skipped).
func (ix *Indexer) RunOnce(ctx context.Context) (int, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	total := 0
	for {
		picked, done, err := ix.batch(ctx)
		total += done
		// done == 0 with rows picked only happens when every row changed under
		// us; stop rather than spin, the next run (or Flush) picks them up.
		if err != nil || picked == 0 || done == 0 {
			return total, err
		}
	}
}

// Flush blocks until embed_queue is empty (the flush tool: "waits for the
// index to catch up with writes"). It drains in the caller's goroutine, so it
// works with or without the background loop.
func (ix *Indexer) Flush(ctx context.Context) error {
	for {
		if _, err := ix.RunOnce(ctx); err != nil {
			return err
		}
		n, err := ix.Pending(ctx, "")
		if err != nil || n == 0 {
			return err
		}
	}
}

// Pending counts queued records in namespace ("" = all namespaces).
func (ix *Indexer) Pending(ctx context.Context, namespace string) (int, error) {
	var n int
	var err error
	if namespace == "" {
		err = ix.env.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM embed_queue`).Scan(&n)
	} else {
		err = ix.env.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM embed_queue WHERE namespace = ?`, namespace).Scan(&n)
	}
	return n, err
}

// Start runs the background loop until ctx is cancelled: it drains on
// Notify and every cfg.Embed.PollIntervalMS (so rows enqueued by writers that
// did not call Notify are picked up too). The returned channel closes when
// the loop has exited. The ticker only schedules work; no output depends on
// wall-clock time.
func (ix *Indexer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	poll := time.Duration(ix.env.Cfg.Embed.PollIntervalMS) * time.Millisecond
	if poll <= 0 {
		poll = time.Second
	}
	go func() {
		defer close(done)
		t := time.NewTicker(poll)
		defer t.Stop()
		for {
			if _, err := ix.RunOnce(ctx); err != nil && ctx.Err() == nil {
				ix.setErr(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ix.wake:
			case <-t.C:
			}
		}
	}()
	return done
}

// LastError is the most recent error of the background loop, if any.
func (ix *Indexer) LastError() error {
	ix.errMu.Lock()
	defer ix.errMu.Unlock()
	return ix.lastErr
}

func (ix *Indexer) setErr(err error) {
	ix.errMu.Lock()
	ix.lastErr = err
	ix.errMu.Unlock()
}

type queued struct {
	ns, rt, id, at string
	text           string
	live           bool
	vec            []float32
}

// batch processes up to BatchSize queue rows. picked is how many rows were
// read, done how many were removed from the queue.
func (ix *Indexer) batch(ctx context.Context) (picked, done int, err error) {
	size := ix.env.Cfg.Embed.BatchSize
	if size <= 0 {
		size = 32
	}
	db := ix.env.Store.DB()
	rows, err := db.QueryContext(ctx,
		`SELECT namespace, record_type, record_id, enqueued_at FROM embed_queue
		 ORDER BY enqueued_at, record_type, record_id LIMIT ?`, size)
	if err != nil {
		return 0, 0, err
	}
	var items []*queued
	for rows.Next() {
		q := &queued{}
		if err := rows.Scan(&q.ns, &q.rt, &q.id, &q.at); err != nil {
			rows.Close()
			return 0, 0, err
		}
		items = append(items, q)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(items) == 0 {
		return 0, 0, err
	}

	var texts []string
	var toEmbed []*queued
	for _, q := range items {
		if q.text, q.live, err = recordText(ctx, db, q.ns, core.RecordType(q.rt), q.id); err != nil {
			return 0, 0, err
		}
		if q.live {
			texts = append(texts, q.text)
			toEmbed = append(toEmbed, q)
		}
	}
	if len(texts) > 0 {
		vecs, err := ix.emb.Embed(ctx, texts)
		if err != nil {
			return 0, 0, err
		}
		if len(vecs) != len(texts) {
			return 0, 0, core.Errf(core.CodeInternal, "embedder returned %d vectors for %d texts", len(vecs), len(texts))
		}
		for i, q := range toEmbed {
			if err := CheckDim(vecs[i], ix.emb.Dim()); err != nil {
				return 0, 0, err
			}
			q.vec = Normalized(vecs[i])
		}
	}

	model := ix.emb.ModelID()
	var ds []delta
	err = ix.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		type counts struct{ indexed, skipped int }
		perNS := map[string]*counts{}
		var ops []vecOp
		var dequeue []*queued
		for _, q := range items {
			var still int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM embed_queue WHERE record_type = ? AND record_id = ? AND enqueued_at = ? AND namespace = ?`,
				q.rt, q.id, q.at, q.ns).Scan(&still); err != nil {
				return err
			}
			if still == 0 {
				continue // removed or re-enqueued meanwhile: not ours any more
			}
			text, live, err := recordText(ctx, tx, q.ns, core.RecordType(q.rt), q.id)
			if err != nil {
				return err
			}
			if live && (!q.live || text != q.text) {
				continue // changed since the read: leave queued, the next batch embeds the new text
			}
			if perNS[q.ns] == nil {
				perNS[q.ns] = &counts{}
			}
			if live {
				ops = append(ops, vecOp{q.ns, model, core.RecordType(q.rt), q.id, q.vec})
				perNS[q.ns].indexed++
			} else {
				perNS[q.ns].skipped++ // forgotten, retired or unknown type
			}
			dequeue = append(dequeue, q)
		}
		var err error
		if ds, err = writeOps(ctx, tx, ops); err != nil {
			return err
		}
		for _, q := range dequeue {
			if _, err := tx.ExecContext(ctx, `DELETE FROM embed_queue WHERE record_type = ? AND record_id = ?`, q.rt, q.id); err != nil {
				return err
			}
		}
		nss := make([]string, 0, len(perNS))
		for ns := range perNS {
			nss = append(nss, ns)
		}
		sort.Strings(nss)
		for _, ns := range nss {
			c := perNS[ns]
			if _, err := ix.env.Audit.Append(ctx, tx, audit.Entry{
				Namespace: ns, Actor: Actor, Action: ActEmbedIndex, Target: model,
				Details: map[string]any{"model_id": model, "indexed": c.indexed, "skipped": c.skipped},
			}); err != nil {
				return err
			}
		}
		done = len(dequeue)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	ix.idx.apply(ds)
	return len(items), done, nil
}

// Backfill enqueues every live record of namespace that lacks a vector for
// the current model (the first step of a model migration, or a repair after
// a crash). Returns how many rows were enqueued. Audited.
func (ix *Indexer) Backfill(ctx context.Context, namespace string) (int, error) {
	model := ix.emb.ModelID()
	n := 0
	err := ix.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		refs, err := missingFor(ctx, tx, namespace, model)
		if err != nil {
			return err
		}
		now := core.FormatTime(ix.env.Clock.Now())
		for _, r := range refs {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at) VALUES (?,?,?,?)
				 ON CONFLICT(record_type, record_id) DO NOTHING`, namespace, string(r.Type), r.ID, now)
			if err != nil {
				return err
			}
			k, _ := res.RowsAffected()
			n += int(k)
		}
		if n == 0 {
			return nil
		}
		_, err = ix.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: namespace, Actor: Actor, Action: ActEmbedBackfill, Target: model,
			Details: map[string]any{"model_id": model, "enqueued": n},
		})
		return err
	})
	if err == nil && n > 0 {
		ix.Notify()
	}
	return n, err
}

// CompleteMigration drops the vectors of oldModelID in namespace once the
// current model covers every live record. It refuses (CodeConflict) while
// records are still missing or when oldModelID is the current model.
func (ix *Indexer) CompleteMigration(ctx context.Context, namespace, oldModelID string) (int64, error) {
	if oldModelID == ix.emb.ModelID() {
		return 0, core.Errf(core.CodeConflict, "model %s is the current model", oldModelID)
	}
	missing, err := ix.idx.MissingFor(ctx, namespace, ix.emb.ModelID())
	if err != nil {
		return 0, err
	}
	if len(missing) > 0 {
		return 0, core.Errf(core.CodeConflict, "migration to %s incomplete: %d records lack vectors", ix.emb.ModelID(), len(missing))
	}
	return ix.idx.DropModel(ctx, namespace, oldModelID)
}
