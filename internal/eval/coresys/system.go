// Package coresys runs the real Chanda core (internal/app) as evaluation
// systems C1..C5 and implements the own suites that need a live core
// (deletion, injection, document update, deduplication, determinism,
// learning). It is separate from internal/eval so that the core may import
// the harness (e.g. for the sleep gate) without a cycle.
//
// Every system writes through the public API, with the source kinds of a
// real deployment: corpus documents through ingest (kind document, or tool
// and web for external items), sessions through log_session (tool turns
// enter the external zone), statements asserted in a session through
// assert_fact (or remember when the configuration has no structured facts).
// Retrieval is the context tool at the harness budget. Each system has its
// own database in a temporary directory and a fake clock set to each item's
// date, so two runs give byte-identical databases.
package coresys

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/app"
	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/eval"
	"github.com/felipemarts/chanda-mcp/internal/store"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Namespace is the namespace every evaluation system writes to.
const Namespace = "eval"

// Actors of the evaluation: documents are ingested by an operator (a person
// at the CLI); sessions and assertions come from an in-process agent.
var (
	Operator = chanda.Actor{Name: "eval-operator", Human: true, MaxZone: chanda.ZoneUser}
	Agent    = chanda.Actor{Name: "eval-agent", MaxZone: chanda.ZoneInternal}
)

// Variant describes one C configuration.
type Variant struct {
	Name        string
	Description string
	Reranker    bool // C2+: cross-encoder reranking
	Facts       bool // C3+: deterministic extractors and assert_fact (temporal validity)
	Sleep       bool // C4+: a sleep run after ingestion
	Learning    bool // C5: outcomes from a training task sequence
}

// Variants are C1..C5 as BASE.md defines them.
var Variants = []Variant{
	{Name: "C1", Description: "Chanda: hybrid fusion (FTS5 BM25 + vectors, RRF), no reranker, no structured facts."},
	{Name: "C2", Description: "C1 + cross-encoder reranker.", Reranker: true},
	{Name: "C3", Description: "C2 + structured facts with temporal validity (header and kv_table extractors, assert_fact).", Reranker: true, Facts: true},
	{Name: "C4", Description: "C3 + a sleep run after ingestion.", Reranker: true, Facts: true, Sleep: true},
	{Name: "C5", Description: "C4 + learned ranking from a simulated task sequence.", Reranker: true, Facts: true, Sleep: true, Learning: true},
}

// PortugueseSinglePredicates extends the single-valued predicate catalog
// (facts.single_predicates) with the Portuguese keys a pt-BR corpus uses
// for the same attributes as the English defaults (job title, phone,
// address, price, owner, e-mail, ...). C3+ use it.
var PortugueseSinglePredicates = []string{
	"cargo", "ramal", "telefone", "endereco", "e_mail", "preco", "escritorio", "responsavel",
	"gestor", "fornecedor", "centro_de_custo", "prazo", "status", "valor",
}

// Options returns the app options of v (decision 0094): C1 has no facts,
// sleep or learning; C2 adds the reranker; C3 has facts; C4 sleep; C5
// learning. emb is the shared embedder; rr the reranker (nil: none).
func Options(v Variant, emb core.Embedder, rr core.Reranker) app.Options {
	o := app.Options{Embedder: emb, DisableFacts: !v.Facts, DisableSleep: !v.Sleep, DisableLearning: !v.Learning}
	if v.Reranker {
		o.Reranker = rr
	}
	return o
}

// Configure returns the core configuration of v over base (a copy).
func Configure(base *config.Config, v Variant) *config.Config {
	c := *base
	c.Namespace = Namespace
	c.Read.RerankEnabled = v.Reranker
	if v.Facts {
		c.Write.Extractors = []config.ExtractorConfig{{Name: "header", Kind: "header"}, {Name: "kv_table", Kind: "kv_table"}}
		seen := map[string]bool{}
		var preds []string
		for _, p := range append(append([]string{}, base.Facts.SinglePredicates...), PortugueseSinglePredicates...) {
			if !seen[p] {
				seen[p] = true
				preds = append(preds, p)
			}
		}
		c.Facts.SinglePredicates = preds
	}
	return &c
}

// NoRerankerReason explains why C2 cannot run without a reranker.
const NoRerankerReason = "no cross-encoder reranker is configured in this binary " +
	"(the chanda binary has none; chanda-hugot loads one from embed.reranker_model_path), so the reranking stage cannot run"

// System is one C configuration over a private database.
type System struct {
	v         Variant
	cfg       *config.Config
	dir       string
	app       *app.App
	e         *env.Env
	fc        *clock.Fake
	last      time.Time
	byURI     map[string]string   // document or source uri -> corpus item id
	byEvent   map[string]string   // event id -> corpus item id (session turns, remembered statements)
	events    map[string][]string // corpus item id -> its session turn event ids, in order
	note      string              // declared deviation from the variant
	lastSleep string              // report of the last sleep run
	model     string
}

