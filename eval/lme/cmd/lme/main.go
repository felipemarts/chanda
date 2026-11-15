// Command lme is the LongMemEval harness of the Chanda evaluation battery.
// Run it from eval/lme (default paths are relative to that directory):
//
//	lme fetch                                  download the pinned dataset into data/ and check its SHA-256
//	lme stats [-json FILE]                     dataset statistics (DATASET.md)
//	lme sample -seed 42 -n 60 -out ../sample.json
//	lme retrieve -embedder hash [-sample ../sample.json -limit 20] [-conds bm25,rag,chanda]
//	lme contexts -cond B|D|E -sample ../sample.json -embedder hash -out FILE.jsonl [-chanda-set key=value ...]
//	lme embedserve -embedder hash|e5 -addr 127.0.0.1:8089
//	lme chanda serve --config chanda.yaml      the chanda CLI with the caching provider "lme-cached"
//
// No subcommand calls an LLM except "contexts -cond E", which refuses to run
// without -allow-llm.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/felipemarts/chanda-mcp/embedders/hugot"
	"github.com/felipemarts/chanda-mcp/eval/lme/lme"
	"github.com/felipemarts/chanda-mcp/internal/cli"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

const (
	defaultData   = "data/" + lme.DatasetFile
	defaultSample = "../sample.json"
	defaultWork   = "work"
	defaultCache  = "work/embcache.sqlite"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lme fetch|stats|sample|retrieve|contexts|embedserve|chanda ...")
		return 2
	}
	var err error
	switch args[0] {
	case "chanda":
		// The chanda CLI (serve, ...) with the caching provider registered,
		// as embedders/hugot/cmd/chanda-hugot does for "hugot".
		lme.RegisterProvider()
		hugot.Register()
		return cli.Run(ctx, args[1:], os.Stdout, os.Stderr)
	case "fetch":
		err = cmdFetch(ctx, args[1:])
	case "stats":
		err = cmdStats(args[1:])
	case "sample":
		err = cmdSample(args[1:])
	case "retrieve":
		err = cmdRetrieve(ctx, args[1:])
	case "contexts":
		err = cmdContexts(ctx, args[1:])
	case "embedserve":
		err = cmdEmbedServe(ctx, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "lme: unknown command %q\n", args[0])
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lme:", err)
		return 1
	}
	return 0
}

func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// checkData verifies that path is the pinned dataset file.
func checkData(path string) error {
	sum, n, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("%w (run: lme fetch)", err)
	}
	if sum != lme.DatasetSHA256 || n != lme.DatasetSize {
		return fmt.Errorf("%s: sha256 %s (%d bytes), want the pinned %s (%d bytes)", path, sum, n, lme.DatasetSHA256, lme.DatasetSize)
	}
	return nil
}

func cmdFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	out := fs.String("out", defaultData, "destination file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if checkData(*out) == nil {
		fmt.Println("ok", *out, lme.DatasetSHA256)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lme.DatasetURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", lme.DatasetURL, resp.Status)
	}
	tmp := *out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := checkData(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, *out); err != nil {
		return err
	}
	fmt.Println("ok", *out, lme.DatasetSHA256)
	return nil
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	data := fs.String("data", defaultData, "dataset file")
	out := fs.String("json", "", "also write the statistics to this file")
	sample := fs.String("sample", "", "only the questions of this sample file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkData(*data); err != nil {
		return err
	}
	var only map[string]bool
	if *sample != "" {
		s, err := lme.ReadSample(*sample)
		if err != nil {
			return err
		}
		only = map[string]bool{}
		for _, id := range s.IDs() {
			only[id] = true
		}
	}
	st, err := lme.ComputeStatsFor(*data, only)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(b))
	if *out != "" {
		return os.WriteFile(*out, append(b, '\n'), 0o644)
	}
	return nil
}

