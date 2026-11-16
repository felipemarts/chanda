package main

// lme study: the sleep study's steps (eval/sleep/PROTOCOL.md,
// eval/sleep/README.md). Every step works on a study directory (-db DIR)
// whose chanda.db persists between steps; each step starts its own Chanda
// server on it and closes it before returning. No step calls an LLM except
// "sleep" with -llm-url, which needs -allow-llm.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/felipemarts/chanda/eval/lme/lme"
	"github.com/felipemarts/chanda/pkg/chanda"
)

const studyUsage = `usage: lme study <step> [flags]

  ingest     -db DIR (-locomo FILE -conv ID | -data FILE -question QID | -suite pairs.json -pair ID)
  ask        -db DIR -out FILE.jsonl [-split FILE -half use|test|all] [-condition LABEL] [-limit N]
  outcome    -db DIR -ask FILE.jsonl -answers FILE.jsonl [-out FILE.jsonl]
  copy       -from DIR -to DIR
  sleep      -db DIR -out FILE.json [-phases 1|1,2] [-budget-tokens N] [-budget-events N] [-llm-url URL -allow-llm]
  rag        -locomo FILE -conv ID -split FILE -half test -out FILE.jsonl
  fichas     -db DIR [-out FILE.json]        pages and items of a closed database (read-only)
  leakcheck  -db DIR [-split FILE] [-before-test] [-out FILE.json]
             -protocol learn -db DIR -split FILE [-key-table NAME] [-out FILE.json]   (eval/learn/PROTOCOL.md)
             -protocol conflicts -db DIR [-out FILE.json]   before the directory's question is asked (eval/conflicts/PROTOCOL.md)
  keys       -db DIR -action rebuild|stats|reset|enable|disable [-out FILE.json]   admin tool learned_keys (decision 0145)
  conflicts  -db DIR -action scan|stats|list|reset [-details] [-min-cosine X] [-write] [-out FILE.json]
             admin tool conflicts (decision 0148); prints the tool's output as returned
  check      [-set k=v ...] [-admin] [-llm-url URL]   validate a configuration; prints its hash

Common: -embedder hash|e5 -embed-url URL -e5-dir DIR -cache auto|off|FILE -budget 2000
        -set key=value (repeatable) -set-file FILE (one key=value per line, # comments)`

func cmdStudy(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, studyUsage)
		return flag.ErrHelp
	}
	step, rest := args[0], args[1:]
	switch step {
	case "ingest":
		return studyIngest(ctx, rest)
	case "ask":
		return studyAsk(ctx, rest)
	case "outcome":
		return studyOutcome(ctx, rest)
	case "copy":
		return studyCopy(rest)
	case "sleep":
		return studySleep(ctx, rest)
	case "rag":
		return studyRAG(ctx, rest)
	case "fichas":
		return studyFichas(ctx, rest)
	case "leakcheck":
		return studyLeak(ctx, rest)
	case "keys":
		return studyKeys(ctx, rest)
	case "conflicts":
		return studyConflicts(ctx, rest)
	case "check":
		return studyCheck(ctx, rest)
	case "help", "-h", "--help":
		fmt.Fprintln(os.Stdout, studyUsage)
		return nil
	}
	return fmt.Errorf("study: unknown step %q\n%s", step, studyUsage)
}

// studyFlags are the flags every server-starting step shares.
type studyFlags struct {
	db, embedder, embedURL, e5Dir, cache, setFile string
	budget                                        int
	sets                                          []lme.Override
}