// Open builds an empty system for v over the evaluation configuration of
// fe, with fe's embedder (the one B2 uses) and, for C2+, fe's reranker.
func Open(ctx context.Context, v Variant, fe eval.FactoryEnv) (*System, error) {
	cfg := Configure(fe.Config, v)
	dir, err := os.MkdirTemp("", "chanda-eval-"+strings.ToLower(v.Name)+"-")
	if err != nil {
		return nil, err
	}
	s, err := openAt(ctx, v, cfg, dir, Options(v, fe.Embedder, fe.Reranker))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

func openAt(ctx context.Context, v Variant, cfg *config.Config, dir string, opts app.Options) (*System, error) {
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		return nil, err
	}
	cfg.DBPath = filepath.Join(dir, "chanda.db")
	cfg.Sleep.SnapshotDir = filepath.Join(dir, "snapshots")
	if err := cfg.Validate(); err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "config")
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	fc := clock.NewFake(env.TestEpoch, time.Millisecond)
	e := env.New(cfg, st, fc)
	a, err := app.NewWith(e, opts)
	if err != nil {
		st.Close()
		return nil, err
	}
	s := &System{v: v, cfg: cfg, dir: dir, app: a, e: e, fc: fc, byURI: map[string]string{}, byEvent: map[string]string{}, events: map[string][]string{}}
	stats, err := a.Stats(ctx, chanda.StatsInput{Namespace: Namespace})
	if err != nil {
		s.Close()
		return nil, err
	}
	s.model = stats.EmbedModel
	return s, nil
}

// Name is the variant name.
func (s *System) Name() string { return s.v.Name }

// Info describes the system for the report.
func (s *System) Info() eval.SystemInfo {
	d := s.v.Description
	if s.note != "" {
		d += " " + s.note
	}
	return eval.SystemInfo{Description: d, EmbeddingModel: s.model, ConfigHash: s.cfg.Hash()}
}

// API returns the core under evaluation.
func (s *System) API() *app.App { return s.app }

// Env returns the environment of the core (suites inspect its tables).
func (s *System) Env() *env.Env { return s.e }

// Declare appends a declared deviation to the description.
func (s *System) Declare(note string) {
	if s.note != "" {
		s.note += " "
	}
	s.note += note
}

// at moves the clock to the item date, never backwards.
func (s *System) at(date string) error {
	t, err := eval.ItemTime(date)
	if err != nil {
		return err
	}
	if t.After(s.last) {
		s.fc.Set(t)
		s.last = t
	}
	return nil
}

func op(ctx context.Context) context.Context    { return chanda.WithActor(ctx, Operator) }
func agent(ctx context.Context) context.Context { return chanda.WithActor(ctx, Agent) }