func cmdSample(args []string) error {
	fs := flag.NewFlagSet("sample", flag.ContinueOnError)
	data := fs.String("data", defaultData, "dataset file")
	seed := fs.Uint64("seed", 42, "PCG seed")
	n := fs.Int("n", 60, "questions to draw")
	minAbs := fs.Int("min-abs", 6, "minimum abstention questions")
	out := fs.String("out", defaultSample, "sample file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkData(*data); err != nil {
		return err
	}
	pop := map[string]string{}
	err := lme.Each(*data, func(q *lme.Question) error {
		if _, dup := pop[q.QuestionID]; dup {
			return fmt.Errorf("duplicate question id %s", q.QuestionID)
		}
		pop[q.QuestionID] = q.QuestionType
		return nil
	})
	if err != nil {
		return err
	}
	s, err := lme.Draw(pop, *n, *minAbs, *seed)
	if err != nil {
		return err
	}
	s.Dataset, s.File, s.Revision, s.SHA256 = lme.DatasetRepo, lme.DatasetFile, lme.DatasetRevision, lme.DatasetSHA256
	s.Generator = fmt.Sprintf("eval/lme: lme sample -seed %d -n %d -min-abs %d (%s)", *seed, *n, *minAbs, runtime.Version())
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d questions, quota %v, abstention %v\n", *out, len(s.Questions), s.QuotaType, s.QuotaAbs)
	return nil
}

// common flags of retrieve and contexts
type common struct {
	data, sample, ids, embedder, e5Dir, embedURL, cache, work, out string
	limit, workers, budget                                         int
	keep                                                           bool
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.data, "data", defaultData, "dataset file")
	fs.StringVar(&c.sample, "sample", "", "run the questions of this sample file, in its order (default: every question of the dataset)")
	fs.StringVar(&c.ids, "ids", "", "comma-separated question ids (instead of -sample)")
	fs.IntVar(&c.limit, "limit", 0, "only the first N selected questions (0 = all)")
	fs.StringVar(&c.embedder, "embedder", "hash", "hash or e5")
	fs.StringVar(&c.e5Dir, "e5-dir", lme.DefaultE5Dir(), "e5-small model directory (in-process e5)")
	fs.StringVar(&c.embedURL, "embed-url", "", "base URL of a running 'lme embedserve' to embed through (required for e5: one process holds the model)")
	fs.StringVar(&c.cache, "cache", "auto", "embedding cache file, or off; auto = "+defaultCache+" for e5, off for hash")
	fs.StringVar(&c.work, "work", "", "work directory for per-question databases (default work/<command>)")
	fs.IntVar(&c.workers, "workers", 4, "questions processed in parallel")
	fs.IntVar(&c.budget, "budget", 2000, "context budget in tokens (ceil(runes/4))")
	fs.BoolVar(&c.keep, "keep", false, "keep per-question Chanda databases")
}

