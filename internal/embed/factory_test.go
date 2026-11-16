package embed

import (
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

func TestNewFromConfig(t *testing.T) {
	def := config.Default().Embed
	cases := []struct {
		name    string
		cfg     config.EmbedConfig
		code    core.Code // "" = success
		modelID string
	}{
		{"default config is the hash embedder", def, "", "hash-v1-256"},
		{"hash with derived model id", config.EmbedConfig{Provider: "hash", Dim: 384}, "", "hash-v1-384"},
		{"hash with mismatched model id is rejected", config.EmbedConfig{Provider: "hash", Dim: 384, ModelID: "hash-v1-256"}, core.CodeInvalidArgument, ""},
		{"hash with invalid dim", config.EmbedConfig{Provider: "hash", Dim: 0}, core.CodeInvalidArgument, ""},
		{"hugot is unavailable until integrated", config.EmbedConfig{Provider: "hugot", Dim: 384, ModelID: "m"}, core.CodeUnavailable, ""},
		{"unknown provider", config.EmbedConfig{Provider: "openai", Dim: 8}, core.CodeInvalidArgument, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := NewFromConfig(c.cfg)
			if c.code != "" {
				if core.CodeOf(err) != c.code {
					t.Fatalf("err = %v, want code %s", err, c.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if e.ModelID() != c.modelID {
				t.Fatalf("model id %s, want %s", e.ModelID(), c.modelID)
			}
		})
	}
}
