// Command evalrun is the development entry point of the evaluation: it runs
// B0, B1, B2 and B3 on the synthetic Portuguese set and writes
// eval/reports/baseline.md and baseline.json. Run it from the repository root:
//
//	go run ./eval/cmd/evalrun
//
// Any `chanda eval` flag may follow and overrides the defaults.
package main

import (
	"os"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

func main() {
	args := append([]string{
		"-dataset", "eval/data/synthetic-pt",
		"-systems", "B0,B1,B2,B3",
		"-out", "eval/reports",
		"-name", "baseline",
	}, os.Args[1:]...)
	os.Exit(eval.Main(args, os.Stdout))
}
