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
// neighbours among types, in namespace ns. The returned list keeps the
// index order (cosine desc, record id asc) and is re-ranked 1..n.
func (r *Reader) vectorSearch(ctx context.Context, ns, query string, types []core.RecordType) ([]hit, error) {
	if !r.vectorEnabled() {
		return nil, nil
	}
	var vecs [][]float32
	var err error
	if re, ok := r.deps.Embedder.(core.RoleEmbedder); ok {
		vecs, err = re.EmbedQuery(ctx, []string{query})
	} else {
		vecs, err = r.deps.Embedder.Embed(ctx, []string{query})
	}
	if err != nil {
		return nil, core.Wrap(core.CodeUnavailable, err, "embed query")
	}
	if len(vecs) != 1 {
		return nil, core.Errf(core.CodeInternal, "embedder returned %d vectors for 1 text", len(vecs))
	}
	hs, err := r.deps.VectorIndex.Search(ctx, ns, r.deps.Embedder.ModelID(), vecs[0], r.e.Cfg.Read.VectorTopK, types)
	if err != nil {
		return nil, core.Wrap(core.CodeUnavailable, err, "vector search")
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
	return out, nil
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
