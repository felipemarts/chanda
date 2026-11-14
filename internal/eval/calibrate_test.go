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

// Decision 0121: each floor is chosen on the training split only, as the
// value the best gold record of 95% of the training questions reaches on
// that signal.
func TestCalibrateEvidenceUsesTheTrainingSplitOnly(t *testing.T) {
	ds := &Dataset{Questions: []Question{
		{ID: "q1", Question: "evaluated", Gold: []Gold{{DocID: "d1"}}},
		{ID: "g1", Question: "gate", Split: SplitGate, Gold: []Gold{{DocID: "d1"}}},
	}}
	p := &fakeProber{ev: map[string][]EvidenceCandidate{}}
	// 20 training questions: gold coverage 1.0 for 19 and 0.25 for one; gold
	// cosine 0.80 + i/100; a noise candidate at coverage 0.5, cosine 0.5.
	for i := 0; i < 20; i++ {
		text := string(rune('a' + i))
		ds.Questions = append(ds.Questions, Question{ID: "t" + text, Question: text, Split: SplitTrain, Gold: []Gold{{DocID: "d" + text}}})
		cov := 1.0
		if i == 0 {
			cov = 0.25
		}
		p.ev[text] = []EvidenceCandidate{cand("d"+text, "gold", cov, 0.80+float64(i)/100), cand("noise", "x", 0.5, 0.5)}
	}
	cal, err := CalibrateEvidence(context.Background(), p, ds, 800, CalibrateRetention)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 20 {
		t.Fatalf("asked %d questions, want only the 20 of the training split", len(p.asked))
	}
	for _, q := range p.asked {
		if q == "evaluated" || q == "gate" {
			t.Fatalf("asked %q", q)
		}
	}
	// 95% of 20 = 19 questions: coverage floor 1.0 (one question is below),
	// cosine floor 0.81 (the 19th best of 0.80..0.99).
	if cal.MinTermCoverage != 1 || cal.MinCosine < 0.8099 || cal.MinCosine > 0.8101 {
		t.Fatalf("floors %+v", cal)
	}
	if cal.Questions != 20 || cal.Kept != 19 || cal.NoisePassing != 0 || cal.NoiseTotal != 20 {
		t.Fatalf("%+v", cal)
	}
}

func TestQuantileFloor(t *testing.T) {
	if got := quantileFloor([]float64{0.9, 0.1, 0.5, 0.7}, 0.75); got != 0.5 {
		t.Fatalf("got %v", got)
	}
	if got := quantileFloor([]float64{0.3}, 0.95); got != 0.3 {
		t.Fatalf("got %v", got)
	}
}
