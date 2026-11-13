// Package env bundles the shared dependencies every component receives.
// Components take an *env.Env in their constructor instead of globals.
package env

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/ids"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

type Env struct {
	Cfg   *config.Config
	Store *store.Store
	Clock core.Clock
	IDs   *ids.Generator
	Audit *audit.Log
}

// New wires an Env from an open store.
func New(cfg *config.Config, st *store.Store, c core.Clock) *Env {
	return &Env{Cfg: cfg, Store: st, Clock: c, IDs: ids.New(c, cfg.IDSeed), Audit: audit.New(c)}
}

// Open opens cfg.DBPath with the system clock.
func Open(ctx context.Context, cfg *config.Config) (*Env, error) {
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return New(cfg, st, clock.System{}), nil
}
