package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

func TestDefaultsMatchSpec(t *testing.T) {
	c := config.Default()
	checks := map[string]bool{
		"rrf k=60":            c.Read.RRFK == 60,
		"top50 kw":            c.Read.KeywordTopK == 50,
		"top50 vec":           c.Read.VectorTopK == 50,
		"rerank top 30":       c.Read.RerankTopN == 30,
		"3 per doc":           c.Read.MaxPerDocument == 3,
		"search 20":           c.Read.SearchDefaultLimit == 20,
		"context 800/1500":    c.Read.ContextDefaultTokens == 800 && c.Read.ContextMaxTokens == 1500,
		"near dup 0.95":       c.Write.NearDupThreshold == 0.95,
		"sleep idle 30":       c.Sleep.IdleMinutes == 30,
		"sleep volume 200":    c.Sleep.VolumeEvents == 200,
		"sleep 24h":           c.Sleep.IntervalHours == 24,
		"gate tol 2":          c.Sleep.GateTolerance == 2,
		"lesson 3":            c.Sleep.LessonMinEvents == 3,
		"min exposures 5":     c.Ranking.MinExposures == 5,
		"lambda max 0.3":      c.Ranking.LambdaMax == 0.3,
		"unused penalty 0.25": c.Ranking.UnusedPenalty == 0.25,
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("default %s does not match BASE.md", name)
		}
	}
}

func TestLoadOverridesAndRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chanda.yaml")
	os.WriteFile(p, []byte("read:\n  rrf_k: 10\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || c.Read.RRFK != 10 || c.Read.KeywordTopK != 50 {
		t.Fatalf("load: %+v %v", c.Read, err)
	}
	os.WriteFile(p, []byte("nope: 1\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("unknown key must fail")
	}
	if c, err := config.Load(filepath.Join(dir, "missing.yaml")); err != nil || c.Read.RRFK != 60 {
		t.Fatal("missing file must give defaults")
	}
}

func TestMaxZoneValidation(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		human bool
		ok    bool
	}{
		{"internal", false, true}, {"external", false, true}, {"user", false, false},
		{"user", true, true}, {"", false, false}, {"root", true, false},
	} {
		c := config.Default()
		c.MCP.MaxZone, c.MCP.HumanActor = tc.zone, tc.human
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("max_zone %q human %v: err %v, want ok=%v", tc.zone, tc.human, err, tc.ok)
		}
	}
	if config.Default().MCP.MaxZone != "internal" || len(config.Default().Write.IngestRoots) != 0 {
		t.Fatal("defaults: max_zone internal, no ingest roots")
	}
}
