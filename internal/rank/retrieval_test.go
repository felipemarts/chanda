package rank

import (
	"reflect"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
)

func TestRetrievalLogFormatToleratesMissingFields(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Candidate
	}{
		{"empty", ``, nil},
		{"not json", `{oops`, nil},
		{"only id derives type from prefix", `[{"id":"fct_1"}]`, []Candidate{{ID: "fct_1"}}},
		{"full", `[{"id":"evt_1","type":"event","bm25":1.5,"cosine":0.5,"rerank":0.2,"rrf":0.03,"utility":0.6,"learned":0.4,"final":0.2,"zone":"user","age_seconds":10}]`,
			[]Candidate{{ID: "evt_1", Type: core.RecEvent, BM25: 1.5, Cosine: 0.5, Rerank: 0.2, RRF: 0.03, Utility: 0.6, Learned: 0.4, Final: 0.2, Zone: core.ZoneUser, AgeSeconds: 10}}},
		{"malformed element skipped, nulls are zero, unknown ignored", `[{"id":"evt_1","bm25":null,"x":1},{"id":3},{"bm25":2}]`, []Candidate{{ID: "evt_1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCandidates(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
	if c := ParseCandidates(`[{"id":"fct_1"}]`)[0]; c.RecordType() != core.RecFact {
		t.Fatalf("type from prefix: %q", c.RecordType())
	}
	if c := (Candidate{RRF: 0.1}); c.Relevance() != 0.1 {
		t.Fatal("relevance falls back to rrf")
	}
	if c := ParseCandidates(`[{"id":"a","features":{"entity_match":1}}]`)[0]; !c.HasEntityMatch() {
		t.Fatal("entity match from features")
	}
	if got := ParseIDs(`["a", 1, "", "b", "a"]`); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("ids: %v", got)
	}
}
