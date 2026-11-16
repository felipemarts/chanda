package read

import (
	"context"
	"sort"

	"github.com/felipemarts/chanda/internal/core"
)

// plainEmb embeds every text as [1,0].
type plainEmb struct{}

func (plainEmb) ModelID() string { return "fake-v1" }
func (plainEmb) Dim() int        { return 2 }
func (plainEmb) Embed(_ context.Context, ts []string) ([][]float32, error) {
	out := make([][]float32, len(ts))
	for i := range ts {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

// roleEmb embeds passages as [1,0] and queries as [0,1].
type roleEmb struct{ plainEmb }

func (roleEmb) EmbedQuery(_ context.Context, ts []string) ([][]float32, error) {
	out := make([][]float32, len(ts))
	for i := range ts {
		out[i] = []float32{0, 1}
	}
	return out, nil
}

// fakeIndex returns fixed hits per namespace; leak hits are returned for any
// namespace (simulates a buggy index for defence-in-depth tests).
type fakeIndex struct {
	hits  map[string][]core.VectorHit
	leak  []core.VectorHit
	lastQ []float32
	lastK int
	lastM string
}

func (f *fakeIndex) Upsert(context.Context, string, core.RecordType, string, string, []float32) error {
	return nil
}
func (f *fakeIndex) Remove(context.Context, string, core.RecordType, string) error { return nil }
func (f *fakeIndex) Search(_ context.Context, ns, model string, q []float32, k int, types []core.RecordType) ([]core.VectorHit, error) {
	f.lastQ, f.lastK, f.lastM = q, k, model
	var out []core.VectorHit
	for _, h := range append(append([]core.VectorHit{}, f.hits[ns]...), f.leak...) {
		if len(types) == 0 || containsType(types, h.RecordType) {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].RecordID < out[j].RecordID
	})
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

// fakeRerank scores a doc by a fixed table keyed on text; unknown texts get 0.
type fakeRerank struct{ by map[string]float32 }

func (fakeRerank) ModelID() string { return "fake-rerank" }
func (f fakeRerank) Score(_ context.Context, _ string, docs []string) ([]float32, error) {
	out := make([]float32, len(docs))
	for i, d := range docs {
		out[i] = f.by[d]
	}
	return out, nil
}

type fakeUtility struct {
	lambda float64
	by     map[string]float64
}

func (f fakeUtility) Scores(_ context.Context, _ string, ids []string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, id := range ids {
		if v, ok := f.by[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}
func (f fakeUtility) Lambda(context.Context, string) (float64, error) { return f.lambda, nil }

type fakeLearned struct{}

func (fakeLearned) Score(_ context.Context, _ string, c Candidate) (float64, bool) {
	return 0.42, c.Type == core.RecEvent
}
