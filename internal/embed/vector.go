// Package embed holds the embedding side of Chanda (BASE.md "Go stack":
// Embeddings, Vector search; write step 7; the flush tool): the built-in
// deterministic HashEmbedder, the vector BLOB encoding, the exact in-memory
// vector index over the embeddings table, and the Indexer that drains
// embed_queue.
package embed

import (
	"encoding/binary"
	"math"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Encode serializes vec as little-endian float32, the embeddings.vector format.
func Encode(vec []float32) []byte {
	b := make([]byte, 4*len(vec))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

// Decode parses a little-endian float32 BLOB of exactly dim values.
func Decode(b []byte, dim int) ([]float32, error) {
	v := make([]float32, dim)
	return v, DecodeInto(v, b)
}

// DecodeInto parses b into dst; len(b) must be 4*len(dst).
func DecodeInto(dst []float32, b []byte) error {
	if len(b) != 4*len(dst) {
		return core.Errf(core.CodeIntegrity, "vector blob has %d bytes, want %d (dim %d)", len(b), 4*len(dst), len(dst))
	}
	for i := range dst {
		dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return nil
}

// CheckDim validates that vec has dimension dim and only finite values.
func CheckDim(vec []float32, dim int) error {
	if dim <= 0 {
		return core.Errf(core.CodeInvalidArgument, "dimension must be positive, got %d", dim)
	}
	if len(vec) != dim {
		return core.Errf(core.CodeInvalidArgument, "vector has dimension %d, want %d", len(vec), dim)
	}
	for _, f := range vec {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return core.Errf(core.CodeInvalidArgument, "vector contains a non-finite value")
		}
	}
	return nil
}

// Dot is the dot product of two equal-length vectors. The summation order is
// fixed (four interleaved accumulators), so the result is bit-identical for
// the same inputs regardless of where the vectors sit in memory.
func Dot(a, b []float32) float32 {
	n := len(a)
	b = b[:n]
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= n; i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < n; i++ {
		s0 += a[i] * b[i]
	}
	return (s0 + s1) + (s2 + s3)
}

// Norm is the L2 norm of v, computed in float64.
func Norm(v []float32) float64 {
	var s float64
	for _, f := range v {
		s += float64(f) * float64(f)
	}
	return math.Sqrt(s)
}

// normTolerance is how far from 1 a norm may be before Normalized rescales.
const normTolerance = 1e-4

// Normalized returns v scaled to unit L2 norm. A vector already within
// tolerance of unit length is returned unchanged (same bits); a zero vector
// is returned as is.
func Normalized(v []float32) []float32 {
	n := Norm(v)
	if n == 0 || math.Abs(n-1) <= normTolerance {
		return v
	}
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(float64(f) / n)
	}
	return out
}