func (c *common) options(cmd string) (*lme.RunOptions, error) {
	if err := checkData(c.data); err != nil {
		return nil, err
	}
	o := &lme.RunOptions{Dataset: c.data, Workers: c.workers, Budget: c.budget, Keep: c.keep, WorkDir: c.work,
		Log: func(f string, a ...any) { log.Printf(f, a...) }}
	if o.WorkDir == "" {
		o.WorkDir = filepath.Join(defaultWork, cmd)
	}
	switch {
	case c.sample != "" && c.ids != "":
		return nil, fmt.Errorf("-sample and -ids are exclusive")
	case c.sample != "":
		s, err := lme.ReadSample(c.sample)
		if err != nil {
			return nil, err
		}
		if s.Revision != lme.DatasetRevision {
			return nil, fmt.Errorf("%s was drawn from revision %s, not the pinned %s", c.sample, s.Revision, lme.DatasetRevision)
		}
		o.IDs = s.IDs()
	case c.ids != "":
		o.IDs = strings.Split(c.ids, ",")
	}
	if c.limit > 0 {
		if o.IDs == nil {
			var all []string
			if err := lme.Each(c.data, func(q *lme.Question) error { all = append(all, q.QuestionID); return nil }); err != nil {
				return nil, err
			}
			o.IDs = all
		}
		o.IDs = o.IDs[:min(c.limit, len(o.IDs))]
	}
	o.Embed = lme.EmbedOptions{Kind: c.embedder, E5Dir: c.e5Dir, URL: c.embedURL}
	switch c.cache {
	case "off", "":
	case "auto":
		if c.embedder == "e5" {
			o.Embed.Cache = defaultCache
		}
	default:
		o.Embed.Cache = c.cache
	}
	if o.Embed.Cache != "" {
		abs, err := filepath.Abs(o.Embed.Cache)
		if err != nil {
			return nil, err
		}
		o.Embed.Cache = abs
	}
	if c.embedder == "e5" && c.embedURL == "" {
		// In process, the harness and every chanda server would each load
		// the 1.5 GB model; at most one e5 process may run on the machine.
		return nil, fmt.Errorf("e5 runs in one process only: start 'lme embedserve -embedder e5 -e5-dir DIR -role query' and pass -embed-url http://ADDR")
	}
	if c.e5Dir != "" {
		abs, err := filepath.Abs(c.e5Dir)
		if err != nil {
			return nil, err
		}
		o.Embed.E5Dir = abs
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	wd, err := filepath.Abs(o.WorkDir)
	if err != nil {
		return nil, err
	}
	o.WorkDir = wd
	o.Chanda = lme.ChandaOptions{Exe: exe, Args: []string{"chanda"}, Embed: o.Embed, Budget: c.budget}
	return o, nil
}

func cmdRetrieve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("retrieve", flag.ContinueOnError)
	var c common
	c.register(fs)
	conds := fs.String("conds", "bm25,rag,chanda", "conditions: bm25, rag, chanda")
	unit := fs.String("bm25-unit", lme.UnitSession, "BM25 document unit: session or turn")
	limit := fs.Int("search-limit", 100, "Chanda search limit (items; Chanda's ceiling is 100)")
	out := fs.String("out", "", "output directory (default out/phase1-<embedder>-<n>)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o, err := c.options("retrieve")
	if err != nil {
		return err
	}
	o.Conds, o.BM25Unit, o.SearchLimit = strings.Split(*conds, ","), *unit, *limit
	emb, err := lme.OpenEmbedder(ctx, o.Embed)
	if err != nil {
		return err
	}
	defer emb.Close()
	start := time.Now()
	qs, err := lme.Phase1(ctx, o, emb)
	if err != nil {
		return err
	}
	wall := time.Since(start)
	hits, misses := emb.Stats()
	dir := *out
	if dir == "" {
		dir = filepath.Join("out", fmt.Sprintf("phase1-%s-%d", c.embedder, len(qs)))
	}
	rep := &lme.Phase1Report{
		Meta: map[string]any{
			"dataset": lme.DatasetRepo + "@" + lme.DatasetRevision + "/" + lme.DatasetFile, "questions": len(qs),
			"selection": selection(c), "embedder": c.embedder, "embed_model": emb.ModelID(), "embed_dim": emb.Dim(),
			"conditions": o.Conds, "bm25_unit": o.BM25Unit, "rag_chunk_chars": lme.ChunkChars, "rag_top_k": lme.RAGTopK,
			"budget_tokens": o.Budget, "token_estimator": "ceil(runes/4)", "chanda_search_limit": o.SearchLimit,
			"workers": o.Workers, "wall_seconds": wall.Seconds(), "harness_cache_hits": hits, "harness_cache_misses": misses,
			"date": time.Now().UTC().Format(time.RFC3339), "go": runtime.Version(), "goos_goarch": runtime.GOOS + "/" + runtime.GOARCH,
			"chanda_search_as_of": "filters.as_of = question day (YYYY-MM-DD, end of day; events dated after it are dropped, decision 0135)",
		},
		Conditions: lme.Summarize(qs, o.Conds),
	}
	if err := lme.WritePhase1(dir, rep, qs, o.Conds); err != nil {
		return err
	}
	fmt.Printf("wrote %s (wall %s)\n", dir, wall.Round(time.Millisecond))
	return nil
}

func selection(c common) string {
	s := "all"
	switch {
	case c.sample != "":
		s = "sample " + c.sample
	case c.ids != "":
		s = "ids"
	}
	if c.limit > 0 {
		s += fmt.Sprintf(", first %d", c.limit)
	}
	return s
}

func cmdContexts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("contexts", flag.ContinueOnError)
	var c common
	c.register(fs)
	cond := fs.String("cond", "", "B (plain RAG), D (Chanda context) or E (Chanda context after sleep_run)")
	out := fs.String("out", "", "output JSONL file")
	llmURL := fs.String("llm-url", "http://127.0.0.1:8787/tag/E-sleep/v1", "E: OpenAI-compatible base URL for Chanda's sleep phase 2 (the cost proxy, tag E-sleep)")
	llmModel := fs.String("llm-model", "deepseek-flash", "E: model name")
	llmKey := fs.String("llm-key-env", "", "E: environment variable holding the API key (empty: none)")
	sleepTokens := fs.Int("sleep-budget-tokens", 0, "E: LLM token ceiling per sleep_run (0: Chanda's default, 200000)")
	sleepEvents := fs.Int("sleep-budget-events", 0, "E: events ceiling per sleep_run (0: Chanda's default, 500)")
	allowLLM := fs.Bool("allow-llm", false, "E: confirm that LLM calls may be made")
	var sets []lme.Override
	fs.Func("chanda-set", "D/E: override a Chanda configuration key in the per-question chanda.yaml, key=value (dotted key, YAML value; repeatable)", func(s string) error {
		ov, err := lme.ParseOverride(s)
		if err == nil {
			sets = append(sets, ov)
		}
		return err
	})
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" || *cond == "" {
		return fmt.Errorf("-cond and -out are required")
	}
	if *cond == lme.CondE && !*allowLLM {
		return fmt.Errorf("condition E calls the LLM through sleep_run: pass -allow-llm (and -llm-url, -llm-model)")
	}
	if len(sets) > 0 && *cond != lme.CondD && *cond != lme.CondE {
		return fmt.Errorf("-chanda-set applies to conditions D and E only")
	}
	o, err := c.options("contexts-" + *cond)
	if err != nil {
		return err
	}
	o.Chanda.LLMURL, o.Chanda.LLMModel, o.Chanda.LLMKeyEnv = *llmURL, *llmModel, *llmKey
	o.Chanda.SleepBudget = chanda.SleepBudget{Tokens: *sleepTokens, Events: *sleepEvents}
	o.Chanda.Set = sets
	emb, err := lme.OpenEmbedder(ctx, o.Embed)
	if err != nil {
		return err
	}
	defer emb.Close()
	if *cond != lme.CondB {
		// Fail before any question if an override names an unknown key or
		// has the wrong type: Chanda's own loader, on the config a server
		// would get (E's admin profile and LLM endpoint included).
		co := o.Chanda
		co.Admin = *cond == lme.CondE
		text, err := co.Config(filepath.Join(o.WorkDir, "check"), emb.ModelID(), emb.Dim())
		if err != nil {
			return err
		}
		h, err := lme.CheckChandaConfig(text)
		if err != nil {
			return fmt.Errorf("chanda configuration with -chanda-set: %w", err)
		}
		log.Printf("chanda config hash %s, overrides %v", h, lme.OverrideStrings(sets))
	}
	lines, err := lme.Contexts(ctx, o, emb, *cond)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, l := range lines {
		j, err := json.Marshal(l)
		if err != nil {
			return err
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d lines\n", *out, len(lines))
	return nil
}

func cmdEmbedServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("embedserve", flag.ContinueOnError)
	embedder := fs.String("embedder", "hash", "hash or e5")
	e5Dir := fs.String("e5-dir", lme.DefaultE5Dir(), "e5-small model directory")
	addr := fs.String("addr", "127.0.0.1:8089", "listen address (loopback only)")
	role := fs.String("role", lme.RoleQuery, "prefix role when a request names none: query or passage")
	cache := fs.String("cache", "auto", "embedding cache file, or off; auto = "+defaultCache+" for e5, off for hash")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *role != lme.RoleQuery && *role != lme.RolePassage {
		return fmt.Errorf("-role must be query or passage")
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr %s: loopback addresses only (no authentication)", *addr)
	}
	o := lme.EmbedOptions{Kind: *embedder, E5Dir: *e5Dir}
	switch *cache {
	case "off", "":
	case "auto":
		if *embedder == "e5" {
			o.Cache = defaultCache
		}
	default:
		o.Cache = *cache
	}
	emb, err := lme.OpenEmbedder(ctx, o)
	if err != nil {
		return err
	}
	defer emb.Close()
	s := &lme.EmbedServer{Emb: emb, DefaultRole: *role}
	srv := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("embedserve: %s (dim %d) on http://%s/v1/embeddings, default role %s", emb.ModelID(), emb.Dim(), *addr, *role)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
