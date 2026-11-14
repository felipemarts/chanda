package embed

import (
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
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
			"embed provider %q is not built in yet (pure-Go ONNX integration pending, see docs/research/embeddings.md); use provider %q",
			ProviderHugot, ProviderHash)
	default:
		return nil, core.Errf(core.CodeInvalidArgument, "unknown embed provider %q (want %q or %q)", cfg.Provider, ProviderHash, ProviderHugot)
	}
}
