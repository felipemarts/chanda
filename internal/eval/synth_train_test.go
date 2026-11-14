package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Decision 0123: the hard training questions are appended from their own
// stream; the corpus, the evaluated and gate questions and the first 99
// training questions stay byte-identical to the set without them (SHA-256 of
// their canonical JSONL, as committed before the training split was
// extended).
func TestHardTrainingLeavesEvaluatedAndGateUnchanged(t *testing.T) {
	ds := GenerateSyntheticPT(SyntheticSeed)
	var kept []Question
	for _, q := range ds.Questions {
		if q.Split != SplitTrain {
			kept = append(kept, q)
		}
	}
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	if got, want := sum(jsonl(kept)), "089674cefa340b0a4e14b28261ce023fd53e5f0738abfe33e2c87108a6a56483"; got != want {
		t.Errorf("evaluated + gate questions sha256 %s, want %s", got, want)
	}
	if got, want := sum(jsonl(ds.Corpus)), "5da060066917c6adb4399e9512292d070c101b52d6e0f92434cc5505fe9ea70b"; got != want {
		t.Errorf("corpus sha256 %s, want %s", got, want)
	}
	if got, want := sum(jsonl(ds.Training()[:99])), "66e3ba1a7ab047755fdb93f76e9d4bab3aa5cfc437c75b728cf7f364436d5671"; got != want {
		t.Errorf("first 99 training questions sha256 %s, want %s", got, want)
	}
}

// Decision 0123: the training split holds paraphrased and unanswerable
// questions in about the proportions of the evaluated split, and its
// paraphrases share as little vocabulary with their evidence as the
// evaluated ones do.
func TestTrainingSplitResemblesEvaluated(t *testing.T) {
	ds := GenerateSyntheticPT(SyntheticSeed)
	share := func(qs []Question, tag string) float64 {
		n := 0
		for _, q := range qs {
			if q.HasTag(tag) {
				n++
			}
		}
		return float64(n) / float64(len(qs))
	}
	ev, tr := ds.Evaluated(), ds.Training()
	for _, tag := range []string{TagParaphrase, TagAbstention} {
		e, g := share(ev, tag), share(tr, tag)
		t.Logf("tag %s: %.3f of training questions, %.3f of evaluated", tag, g, e)
		if g < 0.75*e || g > 1.25*e {
			t.Errorf("tag %s: %.3f of training questions, evaluated %.3f", tag, g, e)
		}
	}
	// overlap: share of the question's content terms found in its gold spans.
	overlap := func(qs []Question, para bool) float64 {
		sum, n := 0.0, 0
		for _, q := range qs {
			if !q.Answerable() || q.HasTag(TagParaphrase) != para {
				continue
			}
			have := map[string]bool{}
			for _, g := range q.Gold {
				for _, w := range terms(g.Span) {
					have[w] = true
				}
			}
			ws := contentTerms(q.Question)
			hit := 0
			for _, w := range ws {
				if have[w] {
					hit++
				}
			}
			sum += float64(hit) / float64(max(1, len(ws)))
			n++
		}
		return sum / float64(n)
	}
	pe, pt, plain := overlap(ev, true), overlap(tr, true), overlap(tr, false)
	t.Logf("gold-span overlap: evaluated paraphrases %.3f, training paraphrases %.3f, other training questions %.3f", pe, pt, plain)
	if pt > pe+0.05 || pt >= plain {
		t.Errorf("training paraphrases overlap %.3f with their gold, evaluated paraphrases %.3f, plain training %.3f", pt, pe, plain)
	}
	// Every responsibility wording matches the departments table, and no
	// question text repeats another (across all splits).
	text := map[string]bool{}
	for _, q := range ds.Questions {
		text[Normalize(q.Question)] = true
	}
	used := 0
	for _, k := range trainKeys(trainRespQ) {
		if text[Normalize(trainRespQ[k])] {
			used++
		}
	}
	if used != len(trainRespQ) {
		t.Errorf("%d of %d responsibility paraphrases used (a key does not match the departments table)", used, len(trainRespQ))
	}
	if len(text) != len(ds.Questions) {
		t.Errorf("%d distinct question texts for %d questions", len(text), len(ds.Questions))
	}
}
