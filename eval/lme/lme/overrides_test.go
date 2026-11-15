package lme

import (
	"strings"
	"testing"
)

func TestParseOverride(t *testing.T) {
	ov, err := ParseOverride("read.max_per_document=5")
	if err != nil || ov.Key != "read.max_per_document" || ov.Value != 5 || ov.Raw != "read.max_per_document=5" {
		t.Fatalf("%+v %v", ov, err)
	}
	if ov, err := ParseOverride("read.rerank_enabled = true"); err != nil || ov.Value != true || ov.Raw != "read.rerank_enabled=true" {
		t.Fatalf("%+v %v", ov, err)
	}
	if ov, err := ParseOverride("write.ingest_extensions=[.md, .txt]"); err != nil || len(ov.Value.([]any)) != 2 {
		t.Fatalf("%+v %v", ov, err)
	}
	for _, bad := range []string{"", "read.max_per_document", "=5", "read.max_per_document=", "read..x=1", "db_path=/tmp/x.db", "sleep.snapshot_dir=/x", "a=~", "a=[1"} {
		if _, err := ParseOverride(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestOverridesInChandaConfig(t *testing.T) {
	o := ChandaOptions{Embed: EmbedOptions{Kind: "hash"}, Budget: 2000}
	base := o.ConfigYAML("/tmp/q", "hash-v1-256", 256)
	// No override: the text is exactly the battery's.
	if got, err := o.Config("/tmp/q", "hash-v1-256", 256); err != nil || got != base {
		t.Fatalf("no override changed the config: %v\n%s", err, got)
	}
	baseHash, err := CheckChandaConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	set := func(args ...string) []Override {
		var out []Override
		for _, a := range args {
			ov, err := ParseOverride(a)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, ov)
		}
		return out
	}
	o.Set = set("read.max_per_document=5", "read.context_max_tokens=3000", "facts.confirm_boost=0.2", "read.max_per_document=7")
	cfg, err := o.Config("/tmp/q", "hash-v1-256", 256)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"max_per_document: 7", "context_max_tokens: 3000", "confirm_boost: 0.2", "provider: lme-cached", "scheduler: false"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
	h, err := CheckChandaConfig(cfg)
	if err != nil || h == baseHash {
		t.Fatalf("hash %s (base %s): %v", h, baseHash, err)
	}
	// Unknown keys and wrong types fail with Chanda's loader.
	for _, bad := range []string{"read.max_per_doc=5", "nosuch.key=1", "read.max_per_document=many"} {
		o.Set = set(bad)
		cfg, err := o.Config("/tmp/q", "hash-v1-256", 256)
		if err != nil {
			t.Fatalf("%s: %v", bad, err)
		}
		if _, err := CheckChandaConfig(cfg); err == nil {
			t.Errorf("%s: want a config error", bad)
		}
	}
	// A key under a scalar cannot be set.
	o.Set = set("namespace.x=1")
	if _, err := o.Config("/tmp/q", "hash-v1-256", 256); err == nil {
		t.Error("namespace.x: want an error")
	}
}
