package embed

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"sort"
	"sync"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Actor is recorded in the audit log for mutations made by this package.
const Actor = "system:embed"

// Audit actions of this package (registered in internal/audit).
const (
	ActEmbedIndex    = audit.ActEmbedIndex     // vectors written (indexer batch or Upsert)
	ActEmbedRemove   = audit.ActEmbedRemove    // vectors of one record removed
	ActEmbedDrop     = audit.ActEmbedDropModel // all vectors of an old model dropped after migration
	ActEmbedBackfill = audit.ActEmbedBackfill  // records lacking a vector enqueued for the current model
)

// minPerWorker is the smallest slice of vectors worth a goroutine.
const minPerWorker = 4096

// ExactIndex implements core.VectorIndex over the embeddings table with an
// exact cosine scan (BASE.md "Vector search": deterministic and simple).
//
// Vectors are cached in memory per (namespace, model_id), loaded lazily on
// the first Search. The cache stays coherent in two ways: mutations made
// through this index are applied to the cache after their transaction
// commits, and every Search first reads embedding_generations (bumped by
// trigger on any change to embeddings, migration 0030) and reloads when the
// counter moved — so rows deleted by plain SQL elsewhere (forget) can never
// be returned from a stale cache.
//
// Search must not be called while the caller holds a transaction: the store
// has a single connection.
type ExactIndex struct {
	env     *env.Env
	workers int

	mu     sync.RWMutex
	shards map[shardKey]*shard
}

// NewExactIndex returns an index over e.Store. Scan parallelism comes from
// e.Cfg.Embed.ScanWorkers (0 = GOMAXPROCS).
func NewExactIndex(e *env.Env) *ExactIndex {
	w := e.Cfg.Embed.ScanWorkers
	if w <= 0 {
		w = runtime.GOMAXPROCS(0)
	}
	return &ExactIndex{env: e, workers: w, shards: map[shardKey]*shard{}}
}

type shardKey struct{ ns, model string }

type recKey struct {
	rt core.RecordType
	id string
}

// shard holds the vectors of one (namespace, model_id), flat for scanning.
// Slot order depends on history; results never do, because every score is
// computed independently and the output is totally ordered.
type shard struct {
	gen       int64
	dim       int
	ids       []string
	types     []uint8
	vecs      []float32 // len(ids) * dim
	pos       map[recKey]int
	typeNames []core.RecordType
	typeCode  map[core.RecordType]uint8
}

func newShard(gen int64, dim int) *shard {
	return &shard{gen: gen, dim: dim, pos: map[recKey]int{}, typeCode: map[core.RecordType]uint8{}}
}

func (s *shard) code(rt core.RecordType) uint8 {
	if c, ok := s.typeCode[rt]; ok {
		return c
	}
	c := uint8(len(s.typeNames))
	s.typeNames = append(s.typeNames, rt)
	s.typeCode[rt] = c
	return c
}

func (s *shard) set(rt core.RecordType, id string, vec []float32) {
	k := recKey{rt, id}
	if i, ok := s.pos[k]; ok {
		copy(s.vecs[i*s.dim:(i+1)*s.dim], vec)
		return
	}
	s.pos[k] = len(s.ids)
	s.ids = append(s.ids, id)
	s.types = append(s.types, s.code(rt))
	s.vecs = append(s.vecs, vec...)
}

func (s *shard) del(rt core.RecordType, id string) {
	k := recKey{rt, id}
	i, ok := s.pos[k]
	if !ok {
		return
	}
	last := len(s.ids) - 1
	if i != last {
		lk := recKey{s.typeNames[s.types[last]], s.ids[last]}
		s.ids[i], s.types[i] = s.ids[last], s.types[last]
		copy(s.vecs[i*s.dim:(i+1)*s.dim], s.vecs[last*s.dim:])
		s.pos[lk] = i
	}
	delete(s.pos, k)
	s.ids, s.types, s.vecs = s.ids[:last], s.types[:last], s.vecs[:last*s.dim]
}

// ---------------------------------------------------------------------------
// Search

