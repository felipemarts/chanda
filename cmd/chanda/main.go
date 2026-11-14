// Command chanda is the CLI and MCP server entry point.
//
//	chanda serve  [--config chanda.yaml] [--http addr] [--profiles read,standard]
//	chanda ingest [--config f] [--namespace ns] [--kind document] [--uri u] <path>
//	chanda sleep  [--config f] [--namespace ns] [--phases 1,2] [--budget-tokens n] [--budget-events n]
//	chanda eval   [--config f] [eval args...]
//	chanda forget [--config f] [--namespace ns] --kind record|document|source|entity|date_range|search
//	              [--value v] [--from t] [--to t] [--query q] [--dry-run]
//	chanda version
//
// Commands other than serve print the result envelope as JSON on stdout and
// exit 0 when ok, 1 when not ok, 2 on a usage error. In stdio serve mode
// stdout carries only MCP frames; every log goes to stderr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/felipemarts/chanda-mcp/internal/app"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/mcp"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

var version = "dev"

// newAPI builds the Chanda core for cfg and returns it with a close function.
// serve starts its background indexer (see starter). Tests replace it.
var newAPI = func(cfg *config.Config) (chanda.API, func() error, error) {
	a, err := app.Open(context.Background(), cfg)
	if err != nil {
		return nil, nil, err
	}
	return a, a.Close, nil
}

// starter is implemented by an API with background work (the embedding
// indexer of app.App); serve starts it, one-shot commands do not.
type starter interface{ Start(context.Context) }

// runEval runs the evaluation harness (BASE.md "Evaluation"). Integration
// replaces it; it may call newAPI for each configuration it compares.
var runEval = func(ctx context.Context, cfg *config.Config, args []string, stdout, stderr io.Writer) error {
	return chanda.Errf(chanda.CodeUnavailable, "eval is not wired into this binary yet")
}

