package hugot

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/pkg/chanda/embedreg"
)

// ProviderName is the embed.provider value this module serves.
const ProviderName = "hugot"

// Register adds provider "hugot" to pkg/chanda/embedreg, so that
// internal/embed.NewFromConfig can build it. Call it once, from main.
func Register() { embedreg.Register(ProviderName, NewFromConfig) }

// NewFromConfig opens the embedder in cfg.ModelPath. A configured model_id or
// dim that disagrees with the model on disk is rejected rather than stored as
// a misleading label (the same rule as the hash embedder).
func NewFromConfig(cfg embedreg.Config) (embedreg.Embedder, error) {
	if cfg.ModelPath == "" {
		return nil, core.Errf(core.CodeInvalidArgument, "hugot: embed.model_path is empty (a directory made by: chanda-hugot models pull e5-small <dir>)")
	}
	m, err := detect(cfg.ModelPath, KindEmbedder)
	if err != nil {
		return nil, err
	}
	if cfg.ModelID != "" && cfg.ModelID != m.ModelID() {
		return nil, core.Errf(core.CodeInvalidArgument,
			"embed.model_id %q does not match the model in %q built as %s; set model_id to %s or leave it empty",
			cfg.ModelID, cfg.ModelPath, m.ModelID(), m.ModelID())
	}
	if cfg.Dim != 0 && cfg.Dim != m.Dim {
		return nil, core.Errf(core.CodeInvalidArgument, "embed.dim %d does not match %s (%d); set embed.dim: %d", cfg.Dim, m.Name, m.Dim, m.Dim)
	}
	e, err := open(context.Background(), cfg.ModelPath, m)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return e, nil
}
