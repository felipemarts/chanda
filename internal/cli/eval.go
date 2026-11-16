package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/embed"
	"github.com/felipemarts/chanda/internal/eval"
	"github.com/felipemarts/chanda/internal/eval/coresys"
)

// NewReranker builds the cross-encoder the evaluation gives C2..C5 from the
// configuration (nil, nil when none). The chanda binary has none; a binary
// with a reranker (chanda-hugot) sets it before calling Run.
var NewReranker func(cfg *config.Config) (core.Reranker, error)

// EvalCLI is the evaluation front end used by "chanda eval": the baselines
// B0..B3 and the Chanda configurations C1..C5, compared with the configured
// embedder (internal/embed: the hash embedder by default, hugot in
// chanda-hugot), so B2 and every C system share one embedding model; and the
// eight own suites, those needing a live core run by internal/eval/coresys.
func EvalCLI(version string) eval.CLI {
	fs := eval.BaselineFactories()
	for n, f := range coresys.Factories() {
		fs[n] = f
	}
	return eval.CLI{
		Factories:   fs,
		NewEmbedder: func(cfg *config.Config) (core.Embedder, error) { return embed.NewFromConfig(cfg.Embed) },
		NewReranker: func(cfg *config.Config) (core.Reranker, error) {
			if NewReranker == nil {
				return nil, nil
			}
			return NewReranker(cfg)
		},
		Suites: func(fe eval.FactoryEnv) *eval.Registry {
			r := eval.NewRegistry()
			if err := coresys.Register(r, fe); err != nil {
				panic(err) // static suite names
			}
			return r
		},
		Version: version,
	}
}

// RunEval is the default Eval hook: the evaluation harness (BASE.md
// "Evaluation") with every compared configuration and suite.
func RunEval(_ context.Context, args []string, stdout, stderr io.Writer) int {
	coresys.Logf = func(format string, a ...any) { fmt.Fprintf(stderr, "chanda eval: "+format+"\n", a...) }
	return EvalCLI(Version).Main(args, stdout)
}