func (f *studyFlags) register(fs *flag.FlagSet, needDB bool) {
	if needDB {
		fs.StringVar(&f.db, "db", "", "study directory (holds chanda.db and study.json)")
	}
	fs.StringVar(&f.embedder, "embedder", "hash", "hash or e5 (must match the one the directory was ingested with)")
	fs.StringVar(&f.embedURL, "embed-url", "", "base URL of a running 'lme embedserve' (required for e5)")
	fs.StringVar(&f.e5Dir, "e5-dir", lme.DefaultE5Dir(), "e5-small model directory")
	fs.StringVar(&f.cache, "cache", "auto", "embedding cache file, or off; auto = "+defaultCache+" for e5, off for hash")
	fs.IntVar(&f.budget, "budget", 2000, "context budget in tokens")
	fs.StringVar(&f.setFile, "set-file", "", "file of key=value overrides, one per line (# comments); applied before -set")
	fs.Func("set", "override a Chanda configuration key, key=value (dotted key, YAML value; repeatable)", func(s string) error {
		ov, err := lme.ParseOverride(s)
		if err == nil {
			f.sets = append(f.sets, ov)
		}
		return err
	})
}

func readSetFile(path string) ([]lme.Override, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []lme.Override
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ov, err := lme.ParseOverride(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		out = append(out, ov)
	}
	return out, nil
}

func (f *studyFlags) embedOptions() (lme.EmbedOptions, error) {
	o := lme.EmbedOptions{Kind: f.embedder, URL: f.embedURL}
	switch f.cache {
	case "off", "":
	case "auto":
		if f.embedder == "e5" {
			o.Cache = defaultCache
		}
	default:
		o.Cache = f.cache
	}
	if o.Cache != "" {
		abs, err := filepath.Abs(o.Cache)
		if err != nil {
			return o, err
		}
		o.Cache = abs
	}
	if f.e5Dir != "" {
		abs, err := filepath.Abs(f.e5Dir)
		if err != nil {
			return o, err
		}
		o.E5Dir = abs
	}
	if f.embedder == "e5" && f.embedURL == "" {
		return o, fmt.Errorf("e5 runs in one process only: start 'lme embedserve -embedder e5' and pass -embed-url")
	}
	return o, nil
}

// options builds the study options; the embedder is opened to learn its
// model id and dimension.
func (f *studyFlags) options(ctx context.Context) (*lme.StudyOptions, *lme.Cached, error) {
	eo, err := f.embedOptions()
	if err != nil {
		return nil, nil, err
	}
	if f.setFile != "" {
		pre, err := readSetFile(f.setFile)
		if err != nil {
			return nil, nil, err
		}
		f.sets = append(pre, f.sets...)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	emb, err := lme.OpenEmbedder(ctx, eo)
	if err != nil {
		return nil, nil, err
	}
	o := &lme.StudyOptions{Budget: f.budget, Log: func(s string, a ...any) { log.Printf(s, a...) },
		Chanda: lme.ChandaOptions{Exe: exe, Args: []string{"chanda"}, Embed: eo, Budget: f.budget, Set: f.sets}}
	return o, emb, nil
}

func (f *studyFlags) dir() (string, error) {
	if f.db == "" {
		return "", fmt.Errorf("-db is required")
	}
	return filepath.Abs(f.db)
}

// checkMeta refuses an embedder other than the directory's.
func checkMeta(m *lme.StudyMeta, emb *lme.Cached) error {
	if m.EmbedModel != emb.ModelID() || m.EmbedDim != emb.Dim() {
		return fmt.Errorf("the study directory was ingested with %s (dim %d), this step runs %s (dim %d)", m.EmbedModel, m.EmbedDim, emb.ModelID(), emb.Dim())
	}
	return nil
}

func studyIngest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study ingest", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	locomo := fs.String("locomo", "", "LoCoMo locomo10.json")
	conv := fs.String("conv", "", "LoCoMo sample_id (e.g. conv-30)")
	data := fs.String("data", "", "LongMemEval file (longmemeval_s_cleaned.json)")
	qid := fs.String("question", "", "LongMemEval question id")
	suite := fs.String("suite", "", "conflict suite pairs.json (eval/conflicts/suite)")
	pair := fs.String("pair", "", "suite pair id (u01 ... c20)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	m := &lme.StudyMeta{Embedder: f.embedder, EmbedModel: emb.ModelID(), EmbedDim: emb.Dim(), Steps: []lme.StudyStep{}}
	switch {
	case *locomo != "" && *conv != "" && *data == "":
		p, err := filepath.Abs(*locomo)
		if err != nil {
			return err
		}
		sum, _, err := lme.FileSHA256(p)
		if err != nil {
			return err
		}
		if sum != lme.LocomoSHA256 {
			return fmt.Errorf("%s: sha256 %s, want the pinned %s (eval/locomo/SOURCE.md)", p, sum, lme.LocomoSHA256)
		}
		convs, err := lme.LoadLocomo(p)
		if err != nil {
			return err
		}
		c, err := lme.FindLocomo(convs, *conv)
		if err != nil {
			return err
		}
		m.Dataset, m.DatasetSHA256 = p, sum
		if err := lme.StudyIngestLocomo(ctx, o, dir, c, m); err != nil {
			return err
		}
	case *suite != "" && *pair != "" && *data == "" && *locomo == "":
		p, err := filepath.Abs(*suite)
		if err != nil {
			return err
		}
		sp, sum, err := lme.LoadSuitePair(p, *pair)
		if err != nil {
			return err
		}
		m.Dataset, m.DatasetSHA256 = p, sum
		if err := lme.StudyIngestPair(ctx, o, dir, sp, m); err != nil {
			return err
		}
	case *data != "" && *qid != "" && *locomo == "":
		p, err := filepath.Abs(*data)
		if err != nil {
			return err
		}
		if err := checkData(p); err != nil {
			return err
		}
		q, err := findLME(p, *qid)
		if err != nil {
			return err
		}
		m.Dataset, m.DatasetSHA256 = p, lme.DatasetSHA256
		if err := lme.StudyIngestLME(ctx, o, dir, q, m); err != nil {
			return err
		}
	default:
		return fmt.Errorf("give -locomo FILE -conv ID, -data FILE -question QID, or -suite FILE -pair ID")
	}
	fmt.Printf("ingested %s: %d turns mapped to events\n", dir, len(m.Turns))
	return nil
}

func findLME(path, id string) (*lme.Question, error) {
	var found *lme.Question
	err := lme.Select(path, map[string]bool{id: true}, func(q *lme.Question) error { found = q; return nil })
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%s: no question %s", path, id)
	}
	return found, nil
}