// Search returns the k nearest records to q by cosine similarity among the
// vectors of modelID in namespace, optionally restricted to record types.
// Ordering is total: score descending, then record id ascending, then record
// type. q is normalized first; a zero query returns no hits.
func (x *ExactIndex) Search(ctx context.Context, namespace, modelID string, q []float32, k int, types []core.RecordType) ([]core.VectorHit, error) {
	if k <= 0 {
		return nil, nil
	}
	if namespace == "" || modelID == "" {
		return nil, core.Errf(core.CodeInvalidArgument, "vector search needs namespace and model_id")
	}
	if err := CheckDim(q, len(q)); err != nil {
		return nil, err
	}
	q = Normalized(q)
	if Norm(q) == 0 {
		return nil, nil
	}
	sh, release, err := x.acquire(ctx, namespace, modelID)
	if err != nil {
		return nil, err
	}
	defer release()
	if len(sh.ids) == 0 {
		return nil, nil
	}
	if len(q) != sh.dim {
		return nil, core.Errf(core.CodeInvalidArgument, "query has dimension %d, model %s has %d", len(q), modelID, sh.dim)
	}
	var mask *[256]bool
	if len(types) > 0 {
		mask = new([256]bool)
		found := false
		for _, t := range types {
			if c, ok := sh.typeCode[t]; ok {
				mask[c], found = true, true
			}
		}
		if !found {
			return nil, nil
		}
	}
	top := sh.scan(ctx, q, k, mask, x.workers)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]core.VectorHit, len(top))
	for i, h := range top {
		out[i] = core.VectorHit{RecordType: sh.typeNames[sh.types[h.i]], RecordID: sh.ids[h.i], Score: h.score}
	}
	return out, nil
}

type hit struct {
	score float32
	i     int
}

// better is the total result order: score desc, id asc, type asc.
func (s *shard) better(a, b hit) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if s.ids[a.i] != s.ids[b.i] {
		return s.ids[a.i] < s.ids[b.i]
	}
	return s.typeNames[s.types[a.i]] < s.typeNames[s.types[b.i]]
}

// scan splits the slots into fixed contiguous ranges, keeps a bounded top-k
// per range and merges them under the total order. The result does not
// depend on the number of workers.
func (s *shard) scan(ctx context.Context, q []float32, k int, mask *[256]bool, workers int) []hit {
	n := len(s.ids)
	w := (n + minPerWorker - 1) / minPerWorker
	if w > workers {
		w = workers
	}
	if w < 1 {
		w = 1
	}
	chunk := (n + w - 1) / w
	parts := make([][]hit, w)
	var wg sync.WaitGroup
	for p := 0; p < w; p++ {
		lo, hi := p*chunk, min((p+1)*chunk, n)
		wg.Add(1)
		go func(p, lo, hi int) {
			defer wg.Done()
			parts[p] = s.scanRange(ctx, q, k, mask, lo, hi)
		}(p, lo, hi)
	}
	wg.Wait()
	var all []hit
	for _, p := range parts {
		all = append(all, p...)
	}
	sort.Slice(all, func(a, b int) bool { return s.better(all[a], all[b]) })
	if len(all) > k {
		all = all[:k]
	}
	return all
}

func (s *shard) scanRange(ctx context.Context, q []float32, k int, mask *[256]bool, lo, hi int) []hit {
	top := make([]hit, 0, min(k, hi-lo)+1) // best first
	d := s.dim
	for i := lo; i < hi; i++ {
		if (i-lo)&4095 == 4095 && ctx.Err() != nil {
			return nil
		}
		if mask != nil && !mask[s.types[i]] {
			continue
		}
		h := hit{Dot(q, s.vecs[i*d:(i+1)*d]), i}
		if len(top) == k {
			worst := top[k-1]
			if h.score < worst.score || !s.better(h, worst) {
				continue
			}
			top = top[:k-1]
		}
		j := sort.Search(len(top), func(j int) bool { return s.better(h, top[j]) })
		top = append(top, hit{})
		copy(top[j+1:], top[j:])
		top[j] = h
	}
	return top
}

// Warm loads the vectors of (namespace, modelID) into memory now, so the
// first Search does not pay the cold load (about 0.5-1.2 s at 100k vectors;
// decision 0030). Call it at startup for the default namespace.
func (x *ExactIndex) Warm(ctx context.Context, namespace, modelID string) error {
	_, release, err := x.acquire(ctx, namespace, modelID)
	if err == nil {
		release()
	}
	return err
}

// acquire returns the current shard with the read lock held.
func (x *ExactIndex) acquire(ctx context.Context, ns, model string) (*shard, func(), error) {
	key := shardKey{ns, model}
	gen, err := genOf(ctx, x.env.Store.DB(), ns, model)
	if err != nil {
		return nil, nil, err
	}
	x.mu.RLock()
	if sh := x.shards[key]; sh != nil && sh.gen == gen {
		return sh, x.mu.RUnlock, nil
	}
	x.mu.RUnlock()

	x.mu.Lock()
	if sh := x.shards[key]; sh == nil || sh.gen != gen {
		loaded, err := x.load(ctx, ns, model)
		if err != nil {
			x.mu.Unlock()
			return nil, nil, err
		}
		x.shards[key] = loaded
	}
	sh := x.shards[key]
	x.mu.Unlock()
	x.mu.RLock()
	return sh, x.mu.RUnlock, nil
}