const usage = `usage: chanda <command> [flags]

commands:
  serve    run the MCP server (stdio, or Streamable HTTP with --http)
  ingest   ingest a document or folder
  sleep    run memory consolidation now
  eval     run the evaluation report
  forget   delete in cascade and verify
  version  print the version

run "chanda <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(ctx, rest, stderr)
	case "ingest":
		return cmdIngest(ctx, rest, stdout, stderr)
	case "sleep":
		return cmdSleep(ctx, rest, stdout, stderr)
	case "eval":
		return cmdEval(ctx, rest, stdout, stderr)
	case "forget":
		return cmdForget(ctx, rest, stdout, stderr)
	case "version", "--version", "-version", "-v":
		fmt.Fprintln(stdout, "chanda", version)
		return 0
	case "help", "-h", "--help", "-help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "chanda: unknown command %q\n\n%s", cmd, usage)
	return 2
}

// common holds the flags shared by the API commands.
type common struct {
	config    string
	namespace string
	actor     string
}

func newFlags(name string, stderr io.Writer, c *common) *flag.FlagSet {
	fs := flag.NewFlagSet("chanda "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.config, "config", "chanda.yaml", "configuration file (defaults apply when missing)")
	if name != "serve" {
		fs.StringVar(&c.namespace, "namespace", "", "namespace (default: the configured one)")
		fs.StringVar(&c.actor, "actor", "cli", "actor recorded in the audit log")
	}
	return fs
}

// parse parses args; it returns -1 to continue, or an exit code.
func parse(fs *flag.FlagSet, args []string) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	return -1
}

// withAPI loads the configuration, builds the API and runs fn with the CLI
// actor (a person at a terminal, hence Human, with the user zone ceiling). The result is printed as an
// envelope.
func (c *common) withAPI(ctx context.Context, stdout, stderr io.Writer, fn func(context.Context, chanda.API, string) (any, error)) int {
	cfg, err := config.Load(c.config)
	if err != nil {
		fmt.Fprintln(stderr, "chanda:", err)
		return 2
	}
	api, closeAPI, err := newAPI(cfg)
	if err != nil {
		return emit(stdout, stderr, nil, err)
	}
	if closeAPI != nil {
		defer func() {
			if err := closeAPI(); err != nil {
				fmt.Fprintln(stderr, "chanda: close:", err)
			}
		}()
	}
	ns := c.namespace
	if ns == "" {
		ns = cfg.Namespace
	}
	// A person at the terminal may state things in the user zone (decision 0090).
	ctx = chanda.WithActor(ctx, chanda.Actor{Name: c.actor, Human: true, MaxZone: chanda.ZoneUser})
	out, err := fn(ctx, api, ns)
	return emit(stdout, stderr, out, err)
}

// emit prints the envelope for (out, err) and returns the exit code.
func emit(stdout, stderr io.Writer, out any, err error) int {
	env := chanda.NewEnvelope(out, err)
	b, merr := json.MarshalIndent(env, "", "  ")
	if merr != nil {
		fmt.Fprintln(stderr, "chanda: encoding result:", merr)
		return 1
	}
	fmt.Fprintln(stdout, string(b))
	if !env.OK {
		return 1
	}
	return 0
}

func cmdServe(ctx context.Context, args []string, stderr io.Writer) int {
	var c common
	var httpAddr, profiles string
	fs := newFlags("serve", stderr, &c)
	fs.StringVar(&httpAddr, "http", "", "serve Streamable HTTP on this address instead of stdio (overrides mcp.http_addr)")
	fs.StringVar(&profiles, "profiles", "", "comma-separated tool profiles: read, standard, admin (overrides mcp.profiles)")
	if code := parse(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "chanda serve: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	cfg, err := config.Load(c.config)
	if err != nil {
		logger.Error("load config", "err", err)
		return 2
	}
	if httpAddr != "" {
		cfg.MCP.HTTPAddr = httpAddr
	}
	if profiles != "" {
		cfg.MCP.Profiles = splitList(profiles)
	}
	api, closeAPI, err := newAPI(cfg)
	switch {
	case err == nil:
		if closeAPI != nil {
			defer func() {
				if err := closeAPI(); err != nil {
					logger.Error("close", "err", err)
				}
			}()
		}
	case chanda.CodeOf(err) == chanda.CodeUnavailable:
		logger.Warn("serving without a core: every tool answers unavailable", "reason", err.Error())
		api = chanda.Unwired{Err: err}
	default:
		logger.Error("open", "err", err)
		return 1
	}
	if st, ok := api.(starter); ok {
		st.Start(ctx)
	}
	srv, err := mcp.New(api, cfg, mcp.Options{Version: version, Logger: logger})
	if err != nil {
		logger.Error("mcp", "err", err)
		return 2
	}
	if cfg.MCP.HTTPAddr == "" {
		logger.Info("serving MCP over stdio", "tools", strings.Join(srv.Tools(), ","))
		if err := srv.RunStdio(ctx); err != nil && ctx.Err() == nil {
			logger.Error("stdio", "err", err)
			return 1
		}
		return 0
	}
	ln, err := net.Listen("tcp", cfg.MCP.HTTPAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		return 1
	}
	logger.Info("serving MCP over Streamable HTTP", "url", "http://"+ln.Addr().String()+mcp.HTTPPath,
		"tools", strings.Join(srv.Tools(), ","))
	if err := srv.Serve(ctx, ln); err != nil {
		logger.Error("http", "err", err)
		return 1
	}
	return 0
}

func cmdIngest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var c common
	var kind, uri string
	fs := newFlags("ingest", stderr, &c)
	fs.StringVar(&kind, "kind", "", "source kind (default document)")
	fs.StringVar(&uri, "uri", "", "source uri (default the path)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chanda ingest [flags] <path>")
		fs.PrintDefaults()
	}
	if code := parse(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	in := chanda.IngestInput{Path: fs.Arg(0), Source: chanda.SourceRef{Kind: chanda.SourceKind(kind), URI: uri}}
	return c.withAPI(ctx, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.Ingest(ctx, in)
	})
}

func cmdSleep(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var c common
	var phases string
	var in chanda.SleepRunInput
	fs := newFlags("sleep", stderr, &c)
	fs.StringVar(&phases, "phases", "", "comma-separated phases, 1 and/or 2 (default per configuration)")
	fs.IntVar(&in.Budget.Tokens, "budget-tokens", 0, "LLM token ceiling (default per configuration)")
	fs.IntVar(&in.Budget.Events, "budget-events", 0, "events ceiling (default per configuration)")
	if code := parse(fs, args); code >= 0 {
		return code
	}
	for _, p := range splitList(phases) {
		n, err := strconv.Atoi(p)
		if err != nil || (n != 1 && n != 2) {
			fmt.Fprintf(stderr, "chanda sleep: invalid phase %q (want 1 or 2)\n", p)
			return 2
		}
		in.Phases = append(in.Phases, n)
	}
	return c.withAPI(ctx, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.SleepRun(ctx, in)
	})
}

func cmdEval(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cfgPath, rest := extractConfig(args)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(stderr, "chanda:", err)
		return 2
	}
	if err := runEval(ctx, cfg, rest, stdout, stderr); err != nil {
		return emit(stdout, stderr, nil, err)
	}
	return 0
}

func cmdForget(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var c common
	var in chanda.ForgetInput
	fs := newFlags("forget", stderr, &c)
	fs.StringVar(&in.Scope.Kind, "kind", "", "scope kind: "+strings.Join(chanda.ScopeKinds(), ", "))
	fs.StringVar(&in.Scope.Value, "value", "", "record id, document, source or entity (for those kinds)")
	fs.StringVar(&in.Scope.From, "from", "", "date_range start, inclusive")
	fs.StringVar(&in.Scope.To, "to", "", "date_range end, exclusive")
	fs.StringVar(&in.Scope.Query, "query", "", "search scope query")
	fs.BoolVar(&in.DryRun, "dry-run", false, "report what would be removed without removing it")
	if code := parse(fs, args); code >= 0 {
		return code
	}
	if in.Scope.Kind == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: chanda forget --kind <kind> [--value v | --from t --to t | --query q] [--dry-run]")
		return 2
	}
	return c.withAPI(ctx, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.Forget(ctx, in)
	})
}

// extractConfig takes --config out of args and leaves the rest untouched, so
// the evaluation harness can define its own flags.
func extractConfig(args []string) (string, []string) {
	path := "chanda.yaml"
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "--config" || a == "-config") && i+1 < len(args):
			path = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="), strings.HasPrefix(a, "-config="):
			_, path, _ = strings.Cut(a, "=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