// studyQuestions returns the questions a directory is asked: the split's
// half of its LoCoMo conversation, or its LongMemEval question.
func studyQuestions(m *lme.StudyMeta, splitPath, half string, limit int) ([]lme.StudyQuestion, error) {
	var qs []lme.StudyQuestion
	switch m.Source {
	case lme.SourceLocomo:
		if splitPath == "" {
			return nil, fmt.Errorf("-split is required for a LoCoMo directory")
		}
		c, err := lme.LoadStudyLocomo(m)
		if err != nil {
			return nil, err
		}
		sp, err := lme.ReadLocomoSplit(splitPath)
		if err != nil {
			return nil, err
		}
		if sp.SHA256 != m.DatasetSHA256 {
			return nil, fmt.Errorf("%s was drawn from %s, the directory from %s", splitPath, sp.SHA256, m.DatasetSHA256)
		}
		if qs, err = lme.LocomoStudyQuestions(c, sp, half); err != nil {
			return nil, err
		}
	case lme.SourceLongMemEval:
		if err := checkData(m.Dataset); err != nil {
			return nil, err
		}
		q, err := findLME(m.Dataset, m.QuestionID)
		if err != nil {
			return nil, err
		}
		qs = []lme.StudyQuestion{lme.LMEStudyQuestion(q)}
	case lme.SourceSuite:
		return nil, fmt.Errorf("a conflict suite directory has no question")
	default:
		return nil, fmt.Errorf("unknown study source %q", m.Source)
	}
	if limit > 0 && limit < len(qs) {
		qs = qs[:limit]
	}
	return qs, nil
}

