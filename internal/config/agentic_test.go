package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
)

// Decision 0151: read.agentic_search is on by default, spelling the default
// out keeps the hash, and turning it off or setting the timeout changes it.
func TestAgenticSearchDefaultsAndHash(t *testing.T) {
	c := config.Default()
	if !c.Read.Agentic() || c.Read.AgenticTimeout() != config.DefaultAgenticTimeoutMS {
		t.Fatalf("agentic defaults: %+v", c.Read)
	}
	if got := c.Hash(); got != "4dd590544d868b15" {
		t.Fatalf("default configuration hash changed: %s", got)
	}
	on, off := true, false
	x := config.Default()
	x.Read.AgenticSearch = &on
	if x.Hash() != c.Hash() {
		t.Fatal("agentic_search: true is the default and must keep the hash")
	}
	x.Read.AgenticSearch = &off
	if x.Read.Agentic() || x.Hash() == c.Hash() {
		t.Fatal("agentic_search: false must turn it off and change the hash")
	}
	y := config.Default()
	y.Read.AgenticTimeoutMS = 2000
	if y.Hash() == c.Hash() || y.Read.AgenticTimeout() != 2000 {
		t.Fatal("agentic_timeout_ms must take effect and change the hash")
	}
	y.Read.AgenticTimeoutMS = -1
	if y.Validate() == nil {
		t.Fatal("a negative agentic_timeout_ms must be refused")
	}

	p := filepath.Join(t.TempDir(), "chanda.yaml")
	os.WriteFile(p, []byte("read:\n  agentic_search: false\n"), 0o644)
	l, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if l.Read.Agentic() {
		t.Fatal("agentic_search: false in the file must turn it off")
	}
}
