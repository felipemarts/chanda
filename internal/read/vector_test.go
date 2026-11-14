package read

import (
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// BASE.md read step 3: the query embedding is computed on CPU; top K nearest neighbours.
func TestVectorSearchTopKUsesQueryRole(t *testing.T) {
	f := newFx(t)
	idx := &fakeIndex{hits: map[string][]core.VectorHit{"ns": {
		{RecordType: core.RecEvent, RecordID: "evt_2", Score: 0.9},
		{RecordType: core.RecFact, RecordID: "fct_1", Score: 0.8},
		{RecordType: core.RecEvent, RecordID: "evt_1", Score: 0.9},
		{RecordType: core.RecEntity, RecordID: "ent_1", Score: 0.99},
	}}}
	f.e.Cfg.Read.VectorTopK = 3
	r := New(f.e, Deps{Embedder: roleEmb{}, VectorIndex: idx})
	got, err := r.vectorSearch(f.ctx, "ns", "q", nil)
	if err != nil {
		t.Fatal(err)
	}
	if idx.lastK != 3 || idx.lastM != "fake-v1" || idx.lastQ[1] != 1 {
		t.Fatalf("index called with k=%d model=%s q=%v (EmbedQuery must be used)", idx.lastK, idx.lastM, idx.lastQ)
	}
	// entity hit is dropped (not a searchable type); ties keep index order (id asc).
	if len(got) != 2 || got[0].id != "evt_1" || got[0].rank != 1 || got[1].id != "evt_2" || got[1].rank != 2 {
		t.Fatalf("hits: %+v", got)
	}

	r = New(f.e, Deps{Embedder: plainEmb{}, VectorIndex: idx})
	if _, err := r.vectorSearch(f.ctx, "ns", "q", []core.RecordType{core.RecFact}); err != nil || idx.lastQ[0] != 1 {
		t.Fatalf("plain embedder: %v %v", idx.lastQ, err)
	}

	for _, d := range []Deps{{}, {Embedder: plainEmb{}}, {VectorIndex: idx}} {
		if hs, err := New(f.e, d).vectorSearch(f.ctx, "ns", "q", nil); err != nil || hs != nil {
			t.Fatalf("missing dependency must give keyword-only: %v %v", hs, err)
		}
	}
}
