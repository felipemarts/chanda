package config_test

import (
	"os"
	"path/filepath"
	"strings"
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

// Decision 0153: sources.* keeps the default hash, a configured label must
// already be clean, and the longest matching prefix wins.
func TestSourceLabelsConfig(t *testing.T) {
	c := config.Default()
	if !c.Sources.AgentLabelsOn() || c.Hash() != "4dd590544d868b15" {
		t.Fatalf("defaults: %+v hash %s", c.Sources, c.Hash())
	}
	on := true
	c.Sources.AgentLabels = &on
	if c.Hash() != "4dd590544d868b15" {
		t.Fatal("agent_labels: true is the default and must keep the hash")
	}
	c.Sources.Labels = map[string]string{"https://wiki/": "Wiki", "https://wiki/eng/": "Wiki Eng"}
	if err := c.Validate(); err != nil || c.Hash() == "4dd590544d868b15" {
		t.Fatalf("labels: %v", err)
	}
	if c.Sources.Label("https://wiki/eng/a") != "Wiki Eng" || c.Sources.Label("https://wiki/x") != "Wiki" || c.Sources.Label("other") != "" {
		t.Fatal("longest prefix must win")
	}
	for _, bad := range []map[string]string{{"": "x"}, {"p": ""}, {"p": "a|b"}, {"p": "two\nlines"}, {"p": strings.Repeat("x", 61)}} {
		c.Sources.Labels = bad
		if c.Validate() == nil {
			t.Errorf("sources.labels %q must be refused", bad)
		}
	}
	if got := config.CleanLabel("  Blog [da]\tAcme | <b> "); got != "Blog da Acme b" {
		t.Fatalf("CleanLabel: %q", got)
	}
}
