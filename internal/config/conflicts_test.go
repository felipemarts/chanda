package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
)

// Decision 0148: sleep.conflicts.* is off by default, zero means the
// documented default, the default hash is unchanged and every set key
// changes it.
func TestConflictsDefaultsAndHash(t *testing.T) {
	c := config.Default()
	k := c.Sleep.Conflicts
	if k.On() || k.Cosine() != 0.80 || k.PerRecord() != 3 || k.Turns() != 1 || k.Confidence() != 0.7 || k.PairCap() != 0 {
		t.Fatalf("conflicts defaults: %+v", k)
	}
	if c.Sleep.IsMaintenance() {
		t.Fatal("maintenance is not the default mode")
	}
	if got := c.Hash(); got != "4dd590544d868b15" {
		t.Fatalf("default configuration hash changed: %s", got)
	}
	for name, mut := range map[string]func(*config.SleepConfig){
		"enabled":        func(s *config.SleepConfig) { s.Conflicts.Enabled = true },
		"min_cosine":     func(s *config.SleepConfig) { s.Conflicts.MinCosine = 0.85 },
		"max_per_record": func(s *config.SleepConfig) { s.Conflicts.MaxPerRecord = 2 },
		"context_turns":  func(s *config.SleepConfig) { s.Conflicts.ContextTurns = 2 },
		"min_confidence": func(s *config.SleepConfig) { s.Conflicts.MinConfidence = 0.9 },
		"max_pairs":      func(s *config.SleepConfig) { s.Conflicts.MaxPairs = 5 },
		"maintenance":    func(s *config.SleepConfig) { s.Mode = config.SleepModeMaintenance },
	} {
		x := config.Default()
		mut(&x.Sleep)
		if x.Hash() == c.Hash() {
			t.Errorf("%s must change the hash", name)
		}
	}
	set := config.ConflictsConfig{Enabled: true, MinCosine: 0.5, MaxPerRecord: 7, ContextTurns: 2, MinConfidence: 0.9, MaxPairs: 4}
	if !set.On() || set.Cosine() != 0.5 || set.PerRecord() != 7 || set.Turns() != 2 || set.Confidence() != 0.9 || set.PairCap() != 4 {
		t.Fatalf("set values must be returned: %+v", set)
	}
}

func TestConflictsValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*config.SleepConfig)
		ok   bool
	}{
		{"defaults", func(s *config.SleepConfig) {}, true},
		{"maintenance", func(s *config.SleepConfig) { s.Mode = "maintenance" }, true},
		{"min_cosine 0.99", func(s *config.SleepConfig) { s.Conflicts.MinCosine = 0.99 }, true},
		{"min_cosine 1", func(s *config.SleepConfig) { s.Conflicts.MinCosine = 1 }, false},
		{"min_cosine negative", func(s *config.SleepConfig) { s.Conflicts.MinCosine = -0.1 }, false},
		{"min_confidence 1", func(s *config.SleepConfig) { s.Conflicts.MinConfidence = 1 }, true},
		{"min_confidence above 1", func(s *config.SleepConfig) { s.Conflicts.MinConfidence = 1.1 }, false},
		{"min_confidence negative", func(s *config.SleepConfig) { s.Conflicts.MinConfidence = -1 }, false},
		{"negative max_per_record", func(s *config.SleepConfig) { s.Conflicts.MaxPerRecord = -1 }, false},
		{"negative context_turns", func(s *config.SleepConfig) { s.Conflicts.ContextTurns = -1 }, false},
		{"negative max_pairs", func(s *config.SleepConfig) { s.Conflicts.MaxPairs = -1 }, false},
	} {
		c := config.Default()
		tc.mut(&c.Sleep)
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
	p := filepath.Join(t.TempDir(), "chanda.yaml")
	os.WriteFile(p, []byte("sleep:\n  mode: maintenance\n  conflicts:\n    enabled: true\n    min_cosine: 0.75\n    max_pairs: 9\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || !c.Sleep.IsMaintenance() || !c.Sleep.Conflicts.On() || c.Sleep.Conflicts.Cosine() != 0.75 ||
		c.Sleep.Conflicts.PairCap() != 9 || c.Sleep.Conflicts.PerRecord() != 3 {
		t.Fatalf("load: %+v %v", c.Sleep.Conflicts, err)
	}
	os.WriteFile(p, []byte("sleep:\n  conflicts:\n    nope: 1\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("an unknown sleep.conflicts key must fail to load")
	}
}
