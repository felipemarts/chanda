// Package app wires every area of Chanda into one chanda.API over one
// env.Env: the write path with its in-transaction hook chain (facts,
// embedding queue, near-duplicates, integrity check), the read path with
// vectors and the ranking adapters, the embedding indexer, provenance,
// forgetting and ranking. The MCP server, the CLI and pkg/chanda/local are
// thin shells over an App.
//
// See docs/decisions/0091-integration-wiring.md for the choices made here.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/dedup"
	"github.com/felipemarts/chanda-mcp/internal/embed"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/facts"
	"github.com/felipemarts/chanda-mcp/internal/rank"
	"github.com/felipemarts/chanda-mcp/internal/read"
	"github.com/felipemarts/chanda-mcp/internal/store"
	"github.com/felipemarts/chanda-mcp/internal/write"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// DefaultActor is recorded when a call carries no chanda.Actor (a Go caller
// that did not set one).
const DefaultActor = "api"

// App implements chanda.API.
type App struct {
	e       *env.Env
	owns    bool // Close closes the store
	writer  *write.Writer
	facts   *facts.Service
	guard   *dedup.Guard
	emb     core.Embedder
	index   *embed.ExactIndex
	indexer *embed.Indexer
	reader  *read.Reader
	ranker  *rank.Ranker
	roots   []string // resolved write.ingest_roots

	mu     sync.Mutex
	cancel context.CancelFunc
	done   <-chan struct{}
}

var _ chanda.API = (*App)(nil)

// Open opens cfg.DBPath with the system clock and wires an App that owns the
// store. Call Start to run the background embedding indexer, Close to stop.
func Open(ctx context.Context, cfg *config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "config")
	}
	e, err := env.Open(ctx, cfg)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "open database")
	}
	a, err := New(e)
	if err != nil {
		e.Store.Close()
		return nil, err
	}
	a.owns = true
	return a, nil
}

// New wires an App over an existing Env (tests, determinism). The caller
// keeps ownership of the store.
func New(e *env.Env) (*App, error) {
	cfg := e.Cfg
	if err := facts.ValidateExtractors(cfg.Write.Extractors); err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "write.extractors")
	}
	emb, err := embed.NewFromConfig(cfg.Embed)
	if err != nil {
		return nil, err
	}
	a := &App{e: e, facts: facts.New(e), guard: dedup.New(e), emb: emb, ranker: rank.New(e)}
	for _, r := range cfg.Write.IngestRoots {
		rr, err := resolvePath(r)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, "write.ingest_roots: "+r)
		}
		a.roots = append(a.roots, rr)
	}
	a.index = embed.NewExactIndex(e)
	a.indexer = embed.NewIndexer(e, emb, a.index)
	if a.writer, err = write.New(e, write.EventHookFunc(a.afterEvent)); err != nil {
		return nil, err
	}
	deps := read.Deps{
		Embedder: emb, VectorIndex: a.index, Utility: a.ranker,
		Learned: learnedAdapter{a.ranker.Learned()},
	}
	a.reader = read.New(e, deps)
	return a, nil
}

// Env returns the environment the App runs on.
func (a *App) Env() *env.Env { return a.e }

// Indexer returns the embedding indexer (flush, determinism tests).
func (a *App) Indexer() *embed.Indexer { return a.indexer }

// Start runs the background embedding indexer until ctx ends or Close is
// called. Calling it twice does nothing.
func (a *App) Start(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return
	}
	ctx, a.cancel = context.WithCancel(ctx)
	a.done = a.indexer.Start(ctx)
}

// Close stops the indexer and, when the App opened it, closes the store.
func (a *App) Close() error {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	a.cancel, a.done = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if a.owns {
		return a.e.Store.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Call context: namespace, actor and channel ceiling.

type call struct {
	ns      string
	actor   chanda.Actor
	ceiling core.Zone
	notices []string
}

// begin resolves the namespace and the caller of one API call.
func (a *App) begin(ctx context.Context, ns string) *call {
	c := &call{ns: strings.TrimSpace(ns)}
	if c.ns == "" {
		c.ns = a.e.Cfg.Namespace
	}
	act, ok := chanda.ActorFrom(ctx)
	if !ok || strings.TrimSpace(act.Name) == "" {
		act.Name = DefaultActor
	}
	c.actor = act
	c.ceiling = act.MaxZone
	if !c.ceiling.Valid() {
		c.ceiling = core.Zone(a.e.Cfg.MCP.MaxZone)
	}
	if !c.ceiling.Valid() {
		c.ceiling = core.ZoneInternal
	}
	if c.ceiling == core.ZoneUser && !act.Human {
		c.ceiling = core.ZoneInternal
		c.notice("zone_ceiling_lowered: the user zone needs a human actor; this caller writes at most internal")
	}
	return c
}

func (c *call) notice(s string) { c.notices = append(c.notices, s) }

// clampNotice reports a write whose source kind asked for more than the
// channel ceiling allows (decision 0090).
func (c *call) clampNotice(kind core.SourceKind) {
	if want := core.ZoneForSource(kind); want.Rank() > c.ceiling.Rank() {
		c.notice("zone_clamped: source kind " + string(kind) + " (" + string(want) + ") capped at " +
			string(c.ceiling) + " by this channel's ceiling")
	}
}

func (c *call) meta(auditID int64) chanda.Meta {
	m := chanda.Meta{AuditID: auditID}
	if len(c.notices) > 0 {
		m.Notices = append([]string{}, c.notices...)
	}
	return m
}

// ---------------------------------------------------------------------------
// Errors.

// coded gives err a stable code (CodeInternal when it carries none).
func coded(err error) error {
	if err == nil {
		return nil
	}
	var ce *core.Error
	if errors.As(err, &ce) {
		return err
	}
	if store.IsImmutableViolation(err) {
		return store.AsCoreError(err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return core.Wrap(core.CodeUnavailable, err, "call interrupted")
	}
	return core.Wrap(core.CodeInternal, err, "internal failure")
}

// resolvePath returns the absolute path of p with every symlink resolved.
func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// allowedPath enforces write.ingest_roots for a remote caller: the resolved
// path must lie inside one resolved root.
func (a *App) allowedPath(path string) (string, error) {
	resolved, err := resolvePath(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", core.Errf(core.CodeNotFound, "path %s not found", path)
	}
	if err != nil {
		return "", core.Wrap(core.CodeInvalidArgument, err, "ingest path")
	}
	for _, root := range a.roots {
		rel, err := filepath.Rel(root, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return resolved, nil
		}
	}
	if len(a.roots) == 0 {
		return "", core.Errf(core.CodePermissionDenied, "ingest over MCP is disabled: configure write.ingest_roots")
	}
	return "", core.Errf(core.CodePermissionDenied, "path %s is outside write.ingest_roots", path)
}
