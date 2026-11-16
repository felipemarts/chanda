// Package cli is the chanda command line: every command of the chanda
// binary lives here so that other binaries (chanda-hugot, which registers
// the hugot embed provider) run the same CLI through cli.Run.
//
//	chanda serve  [--config chanda.yaml] [--http addr] [--profiles read,standard]
//	chanda ingest [--config f] [--namespace ns] [--kind document] [--uri u] <path>
//	chanda sleep  [--config f] [--namespace ns] [--phases 1,2] [--budget-tokens n] [--budget-events n]
//	chanda eval   [eval flags...]   (see "chanda eval -h")
//	chanda forget [--config f] [--namespace ns] --kind record|document|source|entity|date_range|search
//	              [--value v] [--from t] [--to t] [--query q] [--dry-run]
//	chanda gate list|approve <id>|retire <id>|add --question q [--answer a]... [--as-of d]
//	chanda ranking on|off
//	chanda version
//
// Commands other than serve and eval print the result envelope as JSON on
// stdout and exit 0 when ok, 1 when not ok, 2 on a usage error. In stdio
// serve mode stdout carries only MCP frames; every log goes to stderr.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/felipemarts/chanda/internal/app"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/mcp"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Version is the version the binary reports. The Makefile stamps it with
//
//	-ldflags "-X github.com/felipemarts/chanda/internal/cli.Version=$(git describe)"
var Version = "dev"

// CLI holds the hooks a binary may replace. Start from Default.
type CLI struct {
	// NewAPI builds the Chanda core for cfg and returns it with a close
	// function. serve starts its background indexer when it has one.
	NewAPI func(cfg *config.Config) (chanda.API, func() error, error)
	// Eval runs "chanda eval" with the arguments after the command name and
	// returns the exit code. Nil answers unavailable.
	Eval func(ctx context.Context, args []string, stdout, stderr io.Writer) int
	// Version is printed by "chanda version" and recorded in eval reports.
	Version string
}

// Default returns the CLI over the real core (internal/app) and the
// evaluation harness.
func Default() CLI {
	return CLI{NewAPI: OpenApp, Eval: RunEval, Version: Version}
}

// OpenApp is the default NewAPI: app.Open over cfg.
func OpenApp(cfg *config.Config) (chanda.API, func() error, error) {
	a, err := app.Open(context.Background(), cfg)
	if err != nil {
		return nil, nil, err
	}
	return a, a.Close, nil
}

// Run executes one command with the default hooks and returns the process
// exit code. It is the whole of cmd/chanda's main.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return Default().Run(ctx, args, stdout, stderr)
}

// starter is implemented by an API with background work (the embedding
// indexer of app.App); serve starts it, one-shot commands do not.
type starter interface{ Start(context.Context) }

const usage = `usage: chanda <command> [flags]

commands:
  serve    run the MCP server (stdio, or Streamable HTTP with --http)
  ingest   ingest a document or folder
  sleep    run memory consolidation now
  eval     run the evaluation report
  forget   delete in cascade and verify
  gate     review the gate set (list, approve, retire, add)
  ranking  switch learned ranking on or off
  keys     learned query keys: enable, disable, reset, rebuild, stats
  conflicts  conflict resolution: list, revert, reset, scan, stats
  version  print the version

run "chanda <command> -h" for the flags of a command.
`

// Run executes one command and returns the process exit code.
func (cl CLI) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cl.cmdServe(ctx, rest, stderr)
	case "ingest":
		return cl.cmdIngest(ctx, rest, stdout, stderr)
	case "sleep":
		return cl.cmdSleep(ctx, rest, stdout, stderr)
	case "eval":
		if cl.Eval == nil {
			return emit(stdout, stderr, nil, chanda.Errf(chanda.CodeUnavailable, "eval is not wired into this binary"))
		}
		return cl.Eval(ctx, rest, stdout, stderr)
	case "forget":
		return cl.cmdForget(ctx, rest, stdout, stderr)
	case "gate":
		return cl.cmdGate(ctx, rest, stdout, stderr)
	case "ranking":
		return cl.cmdRanking(ctx, rest, stdout, stderr)
	case "keys":
		return cl.cmdKeys(ctx, rest, stdout, stderr)
	case "conflicts":
		return cl.cmdConflicts(ctx, rest, stdout, stderr)
	case "version", "--version", "-version", "-v":
		fmt.Fprintln(stdout, "chanda", cl.Version)
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
func (c *common) withAPI(ctx context.Context, newAPI func(*config.Config) (chanda.API, func() error, error), stdout, stderr io.Writer, fn func(context.Context, chanda.API, string) (any, error)) int {
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

func (cl CLI) cmdServe(ctx context.Context, args []string, stderr io.Writer) int {
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
	api, closeAPI, err := cl.NewAPI(cfg)
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
	srv, err := mcp.New(api, cfg, mcp.Options{Version: cl.Version, Logger: logger})
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

func (cl CLI) cmdIngest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.Ingest(ctx, in)
	})
}

func (cl CLI) cmdSleep(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.SleepRun(ctx, in)
	})
}

func (cl CLI) cmdForget(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		in.Namespace = ns
		return api.Forget(ctx, in)
	})
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
