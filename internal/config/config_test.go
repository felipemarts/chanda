package config_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
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
		"near dup 0.95":       config.DefaultNearDupThreshold == 0.95, // models without a calibration (decision 0125)
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

func TestHashIgnoresFileLocations(t *testing.T) {
	a, b := config.Default(), config.Default()
	b.DBPath, b.Sleep.SnapshotDir, b.Write.IngestRoots, b.MCP.HTTPAddr = "/tmp/x/chanda.db", "/tmp/snap", []string{"/data"}, ":8080"
	if a.Hash() != b.Hash() {
		t.Fatal("file locations must not change the configuration hash")
	}
	b.Read.RRFK = 61
	if a.Hash() == b.Hash() {
		t.Fatal("a behavioural setting must change the hash")
	}
	if b.DBPath != "/tmp/x/chanda.db" {
		t.Fatal("Hash must not modify the configuration")
	}
}

// Decision 0137: the context selection knobs default to the behaviour before
// them, and a configuration that leaves them unset hashes as before (the
// hash of committed evaluation reports, eval/reports/latest.json; it moved
// from 84f1f46535aa2347 to 3cc17014d76d162e with the lexical near-duplicate
// default of decision 0138, not with these knobs, then to 4dd590544d868b15
// with the selection defaults of decision 0141).
func TestContextKnobsDefaultsAndHash(t *testing.T) {
	c := config.Default()
	// Decision 0141 set the item cap defaults; the other knobs stay off.
	if c.Read.ContextItemMaxTokens != 150 || c.Read.ContextTruncate != config.TruncateBestWindow || c.Read.ContextItemCapAfter != 2 ||
		c.Read.ContextMaxPerDocument != 0 || c.Read.ContextMaxPerSession != 0 || c.Read.ContextShowRole || !c.Read.BestWindow() {
		t.Fatalf("defaults: %+v", c.Read)
	}
	if got := c.Hash(); got != "4dd590544d868b15" {
		t.Fatalf("default configuration hash changed: %s", got)
	}
	head, empty := config.Default(), config.Default()
	head.Read.ContextTruncate, empty.Read.ContextTruncate = config.TruncateHead, ""
	if head.Hash() != empty.Hash() || head.Hash() == c.Hash() {
		t.Fatal("head and empty are the same mode, and differ from the best_window default")
	}
	for _, mut := range []func(*config.Config){
		func(c *config.Config) { c.Read.ContextItemMaxTokens = 120 },
		func(c *config.Config) { c.Read.ContextTruncate = config.TruncateHead },
		func(c *config.Config) { c.Read.ContextItemCapAfter = 0 },
		func(c *config.Config) { c.Read.ContextMaxPerDocument = 5 },
		func(c *config.Config) { c.Read.ContextMaxPerSession = 2 },
		func(c *config.Config) { c.Read.ContextShowRole = true },
		func(c *config.Config) { c.Read.ContextDerivedMaxShare = 0.5 },
		func(c *config.Config) { c.Read.ContextProtectRaw = 3 },
		func(c *config.Config) { c.Read.ContextProtectRaw = -1 },
	} {
		x := config.Default()
		mut(x)
		if x.Hash() == c.Hash() {
			t.Errorf("a set knob must change the hash: %+v", x.Read)
		}
	}
	if c.Read.ContextPerDocument() != 3 {
		t.Fatal("0 inherits read.max_per_document")
	}
	c.Read.ContextMaxPerDocument = 7
	if c.Read.ContextPerDocument() != 7 {
		t.Fatal("a positive value overrides")
	}
	c.Read.ContextMaxPerDocument = -1
	if c.Read.ContextPerDocument() != 0 {
		t.Fatal("-1 means no limit")
	}
}

func TestContextKnobsValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*config.ReadConfig)
		ok   bool
	}{
		{"defaults", func(r *config.ReadConfig) {}, true},
		{"head", func(r *config.ReadConfig) { r.ContextTruncate = "head" }, true},
		{"best_window", func(r *config.ReadConfig) { r.ContextTruncate = "best_window" }, true},
		{"unknown truncate", func(r *config.ReadConfig) { r.ContextTruncate = "tail" }, false},
		{"cap 120", func(r *config.ReadConfig) { r.ContextItemMaxTokens = 120 }, true},
		{"cap = min", func(r *config.ReadConfig) { r.ContextItemMaxTokens = r.ContextMinItemTokens }, true},
		{"cap below min", func(r *config.ReadConfig) { r.ContextItemMaxTokens = r.ContextMinItemTokens - 1 }, false},
		{"negative cap", func(r *config.ReadConfig) { r.ContextItemMaxTokens = -1 }, false},
		{"per doc -1", func(r *config.ReadConfig) { r.ContextMaxPerDocument = -1 }, true},
		{"per doc -2", func(r *config.ReadConfig) { r.ContextMaxPerDocument = -2 }, false},
		{"per session 2", func(r *config.ReadConfig) { r.ContextMaxPerSession = 2 }, true},
		{"per session -1", func(r *config.ReadConfig) { r.ContextMaxPerSession = -1 }, false},
		{"share 0.4", func(r *config.ReadConfig) { r.ContextDerivedMaxShare = 0.4 }, true},
		{"share 1", func(r *config.ReadConfig) { r.ContextDerivedMaxShare = 1 }, true},
		{"share above 1", func(r *config.ReadConfig) { r.ContextDerivedMaxShare = 1.01 }, false},
		{"negative share", func(r *config.ReadConfig) { r.ContextDerivedMaxShare = -0.1 }, false},
		{"NaN share", func(r *config.ReadConfig) { r.ContextDerivedMaxShare = math.NaN() }, false},
		{"protect 5", func(r *config.ReadConfig) { r.ContextProtectRaw = 5 }, true},
		{"protect -1", func(r *config.ReadConfig) { r.ContextProtectRaw = -1 }, true},
		{"protect -2", func(r *config.ReadConfig) { r.ContextProtectRaw = -2 }, false},
	} {
		c := config.Default()
		tc.mut(&c.Read)
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "chanda.yaml")
	os.WriteFile(p, []byte("read:\n  context_item_max_tokens: 150\n  context_truncate: best_window\n"+
		"  context_max_per_document: 4\n  context_max_per_session: 2\n  context_show_role: true\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || c.Read.ContextItemMaxTokens != 150 || !c.Read.BestWindow() || c.Read.ContextMaxPerDocument != 4 ||
		c.Read.ContextMaxPerSession != 2 || !c.Read.ContextShowRole {
		t.Fatalf("load: %+v %v", c.Read, err)
	}
	os.WriteFile(p, []byte("read:\n  context_truncate: middle\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("an unknown context_truncate must fail to load")
	}
}

// Decision 0143: the derived share is off by default; context_protect_raw
// is 2 when the share is on and the key is 0, none with -1, and unused
// with the share off.
func TestDerivedShareKeys(t *testing.T) {
	r := config.Default().Read
	if r.ContextDerivedMaxShare != 0 || r.ContextProtectRaw != 0 || r.DerivedShareOn() || r.ProtectRaw() != 0 || r.DerivedShareTokens(800) != 800 {
		t.Fatalf("defaults: %+v", r)
	}
	r.ContextProtectRaw = 3
	if r.ProtectRaw() != 0 {
		t.Fatal("protect_raw is unused with the share off")
	}
	r.ContextDerivedMaxShare = 0.29
	for _, c := range []struct{ set, want int }{{0, config.DefaultContextProtectRaw}, {-1, 0}, {1, 1}, {5, 5}} {
		r.ContextProtectRaw = c.set
		if got := r.ProtectRaw(); got != c.want {
			t.Errorf("protect_raw %d: %d, want %d", c.set, got, c.want)
		}
	}
	if config.DefaultContextProtectRaw != 2 {
		t.Fatal("default protect_raw is 2")
	}
	for _, c := range []struct{ budget, want int }{{100, 29}, {800, 232}, {0, 0}, {3, 0}} {
		if got := r.DerivedShareTokens(c.budget); got != c.want {
			t.Errorf("floor(0.29 × %d) = %d, want %d", c.budget, got, c.want)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "chanda.yaml")
	os.WriteFile(p, []byte("read:\n  context_derived_max_share: 0.3\n  context_protect_raw: -1\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || c.Read.ContextDerivedMaxShare != 0.3 || c.Read.ContextProtectRaw != -1 || c.Read.ProtectRaw() != 0 {
		t.Fatalf("load: %+v %v", c.Read, err)
	}
	os.WriteFile(p, []byte("read:\n  context_derived_max_share: 1.5\n"), 0o644)
	if _, err := config.Load(p); err == nil {
		t.Fatal("a share above 1 must fail to load")
	}
}
