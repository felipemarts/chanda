package cli

import (
	"context"
	"io"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// EvalCLI is the evaluation front end used by "chanda eval".
func EvalCLI(version string) eval.CLI {
	c := eval.DefaultCLI()
	c.Version = version
	return c
}

// RunEval is the default Eval hook: the evaluation harness (BASE.md
// "Evaluation") with every compared configuration and suite.
func RunEval(_ context.Context, args []string, stdout, _ io.Writer) int {
	return EvalCLI(Version).Main(args, stdout)
}
