package config_test

import (
	"math"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
)

// Decision 0145: the learned-keys knobs are off by default, zero means the
// documented default, the default hash is unchanged and each set key moves it.
func TestLearnedKeysDefaultsAndHash(t *testing.T) {
	c := config.Default()
	if c.Write.LearnedKeys || c.Read.LearnedKeys || c.Read.LearnedKeysWeight != 0 || c.Read.LearnedKeysTopK != 0 {
		t.Fatalf("learned keys must be off by default: write %v read %+v", c.Write.LearnedKeys, c.Read)
	}
	if c.Read.KeysWeight() != 1 || c.Read.KeysTopK() != 20 {
		t.Fatalf("effective defaults: weight %v top_k %d", c.Read.KeysWeight(), c.Read.KeysTopK())
	}
	if got := c.Hash(); got != "4dd590544d868b15" {
		t.Fatalf("default configuration hash changed: %s", got)
	}
	seen := map[string]bool{c.Hash(): true}
	for _, mut := range []func(*config.Config){
		func(c *config.Config) { c.Write.LearnedKeys = true },
		func(c *config.Config) { c.Read.LearnedKeys = true },
		func(c *config.Config) { c.Read.LearnedKeysWeight = 0.5 },
		func(c *config.Config) { c.Read.LearnedKeysTopK = 10 },
	} {
		x := config.Default()
		mut(x)
		h := x.Hash()
		if seen[h] {
			t.Errorf("a set learned-keys knob must change the hash: write %v read %+v", x.Write.LearnedKeys, x.Read)
		}
		seen[h] = true
		if err := x.Validate(); err != nil {
			t.Errorf("valid knob refused: %v", err)
		}
	}
	set := config.ReadConfig{LearnedKeysWeight: 2.5, LearnedKeysTopK: 7}
	if set.KeysWeight() != 2.5 || set.KeysTopK() != 7 {
		t.Fatalf("set values must be returned: %+v", set)
	}
	for _, mut := range []func(*config.Config){
		func(c *config.Config) { c.Read.LearnedKeysWeight = -0.1 },
		func(c *config.Config) { c.Read.LearnedKeysWeight = math.NaN() },
		func(c *config.Config) { c.Read.LearnedKeysTopK = -1 },
	} {
		x := config.Default()
		mut(x)
		if err := x.Validate(); err == nil {
			t.Errorf("invalid knob accepted: %+v", x.Read)
		}
	}
}
