package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Settings at the terminal (decision 0154): chanda init writes a commented
// chanda.yaml with the main switches spelled out, chanda config shows them
// and chanda config set changes one key of the file, comments kept, and
// refuses a result that does not load.

// feature is one main switch: a short name for a boolean key.
type feature struct {
	Name, Key, About string
	on               func(*config.Config) bool
	needsLLM         bool
}

var features = []feature{
	{"agentic-search", "read.agentic_search", "LLM search terms before each context and search",
		func(c *config.Config) bool { return c.Read.Agentic() }, true},
	{"sleep", "sleep.scheduler", "consolidation on its own schedule (phase 2 needs llm.endpoint)",
		func(c *config.Config) bool { return c.Sleep.Scheduler }, false},
	{"learning", "ranking.enabled", "learned ranking from outcomes (self-improvement)",
		func(c *config.Config) bool { return c.Ranking.Enabled }, false},
	{"agent-labels", "sources.agent_labels", "agents may name the sources they write from",
		func(c *config.Config) bool { return c.Sources.AgentLabelsOn() }, false},
	{"conflicts", "sleep.conflicts.enabled", "outdated-fact detection and verdicts in sleep (needs llm.endpoint)",
		func(c *config.Config) bool { return c.Sleep.Conflicts.On() }, true},
}

func featureNamed(name string) (feature, bool) {
	for _, f := range features {
		if f.Name == name {
			return f, true
		}
	}
	return feature{}, false
}

const initTemplate = `# chanda.yaml, written by "chanda init". Every key is optional; a missing key
# keeps its default (docs/usage.md lists them).
#
# Main switches, changed with "chanda config set <name> on|off":
#   agentic-search  read.agentic_search      LLM search terms before each read (needs llm.endpoint)
#   sleep           sleep.scheduler          consolidation on its own schedule
#   learning        ranking.enabled          learned ranking from outcomes (self-improvement)
#   agent-labels    sources.agent_labels     agents may name the sources they write from
#   conflicts       sleep.conflicts.enabled  outdated-fact detection in sleep (needs llm.endpoint)
# "chanda config" shows what is on. Any other key: chanda config set <dotted.key> <value>.

db_path: chanda.db
namespace: default

read:
  agentic_search: true

sleep:
  scheduler: true

ranking:
  enabled: true

sources:
  agent_labels: true
  # labels:                          # URI prefix -> label; wins over an agent's label
  #   "https://wiki.example.com/": "Wiki"

mcp:
  profiles: [read, standard]         # add admin only for a client a person drives
  actor: agent
  max_zone: internal
  # read_namespaces: [default]       # namespaces this agent may read; empty = any
  # write_namespaces: [default]      # namespaces this agent may write; empty = any

# An OpenAI-compatible LLM turns on sleep phase 2 and agentic search:
# llm:
#   endpoint: http://localhost:11434/v1
#   model: qwen2.5:7b
#   api_key_env: OPENAI_API_KEY
`

// cmdInit writes the commented template; it never overwrites without --force.
func (cl CLI) cmdInit(args []string, stdout, stderr io.Writer) int {
	var c common
	var force bool
	fs := newFlags("init", stderr, &c)
	fs.BoolVar(&force, "force", false, "overwrite an existing file")
	if code := parse(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: chanda init [--config chanda.yaml] [--force]")
		return 2
	}
	if _, err := os.Stat(c.config); err == nil && !force {
		return emit(stdout, stderr, nil, chanda.Errf(chanda.CodeConflict, "%s exists; pass --force to overwrite it", c.config))
	}
	if err := writeAtomic(c.config, []byte(initTemplate)); err != nil {
		return emit(stdout, stderr, nil, chanda.Errf(chanda.CodeInternal, "write %s: %v", c.config, err))
	}
	return cl.showConfig(c.config, stdout, stderr)
}

// settingOutput is one main switch as chanda config shows it. Active is
// false when the switch is on but needs an LLM that is not configured.
type settingOutput struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	On     bool   `json:"on"`
	Active bool   `json:"active"`
	About  string `json:"about"`
}

type configOutput struct {
	chanda.Meta
	Config     string          `json:"config"`
	Exists     bool            `json:"exists"`
	LLM        bool            `json:"llm"`
	Settings   []settingOutput `json:"settings"`
	Namespace  string          `json:"namespace"`
	ReadNS     []string        `json:"read_namespaces"`
	WriteNS    []string        `json:"write_namespaces"`
	Profiles   []string        `json:"profiles"`
	Actor      string          `json:"actor"`
	SetCommand string          `json:"set_command"`
}

