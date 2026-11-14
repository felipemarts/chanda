// Package local is the importable Go entry point of Chanda: it opens the
// core in this process from a chanda.yaml and returns it as a chanda.API.
//
//	api, closeFn, err := local.Open(ctx, "chanda.yaml")
//	if err != nil { ... }
//	defer closeFn()
//	ctx = chanda.WithActor(ctx, chanda.Actor{Name: "my-agent"})
//	out, err := api.Context(ctx, chanda.ContextInput{Query: "..."})
//
// It lives apart from pkg/chanda because the core (internal/app) imports
// pkg/chanda for its types; a separate package avoids the import cycle.
//
// Callers are trusted code in the same process: they may ingest any path.
// Writes are capped at the Actor's MaxZone, or at mcp.max_zone when it is
// empty; the user zone needs a Human actor.
package local

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/app"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Open loads configPath (defaults apply when the file is missing), opens the
// database and starts the background embedding indexer. The returned
// function stops the indexer and closes the database.
func Open(ctx context.Context, configPath string) (chanda.API, func() error, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, chanda.Errf(chanda.CodeInvalidArgument, "%v", err)
	}
	a, err := app.Open(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	a.Start(context.WithoutCancel(ctx))
	return a, a.Close, nil
}
