package read

import (
	"encoding/json"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// BASE.md ranking signals: a record marked wrong or outdated (flags table)
// leaves reads, whatever its type and whatever record_type the flag carries.
func TestFlaggedRecordsLeaveReads(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	f.flag("ns", "event", "evt_backup_ns")
	f.flag("ns", "facts", "fct_backup_ns") // unusual record_type string: record_id alone decides
	r := New(f.e, Deps{})
	kw, err := r.keywordSearch(f.ctx, "ns", []string{"backup"}, keywordFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range kw {
		for _, h := range list {
			if h.id == "evt_backup_ns" || h.id == "fct_backup_ns" {
				t.Fatalf("flagged record in the keyword query: %s", h.id)
			}
		}
	}
	idx := &fakeIndex{hits: map[string][]core.VectorHit{"ns": {{RecordType: core.RecEvent, RecordID: "evt_backup_ns", Score: 0.99}}}}
	res, _ := New(f.e, Deps{Embedder: plainEmb{}, VectorIndex: idx}).Search(f.ctx, SearchInput{Namespace: "ns", Query: "database backup"})
	if find(res.Items, "evt_backup_ns") != nil || find(res.Items, "fct_backup_ns") != nil || find(res.Items, "pg_backup_ns") == nil {
		t.Fatalf("flagged records must leave reads: %v", ids(res.Items))
	}
}

// Gate-set queries are marked in retrievals.params so ranking never trains on them.
func TestGateQueriesMarkedInParams(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	r := New(f.e, Deps{})
	s, _ := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "database backup", Gate: true})
	c, _ := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "database backup", Gate: true})
	n, _ := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "database backup"})
	for id, want := range map[string]bool{s.RetrievalID: true, c.RetrievalID: true, n.RetrievalID: false} {
		var params string
		f.e.Store.DB().QueryRow(`SELECT params FROM retrievals WHERE id = ?`, id).Scan(&params)
		var p map[string]any
		if err := json.Unmarshal([]byte(params), &p); err != nil || p["gate"] != want {
			t.Fatalf("%s gate = %v, want %v (%s)", id, p["gate"], want, params)
		}
	}
}

// The logged candidates decode with the shape internal/rank reads: numbers
// for every stage and a features map of numbers only.
func TestLoggedCandidatesMatchRankContract(t *testing.T) {
	f := newFx(t)
	corpus(f, "ns")
	res, err := New(f.e, Deps{}).Search(f.ctx, SearchInput{Namespace: "ns", Query: "database backup"})
	if err != nil {
		t.Fatal(err)
	}
	var cands string
	f.e.Store.DB().QueryRow(`SELECT candidates FROM retrievals WHERE id = ?`, res.RetrievalID).Scan(&cands)
	type rankCandidate struct { // mirror of rank.Candidate (not imported)
		ID         string             `json:"id"`
		Type       string             `json:"type"`
		BM25       float64            `json:"bm25"`
		Cosine     float64            `json:"cosine"`
		Rerank     float64            `json:"rerank"`
		RRF        float64            `json:"rrf"`
		Utility    float64            `json:"utility"`
		Learned    float64            `json:"learned"`
		Final      float64            `json:"final"`
		Zone       string             `json:"zone"`
		AgeSeconds float64            `json:"age_seconds"`
		Features   map[string]float64 `json:"features"`
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(cands), &raw); err != nil || len(raw) == 0 {
		t.Fatalf("candidates: %v %s", err, cands)
	}
	for _, r := range raw {
		var c rankCandidate
		if err := json.Unmarshal(r, &c); err != nil || c.ID == "" || c.Type == "" || c.Zone == "" {
			t.Fatalf("element does not decode as rank.Candidate: %v %s", err, r)
		}
		var keys map[string]json.RawMessage
		json.Unmarshal(r, &keys)
		for _, k := range []string{"id", "type", "bm25", "cosine", "rerank", "rrf", "utility", "learned", "final", "zone", "age_seconds", "features"} {
			if _, ok := keys[k]; !ok {
				t.Fatalf("key %s missing: %s", k, r)
			}
		}
	}
}
