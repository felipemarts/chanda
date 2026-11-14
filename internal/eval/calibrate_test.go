package eval

import (
	"context"
	"testing"
)

// fakeProber answers Evidence from a table keyed by question text.
type fakeProber struct {
	NoMemory
	ev    map[string][]EvidenceCandidate
	asked []string
}

func (f *fakeProber) Evidence(_ context.Context, q Query, _ int) ([]EvidenceCandidate, error) {
	f.asked = append(f.asked, q.Text)
	return f.ev[q.Text], nil
}

func cand(doc, text string, cov, cos float64) EvidenceCandidate {
	return EvidenceCandidate{Retrieved: Retrieved{ID: doc + text, DocID: doc, Text: text}, Coverage: cov, Cosine: cos, HasCosine: cos > 0}
}

// Decision 0124: the floors are chosen on the training split only, with a
// criterion that weighs both errors of the cut: an answerable question
// whose gold record is cut, and an unanswerable question that is filled.
// Semantic evidence passes on its own (the paraphrases have no coverage).
func TestCalibrateEvidenceWeighsBothErrorsOnTheTrainingSplit(t *testing.T) {
	ds := &Dataset{Questions: []Question{
		{ID: "q1", Question: "evaluated", Gold: []Gold{{DocID: "d1"}}},
		{ID: "g1", Question: "gate", Split: SplitGate, Gold: []Gold{{DocID: "d1"}}},
	}}
	p := &fakeProber{ev: map[string][]EvidenceCandidate{}}
	add := func(text string, tags []string, gold bool, cs ...EvidenceCandidate) {
		q := Question{ID: "t" + text, Question: text, Split: SplitTrain, Tags: tags}
		if gold {
			q.Gold = []Gold{{DocID: "d" + text}}
		}
		ds.Questions = append(ds.Questions, q)
		p.ev[text] = cs
	}
	for _, x := range []string{"a", "b", "c", "d"} { // plain: lexical and semantic evidence
		add(x, nil, true, cand("d"+x, "gold", 1, 0.9), cand("noise", "x", 0.5, 0.8))
	}
	add("e", []string{TagParaphrase}, true, cand("de", "gold", 0, 0.82), cand("noise", "x", 0.5, 0.8))
	add("f", []string{TagParaphrase}, true, cand("df", "gold", 0, 0.84), cand("noise", "x", 0.25, 0.8))
	add("g", []string{TagAbstention}, false, cand("near", "miss", 0.5, 0.78))
	add("h", []string{TagAbstention}, false, cand("near", "miss", 0.25, 0.79))
	cal, err := CalibrateEvidence(context.Background(), p, ds, 800)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 8 {
		t.Fatalf("asked %d questions, want only the 8 of the training split", len(p.asked))
	}
	for _, q := range p.asked {
		if q == "evaluated" || q == "gate" {
			t.Fatalf("asked %q", q)
		}
	}
	// Coverage 0.5 would fill "g"; cosine must stay above 0.79 (h) and at or
	// below 0.82 (e): of the optimal cosine floors 0.795 and 0.81, 0.81 lets
	// no noise (0.8) through.
	if cal.MinTermCoverage != 1 || cal.MinCosine != 0.81 {
		t.Fatalf("floors %v / %v", cal.MinTermCoverage, cal.MinCosine)
	}
	if cal.Cost != 0 || cal.Kept != 6 || cal.Filled != 0 || cal.Unanswerable != 2 || cal.ByTag[TagParaphrase].Kept != 2 {
		t.Fatalf("%+v", cal)
	}
	if cal.NoisePassing != 0 || cal.NoiseTotal != 6 {
		t.Fatalf("noise %d of %d", cal.NoisePassing, cal.NoiseTotal)
	}
}

// Decision 0125: a similarity threshold keeps its value when no negative
// pair reaches it and at most 5% of the positive pairs miss it; otherwise
// the middle of the acceptable interval; with none, just above the
// negatives.
func TestChooseThreshold(t *testing.T) {
	pos := make([]float64, 20)
	for i := range pos {
		pos[i] = 0.96 + float64(i)/1000 // 0.960 .. 0.979
	}
	if r := chooseThreshold("k", pos, []float64{0.5, 0.9}, 0.95, true); r.Rule != "kept" || r.Chosen != 0.95 || r.Missed != 0 {
		t.Fatalf("kept: %+v", r)
	}
	// e5-like: positives 0.90..0.919, negatives up to 0.85; 0.95 misses all.
	for i := range pos {
		pos[i] = 0.90 + float64(i)/1000
	}
	r := chooseThreshold("k", pos, []float64{0.85}, 0.95, true)
	if r.Rule != "middle" || r.Chosen != 0.8755 || r.Missed != 0 || r.FalseHits != 0 || r.MissedAt != 20 {
		t.Fatalf("middle: %+v", r)
	}
	// Overlap: a negative at 0.93 is above most positives.
	r = chooseThreshold("k", pos, []float64{0.93}, 0.92, false)
	if r.Rule != "above_negatives" || r.Chosen != 0.9301 || r.FalseHits != 0 || r.Missed != 20 {
		t.Fatalf("above negatives: %+v", r)
	}
}
