package lme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

// Override sets one Chanda configuration key in the per-question
// chanda.yaml ("lme contexts -chanda-set key=value"). Key is the dotted YAML
// path ("read.max_per_document"); Value is parsed as a YAML scalar or flow
// collection ("5", "true", "0.4", "[a, b]").
type Override struct {
	Key   string
	Value any
	Raw   string // the flag argument, key=value, as given
}

// harnessKeys are owned by the harness: overriding them would break the
// per-question isolation.
var harnessKeys = map[string]bool{"db_path": true, "sleep.snapshot_dir": true}

// ParseOverride parses "key=value".
func ParseOverride(s string) (Override, error) {
	k, v, ok := strings.Cut(s, "=")
	k = strings.TrimSpace(k)
	if !ok || k == "" || strings.TrimSpace(v) == "" {
		return Override{}, fmt.Errorf("-chanda-set %q: want key=value", s)
	}
	for _, p := range strings.Split(k, ".") {
		if p == "" {
			return Override{}, fmt.Errorf("-chanda-set %q: empty key segment", s)
		}
	}
	if harnessKeys[k] {
		return Override{}, fmt.Errorf("-chanda-set %q: %s is set by the harness", s, k)
	}
	var val any
	if err := yaml.Unmarshal([]byte(v), &val); err != nil {
		return Override{}, fmt.Errorf("-chanda-set %q: value: %w", s, err)
	}
	if val == nil {
		return Override{}, fmt.Errorf("-chanda-set %q: null value", s)
	}
	return Override{Key: k, Value: val, Raw: k + "=" + strings.TrimSpace(v)}, nil
}

// ApplyOverrides sets each override in a chanda.yaml text and returns the
// new text. Later overrides of the same key win. It does not check that the
// keys exist; CheckChandaConfig does, with Chanda's own loader.
func ApplyOverrides(cfg string, ovs []Override) (string, error) {
	if len(ovs) == 0 {
		return cfg, nil
	}
	root := map[string]any{}
	if err := yaml.Unmarshal([]byte(cfg), &root); err != nil {
		return "", fmt.Errorf("chanda.yaml: %w", err)
	}
	for _, o := range ovs {
		parts := strings.Split(o.Key, ".")
		m := root
		for i, p := range parts[:len(parts)-1] {
			next, ok := m[p]
			if !ok {
				nm := map[string]any{}
				m[p] = nm
				m = nm
				continue
			}
			nm, ok := next.(map[string]any)
			if !ok {
				return "", fmt.Errorf("-chanda-set %s: %s is not a mapping", o.Raw, strings.Join(parts[:i+1], "."))
			}
			m = nm
		}
		m[parts[len(parts)-1]] = o.Value
	}
	b, err := yaml.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// OverrideStrings returns the overrides as given (key=value), for the output.
func OverrideStrings(ovs []Override) []string {
	if len(ovs) == 0 {
		return nil
	}
	out := make([]string, len(ovs))
	for i, o := range ovs {
		out[i] = o.Raw
	}
	return out
}

// CheckChandaConfig loads a chanda.yaml text with Chanda's loader (unknown
// keys and wrong types are errors there: yaml KnownFields) and returns the
// configuration hash Chanda reports.
func CheckChandaConfig(cfg string) (string, error) {
	dir, err := os.MkdirTemp("", "lme-chanda-config-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "chanda.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		return "", err
	}
	c, err := config.Load(p)
	if err != nil {
		return "", err
	}
	return c.Hash(), nil
}
