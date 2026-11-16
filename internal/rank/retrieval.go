package rank

import (
	"encoding/json"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
)

// Candidate is one element of retrievals.candidates, the JSON array the read
// path logs for every query (read step 11). This is the contract between the
// read path (writer) and this package (reader):
//
//	[{"id":"evt_...","type":"event","bm25":3.1,"cosine":0.82,"rerank":0,
//	  "rrf":0.031,"utility":0.5,"learned":0.4,"final":0.031,"zone":"internal",
//	  "age_seconds":86400,"entity_match":true,"features":{...}}, ...]
//
// Every field is optional when reading: a missing number reads as 0, a
// missing type is derived from the id prefix, a missing zone reads as
// external (the safe default), a missing entity_match falls back to
// features["entity_match"] and then to false. Unknown fields are ignored and
// a malformed element is skipped instead of failing the whole retrieval.
//
// retrievals.delivered is a JSON array of ids in delivered order.
type Candidate struct {
	ID          string             `json:"id"`
	Type        core.RecordType    `json:"type,omitempty"`
	BM25        float64            `json:"bm25"`
	Cosine      float64            `json:"cosine"`
	Rerank      float64            `json:"rerank"`
	RRF         float64            `json:"rrf"`
	Utility     float64            `json:"utility"`
	Learned     float64            `json:"learned"`
	Final       float64            `json:"final"`
	Zone        core.Zone          `json:"zone,omitempty"`
	AgeSeconds  float64            `json:"age_seconds"`
	EntityMatch *bool              `json:"entity_match,omitempty"`
	Features    map[string]float64 `json:"features,omitempty"`
}

// RecordType returns the logged type, or the type encoded in the id.
func (c Candidate) RecordType() core.RecordType {
	if c.Type != "" {
		return c.Type
	}
	return core.RecordTypeOf(c.ID)
}

// Relevance is the pre-learning relevance of the candidate: the reranker
// score when the reranker ran (non-zero), the RRF score otherwise. It is the
// baseline the learned ranking is compared against.
func (c Candidate) Relevance() float64 {
	if c.Rerank != 0 {
		return c.Rerank
	}
	return c.RRF
}

// HasEntityMatch reports the entity-match feature, false when absent.
func (c Candidate) HasEntityMatch() bool {
	if c.EntityMatch != nil {
		return *c.EntityMatch
	}
	return c.Features["entity_match"] > 0
}

// ParseCandidates decodes retrievals.candidates tolerantly (see Candidate).
// Elements without an id are dropped.
func ParseCandidates(s string) []Candidate {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &raw); err != nil {
		return nil
	}
	out := make([]Candidate, 0, len(raw))
	for _, r := range raw {
		var c Candidate
		if err := json.Unmarshal(r, &c); err != nil || c.ID == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ParseIDs decodes a JSON array of ids (retrievals.delivered, outcomes.used_ids)
// tolerantly: non-string and empty elements are skipped, duplicates keep their
// first position.
func ParseIDs(s string) []string {
	var raw []any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &raw); err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		id, ok := v.(string)
		if !ok || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