func genOf(ctx context.Context, q store.Queryer, ns, model string) (int64, error) {
	var g int64
	err := q.QueryRowContext(ctx, `SELECT gen FROM embedding_generations WHERE namespace = ? AND model_id = ?`, ns, model).Scan(&g)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return g, err
}

// load reads every vector of (ns, model) and its generation in one transaction.
func (x *ExactIndex) load(ctx context.Context, ns, model string) (*shard, error) {
	var sh *shard
	err := x.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		gen, err := genOf(ctx, tx, ns, model)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT record_type, record_id, dim, vector FROM embeddings
			 WHERE namespace = ? AND model_id = ? ORDER BY record_type, record_id`, ns, model)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rt, id string
			var dim int
			var blob []byte
			if err := rows.Scan(&rt, &id, &dim, &blob); err != nil {
				return err
			}
			if sh == nil {
				sh = newShard(gen, dim)
			}
			if dim != sh.dim {
				return core.Errf(core.CodeIntegrity, "model %s has vectors of dimension %d and %d in namespace %s", model, sh.dim, dim, ns)
			}
			v := make([]float32, dim)
			if err := DecodeInto(v, blob); err != nil {
				return err
			}
			sh.set(core.RecordType(rt), id, v)
		}
		if sh == nil {
			sh = newShard(gen, 0)
		}
		return rows.Err()
	})
	return sh, err
}

// ---------------------------------------------------------------------------
// Mutations. SQL runs inside a transaction; the cache is updated only after
// commit, and only if no other change slipped in (otherwise it is dropped and
// reloaded on the next Search).

type vecOp struct {
	ns, model string
	rt        core.RecordType
	id        string
	vec       []float32 // nil = delete
}

type delta struct {
	key           shardKey
	before, after int64
	ops           []vecOp
}

// writeOps applies ops in tx and returns the cache deltas to apply after
// commit. Upserts are rejected with CodeConflict when the record already has
// a vector for the model under another namespace, and with
// CodeInvalidArgument when the dimension disagrees with stored vectors.
func writeOps(ctx context.Context, tx *sql.Tx, ops []vecOp) ([]delta, error) {
	byKey := map[shardKey]*delta{}
	var keys []shardKey
	for _, op := range ops {
		k := shardKey{op.ns, op.model}
		if byKey[k] == nil {
			g, err := genOf(ctx, tx, op.ns, op.model)
			if err != nil {
				return nil, err
			}
			byKey[k] = &delta{key: k, before: g}
			keys = append(keys, k)
		}
	}
	dims := map[shardKey]int{}
	for _, op := range ops {
		k := shardKey{op.ns, op.model}
		if op.vec == nil {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM embeddings WHERE namespace = ? AND record_type = ? AND record_id = ? AND model_id = ?`,
				op.ns, string(op.rt), op.id, op.model); err != nil {
				return nil, err
			}
		} else {
			want, ok := dims[k]
			if !ok {
				err := tx.QueryRowContext(ctx, `SELECT dim FROM embeddings WHERE namespace = ? AND model_id = ? LIMIT 1`, op.ns, op.model).Scan(&want)
				if errors.Is(err, sql.ErrNoRows) {
					want = len(op.vec)
				} else if err != nil {
					return nil, err
				}
				dims[k] = want
			}
			if len(op.vec) != want {
				return nil, core.Errf(core.CodeInvalidArgument, "vector for %s has dimension %d, model %s uses %d", op.id, len(op.vec), op.model, want)
			}
			res, err := tx.ExecContext(ctx,
				`INSERT INTO embeddings(namespace, record_type, record_id, model_id, dim, vector) VALUES (?,?,?,?,?,?)
				 ON CONFLICT(record_type, record_id, model_id) DO UPDATE SET dim = excluded.dim, vector = excluded.vector
				 WHERE embeddings.namespace = excluded.namespace`,
				op.ns, string(op.rt), op.id, op.model, len(op.vec), Encode(op.vec))
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, core.Errf(core.CodeConflict, "record %s already has a %s vector in another namespace", op.id, op.model)
			}
		}
		byKey[k].ops = append(byKey[k].ops, op)
	}
	out := make([]delta, 0, len(keys))
	for _, k := range keys {
		g, err := genOf(ctx, tx, k.ns, k.model)
		if err != nil {
			return nil, err
		}
		d := byKey[k]
		d.after = g
		out = append(out, *d)
	}
	return out, nil
}

