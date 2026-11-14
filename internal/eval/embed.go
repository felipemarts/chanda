package eval

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// LocalEmbedder is a tiny deterministic embedder for the harness: signed
// feature hashing of content terms (weight 1) and their character trigrams
// (weight 0.5), L2-normalized. It is a stand-in until the real Embedder is
// injected; it captures lexical and sub-word overlap, not meaning.
type LocalEmbedder struct{ dim int }

var _ core.Embedder = (*LocalEmbedder)(nil)

// NewLocalEmbedder returns a LocalEmbedder of the given dimension.
func NewLocalEmbedder(dim int) *LocalEmbedder { return &LocalEmbedder{dim: dim} }

func (e *LocalEmbedder) ModelID() string { return fmt.Sprintf("eval-localhash-v1-%d", e.dim) }
func (e *LocalEmbedder) Dim() int        { return e.dim }

func (e *LocalEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.one(t)
	}
	return out, nil
}

func (e *LocalEmbedder) one(text string) []float32 {
	acc := make([]float64, e.dim)
	add := func(feature string, w float64) {
		h := fnv.New64a()
		h.Write([]byte(feature))
		s := h.Sum64()
		if s>>63 == 1 {
			w = -w
		}
		acc[s%uint64(e.dim)] += w
	}
	for _, t := range contentTerms(text) {
		add("w:"+t, 1)
		r := []rune("^" + t + "$")
		for i := 0; i+3 <= len(r); i++ {
			add("c:"+string(r[i:i+3]), 0.5)
		}
	}
	var norm float64
	for _, v := range acc {
		norm += v * v
	}
	vec := make([]float32, e.dim)
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for i, v := range acc {
		vec[i] = float32(v / norm)
	}
	return vec
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
