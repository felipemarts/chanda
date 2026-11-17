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

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/dedup"
	"github.com/felipemarts/chanda/internal/embed"
	"github.com/felipemarts/chanda/internal/env"
	"github.com/felipemarts/chanda/internal/facts"
	"github.com/felipemarts/chanda/internal/llm"
	"github.com/felipemarts/chanda/internal/rank"
	"github.com/felipemarts/chanda/internal/read"
	"github.com/felipemarts/chanda/internal/sleep"
	"github.com/felipemarts/chanda/internal/store"
	"github.com/felipemarts/chanda/internal/write"
	"github.com/felipemarts/chanda/pkg/chanda"
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
	sleeper *sleep.Sleeper   // nil with Options.DisableSleep
	llm     core.LLMProvider // sleep phase 2 and agentic search; nil without llm.endpoint
	roots   []string         // resolved write.ingest_roots
	opt     Options

	mu     sync.Mutex
	cancel context.CancelFunc
	done   <-chan struct{}
}

var _ chanda.API = (*App)(nil)

// Options select the parts of the system an App runs. The zero value is
// the full system; the evaluation configurations of BASE.md are subsets:
//
//	C1 hybrid search, no reranker   Options{DisableFacts: true, DisableSleep: true, DisableLearning: true}
//	C2 C1 plus the reranker         C1 with Reranker set (and read.rerank_enabled: true)
//	C3 C2 plus facts                Options{Reranker: rr, DisableSleep: true, DisableLearning: true}
//	C4 C3 plus sleep                Options{Reranker: rr, DisableLearning: true}
//	C5 C4 plus learned ranking      Options{Reranker: rr}
type Options struct {
	// DisableVectors makes the read path keyword only (no query embedding,
	// no vector search). The write path still queues embeddings.
	DisableVectors bool
	// DisableFacts turns off deterministic fact extraction on write and
	// refuses assert_fact with CodeUnavailable; no fact is ever created.
	DisableFacts bool
	// DisableSleep refuses sleep_run with CodeUnavailable and never starts
	// the sleep scheduler.
	DisableSleep bool
	// DisableLearning keeps λ at 0 (the read order is exactly the
	// pre-learning order) and skips training and promotion in sleep.
	// Outcomes and flags are still recorded.
	DisableLearning bool

	// Embedder replaces the embedder built from cfg.Embed (nil: from config).
	Embedder core.Embedder
	// Reranker is the cross-encoder of read step 6 (nil: no rerank). It is
	// used only when read.rerank_enabled is true.
	Reranker core.Reranker
	// LLM replaces the provider built from cfg.LLM for sleep phase 2 and
	// agentic search (nil: llm.FromConfig; tests pass an llm.Fake).
	LLM core.LLMProvider
	// Logf receives background errors (sleep scheduler); nil discards them.
	Logf func(format string, args ...any)
}

// Open opens cfg.DBPath with the system clock and wires an App that owns the
// store. Call Start to run the background embedding indexer, Close to stop.
func Open(ctx context.Context, cfg *config.Config) (*App, error) {
	return OpenWith(ctx, cfg, Options{})
}

// OpenWith is Open with options.
func OpenWith(ctx context.Context, cfg *config.Config, o Options) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "config")
	}
	e, err := env.Open(ctx, cfg)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "open database")
	}
	a, err := NewWith(e, o)
	if err != nil {
		e.Store.Close()
		return nil, err
	}
	a.owns = true
	return a, nil
}

// New wires the full App over an existing Env (tests, determinism). The
// caller keeps ownership of the store.
func New(e *env.Env) (*App, error) { return NewWith(e, Options{}) }

// NewWith wires an App with options over an existing Env. The caller keeps
// ownership of the store.
func NewWith(e *env.Env, o Options) (*App, error) {
	cfg := e.Cfg
	if err := tuneStore(e); err != nil {
		return nil, err
	}
	if err := facts.ValidateExtractors(cfg.Write.Extractors); err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "write.extractors")
	}
	emb := o.Embedder
	if emb == nil {
		var err error
		if emb, err = embed.NewFromConfig(cfg.Embed); err != nil {
			return nil, err
		}
	}
	a := &App{e: e, facts: facts.New(e), guard: dedup.New(e), emb: emb, ranker: rank.New(e), opt: o, llm: o.LLM}
	if a.llm == nil {
		p, err := llm.FromConfig(cfg.LLM, os.Getenv)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, "llm")
		}
		a.llm = p
	}
	for _, r := range cfg.Write.IngestRoots {
		rr, err := resolvePath(r)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, "write.ingest_roots: "+r)
		}
		a.roots = append(a.roots, rr)
	}
	a.index = embed.NewExactIndex(e)
	a.indexer = embed.NewIndexer(e, emb, a.index)
	var err error
	if a.writer, err = write.New(e, write.EventHookFunc(a.afterEvent)); err != nil {
		return nil, err
	}
	deps := read.Deps{Embedder: emb, VectorIndex: a.index, Reranker: o.Reranker}
	if o.DisableVectors {
		deps.Embedder, deps.VectorIndex = nil, nil
	}
	if !o.DisableLearning {
		deps.Utility, deps.Term, deps.Learned = a.ranker, termAdapter{a.ranker}, learnedAdapter{a.ranker.Learned()}
	}
	a.reader = read.New(e, deps)
	if !o.DisableSleep {
		if a.sleeper, err = a.newSleeper(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Options returns the options the App was wired with.
func (a *App) Options() Options { return a.opt }

// Env returns the environment the App runs on.
func (a *App) Env() *env.Env { return a.e }

// Indexer returns the embedding indexer (flush, determinism tests).
func (a *App) Indexer() *embed.Indexer { return a.indexer }

// Start runs the background loops until ctx ends or Close is called: the
// embedding indexer and, when sleep.scheduler is on and sleep is not
// disabled, the sleep scheduler (BASE.md "When it runs"). Calling it twice
// does nothing.
func (a *App) Start(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return
	}
	ctx, a.cancel = context.WithCancel(ctx)
	indexed := a.indexer.Start(ctx)
	done := make(chan struct{})
	a.done = done
	scheduler := a.sleeper != nil && a.e.Cfg.Sleep.Scheduler
	if a.sleeper != nil && !scheduler {
		// The scheduler recovers stale runs itself when it starts.
		if _, err := a.sleeper.RecoverStale(ctx); err != nil && a.opt.Logf != nil {
			a.opt.Logf("sleep: recover stale runs: %v", err)
		}
	}
	go func() {
		defer close(done)
		if scheduler {
			if err := a.sleeper.Start(ctx); err != nil && a.opt.Logf != nil {
				a.opt.Logf("sleep scheduler: %v", err)
			}
		}
		<-indexed
	}()
}

// Sleeper returns the sleep runner (nil with Options.DisableSleep).
func (a *App) Sleeper() *sleep.Sleeper { return a.sleeper }

// Close stops the background loops and, when the App opened it, closes the store.
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
