package read

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// vectorEnabled reports whether the vector list can be produced.
func (r *Reader) vectorEnabled() bool {
	return r.deps.Embedder != nil && r.deps.VectorIndex != nil && r.e.Cfg.Read.VectorTopK > 0
}

// vectorSearch embeds the query on CPU (EmbedQuery when the embedder knows
// the query role) and asks the index for the top cfg.Read.VectorTopK
// neighbours among types, in namespace ns. An empty types means every
// record type of the memory (core.RecordTypes): learned-key vectors
// (decision 0145) never take a slot. The returned list keeps the index order (cosine desc,
// record id asc) and is re-ranked 1..n.
func (r *Reader) vectorSearch(ctx context.Context, ns, query string, types []core.RecordType) ([]hit, error) {
	hs, _, err := r.vectorSearchVec(ctx, ns, query, types)
	return hs, err
}

// vectorSearchVec is vectorSearch that also returns the query vector (nil
// when the query could not be embedded), for the learned-keys search.
func (r *Reader) vectorSearchVec(ctx context.Context, ns, query string, types []core.RecordType) ([]hit, []float32, error) {
	if !r.vectorEnabled() {
		return nil, nil, nil
	}
	var vecs [][]float32
	var err error
	if re, ok := r.deps.Embedder.(core.RoleEmbedder); ok {
		vecs, err = re.EmbedQuery(ctx, []string{query})
	} else {
		vecs, err = r.deps.Embedder.Embed(ctx, []string{query})
	}
	if err != nil {
		return nil, nil, core.Wrap(core.CodeUnavailable, err, "embed query")
	}
	if len(vecs) != 1 {
		return nil, nil, core.Errf(core.CodeInternal, "embedder returned %d vectors for 1 text", len(vecs))
	}
	search := types
	if len(search) == 0 {
		search = core.RecordTypes()
	}
	hs, err := r.deps.VectorIndex.Search(ctx, ns, r.deps.Embedder.ModelID(), vecs[0], r.e.Cfg.Read.VectorTopK, search)
	if err != nil {
		return nil, vecs[0], core.Wrap(core.CodeUnavailable, err, "vector search")
	}
	out := make([]hit, 0, len(hs))
	for _, h := range hs {
		if !searchable(h.RecordType) {
			continue
		}
		if len(types) > 0 && !containsType(types, h.RecordType) {
			continue
		}
		out = append(out, hit{typ: h.RecordType, id: h.RecordID, score: float64(h.Score), rank: len(out) + 1})
	}
	return out, vecs[0], nil
}

// searchable record types: the ones the read path returns.
func searchable(t core.RecordType) bool {
	return t == core.RecEvent || t == core.RecFact || t == core.RecPage
}

func containsType(ts []core.RecordType, t core.RecordType) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}
