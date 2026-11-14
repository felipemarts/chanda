package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Options configures one evaluation run.
type Options struct {
	Dataset *Dataset
	Systems []System // fresh (empty) systems; Run ingests the corpus into each
	Budget  int      // context tokens per query, equal for all systems; 0 = Config.Read.ContextDefaultTokens
	OutDir  string   // when set, Run writes <OutDir>/<Name>.md and .json
	Name    string   // report name; default "report"

	Config   *config.Config // its Hash() is the report's configuration hash; default config.Default()
	Embedder core.Embedder  // the embedding model shared by the compared systems (model_id is reported); nil = n/a
	Clock    core.Clock     // report date; default clock.System
	Timer    core.Clock     // latency stopwatch; default clock.System
	Version  string         // Chanda version; default "dev"
	Suites   *Registry      // default NewRegistry()
	Hardware *Hardware      // default DetectHardware()
}

func (o Options) withDefaults() Options {
	if o.Config == nil {
		o.Config = config.Default()
	}
	if o.Budget <= 0 {
		o.Budget = o.Config.Read.ContextDefaultTokens
	}
	if o.Name == "" {
		o.Name = "report"
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Timer == nil {
		o.Timer = clock.System{}
	}
	if o.Version == "" {
		o.Version = "dev"
	}
	if o.Suites == nil {
		o.Suites = NewRegistry()
	}
	if o.Hardware == nil {
		h := DetectHardware()
		o.Hardware = &h
	}
	return o
}

// Run ingests the dataset into every system, retrieves every question at the
// same budget, judges the results, runs the own suites and builds the report.
// Systems run one after the other, in the given order; Run does not close them.
func Run(ctx context.Context, o Options) (*Report, error) {
	o = o.withDefaults()
	if o.Dataset == nil || len(o.Systems) == 0 {
		return nil, core.Errf(core.CodeInvalidArgument, "eval: a dataset and at least one system are required")
	}
	seen := map[string]bool{}
	for _, s := range o.Systems {
		if seen[s.Name()] {
			return nil, core.Errf(core.CodeInvalidArgument, "eval: duplicate system name %q", s.Name())
		}
		seen[s.Name()] = true
	}
	rep := newReport(o)
	for _, sys := range o.Systems {
		sr, lat, err := runSystem(ctx, o, sys)
		if err != nil {
			return nil, fmt.Errorf("eval: system %s: %w", sys.Name(), err)
		}
		rep.Systems = append(rep.Systems, sr)
		rep.Latency = append(rep.Latency, lat)
		for _, s := range o.Suites.Suites() {
			if s.PerSystem {
				rep.Suites = append(rep.Suites, runSuite(ctx, s, SuiteInput{System: sys, Dataset: o.Dataset, Budget: o.Budget, Results: sr.Questions}))
			}
		}
	}
	for _, s := range o.Suites.Suites() {
		if !s.PerSystem {
			rep.Suites = append(rep.Suites, runSuite(ctx, s, SuiteInput{Dataset: o.Dataset, Budget: o.Budget}))
		}
	}
	rep.Losses = losses(rep)
	if o.OutDir != "" {
		if err := rep.Write(o.OutDir, o.Name); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func runSystem(ctx context.Context, o Options, sys System) (SystemResult, Latency, error) {
	lat := Latency{System: sys.Name(), IngestItems: len(o.Dataset.Corpus)}
	var ingest []time.Duration
	start := o.Timer.Now()
	for _, it := range o.Dataset.Corpus {
		t0 := o.Timer.Now()
		if err := sys.Ingest(ctx, it); err != nil {
			return SystemResult{}, lat, fmt.Errorf("ingest %s v%d: %w", it.ID, it.Version, err)
		}
		ingest = append(ingest, o.Timer.Now().Sub(t0))
	}
	if f, ok := sys.(Flusher); ok {
		t0 := o.Timer.Now()
		if err := f.Flush(ctx); err != nil {
			return SystemResult{}, lat, fmt.Errorf("flush: %w", err)
		}
		lat.FlushMs = ms(o.Timer.Now().Sub(t0))
	}
	lat.IngestTotalMs = ms(o.Timer.Now().Sub(start))
	lat.IngestP50Ms, lat.IngestP95Ms = ms(Percentile(ingest, 50)), ms(Percentile(ingest, 95))

	var retrieve []time.Duration
	results, err := judgeAll(ctx, sys, o.Dataset, o.Budget, o.Timer, func(d time.Duration) { retrieve = append(retrieve, d) })
	if err != nil {
		return SystemResult{}, lat, err
	}
	lat.RetrieveP50Ms, lat.RetrieveP95Ms = ms(Percentile(retrieve, 50)), ms(Percentile(retrieve, 95))
	var total time.Duration
	for _, d := range retrieve {
		total += d
	}
	if len(retrieve) > 0 {
		lat.RetrieveMeanMs = ms(total / time.Duration(len(retrieve)))
	}

	sr := SystemResult{Name: sys.Name(), Info: SystemInfo{EmbeddingModel: "n/a"}, Questions: results}
	if d, ok := sys.(Describer); ok {
		sr.Info = d.Info()
	}
	if s, ok := sys.(Sizer); ok {
		n, err := s.SizeBytes(ctx)
		if err != nil {
			return SystemResult{}, lat, fmt.Errorf("size: %w", err)
		}
		sr.SizeBytes = &n
	}
	sr.Overall = Aggregate(results)
	sr.ByTag = map[string]Metrics{}
	for _, tag := range datasetTags(o.Dataset) {
		var sub []QuestionResult
		for i, q := range o.Dataset.Questions {
			if questionTag(q, tag) {
				sub = append(sub, results[i])
			}
		}
		sr.ByTag[tag] = Aggregate(sub)
	}
	return sr, lat, nil
}

// judgeAll retrieves and judges every question of ds with sys, in dataset
// order. When timer is set, onLatency receives each retrieval's duration.
func judgeAll(ctx context.Context, sys System, ds *Dataset, budget int, timer core.Clock, onLatency func(time.Duration)) ([]QuestionResult, error) {
	uris := map[string]string{}
	for _, it := range ds.Corpus {
		uris[it.URI] = it.ID
	}
	out := make([]QuestionResult, 0, len(ds.Questions))
	for _, q := range ds.Questions {
		query := Query{ID: q.ID, Text: q.Question}
		if q.AsOf != "" {
			t, err := ItemTime(q.AsOf)
			if err != nil {
				return nil, err
			}
			query.AsOf = t
		}
		var t0 time.Time
		if timer != nil {
			t0 = timer.Now()
		}
		rs, err := sys.Retrieve(ctx, query, budget)
		if err != nil {
			return nil, fmt.Errorf("retrieve %s: %w", q.ID, err)
		}
		if timer != nil && onLatency != nil {
			onLatency(timer.Now().Sub(t0))
		}
		for i := range rs {
			if rs[i].DocID == "" {
				rs[i].DocID = uris[rs[i].URI]
			}
		}
		kept, dropped := EnforceBudget(rs, budget)
		r := Judge(q, kept)
		r.BudgetViolations = dropped
		out = append(out, r)
	}
	return out, nil
}

// TagPlain groups questions that carry no tag.
const TagPlain = "plain"

func questionTag(q Question, tag string) bool {
	if tag == TagPlain {
		return len(q.Tags) == 0
	}
	return q.HasTag(tag)
}

// datasetTags lists the tags present in ds, sorted, plus TagPlain if any
// question is untagged.
func datasetTags(ds *Dataset) []string {
	m := map[string]bool{}
	for _, q := range ds.Questions {
		if len(q.Tags) == 0 {
			m[TagPlain] = true
		}
		for _, t := range q.Tags {
			m[t] = true
		}
	}
	return sortedKeys(m)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// Write stores the report as <dir>/<name>.md and <dir>/<name>.json.
func (r *Report) Write(dir, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	js, err := r.JSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), js, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(r.Markdown()), 0o644)
}
