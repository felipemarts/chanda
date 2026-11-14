package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

// Decision 0094: the header extractor is on by default and "extractors: []"
// turns every deterministic extractor off.
func TestDefaultExtractorsAndOptOut(t *testing.T) {
	if ex := config.Default().Write.Extractors; len(ex) != 1 || ex[0].Kind != "header" {
		t.Fatalf("default extractors %+v", ex)
	}
	p := filepath.Join(t.TempDir(), "chanda.yaml")
	os.WriteFile(p, []byte("write:\n  extractors: []\n"), 0o644)
	c, err := config.Load(p)
	if err != nil || len(c.Write.Extractors) != 0 {
		t.Fatalf("extractors: [] must disable them: %+v %v", c.Write.Extractors, err)
	}
	os.WriteFile(p, []byte("write:\n  min_chars: 3\n"), 0o644)
	if c, err := config.Load(p); err != nil || len(c.Write.Extractors) != 1 {
		t.Fatalf("an unrelated key keeps the default: %+v %v", c.Write.Extractors, err)
	}
}
