package embed

import (
	"errors"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/pkg/chanda/embedreg"
)

// Providers built outside the main module (decision 0033) are found through
// pkg/chanda/embedreg; "hash" stays built in even if someone registers it.
func TestNewFromConfigRegisteredProvider(t *testing.T) {
	var got embedreg.Config
	embedreg.Register("registry-test", func(c embedreg.Config) (embedreg.Embedder, error) {
		got = c
		return NewHash(c.Dim)
	})
	embedreg.Register("registry-test-fails", func(embedreg.Config) (embedreg.Embedder, error) {
		return nil, errors.New("model files missing")
	})
	embedreg.Register(ProviderHash, func(embedreg.Config) (embedreg.Embedder, error) {
		return nil, errors.New("must not be called")
	})

	e, err := NewFromConfig(config.EmbedConfig{Provider: "registry-test", Dim: 16, ModelPath: "/m", ModelID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ModelID() != HashModelID(16) || got.ModelPath != "/m" || got.ModelID != "x" || got.Provider != "registry-test" {
		t.Fatalf("embedder %s, config passed %+v", e.ModelID(), got)
	}
	if _, err := NewFromConfig(config.EmbedConfig{Provider: "registry-test-fails"}); err == nil || core.CodeOf(err) != core.CodeInternal {
		t.Fatalf("failing factory: err = %v", err)
	}
	if e, err := NewFromConfig(config.EmbedConfig{Provider: ProviderHash, Dim: 8}); err != nil || e.ModelID() != HashModelID(8) {
		t.Fatalf("hash overridden: %v, %v", e, err)
	}
}
