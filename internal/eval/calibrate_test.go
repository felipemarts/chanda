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

// Decision 0121: the floors are chosen on the training split only, as the
// pair that keeps a gold record for 95% of the reachable training questions
// while letting the fewest non-gold candidates through.
func TestCalibrateEvidenceUsesTheTrainingSplitOnly(t *testing.T) {
	ds := &Dataset{Questions: []Question{
		{ID: "t1", Question: "a", Split: SplitTrain, Gold: []Gold{{DocID: "d1", Span: "gold"}}},
		{ID: "t2", Question: "b", Split: SplitTrain, Gold: []Gold{{DocID: "d2", Span: "gold"}}},
		{ID: "q1", Question: "evaluated", Gold: []Gold{{DocID: "d1"}}},
		{ID: "g1", Question: "gate", Split: SplitGate, Gold: []Gold{{DocID: "d1"}}},
	}}
	p := &fakeProber{ev: map[string][]EvidenceCandidate{
		// Gold: coverage 1 and 0.5 (cosine 0.9); noise: coverage 0.5 with low cosine, coverage 0.25.
		"a": {cand("d1", "the gold text", 1, 0.95), cand("x", "noise", 0.5, 0.6), cand("y", "noise", 0.25, 0.7)},
		"b": {cand("d2", "gold again", 0.5, 0.9), cand("z", "noise", 0.5, 0.5)},
	}}
	cal, err := CalibrateEvidence(context.Background(), p, ds, 800, CalibrateRetention)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 2 || p.asked[0] != "a" || p.asked[1] != "b" {
		t.Fatalf("asked %v, want only the training split", p.asked)
	}
	// Both questions must keep gold (95% of 2 rounds up to 2). Coverage 1
	// alone loses t2; coverage 0.5 lets 2 noise items through; coverage
	// above 0.5 with cosine 0.9 keeps both golds and lets no noise through.
	if cal.Questions != 2 || cal.Reachable != 2 || cal.Kept != 2 || cal.NoisePassing != 0 {
		t.Fatalf("%+v", cal)
	}
	if !(cal.MinTermCoverage > 0.5 && cal.MinCosine > 0.7 && cal.MinCosine <= 0.9) {
		t.Fatalf("floors %+v", cal)
	}
}