func studyAsk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study ask", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	out := fs.String("out", "", "output JSONL")
	split := fs.String("split", "", "LoCoMo split file (eval/sleep/split-locomo.json)")
	half := fs.String("half", lme.HalfAll, "LoCoMo half: use, test or all")
	cond := fs.String("condition", "D", "condition label written on each line (answer.py checks it)")
	limit := fs.Int("limit", 0, "only the first N questions (dry runs)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	m, err := lme.ReadStudyMeta(dir)
	if err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	if err := checkMeta(m, emb); err != nil {
		return err
	}
	qs, err := studyQuestions(m, *split, *half, *limit)
	if err != nil {
		return err
	}
	lines, err := lme.StudyAsk(ctx, o, dir, *cond, qs)
	if err != nil {
		return err
	}
	if err := lme.WriteJSONL(*out, lines); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d lines\n", *out, len(lines))
	return nil
}

func studyOutcome(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study outcome", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	ask := fs.String("ask", "", "ask JSONL of this directory (retrieval ids and delivered items)")
	answers := fs.String("answers", "", "judged answers JSONL (question_id, answer, verdict)")
	out := fs.String("out", "", "write the recorded outcomes here (JSONL)")
	allowMissing := fs.Bool("allow-missing", false, "skip ask lines with no answer instead of failing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	if *ask == "" || *answers == "" {
		return fmt.Errorf("-ask and -answers are required")
	}
	m, err := lme.ReadStudyMeta(dir)
	if err != nil {
		return err
	}
	lines, err := lme.ReadJSONL[*lme.StudyAskLine](*ask)
	if err != nil {
		return err
	}
	as, err := lme.ReadJSONL[lme.StudyAnswer](*answers)
	if err != nil {
		return err
	}
	byID := map[string]lme.StudyAnswer{}
	for _, a := range as {
		if _, dup := byID[a.QuestionID]; dup {
			return fmt.Errorf("%s: question %s answered twice", *answers, a.QuestionID)
		}
		byID[a.QuestionID] = a
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	if err := checkMeta(m, emb); err != nil {
		return err
	}
	recs, err := lme.StudyOutcome(ctx, o, dir, lines, byID, *allowMissing)
	if *out != "" && len(recs) > 0 {
		if werr := lme.WriteJSONL(*out, recs); werr != nil && err == nil {
			err = werr
		}
	}
	if err != nil {
		return err
	}
	pos := 0
	for _, r := range recs {
		if r.Value > 0 {
			pos++
		}
	}
	fmt.Printf("recorded %d outcomes on %s (%d positive)\n", len(recs), dir, pos)
	return nil
}

func studyCopy(args []string) error {
	fs := flag.NewFlagSet("study copy", flag.ContinueOnError)
	from := fs.String("from", "", "closed study directory")
	to := fs.String("to", "", "new study directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return fmt.Errorf("-from and -to are required")
	}
	if err := lme.StudyCopy(*from, *to); err != nil {
		return err
	}
	fmt.Printf("copied %s -> %s\n", *from, *to)
	return nil
}

func parsePhases(s string) ([]int, error) {
	var out []int
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || (n != 1 && n != 2) {
			return nil, fmt.Errorf("-phases %q: want 1 or 1,2", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func studySleep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study sleep", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	out := fs.String("out", "", "sleep result JSON")
	cond := fs.String("condition", "", "condition label (S0, S1, ...)")
	phases := fs.String("phases", "1,2", "phases: 1, or 1,2")
	bt := fs.Int("budget-tokens", 0, "sleep budget, LLM tokens (0: Chanda's default)")
	be := fs.Int("budget-events", 0, "sleep budget, events (0: Chanda's default)")
	llmURL := fs.String("llm-url", "", "OpenAI-compatible base URL for phase 2 (the cost proxy with a tag path); empty: no LLM configured")
	llmModel := fs.String("llm-model", "deepseek-flash", "model name")
	llmKey := fs.String("llm-key-env", "", "environment variable holding the API key (empty: none; the proxy holds the key)")
	allowLLM := fs.Bool("allow-llm", false, "confirm that LLM calls may be made (required with -llm-url)")
	allowSkip := fs.Bool("allow-phase2-skip", false, "accept a run whose requested phase 2 was skipped (default: fail)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	if *llmURL != "" && !*allowLLM {
		return fmt.Errorf("-llm-url makes paid calls possible: pass -allow-llm")
	}
	ph, err := parsePhases(*phases)
	if err != nil {
		return err
	}
	m, err := lme.ReadStudyMeta(dir)
	if err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	if err := checkMeta(m, emb); err != nil {
		return err
	}
	o.Chanda.LLMURL, o.Chanda.LLMModel, o.Chanda.LLMKeyEnv = *llmURL, *llmModel, *llmKey
	res, err := lme.StudySleep(ctx, o, dir, *cond, ph, chanda.SleepBudget{Tokens: *bt, Events: *be})
	if err != nil {
		return err
	}
	if err := writeJSON(*out, res); err != nil {
		return err
	}
	var so chanda.SleepRunOutput
	_ = json.Unmarshal(res.Output, &so)
	fmt.Printf("wrote %s: run %s %s, phases %v, %d LLM calls, %d+%d tokens, %.1fs, %d pages\n", *out, so.RunID, so.Status, so.Phases,
		so.Cost.LLMCalls, so.Cost.InputTokens, so.Cost.OutputTokens, res.WallMS/1000, len(res.Fichas))
	if so.Status == "failed" {
		return fmt.Errorf("sleep run %s failed (see %s)", so.RunID, *out)
	}
	if res.Phase2Skipped != "" && len(ph) == 2 && !*allowSkip {
		return fmt.Errorf("phase 2 was requested but skipped: %q (result kept in %s)", res.Phase2Skipped, *out)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func studyRAG(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study rag", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, false)
	locomo := fs.String("locomo", "", "LoCoMo locomo10.json")
	conv := fs.String("conv", "", "sample_id")
	split := fs.String("split", "", "split file")
	half := fs.String("half", lme.HalfTest, "use, test or all")
	cond := fs.String("condition", "B", "condition label")
	out := fs.String("out", "", "output JSONL")
	limit := fs.Int("limit", 0, "only the first N questions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *locomo == "" || *conv == "" || *split == "" || *out == "" {
		return fmt.Errorf("-locomo, -conv, -split and -out are required")
	}
	p, err := filepath.Abs(*locomo)
	if err != nil {
		return err
	}
	sum, _, err := lme.FileSHA256(p)
	if err != nil {
		return err
	}
	if sum != lme.LocomoSHA256 {
		return fmt.Errorf("%s: sha256 %s, want the pinned %s", p, sum, lme.LocomoSHA256)
	}
	m := &lme.StudyMeta{Source: lme.SourceLocomo, Dataset: p, DatasetSHA256: sum, SampleID: *conv}
	qs, err := studyQuestions(m, *split, *half, *limit)
	if err != nil {
		return err
	}
	c, err := lme.LoadStudyLocomo(m)
	if err != nil {
		return err
	}
	eo, err := f.embedOptions()
	if err != nil {
		return err
	}
	emb, err := lme.OpenEmbedder(ctx, eo)
	if err != nil {
		return err
	}
	defer emb.Close()
	lines, err := lme.StudyRAG(ctx, emb, c, qs, f.budget, *cond)
	if err != nil {
		return err
	}
	for _, l := range lines {
		l.DB = "plain-rag:" + emb.ModelID()
	}
	if err := lme.WriteJSONL(*out, lines); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d lines\n", *out, len(lines))
	return nil
}

func studyFichas(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study fichas", flag.ContinueOnError)
	db := fs.String("db", "", "closed study directory")
	out := fs.String("out", "", "JSON file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *db == "" {
		return fmt.Errorf("-db is required")
	}
	fi, err := lme.StudyFichas(ctx, *db)
	if err != nil {
		return err
	}
	if *out == "" {
		b, _ := json.MarshalIndent(fi, "", " ")
		fmt.Println(string(b))
		return nil
	}
	return writeJSON(*out, fi)
}

func studyLeak(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study leakcheck", flag.ContinueOnError)
	db := fs.String("db", "", "closed study directory")
	split := fs.String("split", "", "LoCoMo split (gold answers of both halves; test questions)")
	beforeTest := fs.Bool("before-test", false, "the test half has not been asked yet: its questions must not be in the retrievals")
	out := fs.String("out", "", "JSON report")
	protocol := fs.String("protocol", "sleep", "sleep (eval/sleep/PROTOCOL.md), learn (eval/learn/PROTOCOL.md, always before the test half) "+
		"or conflicts (eval/conflicts/PROTOCOL.md, always before the question is asked)")
	keyTable := fs.String("key-table", "", "learn: the learned-keys table (default: detected)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *db == "" {
		return fmt.Errorf("-db is required")
	}
	m, err := lme.ReadStudyMeta(*db)
	if err != nil {
		return err
	}
	qs, err := studyQuestions(m, *split, lme.HalfAll, 0)
	if err != nil {
		return err
	}
	switch *protocol {
	case "sleep":
		if *keyTable != "" {
			return fmt.Errorf("-key-table is for -protocol learn")
		}
	case "learn":
		return studyLeakLearn(ctx, *db, qs, *keyTable, *out)
	case "conflicts":
		if *keyTable != "" || *split != "" || *beforeTest {
			return fmt.Errorf("-protocol conflicts takes -db and -out only")
		}
		return studyLeakConflicts(ctx, *db, qs, *out)
	default:
		return fmt.Errorf("-protocol %q: want sleep, learn or conflicts", *protocol)
	}
	var golds, forbidden []string
	for _, q := range qs {
		golds = append(golds, q.Gold)
		if *beforeTest && q.Half == lme.HalfTest {
			forbidden = append(forbidden, q.Question)
		}
	}
	r, err := lme.StudyLeakCheck(ctx, *db, golds, forbidden)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := writeJSON(*out, r); err != nil {
			return err
		}
	}
	fmt.Printf("leakcheck %s: ok=%t outcomes=%d bad=%d retrievals=%d test-questions=%d derived=%d gold-hits=%d unexplained=%d judge-words=%d\n",
		*db, r.OK, r.Outcomes, len(r.BadOutcomes), r.Retrievals, len(r.TestQuestionsInRetrievals), r.DerivedTexts, r.GoldHits, len(r.Unexplained), len(r.JudgeWords))
	if !r.OK {
		return fmt.Errorf("leak check failed for %s (see the report)", *db)
	}
	return nil
}

// studyLeakLearn is leakcheck -protocol learn: the split's use and test
// halves of the directory's conversation.
func studyLeakLearn(ctx context.Context, db string, qs []lme.StudyQuestion, keyTable, out string) error {
	var in lme.LearnLeakInput
	in.KeyTable = keyTable
	for _, q := range qs {
		switch q.Half {
		case lme.HalfUse:
			in.Use = append(in.Use, q)
		case lme.HalfTest:
			in.Test = append(in.Test, q)
		}
	}
	if len(in.Use) == 0 || len(in.Test) == 0 {
		return fmt.Errorf("leakcheck -protocol learn: the split gives %d use and %d test questions for %s", len(in.Use), len(in.Test), db)
	}
	r, err := lme.StudyLeakCheckLearn(ctx, db, in)
	if err != nil {
		return err
	}
	if out != "" {
		if err := writeJSON(out, r); err != nil {
			return err
		}
	}
	kt := r.KeyTable
	if kt == "" {
		kt = "none (" + r.KeyNote + ")"
	}
	fmt.Printf("leakcheck learn %s: ok=%t key-table=%s keys=%d retrievals=%d outcomes=%d values=%d test-in-db=%d (inside use questions %d) "+
		"keys-not-use=%d retrievals-not-use=%d bad-outcomes=%d judge=%d gold=%d (inside use questions %d)\n",
		db, r.OK, kt, r.Keys, r.Retrievals, r.Outcomes, r.ValuesScanned, len(r.TestQuestionIn), len(r.TestQuestionInUse),
		len(r.KeysNotUse), len(r.RetrievalsNotUse), len(r.BadOutcomes), len(r.JudgeOutput), len(r.GoldHits), len(r.GoldInUseQuestion))
	if !r.OK {
		return fmt.Errorf("leak check failed for %s: %s (see the report)", db, strings.Join(r.Failures, "; "))
	}
	return nil
}

// studyLeakConflicts is leakcheck -protocol conflicts: the directory's
// question and gold answer, before the question is asked.
func studyLeakConflicts(ctx context.Context, db string, qs []lme.StudyQuestion, out string) error {
	r, err := lme.StudyLeakCheckConflicts(ctx, db, qs)
	if err != nil {
		return err
	}
	if out != "" {
		if err := writeJSON(out, r); err != nil {
			return err
		}
	}
	fmt.Printf("leakcheck conflicts %s: ok=%t values=%d retrievals=%d question-hits=%d gold-hits=%d (golds checked %d, too short %d) judge=%d\n",
		db, r.OK, r.ValuesScanned, r.Retrievals, len(r.QuestionHits), len(r.GoldHits), r.GoldsChecked, r.GoldsTooShort, len(r.JudgeOutput))
	if !r.OK {
		return fmt.Errorf("leak check failed for %s: %s (see the report)", db, strings.Join(r.Failures, "; "))
	}
	return nil
}

// studyConflicts is the conflicts admin step (decision 0148).
func studyConflicts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study conflicts", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	action := fs.String("action", "", "scan, stats, list or reset")
	var a lme.ConflictsArgs
	fs.BoolVar(&a.Write, "write", false, "scan: write the queue (default: counts only)")
	fs.BoolVar(&a.Details, "details", false, "scan: return the candidate pairs and the near-duplicate refusals")
	fs.Float64Var(&a.MinCosine, "min-cosine", 0, "scan: similarity floor (0: the configuration's sleep.conflicts.min_cosine)")
	out := fs.String("out", "", "result JSON (the tool's output as returned, with the step's metadata)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := lme.CheckConflictsAction(*action, a); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	m, err := lme.ReadStudyMeta(dir)
	if err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	if err := checkMeta(m, emb); err != nil {
		return err
	}
	res, err := lme.StudyConflicts(ctx, o, dir, *action, a)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := writeJSON(*out, res); err != nil {
			return err
		}
	}
	fmt.Println(string(res.Output))
	return nil
}

// studyKeys is the learned_keys admin step (decision 0145).
func studyKeys(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study keys", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, true)
	action := fs.String("action", "", "rebuild, stats, reset, enable or disable")
	out := fs.String("out", "", "result JSON (the tool's output as returned)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := lme.CheckKeysAction(*action); err != nil {
		return err
	}
	dir, err := f.dir()
	if err != nil {
		return err
	}
	m, err := lme.ReadStudyMeta(dir)
	if err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	if err := checkMeta(m, emb); err != nil {
		return err
	}
	res, err := lme.StudyKeys(ctx, o, dir, *action)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := writeJSON(*out, res); err != nil {
			return err
		}
	}
	fmt.Printf("keys %s on %s: %s\n", *action, dir, res.Output)
	return nil
}

func studyCheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("study check", flag.ContinueOnError)
	var f studyFlags
	f.register(fs, false)
	admin := fs.Bool("admin", false, "with the admin profile (sleep steps)")
	llmURL := fs.String("llm-url", "", "with an LLM endpoint (no call is made)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o, emb, err := f.options(ctx)
	if err != nil {
		return err
	}
	defer emb.Close()
	o.Chanda.LLMURL, o.Chanda.LLMModel = *llmURL, "deepseek-flash"
	dir, err := os.MkdirTemp("", "lme-study-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	m := &lme.StudyMeta{EmbedModel: emb.ModelID(), EmbedDim: emb.Dim()}
	_, h, err := o.StudyConfig(dir, m, *admin)
	if err != nil {
		return err
	}
	fmt.Printf("ok %s %v\n", h, lme.OverrideStrings(o.Chanda.Set))
	return nil
}
