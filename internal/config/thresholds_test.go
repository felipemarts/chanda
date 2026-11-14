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