// cmdConfig is chanda config [show] and chanda config set <name|key> <value>.
func (cl CLI) cmdConfig(args []string, stdout, stderr io.Writer) int {
	usage := "usage: chanda config [show] [--config f]\n       chanda config set <name|dotted.key> <value> [--config f]"
	sub := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	var c common
	fs := newFlags("config", stderr, &c)
	// Flags may follow the arguments: "config set sleep off --config f".
	var pos []string
	for {
		if code := parse(fs, args); code >= 0 {
			return code
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	switch {
	case sub == "show" && len(pos) == 0:
		return cl.showConfig(c.config, stdout, stderr)
	case sub == "set" && len(pos) == 2:
		key, value, err := settingKey(pos[0], pos[1])
		if err == nil {
			err = setKey(c.config, key, value)
		}
		if err != nil {
			return emit(stdout, stderr, nil, err)
		}
		return cl.showConfig(c.config, stdout, stderr)
	}
	fmt.Fprintln(stderr, usage)
	return 2
}

func (cl CLI) showConfig(path string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, "chanda:", err)
		return 2
	}
	_, statErr := os.Stat(path)
	out := configOutput{
		Config: path, Exists: statErr == nil, LLM: strings.TrimSpace(cfg.LLM.Endpoint) != "",
		Namespace: cfg.Namespace, ReadNS: strsOrEmpty(cfg.MCP.ReadNamespaces), WriteNS: strsOrEmpty(cfg.MCP.WriteNamespaces),
		Profiles: strsOrEmpty(cfg.MCP.Profiles), Actor: cfg.MCP.Actor,
		SetCommand: "chanda config set <name> on|off --config " + path,
	}
	for _, f := range features {
		on := f.on(cfg)
		out.Settings = append(out.Settings, settingOutput{Name: f.Name, Key: f.Key, On: on, Active: on && (!f.needsLLM || out.LLM), About: f.About})
	}
	return emit(stdout, stderr, out, nil)
}

func strsOrEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// settingKey maps a switch name and on/off to its key and YAML value; a
// dotted key passes through with its value as written.
func settingKey(name, value string) (string, string, error) {
	f, ok := featureNamed(name)
	if !ok {
		if !strings.Contains(name, ".") {
			return "", "", chanda.Errf(chanda.CodeInvalidArgument,
				"unknown setting %q: use one of agentic-search, sleep, learning, agent-labels, conflicts, or a dotted key such as read.agentic_search", name)
		}
		return name, value, nil
	}
	switch strings.ToLower(value) {
	case "on", "true", "yes", "1":
		return f.Key, "true", nil
	case "off", "false", "no", "0":
		return f.Key, "false", nil
	}
	return "", "", chanda.Errf(chanda.CodeInvalidArgument, "%s takes on or off, not %q", name, value)
}

// setKey sets the dotted key to the YAML value in the file at path (created
// when missing), keeping the rest of the file and its comments. The result
// must load (known keys, valid values) before it replaces the file.
func setKey(path, key, value string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return chanda.Errf(chanda.CodeInternal, "read %s: %v", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return chanda.Errf(chanda.CodeInvalidArgument, "%s is not YAML: %v", path, err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return chanda.Errf(chanda.CodeInvalidArgument, "%s: the top level is not a mapping", path)
	}
	var val yaml.Node
	if err := yaml.Unmarshal([]byte(value), &val); err != nil || len(val.Content) != 1 {
		return chanda.Errf(chanda.CodeInvalidArgument, "value %q is not a YAML value", value)
	}
	parts := strings.Split(key, ".")
	node := root
	for i, p := range parts {
		if p == "" {
			return chanda.Errf(chanda.CodeInvalidArgument, "bad key %q", key)
		}
		child := mapValue(node, p)
		if i == len(parts)-1 {
			if child == nil {
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p}, val.Content[0])
			} else {
				*child = *val.Content[0]
			}
			break
		}
		if child == nil || child.Kind != yaml.MappingNode {
			m := &yaml.Node{Kind: yaml.MappingNode}
			if child == nil {
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p}, m)
			} else {
				*child = *m // a null or scalar section becomes a mapping
			}
			child = mapValue(node, p)
		}
		node = child
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return chanda.Errf(chanda.CodeInternal, "encode: %v", err)
	}
	enc.Close()
	tmp, err := os.CreateTemp(filepath.Dir(absOr(path)), ".chanda-config-*.yaml")
	if err != nil {
		return chanda.Errf(chanda.CodeInternal, "temp file: %v", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := os.WriteFile(tmp.Name(), buf.Bytes(), 0o600); err != nil {
		return chanda.Errf(chanda.CodeInternal, "write: %v", err)
	}
	if _, err := config.Load(tmp.Name()); err != nil {
		return chanda.Errf(chanda.CodeInvalidArgument, "%s = %s: %v", key, value, err)
	}
	if err := writeAtomic(path, buf.Bytes()); err != nil {
		return chanda.Errf(chanda.CodeInternal, "write %s: %v", path, err)
	}
	return nil
}

// mapValue returns the value node of key in mapping m, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// writeAtomic replaces path with b through a temp file in the same folder.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(absOr(path))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".chanda-*.yaml")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