// apply brings cached shards up to date after a commit.
func (x *ExactIndex) apply(ds []delta) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, d := range ds {
		sh := x.shards[d.key]
		switch {
		case sh == nil || sh.gen == d.after:
			// not cached, or already loaded after the commit
		case sh.gen == d.before:
			for _, op := range d.ops {
				if op.vec == nil {
					sh.del(op.rt, op.id)
					continue
				}
				if len(sh.ids) == 0 {
					sh.dim = len(op.vec) // empty shard: the first vector fixes the dimension
				}
				sh.set(op.rt, op.id, op.vec)
			}
			sh.gen = d.after
		default:
			delete(x.shards, d.key)
		}
	}
}

func validateUpsert(namespace string, rt core.RecordType, id, modelID string, vec []float32) ([]float32, error) {
	if namespace == "" || rt == "" || id == "" || modelID == "" {
		return nil, core.Errf(core.CodeInvalidArgument, "upsert needs namespace, record type, id and model_id")
	}
	if err := CheckDim(vec, len(vec)); err != nil {
		return nil, err
	}
	v := Normalized(vec)
	if &v[0] == &vec[0] {
		v = append([]float32(nil), vec...) // never alias the caller's slice in the cache
	}
	return v, nil
}

// Upsert writes the vector of a record for modelID (audited) and updates the cache.
func (x *ExactIndex) Upsert(ctx context.Context, namespace string, rt core.RecordType, id, modelID string, vec []float32) error {
	v, err := validateUpsert(namespace, rt, id, modelID, vec)
	if err != nil {
		return err
	}
	var ds []delta
	err = x.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if ds, err = writeOps(ctx, tx, []vecOp{{namespace, modelID, rt, id, v}}); err != nil {
			return err
		}
		_, err = x.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: namespace, Actor: Actor, Action: ActEmbedIndex, Target: id,
			Details: map[string]any{"record_type": rt, "model_id": modelID, "dim": len(v)},
		})
		return err
	})
	if err != nil {
		return err
	}
	x.apply(ds)
	return nil
}

// Remove deletes every vector of the record (all models) and its pending
// embed_queue row, audited. Removing a record without vectors is a no-op.
func (x *ExactIndex) Remove(ctx context.Context, namespace string, rt core.RecordType, id string) error {
	var ds []delta
	err := x.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT model_id FROM embeddings WHERE namespace = ? AND record_type = ? AND record_id = ? ORDER BY model_id`,
			namespace, string(rt), id)
		if err != nil {
			return err
		}
		var ops []vecOp
		var models []string
		for rows.Next() {
			var m string
			if err := rows.Scan(&m); err != nil {
				rows.Close()
				return err
			}
			ops = append(ops, vecOp{namespace, m, rt, id, nil})
			models = append(models, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if ds, err = writeOps(ctx, tx, ops); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM embed_queue WHERE namespace = ? AND record_type = ? AND record_id = ?`, namespace, string(rt), id)
		if err != nil {
			return err
		}
		queued, _ := res.RowsAffected()
		if len(ops) == 0 && queued == 0 {
			return nil
		}
		_, err = x.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: namespace, Actor: Actor, Action: ActEmbedRemove, Target: id,
			Details: map[string]any{"record_type": rt, "models": models, "dequeued": queued},
		})
		return err
	})
	if err != nil {
		return err
	}
	x.apply(ds)
	return nil
}

// DropModel deletes every vector of modelID in namespace (audited) and
// returns how many were removed. It is the only way old vectors go away after
// a model change (BASE.md: "embeddings is keyed by model_id"); call it once
// the new model is complete — Indexer.CompleteMigration checks that first.
func (x *ExactIndex) DropModel(ctx context.Context, namespace, modelID string) (int64, error) {
	if namespace == "" || modelID == "" {
		return 0, core.Errf(core.CodeInvalidArgument, "drop model needs namespace and model_id")
	}
	var n int64
	err := x.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE namespace = ? AND model_id = ?`, namespace, modelID)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		_, err = x.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: namespace, Actor: Actor, Action: ActEmbedDrop, Target: modelID,
			Details: map[string]any{"model_id": modelID, "removed": n},
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	x.mu.Lock()
	delete(x.shards, shardKey{namespace, modelID})
	x.mu.Unlock()
	return n, nil
}

// Models lists the model ids with vectors in namespace and their counts,
// ordered by model id.
func (x *ExactIndex) Models(ctx context.Context, namespace string) ([]ModelCount, error) {
	rows, err := x.env.Store.DB().QueryContext(ctx,
		`SELECT model_id, COUNT(*) FROM embeddings WHERE namespace = ? GROUP BY model_id ORDER BY model_id`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelCount
	for rows.Next() {
		var m ModelCount
		if err := rows.Scan(&m.ModelID, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ModelCount is the number of vectors stored for one model.
type ModelCount struct {
	ModelID string
	Count   int64
}

var _ core.VectorIndex = (*ExactIndex)(nil)
