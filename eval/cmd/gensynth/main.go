// Command gensynth writes the deterministic synthetic Portuguese dataset.
//
//	go run ./eval/cmd/gensynth [-seed chanda-synthetic-pt-v1] [-out eval/data/synthetic-pt]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

func main() {
	seed := flag.String("seed", eval.SyntheticSeed, "generator seed")
	out := flag.String("out", "eval/data/synthetic-pt", "output directory")
	flag.Parse()
	ds := eval.GenerateSyntheticPT(*seed)
	if err := ds.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := eval.WriteDataset(*out, ds); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d items, %d questions, hash %s\n", *out, len(ds.Corpus), len(ds.Questions), ds.Hash())
}
