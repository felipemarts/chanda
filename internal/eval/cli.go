package eval

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// FactoryEnv is what a system factory receives.
type FactoryEnv struct {
	Config   *config.Config
	Embedder core.Embedder // the shared embedding model (B2 and C1..C5 use the same one)
}

// Factory builds one fresh, empty System.
type Factory func(ctx context.Context, env FactoryEnv) (System, error)

// BaselineFactories returns B0..B3 configured from env.
func BaselineFactories() map[string]Factory {
	return map[string]Factory{
		"B0": func(context.Context, FactoryEnv) (System, error) { return NoMemory{}, nil },
		"B1": func(ctx context.Context, e FactoryEnv) (System, error) {
			return NewBM25(ctx, e.Config.Read.KeywordTopK, e.Config.Write.MaxChunkChars)
		},
		"B2": func(_ context.Context, e FactoryEnv) (System, error) {
			return NewPlainRAG(e.Embedder, e.Config.Read.VectorTopK, e.Config.Write.MaxChunkChars), nil
		},
		"B3": func(_ context.Context, e FactoryEnv) (System, error) {
			return NewFullContext(e.Config.Write.MaxChunkChars), nil
		},
	}
}

// CLI is the command-line front end. Integration adds C1..C5 to Factories
// and replaces NewEmbedder with the configured Embedder.
type CLI struct {
	Factories   map[string]Factory
	NewEmbedder func(cfg *config.Config) (core.Embedder, error)
	Suites      func(FactoryEnv) *Registry // default NewRegistry
	Clock       core.Clock                 // report date; default clock.System
	Version     string                     // default of -version; empty = the VCS revision stamped by go build
}

// DefaultCLI runs the baselines with the local deterministic embedder.
func DefaultCLI() CLI {
	return CLI{
		Factories:   BaselineFactories(),
		NewEmbedder: func(cfg *config.Config) (core.Embedder, error) { return NewLocalEmbedder(cfg.Embed.Dim), nil },
	}
}

// Main is what `chanda eval` calls: Main(os.Args[2:], os.Stdout). It returns
// the process exit code (0 ok, 1 run error, 2 usage error).
func Main(args []string, stdout io.Writer) int { return DefaultCLI().Main(args, stdout) }

// Main parses args, runs the evaluation and writes the report.
func (c CLI) Main(args []string, stdout io.Writer) int {
	names := make([]string, 0, len(c.Factories))
	for n := range c.Factories {
		names = append(names, n)
	}
	sort.Strings(names)

	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stdout)
	dataset := fs.String("dataset", "eval/data/synthetic-pt", "dataset directory (corpus.jsonl, questions.jsonl)")
	systems := fs.String("systems", strings.Join(names, ","), "comma-separated systems to compare, in report order")
	budget := fs.Int("budget", 0, "context tokens per query (0 = read.context_default_tokens)")
	out := fs.String("out", "eval/reports", "output directory")
	name := fs.String("name", "report", "report name: writes <out>/<name>.md and .json")
	cfgPath := fs.String("config", "", "chanda.yaml (default: built-in defaults)")
	date := fs.String("date", "", "report date YYYY-MM-DD (default: today)")
	defVersion := c.Version
	if defVersion == "" || defVersion == "dev" {
		defVersion = buildVersion()
	}
	version := fs.String("version", defVersion, "Chanda version recorded in the report")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintln(stdout, "chanda eval:", err)
		return code
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fail(2, err)
	}
	clk := c.Clock
	if clk == nil {
		clk = clock.System{}
	}
	if *date != "" {
		t, err := time.Parse("2006-01-02", *date)
		if err != nil {
			return fail(2, fmt.Errorf("-date: %w", err))
		}
		clk = clock.NewFake(t, 0)
	}
	ds, err := LoadDataset(*dataset)
	if err != nil {
		return fail(1, err)
	}
	emb, err := c.NewEmbedder(cfg)
	if err != nil {
		return fail(1, err)
	}
	ctx := context.Background()
	var syss []System
	var unavailable []UnavailableSystem
	defer func() {
		for _, s := range syss {
			_ = s.Close()
		}
	}()
	for _, n := range strings.Split(*systems, ",") {
		n = strings.TrimSpace(n)
		f, ok := c.Factories[n]
		if !ok {
			return fail(2, fmt.Errorf("unknown system %q (known: %s)", n, strings.Join(names, ", ")))
		}
		s, err := f(ctx, FactoryEnv{Config: cfg, Embedder: emb})
		if core.CodeOf(err) == core.CodeUnavailable {
			unavailable = append(unavailable, UnavailableSystem{Name: n, Reason: Reason(err)})
			fmt.Fprintf(stdout, "  %-3s not available: %s\n", n, Reason(err))
			continue
		}
		if err != nil {
			return fail(1, fmt.Errorf("%s: %w", n, err))
		}
		syss = append(syss, s)
	}
	suites := NewRegistry()
	if c.Suites != nil {
		suites = c.Suites(FactoryEnv{Config: cfg, Embedder: emb})
	}
	rep, err := Run(ctx, Options{Dataset: ds, Systems: syss, Budget: *budget, OutDir: *out, Name: *name,
		Config: cfg, Embedder: emb, Clock: clk, Version: *version, Suites: suites, Unavailable: unavailable})
	if err != nil {
		return fail(1, err)
	}
	fmt.Fprintf(stdout, "wrote %s/%s.{md,json,questions.jsonl}: %d systems, %d questions, budget %d tokens\n",
		*out, *name, len(rep.Systems), rep.Dataset.Questions, rep.BudgetTokens)
	for _, s := range rep.Systems {
		m := s.Overall.rounded()
		fmt.Fprintf(stdout, "  %-3s recall@10 %-6s MRR %-6s nDCG@10 %-6s tokens/query %s\n", s.Name,
			fmtNum(m.RecallAt10), fmtNum(m.MRR), fmtNum(m.NDCGAt10), fmtNum(m.TokensMean))
	}
	return 0
}

// buildVersion returns the module version or VCS revision stamped in the
// binary, else "dev".
func buildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
