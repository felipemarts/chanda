package embed

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// HashEmbedder is the built-in deterministic embedder (provider "hash"). It
// is a lexical baseline, not a semantic model: text is folded (NFD, combining
// marks dropped, lowercase), split like FTS5 unicode61 (runs of letters and
// digits), and each word becomes three kinds of features: the word itself, a
// truncation stem (its first 5 runes, a cheap stemmer that works well for
// Portuguese inflection) and its boundary-padded character 3- and 4-grams. Features are signed-hashed (FNV-1a 64, fixed seed) into
// Dim buckets with sublinear term frequency (1 + ln tf) and the result is
// L2-normalized. Stems and n-grams make morphological variants ("preço",
// "preços") land close together.
//
// The output depends only on the text and Dim, so ModelID encodes both the
// algorithm version and the dimension: changing either is a model change.
type HashEmbedder struct {
	dim int
}

// HashVersion is bumped whenever the feature extraction changes, because a
// different algorithm under the same ModelID would mix incompatible vectors.
const HashVersion = "v1"

// Feature weights, tuned on the similarity table in hash_test.go: a shared
// stem counts most, an exact word a little less, each shared n-gram half.
const (
	hashWordWeight = 0.7
	hashStemWeight = 1.0
	hashStemRunes  = 5
	hashGramWeight = 0.5
	hashSeed       = "chanda-hash-v1"
)

// NewHash returns a HashEmbedder of dimension dim.
func NewHash(dim int) (*HashEmbedder, error) {
	if dim < 8 {
		return nil, core.Errf(core.CodeInvalidArgument, "hash embedder: dim must be >= 8, got %d", dim)
	}
	return &HashEmbedder{dim: dim}, nil
}

// HashModelID is the model id of a HashEmbedder of dimension dim.
func HashModelID(dim int) string { return fmt.Sprintf("hash-%s-%d", HashVersion, dim) }

func (h *HashEmbedder) ModelID() string { return HashModelID(h.dim) }
func (h *HashEmbedder) Dim() int        { return h.dim }

// Embed returns one L2-normalized vector per text. Text without any letter
// or digit yields the zero vector (cosine 0 against everything).
func (h *HashEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = h.embedOne(t)
	}
	return out, nil
}

// Feature is one hashed feature: a word ("w:") or a character n-gram ("g3:", "g4:").
type Feature struct {
	Key    string
	Weight float64
}

func (h *HashEmbedder) embedOne(text string) []float32 {
	feats := Features(text)
	acc := make([]float64, h.dim)
	// feats is sorted by key, so equal keys are adjacent: count tf in one pass
	// and accumulate in a fixed order (bit-identical output across runs).
	for i := 0; i < len(feats); {
		j := i
		for j < len(feats) && feats[j].Key == feats[i].Key {
			j++
		}
		tf := float64(j - i)
		w := feats[i].Weight * (1 + math.Log(tf))
		hv := fnv1a(feats[i].Key)
		idx := int((hv >> 1) % uint64(h.dim))
		if hv&1 == 1 {
			w = -w
		}
		acc[idx] += w
		i = j
	}
	var n float64
	for _, a := range acc {
		n += a * a
	}
	v := make([]float32, h.dim)
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	for i, a := range acc {
		v[i] = float32(a / n)
	}
	return v
}

// Features extracts the sorted feature list of text (exported for tests and
// for tools that want to explain a hash similarity).
func Features(text string) []Feature {
	var feats []Feature
	for _, tok := range Tokens(text) {
		feats = append(feats, Feature{"w:" + tok, hashWordWeight})
		r := []rune("^" + tok + "$")
		if len(r)-2 > hashStemRunes {
			feats = append(feats, Feature{"s:" + string(r[1:1+hashStemRunes]), hashStemWeight})
		} else {
			feats = append(feats, Feature{"s:" + tok, hashStemWeight})
		}
		for i := 0; i+3 <= len(r); i++ {
			feats = append(feats, Feature{"g3:" + string(r[i:i+3]), hashGramWeight})
		}
		for i := 0; i+4 <= len(r); i++ {
			feats = append(feats, Feature{"g4:" + string(r[i:i+4]), hashGramWeight})
		}
	}
	sort.SliceStable(feats, func(i, j int) bool { return feats[i].Key < feats[j].Key })
	return feats
}

// Tokens folds text and splits it into unicode61-like tokens: lowercase,
// combining marks removed after canonical decomposition, runs of letters and
// digits; everything else separates.
func Tokens(text string) []string {
	var b strings.Builder
	for _, r := range norm.NFD.String(text) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.FieldsFunc(b.String(), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

// fnv1a is 64-bit FNV-1a over the fixed seed followed by s.
func fnv1a(s string) uint64 {
	const offset, prime = 14695981039346656037, 1099511628211
	h := uint64(offset)
	for i := 0; i < len(hashSeed); i++ {
		h ^= uint64(hashSeed[i])
		h *= prime
	}
	h ^= 0xff // separator between seed and key
	h *= prime
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

var _ core.Embedder = (*HashEmbedder)(nil)
