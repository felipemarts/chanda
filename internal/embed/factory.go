package embed

import (
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/pkg/chanda/embedreg"
)

// Provider names accepted in embed.provider.
const (
	ProviderHash  = "hash"
	ProviderHugot = "hugot"
)

// NewFromConfig builds the configured Embedder. Nothing is downloaded: model
// files, when a provider needs them, come from cfg.ModelPath.
//
// For "hash" the model id is derived (hash-v1-<dim>); a configured model_id
// that disagrees is rejected rather than stored as a misleading label.
func NewFromConfig(cfg config.EmbedConfig) (core.Embedder, error) {
	if f, ok := embedreg.Lookup(cfg.Provider); ok && cfg.Provider != ProviderHash { // built outside this module (decision 0033)
		e, err := f(embedreg.Config{Provider: cfg.Provider, ModelID: cfg.ModelID, ModelPath: cfg.ModelPath,
			RerankerModelPath: cfg.RerankerModelPath, Dim: cfg.Dim, BatchSize: cfg.BatchSize})
		if err != nil {
			return nil, core.Wrap(core.CodeOf(err), err, "embed provider "+cfg.Provider)
		}
		return e, nil
	}
	switch cfg.Provider {
	case ProviderHash, "":
		h, err := NewHash(cfg.Dim)
		if err != nil {
			return nil, err
		}
		if cfg.ModelID != "" && cfg.ModelID != h.ModelID() {
			return nil, core.Errf(core.CodeInvalidArgument,
				"embed.model_id %q does not match the hash embedder of dim %d (%s); set model_id to %s or leave it empty",
				cfg.ModelID, cfg.Dim, h.ModelID(), h.ModelID())
		}
		return h, nil
	case ProviderHugot:
		return nil, core.Errf(core.CodeUnavailable,
			"embed provider %q is not built into this binary: build chanda-hugot (embedders/hugot) or use provider %q",
			ProviderHugot, ProviderHash)
	default:
		return nil, core.Errf(core.CodeInvalidArgument, "unknown embed provider %q (want %q or %q)", cfg.Provider, ProviderHash, ProviderHugot)
	}
}
