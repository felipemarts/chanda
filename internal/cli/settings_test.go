package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
)

type shown struct {
	Exists   bool            `json:"exists"`
	LLM      bool            `json:"llm"`
	Settings []settingOutput `json:"settings"`
	ReadNS   []string        `json:"read_namespaces"`
}

func decodeShown(t *testing.T, out string) map[string]settingOutput {
	t.Helper()
	env := decodeEnv(t, out)
	b, _ := json.Marshal(env.Data)
	var s shown
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	m := map[string]settingOutput{}
	for _, x := range s.Settings {
		m[x.Name] = x
	}
	return m
}

// Decision 0154: init writes a file equal to the defaults with the switches
// spelled out; config shows them; config set changes one key, keeps the
// comments and refuses a file that would not load.
func TestInitAndConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chanda.yaml")
	code, out, errs := runCLI("init", "--config", path)
	if code != 0 {
		t.Fatalf("init: %d %s %s", code, out, errs)
	}
	s := decodeShown(t, out)
	for _, name := range []string{"agentic-search", "sleep", "learning", "agent-labels"} {
		if !s[name].On {
			t.Errorf("%s must be on after init", name)
		}
	}
	if s["agentic-search"].Active || s["conflicts"].On {
		t.Fatalf("agentic search needs an LLM; conflicts stays off: %+v", s)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Hash() != config.Default().Hash() {
		t.Fatalf("the init file must equal the defaults: %v", err)
	}
	if code, _, _ := runCLI("init", "--config", path); code != 1 {
		t.Fatal("init must not overwrite without --force")
	}

	for _, set := range [][]string{
		{"agentic-search", "off"}, {"sleep", "off"}, {"learning", "off"}, {"agent-labels", "no"},
		{"llm.endpoint", "http://localhost:11434/v1"}, {"mcp.read_namespaces", "[default, company]"},
	} {
		if code, out, errs := runCLI(append([]string{"config", "set"}, append(set, "--config", path)...)...); code != 0 {
			t.Fatalf("config set %v: %d %s %s", set, code, out, errs)
		}
	}
	code, out, _ = runCLI("config", "--config", path)
	s = decodeShown(t, out)
	if code != 0 || s["agentic-search"].On || s["sleep"].On || s["learning"].On || s["agent-labels"].On {
		t.Fatalf("after set: %+v", s)
	}
	cfg, err = config.Load(path)
	if err != nil || cfg.LLM.Endpoint != "http://localhost:11434/v1" || len(cfg.MCP.ReadNamespaces) != 2 || cfg.LLM.TimeoutSeconds != 120 {
		t.Fatalf("dotted keys: %+v %v", cfg.LLM, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# Main switches") || !strings.Contains(string(b), "agentic_search: false") {
		t.Fatalf("comments or value lost:\n%s", b)
	}

	before, _ := os.ReadFile(path)
	for _, bad := range [][]string{
		{"read.no_such_key", "1"},           // unknown key
		{"mcp.max_zone", "everywhere"},      // invalid value
		{"mcp.write_namespaces", "[other]"}, // misses the default namespace
		{"sleep", "maybe"},
		{"bogus", "on"},
	} {
		if code, _, _ := runCLI(append([]string{"config", "set"}, append(bad, "--config", path)...)...); code != 1 {
			t.Errorf("config set %v must fail, got %d", bad, code)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a refused set must leave the file unchanged")
	}

	missing := filepath.Join(t.TempDir(), "new.yaml")
	if code, _, errs := runCLI("config", "set", "sleep", "off", "--config", missing); code != 0 {
		t.Fatalf("set on a missing file: %s", errs)
	}
	if cfg, err := config.Load(missing); err != nil || cfg.Sleep.Scheduler {
		t.Fatalf("new file: %+v %v", cfg.Sleep, err)
	}
}
