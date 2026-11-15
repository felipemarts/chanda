package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Decision 0140: -calibrate-dump writes one line per training question
// (never an evaluated or gate one) with every judged candidate, its gold
// and answer marks and the logged features, and the floors stay the same.
func TestCalibrateEvidenceDump(t *testing.T) {
	ds := &Dataset{Questions: []Question{
		{ID: "q1", Question: "evaluated", Gold: []Gold{{DocID: "d1"}}},
		{ID: "t1", Question: "who", Split: SplitTrain, Gold: []Gold{{DocID: "d1"}}, Answers: []string{"Ana"}},
		{ID: "t2", Question: "nobody", Split: SplitTrain, Tags: []string{TagAbstention}},
	}}
	g := cand("d1", "Ana cuida disso", 1, 0.9)
	g.Features = map[string]float64{"delivered": 1, "relevance": 0.7}
	g.BM25, g.HasBM25 = -3.5, true
	p := &fakeProber{ev: map[string][]EvidenceCandidate{
		"who": {g, cand("d2", "outro", 0, 0)}, "nobody": {cand("d3", "x", 0.25, 0.5)},
	}}
	var buf bytes.Buffer
	withDump, err := CalibrateEvidenceDump(context.Background(), p, ds, 800, &buf)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := CalibrateEvidence(context.Background(), &fakeProber{ev: p.ev}, ds, 800)
	if err != nil {
		t.Fatal(err)
	}
	if withDump.MinTermCoverage != plain.MinTermCoverage || withDump.MinCosine != plain.MinCosine {
		t.Fatalf("the dump changed the floors: %+v vs %+v", withDump, plain)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want the 2 training questions:\n%s", len(lines), buf.String())
	}
	var d evidenceDump
	if err := json.Unmarshal([]byte(lines[0]), &d); err != nil {
		t.Fatal(err)
	}
	if d.ID != "t1" || !d.Answerable || len(d.Candidates) != 2 {
		t.Fatalf("%+v", d)
	}
	c := d.Candidates[0]
	if !c.Gold || !c.Answer || c.Cosine == nil || *c.Cosine != 0.9 || c.BM25 == nil || c.Features["delivered"] != 1 {
		t.Fatalf("candidate %+v", c)
	}
	if o := d.Candidates[1]; o.Gold || o.Answer || o.Cosine != nil || o.BM25 != nil {
		t.Fatalf("other candidate %+v", o)
	}
	if err := json.Unmarshal([]byte(lines[1]), &d); err != nil || d.Answerable || d.ID != "t2" {
		t.Fatalf("unanswerable line %+v %v", d, err)
	}
}
