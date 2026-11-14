// Package embedreg lets a separately built binary add an embed provider
// (for example the optional hugot module) without the main module importing
// it. The binary calls Register from main before loading the configuration;
// internal/embed.NewFromConfig consults Lookup for providers it does not know.
package embedreg

import (
	"context"
	"sync"
)

// Embedder has the method set of chanda's internal core.Embedder.
type Embedder interface {
	ModelID() string
	Dim() int
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Config mirrors the embed section of chanda.yaml.
type Config struct {
	Provider, ModelID, ModelPath, RerankerModelPath string
	Dim, BatchSize                                  int
}

// Factory builds an Embedder from the configuration. It must not use the network.
type Factory func(Config) (Embedder, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register adds a provider. It panics on an empty name, a nil factory or a
// duplicate name, like database/sql.Register.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if name == "" || f == nil || factories[name] != nil {
		panic("embedreg: invalid or duplicate provider " + name)
	}
	factories[name] = f
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[name]
	return f, ok
}
