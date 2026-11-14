package config

import "testing"

// Decision 0121: the evidence floors of the context cut are per embedding
// model, by model id or family, with the global floors as the fallback.
func TestEvidenceFloorsPerModel(t *testing.T) {
	r := Default().Read
	r.EvidenceFloors = map[string]EvidenceFloor{
		"hash-v1-256": {MinTermCoverage: 0.5, MinCosine: 1.01},
		"e5-small":    {MinTermCoverage: 0.75, MinCosine: 0.9},
	}
	for _, tc := range []struct {
		model string
		want  EvidenceFloor
	}{
		{"hash-v1-256", EvidenceFloor{0.5, 1.01}},
		{"e5-small@hugot0.8.6+simd", EvidenceFloor{0.75, 0.9}},
		{"e5-small", EvidenceFloor{0.75, 0.9}},
		{"other@x", EvidenceFloor{r.MinTermCoverage, r.MinCosine}},
		{"", EvidenceFloor{r.MinTermCoverage, r.MinCosine}},
	} {
		if got := r.Floors(tc.model); got != tc.want {
			t.Errorf("Floors(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}
}
