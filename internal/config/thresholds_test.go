package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Decision 0125: the near-duplicate threshold and the vector merge
// threshold are per embedding model, looked up like the evidence floors.
// A file that does not set write.near_dup_threshold gets the value of its
// embed.model_id; one that sets it keeps it.
func TestNearDupAndMergeThresholdsPerModel(t *testing.T) {
	w := Default().Write
	w.NearDupThresholds = map[string]float64{"e5-small": 0.9}
	if got := w.NearDup("e5-small@hugot0.8.6+simd"); got != 0.9 {
		t.Errorf("NearDup(e5) = %v", got)
	}
	if got := w.NearDup("other@x"); got != w.NearDupThreshold {
		t.Errorf("NearDup(other) = %v, want the scalar %v", got, w.NearDupThreshold)
	}
	s := Default().Sleep
	s.MergeSimilarities = map[string]float64{"e5-small": 0.97}
	if got := s.MergeVectorSimilarity("e5-small@hugot0.8.6+stock"); got != 0.97 {
		t.Errorf("MergeVectorSimilarity(e5) = %v", got)
	}
	if got := s.MergeVectorSimilarity(""); got != s.MergeSimilarity {
		t.Errorf("MergeVectorSimilarity(\"\") = %v, want %v", got, s.MergeSimilarity)
	}

	dir := t.TempDir()
	load := func(body string) *Config {
		t.Helper()
		p := filepath.Join(dir, "c.yaml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	e5 := "embed:\n  provider: hugot\n  model_id: e5-small@hugot0.8.6+simd\n  dim: 384\nwrite:\n  near_dup_thresholds:\n    e5-small: 0.91\n"
	if c := load(e5); c.Write.NearDupThreshold != 0.91 {
		t.Errorf("unset near_dup_threshold with e5: %v, want 0.91", c.Write.NearDupThreshold)
	}
	if c := load(e5 + "  near_dup_threshold: 0.97\n"); c.Write.NearDupThreshold != 0.97 {
		t.Errorf("explicit near_dup_threshold: %v, want 0.97", c.Write.NearDupThreshold)
	}
	if c := load("namespace: x\n"); c.Write.NearDupThreshold != Default().Write.NearDupThreshold {
		t.Errorf("hash model: %v, want the default %v", c.Write.NearDupThreshold, Default().Write.NearDupThreshold)
	}
}

// Decision 0138: the lexical path of the near-duplicate guard is per model,
// looked up like the thresholds; a model without an entry has it off, and a
// file can turn a built-in entry off with min_cosine 0.
func TestNearDupLexicalPerModel(t *testing.T) {
	w := Default().Write
	if got := w.Lexical("e5-small@hugot0.8.6+simd"); got != (NearDupLexical{MaxAddedWords: 3, MinCosine: 0.9288}) {
		t.Errorf("Lexical(e5) = %+v", got)
	}
	if got := w.Lexical("hash-v1-256"); got != (NearDupLexical{MaxAddedWords: 3, MinCosine: 0.8693}) {
		t.Errorf("Lexical(hash) = %+v", got)
	}
	if got := w.Lexical("other@x"); got != (NearDupLexical{}) {
		t.Errorf("Lexical(other) = %+v, want off", got)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("write:\n  near_dup_lexical:\n    hash-v1-256: {min_cosine: 0}\n"), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Write.Lexical("hash-v1-256"); got.MinCosine != 0 {
		t.Errorf("file entry: %+v, want off", got)
	}
	if got := c.Write.Lexical("e5-small"); got.MinCosine != 0.9288 {
		t.Errorf("other built-in entry kept: %+v", got)
	}
	os.WriteFile(p, []byte("write:\n  near_dup_lexical:\n    m: {min_cosine: 1.5}\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("min_cosine above 1 accepted")
	}
}