// Ingest writes one corpus item through the public API.
func (s *System) Ingest(ctx context.Context, it eval.Item) error {
	if err := s.at(it.Date); err != nil {
		return err
	}
	if it.Type == eval.ItemSession {
		return s.ingestSession(ctx, it)
	}
	dir := filepath.Join(s.dir, "docs", fileSafe(it.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(it.Text), 0o644); err != nil {
		return err
	}
	out, err := s.app.Ingest(op(ctx), chanda.IngestInput{Namespace: Namespace, Path: path,
		Source: chanda.SourceRef{Kind: chanda.SourceKind(it.Source), URI: it.URI}})
	if err != nil {
		return err
	}
	for _, d := range out.Documents {
		if d.Action == "rejected" {
			return fmt.Errorf("document %s rejected: %s", it.ID, d.Reason)
		}
		s.byURI[d.URI] = it.ID
	}
	s.byURI[it.URI] = it.ID
	return nil
}

func (s *System) ingestSession(ctx context.Context, it eval.Item) error {
	turns := make([]chanda.Turn, len(it.Turns))
	for i, t := range it.Turns {
		turns[i] = chanda.Turn{Role: t.Role, Content: t.Text, Name: t.Name}
	}
	out, err := s.app.LogSession(agent(ctx), chanda.LogSessionInput{Namespace: Namespace, SessionID: it.URI, Turns: turns})
	if err != nil {
		return err
	}
	s.byURI[it.URI] = it.ID
	for _, t := range out.Turns {
		s.byEvent[t.ID] = it.ID
		s.events[it.ID] = append(s.events[it.ID], t.ID)
	}
	for _, f := range it.Facts {
		if s.v.Facts {
			r, err := s.app.AssertFact(agent(ctx), chanda.AssertFactInput{Namespace: Namespace, Subject: f.Subject,
				Predicate: f.Predicate, Value: f.Value, SessionID: it.URI})
			if err != nil {
				return fmt.Errorf("assert_fact in %s: %w", it.ID, err)
			}
			s.byEvent[r.ID] = it.ID
			continue
		}
		r, err := s.app.Remember(agent(ctx), chanda.RememberInput{Namespace: Namespace, Content: f.Text(), SessionID: it.URI})
		if err != nil {
			return fmt.Errorf("remember in %s: %w", it.ID, err)
		}
		s.byEvent[r.ID] = it.ID
	}
	return nil
}

// Flush drains the embedding queue (timed as part of ingestion), then runs
// the post-ingestion steps of the variant.
func (s *System) Flush(ctx context.Context) error {
	if err := s.flush(ctx); err != nil {
		return err
	}
	if s.v.Sleep {
		return s.sleep(ctx)
	}
	return nil
}

func (s *System) flush(ctx context.Context) error {
	for range 1000 {
		out, err := s.app.Flush(agent(ctx), chanda.FlushInput{Namespace: Namespace})
		if err != nil {
			return err
		}
		if out.Complete {
			return nil
		}
	}
	return fmt.Errorf("embedding queue did not drain")
}

// sleep runs the maintenance phase once. Without an LLM provider only phase
// 1 can run; the report declares it. The clock first moves past the
// write-burst window (sleep.burst_seconds), as a real idle period would.
func (s *System) sleep(ctx context.Context) error {
	s.queryTime()
	s.fc.Advance(time.Duration(s.cfg.Sleep.BurstSeconds+60) * time.Second)
	phases := []int{1}
	if s.cfg.LLM.Endpoint != "" {
		phases = []int{1, 2}
	}
	out, err := s.app.SleepRun(op(ctx), chanda.SleepRunInput{Namespace: Namespace, Phases: phases})
	if err != nil {
		return err
	}
	s.Declare(fmt.Sprintf("Sleep run: phases %v, status %s.", out.Phases, out.Status))
	s.lastSleep = out.Report
	return s.flush(ctx)
}

// sleepReport runs sleep and returns the ranker line of its report.
func (s *System) sleepReport(ctx context.Context) (string, error) {
	if err := s.sleep(ctx); err != nil {
		return "", err
	}
	for _, l := range strings.Split(s.lastSleep, "\n") {
		if strings.Contains(l, "Ranker:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")), nil
		}
	}
	return "no ranker step in the sleep report", nil
}

// queryTime moves the clock one day past the last item, so retrievals are
// dated after every write.
func (s *System) queryTime() {
	t := s.last.Add(24 * time.Hour)
	if now := s.fc.Now(); now.After(t) {
		t = now
	}
	s.fc.Set(t)
}

// Retrieve calls the context tool at the harness budget and returns its
// items in order, each with the corpus item it comes from.
func (s *System) Retrieve(ctx context.Context, q eval.Query, budget int) ([]eval.Retrieved, error) {
	_, rs, err := s.Context(ctx, q, budget)
	return rs, err
}

// Context is Retrieve that also returns the retrieval id (for outcomes).
func (s *System) Context(ctx context.Context, q eval.Query, budget int) (string, []eval.Retrieved, error) {
	s.queryTime()
	in := chanda.ContextInput{Namespace: Namespace, Query: q.Text, BudgetTokens: budget}
	if !q.AsOf.IsZero() {
		in.AsOf = q.AsOf.UTC().Format(time.RFC3339)
	}
	out, err := s.app.Context(agent(ctx), in)
	if err != nil {
		return "", nil, err
	}
	rs := make([]eval.Retrieved, 0, len(out.Items))
	for _, it := range out.Items {
		rs = append(rs, eval.Retrieved{ID: it.ID, DocID: s.docOf(it), Text: it.Text, Score: it.Score})
	}
	return out.RetrievalID, rs, nil
}

// docOf maps a delivered record to the corpus item it came from: by event
// id (session turns and statements), else by document uri, else by source uri.
func (s *System) docOf(it chanda.Item) string {
	for _, id := range append([]string{it.ID}, it.Origin.EventIDs...) {
		if d, ok := s.byEvent[id]; ok {
			return d
		}
	}
	if d, ok := s.byURI[it.Origin.DocURI]; ok {
		return d
	}
	return s.byURI[it.Origin.SourceURI]
}

// SizeBytes is the database size.
func (s *System) SizeBytes(ctx context.Context) (int64, error) {
	out, err := s.app.Stats(agent(ctx), chanda.StatsInput{Namespace: Namespace})
	return out.DBSizeBytes, err
}

// Checkpoint folds the WAL into the database file and returns its bytes.
func (s *System) Checkpoint(ctx context.Context) ([]byte, error) {
	if err := s.e.Store.Checkpoint(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(s.cfg.DBPath)
}

// Close stops the core and removes the temporary directory.
func (s *System) Close() error {
	err := s.app.Close()
	if cerr := s.e.Store.Close(); err == nil {
		err = cerr
	}
	if s.dir != "" {
		os.RemoveAll(s.dir)
	}
	return err
}

func fileSafe(id string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, id)
}
