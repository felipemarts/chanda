package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

// Decision 0142: sleep.mode and sleep.guided.* default to the old sleep,
// zero means the documented default, and the default hash is unchanged.
func TestGuidedSleepDefaultsAndHash(t *testing.T) {
	c := config.Default()
	s := c.Sleep
	if s.Mode != "" || s.SleepMode() != config.SleepModeUniform || s.IsGuided() {
		t.Fatalf("mode default: %q", s.Mode)
	}
	g := s.Guided
	if g.DigestKind() != config.DigestExtractive || g.TopKEvents() != 10 || g.DemandWeight() != 1 || g.NearMissWeight() != 2 ||
		g.FailureWeight() != 2 || g.FragmentationWeight() != 1 || g.HalfLifeHours() != 168 || g.MaxMembers() != 12 ||
		g.MergeJaccard() != 0.5 || g.GroupCap() != 0 || g.WindowTokens() != 60 {
		t.Fatalf("guided defaults: %+v", g)
	}
	if got := c.Hash(); got != "4dd590544d868b15" {
		t.Fatalf("default configuration hash changed: %s", got)
	}
	uni := config.Default()
	uni.Sleep.Mode = config.SleepModeUniform
	uni.Sleep.Guided.Digest = config.DigestExtractive
	if uni.Hash() != c.Hash() {
		t.Fatal("uniform and extractive spelled out are the defaults")
	}
	guided := config.Default()
	guided.Sleep.Mode = config.SleepModeGuided
	if guided.Hash() == c.Hash() {
		t.Fatal("sleep.mode guided must change the hash")
	}
	for _, mut := range []func(*config.GuidedConfig){
		func(g *config.GuidedConfig) { g.Digest = config.DigestLLM },
		func(g *config.GuidedConfig) { g.TopK = 5 },
		func(g *config.GuidedConfig) { g.WeightFailure = 3 },
		func(g *config.GuidedConfig) { g.LineTokens = 80 },
		func(g *config.GuidedConfig) { g.MaxGroups = 4 },
	} {
		x := config.Default()
		mut(&x.Sleep.Guided)
		if x.Hash() == c.Hash() {
			t.Errorf("a set guided key must change the hash: %+v", x.Sleep.Guided)
		}
	}
	set := config.GuidedConfig{Digest: "llm", TopK: 3, WeightDemand: 0.5, WeightNearMiss: 4, WeightFailure: 5, WeightFragmentation: 6,
		DemandHalfLifeHours: 24, GroupMaxEvents: 7, GroupMergeJaccard: 0.3, MaxGroups: 2, LineTokens: 30}
	if set.DigestKind() != "llm" || set.TopKEvents() != 3 || set.DemandWeight() != 0.5 || set.NearMissWeight() != 4 ||
		set.FailureWeight() != 5 || set.FragmentationWeight() != 6 || set.HalfLifeHours() != 24 || set.MaxMembers() != 7 ||
		set.MergeJaccard() != 0.3 || set.GroupCap() != 2 || set.WindowTokens() != 30 {
		t.Fatalf("set values must be returned: %+v", set)
	}
}

func TestGuidedSleepValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*config.SleepConfig)
		ok   bool
	}{
		{"defaults", func(s *config.SleepConfig) {}, true},
		{"uniform", func(s *config.SleepConfig) { s.Mode = "uniform" }, true},
		{"guided", func(s *config.SleepConfig) { s.Mode = "guided" }, true},
		{"unknown mode", func(s *config.SleepConfig) { s.Mode = "smart" }, false},
		{"llm digest", func(s *config.SleepConfig) { s.Guided.Digest = "llm" }, true},
		{"unknown digest", func(s *config.SleepConfig) { s.Guided.Digest = "abstractive" }, false},
		{"negative demand", func(s *config.SleepConfig) { s.Guided.WeightDemand = -1 }, false},
		{"negative near miss", func(s *config.SleepConfig) { s.Guided.WeightNearMiss = -0.1 }, false},
		{"negative failure", func(s *config.SleepConfig) { s.Guided.WeightFailure = -2 }, false},
		{"negative fragmentation", func(s *config.SleepConfig) { s.Guided.WeightFragmentation = -1 }, false},
		{"negative top_k", func(s *config.SleepConfig) { s.Guided.TopK = -1 }, false},
		{"jaccard 1", func(s *config.SleepConfig) { s.Guided.GroupMergeJaccard = 1 }, true},
		{"jaccard above 1", func(s *config.SleepConfig) { s.Guided.GroupMergeJaccard = 1.5 }, false},
		{"jaccard below 0", func(s *config.SleepConfig) { s.Guided.GroupMergeJaccard = -0.1 }, false},
		{"negative half-life", func(s *config.SleepConfig) { s.Guided.DemandHalfLifeHours = -1 }, false},
		{"negative line_tokens", func(s *config.SleepConfig) { s.Guided.LineTokens = -1 }, false},
		{"negative group_max_events", func(s *config.SleepConfig) { s.Guided.GroupMaxEvents = -1 }, false},
		{"negative max_groups", func(s *config.SleepConfig) { s.Guided.MaxGroups = -1 }, false},
	} {
		c := config.Default()
		tc.mut(&c.Sleep)
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "chanda.yaml")
	os.WriteFile(p, []byte("sleep:\n  mode: guided\n  guided:\n    digest: llm\n    top_k: 5\n    weight_failure: 3\n    max_groups: 4\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || !c.Sleep.IsGuided() || c.Sleep.Guided.DigestKind() != "llm" || c.Sleep.Guided.TopKEvents() != 5 ||
		c.Sleep.Guided.FailureWeight() != 3 || c.Sleep.Guided.GroupCap() != 4 || c.Sleep.Guided.NearMissWeight() != 2 {
		t.Fatalf("load: %+v %v", c.Sleep, err)
	}
	os.WriteFile(p, []byte("sleep:\n  mode: eager\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("an unknown sleep.mode must fail to load")
	}
	os.WriteFile(p, []byte("sleep:\n  guided:\n    nope: 1\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("an unknown sleep.guided key must fail to load")
	}
}
