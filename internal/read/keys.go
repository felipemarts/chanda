package read

import (
	"context"
	"math"
	"sort"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/keys"
)

// Learned query keys on the read path (decision 0145).
//
// With read.learned_keys on and the namespace's runtime switch on, steps 2
// and 3 also search the keys: the query vector against key vectors
// (record type "key", embedded with the query role) and the query terms
// against learned_keys_fts, top read.learned_keys_top_k each. Every key
// hit maps to its record; a record's key rank is the best rank, among its
// keys, in either key search. Those records form one more ranked list:
//
//   - a record not yet a candidate becomes one (and goes through the rule
//     filter, near-duplicate collapse, rerank, utility, cut and selection
//     like any other);
//   - its RRF gains w/(k + key rank), w = read.learned_keys_weight. The
//     normalisation maxRRF = L/(k+1) does not change (L counts keyword and
//     vector only), so a read without key hits scores exactly as before;
//   - for the evidence cut, the key's own match counts too (keyEvidence):
//     the best cosine of the query to a matched key (vector search) reaching
//     the model's MinCosine floor, or the best coverage of the query terms
//     in a matched key's text reaching MinTermCoverage. A record's own
//     coverage and cosine (and the logged "evidence") are unchanged.
//
// Keys themselves never become candidates, items or block text. The logged
// candidates carry features.key_rank (0 when none), key_cosine and
// key_coverage, only on reads where keys were searched; a read with keys
// off logs exactly what it logged before.

// keyMatch is one record reached through keys.
type keyMatch struct {
	typ      core.RecordType
	id       string
	rank     int     // best rank among its keys in either key search (1-based)
	cosine   float64 // best cosine of the query to a matched key (vector search)
	hasCos   bool
	coverage float64 // best coverage of the query terms in a matched key's text
}

// keysSpec is the FTS spec of the key index; any key row is eligible.
var keysSpec = ftsSpec{typ: core.RecKey, fts: "learned_keys_fts", base: "learned_keys", bm25: "bm25(learned_keys_fts)", extra: "1=1"}

// keysActive reports whether this read searches the keys: the configuration
// and the namespace's runtime switch must both be on.
func (r *Reader) keysActive(ctx context.Context, ns string) (bool, error) {
	if !r.e.Cfg.Read.LearnedKeys {
		return false, nil
	}
	on, err := keys.Enabled(ctx, r.db(), ns)
	if err != nil {
		return false, core.Wrap(core.CodeInternal, err, "learned keys switch")
	}
	return on, nil
}

// keySearch runs the two key searches and returns the records they reach,
// best key rank first (then id). qvec is the query vector, nil when the
// vector side did not run. types, when set, restricts the records.
// vecErr reports a failed vector search (the keyword side still counts).
func (r *Reader) keySearch(ctx context.Context, ns string, terms []string, qvec []float32, types map[core.RecordType]bool) (out []keyMatch, vecErr error, err error) {
	k := r.e.Cfg.Read.KeysTopK()
	type keyHit struct {
		rank   int
		cosine float64
		hasCos bool
	}
	hits := map[string]*keyHit{}
	at := func(id string, rank int) *keyHit {
		h := hits[id]
		if h == nil {
			h = &keyHit{rank: rank}
			hits[id] = h
		}
		if rank < h.rank {
			h.rank = rank
		}
		return h
	}
	if qvec != nil && r.deps.VectorIndex != nil && r.deps.Embedder != nil {
		hs, err := r.deps.VectorIndex.Search(ctx, ns, r.deps.Embedder.ModelID(), qvec, k, []core.RecordType{core.RecKey})
		if err != nil {
			vecErr = err
		} else {
			rank := 0
			for _, h := range hs {
				if h.RecordType != core.RecKey {
					continue
				}
				rank++
				kh := at(h.RecordID, rank)
				if c := float64(h.Score); !kh.hasCos || c > kh.cosine {
					kh.cosine, kh.hasCos = c, true
				}
			}
		}
	}
	if matchExpr(terms) != "" && k > 0 {
		list, err := r.keywordTop(ctx, keysSpec, terms, ns, k, keysSpec.extra, nil)
		if err != nil {
			return nil, vecErr, core.Wrap(core.CodeInternal, err, "keyword search keys")
		}
		for _, h := range list {
			at(h.id, h.rank)
		}
	}
	if len(hits) == 0 {
		return nil, vecErr, nil
	}
	ids := sortedKeys(hits)
	byRec := map[string]*keyMatch{}
	for start := 0; start < len(ids); start += inBatch {
		batch := ids[start:min(start+inBatch, len(ids))]
		args := []any{ns}
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := r.db().QueryContext(ctx, `SELECT id, record_type, record_id, text FROM learned_keys
			WHERE namespace = ? AND id IN (`+placeholders(len(batch))+`) ORDER BY id`, args...)
		if err != nil {
			return nil, vecErr, core.Wrap(core.CodeInternal, err, "load keys")
		}
		for rows.Next() {
			var id, rt, rid, text string
			if err := rows.Scan(&id, &rt, &rid, &text); err != nil {
				rows.Close()
				return nil, vecErr, err
			}
			t := core.RecordType(rt)
			if !searchable(t) || (len(types) > 0 && !types[t]) {
				continue
			}
			h := hits[id]
			m := byRec[rid]
			if m == nil {
				m = &keyMatch{typ: t, id: rid, rank: h.rank}
				byRec[rid] = m
			}
			if h.rank < m.rank {
				m.rank = h.rank
			}
			if h.hasCos && (!m.hasCos || h.cosine > m.cosine) {
				m.cosine, m.hasCos = h.cosine, true
			}
			m.coverage = math.Max(m.coverage, coverage(terms, text, r.e.Cfg.Read.CoverageTermCap))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, vecErr, err
		}
	}
	out = make([]keyMatch, 0, len(byRec))
	for _, m := range byRec {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].id < out[j].id
	})
	return out, vecErr, nil
}

// keyEvidence reports whether the key match of c is enough evidence for
// the context cut (see the package comment above).
func (r *Reader) keyEvidence(c *cand) bool {
	if c.keyRank == 0 {
		return false
	}
	return c.keyCoverage >= r.floors.MinTermCoverage || (c.hasKeyCos && c.keyCosine >= r.floors.MinCosine)
}
