// Command chanda-hugot is chanda with the optional hugot embedder (provider
// "hugot") registered, plus the commands that install and try its models:
//
//	chanda-hugot models list
//	chanda-hugot models pull <model> <dir>     download (the only network use)
//	chanda-hugot models adopt <model> <dir>    write the manifest of files already in dir
//	chanda-hugot models verify <dir>           check the manifest checksums
//	chanda-hugot embed  [--query] <model-dir> <text>...
//	chanda-hugot rerank <model-dir> <query> <doc>...
//	chanda-hugot version
//
// Every other command is the chanda CLI (serve, ingest, ...), run through
// chandaMain. Results are JSON on stdout; exit 0 ok, 1 error, 2 usage.
// Build: CGO_ENABLED=0 GOEXPERIMENT=simd go build ./cmd/chanda-hugot
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felipemarts/chanda-mcp/embedders/hugot"
	"github.com/felipemarts/chanda-mcp/internal/cli"
)

var version = "dev"

// chandaMain runs the regular chanda CLI (serve, ingest, sleep, eval,
// forget) with provider "hugot" registered by main (decision 0033).
var chandaMain = cli.Run

const usage = `usage: chanda-hugot <command> [args]

  models list                    supported models
  models pull <model> <dir>      download a model into dir (network; install step only)
  models adopt <model> <dir>     write the manifest for model files already in dir
  models verify <dir>            check the model files against the manifest
  embed [--query] <dir> <text>…  embed texts, print model id, dimension and timing
  rerank <dir> <query> <doc>…    score documents with a cross-encoder
  version                        print versions and build flavour

other commands are the chanda CLI with embed.provider "hugot" available.
`

func main() {
	hugot.Register()
	cli.Version = version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "models":
		return cmdModels(ctx, args[1:], stdout, stderr)
	case "embed":
		return cmdEmbed(ctx, args[1:], stdout, stderr)
	case "rerank":
		return cmdRerank(ctx, args[1:], stdout, stderr)
	case "version", "--version", "-version", "-v":
		return printJSON(stdout, map[string]string{"chanda_hugot": version, "hugot": hugot.HugotVersion, "flavour": hugot.Flavour})
	case "help", "-h", "--help", "-help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	return chandaMain(ctx, args, stdout, stderr)
}

func cmdModels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageErr(stderr, "models needs a subcommand")
	}
	switch sub, rest := args[0], args[1:]; {
	case sub == "list" && len(rest) == 0:
		type row struct {
			Name, Repo, Kind, ModelID string
			Dim, MaxTokens            int
		}
		var rows []row
		for _, m := range hugot.Models {
			rows = append(rows, row{m.Name, m.Repo, string(m.Kind), m.ModelID(), m.Dim, m.MaxTokens})
		}
		return printJSON(stdout, rows)
	case sub == "pull" && len(rest) == 2:
		man, err := hugot.Pull(ctx, rest[0], rest[1])
		return result(stdout, stderr, man, err)
	case sub == "adopt" && len(rest) == 2:
		m, ok := hugot.LookupModel(rest[0])
		if !ok {
			return usageErr(stderr, "unknown model "+rest[0])
		}
		man, err := hugot.WriteManifest(rest[1], m)
		return result(stdout, stderr, man, err)
	case sub == "verify" && len(rest) == 1:
		man, err := hugot.ReadManifest(rest[0])
		if err == nil {
			err = hugot.VerifyManifest(rest[0], man)
		}
		return result(stdout, stderr, man, err)
	}
	return usageErr(stderr, "bad models command")
}

func cmdEmbed(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("chanda-hugot embed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	query := fs.Bool("query", false, "embed as search queries (e5: \"query: \" prefix)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		return usageErr(stderr, "embed needs <model-dir> <text>...")
	}
	start := time.Now()
	e, err := hugot.Open(ctx, fs.Arg(0))
	if err != nil {
		return result(stdout, stderr, nil, err)
	}
	defer e.Close()
	load := time.Since(start)
	start = time.Now()
	embed := e.Embed
	if *query {
		embed = e.EmbedQuery
	}
	vecs, err := embed(ctx, fs.Args()[1:])
	if err != nil {
		return result(stdout, stderr, nil, err)
	}
	return printJSON(stdout, map[string]any{
		"model_id": e.ModelID(), "dim": e.Dim(), "load_ms": load.Milliseconds(),
		"embed_ms": time.Since(start).Milliseconds(), "vectors": vecs,
	})
}

func cmdRerank(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 3 {
		return usageErr(stderr, "rerank needs <model-dir> <query> <doc>...")
	}
	r, err := hugot.OpenReranker(ctx, args[0])
	if err != nil {
		return result(stdout, stderr, nil, err)
	}
	defer r.Close()
	start := time.Now()
	scores, err := r.Score(ctx, args[1], args[2:])
	if err != nil {
		return result(stdout, stderr, nil, err)
	}
	return printJSON(stdout, map[string]any{"model_id": r.ModelID(), "score_ms": time.Since(start).Milliseconds(), "logits": scores})
}

func result(stdout, stderr io.Writer, v any, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, "chanda-hugot:", err)
		return 1
	}
	return printJSON(stdout, v)
}

func usageErr(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "chanda-hugot: %s\n\n%s", msg, usage)
	return 2
}

func printJSON(w io.Writer, v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return 1
	}
	fmt.Fprintln(w, string(b))
	return 0
}
