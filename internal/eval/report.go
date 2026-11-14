package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ReportSchema versions the JSON layout.
const ReportSchema = "chanda-eval-report/v1"

// Report is the evaluation result. Everything except Latency is a pure
// function of the inputs (dataset, systems, budget, config, clock, version,
// hardware), so two runs can be diffed; latency lives in its own section.
type Report struct {
	Schema         string         `json:"schema"`
	Name           string         `json:"name"`
	Date           string         `json:"date"`
	ChandaVersion  string         `json:"chanda_version"`
	ConfigHash     string         `json:"config_hash"`
	EmbeddingModel string         `json:"embedding_model_id"`
	ReaderModel    string         `json:"reader_model"`
	Hardware       Hardware       `json:"hardware"`
	Dataset        DatasetInfo    `json:"dataset"`
	BudgetTokens   int            `json:"budget_tokens"`
	TokenEstimator string         `json:"token_estimator"`
	Systems        []SystemResult `json:"systems"`
	Suites         []SuiteResult  `json:"suites"`
	Losses         []Loss         `json:"losses"`
	// Latency is measured with a real clock and is not deterministic.
	Latency []Latency `json:"latency"`
}

// Hardware describes the machine that produced the report.
type Hardware struct {
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	NumCPU    int    `json:"num_cpu"`
	CPUModel  string `json:"cpu_model"`
	GoVersion string `json:"go_version"`
}

// DatasetInfo identifies the dataset.
type DatasetInfo struct {
	Name      string         `json:"name"`
	Hash      string         `json:"hash"`
	Items     int            `json:"items"`
	Documents int            `json:"documents"` // distinct document ids
	Versions  int            `json:"document_versions"`
	Sessions  int            `json:"sessions"`
	Questions int            `json:"questions"`
	Tags      map[string]int `json:"tags"`
}

// SystemResult is one system's deterministic results.
type SystemResult struct {
	Name      string             `json:"name"`
	Info      SystemInfo         `json:"info"`
	SizeBytes *int64             `json:"size_bytes"`
	Overall   Metrics            `json:"overall"`
	ByTag     map[string]Metrics `json:"by_tag"`
	Questions []QuestionResult   `json:"-"` // written to <name>.questions.jsonl
}

// Latency holds one system's timings, in milliseconds.
type Latency struct {
	System         string  `json:"system"`
	IngestItems    int     `json:"ingest_items"`
	IngestTotalMs  float64 `json:"ingest_total_ms"`
	IngestP50Ms    float64 `json:"ingest_p50_ms"`
	IngestP95Ms    float64 `json:"ingest_p95_ms"`
	FlushMs        float64 `json:"flush_ms"`
	RetrieveMeanMs float64 `json:"retrieve_mean_ms"`
	RetrieveP50Ms  float64 `json:"retrieve_p50_ms"`
	RetrieveP95Ms  float64 `json:"retrieve_p95_ms"`
}

// Loss records a scope (tag or "all") and metric where a system is worse
// than the best compared system.
type Loss struct {
	System string  `json:"system"`
	Scope  string  `json:"scope"`
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Best   float64 `json:"best"`
	BestBy string  `json:"best_by"`
}

func newReport(o Options) *Report {
	ds := o.Dataset
	info := DatasetInfo{Name: ds.Manifest.Name, Hash: ds.Hash(), Items: len(ds.Corpus), Questions: len(ds.Questions), Tags: map[string]int{}}
	docs := map[string]bool{}
	for _, it := range ds.Corpus {
		if it.Type == ItemSession {
			info.Sessions++
		} else {
			info.Versions++
			docs[it.ID] = true
		}
	}
	info.Documents = len(docs)
	for _, tag := range datasetTags(ds) {
		for _, q := range ds.Questions {
			if questionTag(q, tag) {
				info.Tags[tag]++
			}
		}
	}
	emb := "n/a"
	if o.Embedder != nil {
		emb = o.Embedder.ModelID()
	}
	return &Report{
		Schema: ReportSchema, Name: o.Name, Date: o.Clock.Now().UTC().Format("2006-01-02"),
		ChandaVersion: o.Version, ConfigHash: o.Config.Hash(), EmbeddingModel: emb,
		ReaderModel: "n/a (retrieval-only; no LLM reader or judge)", Hardware: *o.Hardware,
		Dataset: info, BudgetTokens: o.Budget, TokenEstimator: "ceil(characters / 4)",
		Systems: []SystemResult{}, Suites: []SuiteResult{}, Losses: []Loss{}, Latency: []Latency{},
	}
}

// DetectHardware reads the platform, CPU count and, when obtainable offline,
// the CPU model (/proc/cpuinfo on Linux, sysctl on macOS).
func DetectHardware() Hardware {
	h := Hardware{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, NumCPU: runtime.NumCPU(), CPUModel: "unknown", GoVersion: runtime.Version()}
	switch runtime.GOOS {
	case "linux":
		if f, err := os.Open("/proc/cpuinfo"); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				k, v, ok := strings.Cut(sc.Text(), ":")
				if k = strings.TrimSpace(k); ok && (k == "model name" || k == "Model" || k == "Hardware") {
					h.CPUModel = strings.TrimSpace(v)
					break
				}
			}
		}
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				h.CPUModel = s
			}
		}
	}
	return h
}

// round4 rounds to 4 decimals so reports stay readable and stable.
func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func round4p(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round4(*v)
	return &r
}

func (m Metrics) rounded() Metrics {
	m.RecallAt5, m.RecallAt10, m.MRR, m.NDCGAt10, m.DocRecallAt10 = round4(m.RecallAt5), round4(m.RecallAt10), round4(m.MRR), round4(m.NDCGAt10), round4(m.DocRecallAt10)
	m.AnswerInContext, m.ForbiddenRate = round4p(m.AnswerInContext), round4p(m.ForbiddenRate)
	m.AbstentionPrecision, m.AbstentionRecall = round4p(m.AbstentionPrecision), round4p(m.AbstentionRecall)
	m.EmptyRate, m.TokensMean = round4(m.EmptyRate), round4(m.TokensMean)
	return m
}

// lossMetrics are compared across systems per scope. higher: bigger is better.
var lossMetrics = []struct {
	name   string
	higher bool
	get    func(Metrics) *float64
}{
	{"recall@10", true, func(m Metrics) *float64 {
		if m.Answerable == 0 {
			return nil
		}
		return &m.RecallAt10
	}},
	{"answer_in_context", true, func(m Metrics) *float64 { return m.AnswerInContext }},
	{"forbidden_rate", false, func(m Metrics) *float64 { return m.ForbiddenRate }},
	{"abstention_recall", true, func(m Metrics) *float64 { return m.AbstentionRecall }},
}

func losses(r *Report) []Loss {
	out := []Loss{}
	if len(r.Systems) < 2 {
		return out
	}
	scopes := append([]string{"all"}, sortedKeys(r.Dataset.Tags)...)
	for _, scope := range scopes {
		for _, lm := range lossMetrics {
			vals := make([]*float64, len(r.Systems))
			var best *float64
			for i, s := range r.Systems {
				m := s.Overall
				if scope != "all" {
					m = s.ByTag[scope]
				}
				if v := lm.get(m.rounded()); v != nil {
					x := *v
					vals[i] = &x
					if best == nil || (lm.higher && x > *best) || (!lm.higher && x < *best) {
						best = &x
					}
				}
			}
			if best == nil {
				continue
			}
			var by []string
			for i, s := range r.Systems {
				if vals[i] != nil && *vals[i] == *best {
					by = append(by, s.Name)
				}
			}
			for i, s := range r.Systems {
				if vals[i] != nil && *vals[i] != *best {
					out = append(out, Loss{System: s.Name, Scope: scope, Metric: lm.name, Value: *vals[i], Best: *best, BestBy: strings.Join(by, ", ")})
				}
			}
		}
	}
	return out
}

// JSON encodes the report (indented, metrics rounded to 4 decimals).
func (r *Report) JSON() ([]byte, error) {
	c := *r
	c.Systems = make([]SystemResult, len(r.Systems))
	for i, s := range r.Systems {
		s.Overall = s.Overall.rounded()
		by := map[string]Metrics{}
		for k, m := range s.ByTag {
			by[k] = m.rounded()
		}
		s.ByTag = by
		c.Systems[i] = s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// QuestionsJSONL encodes the per-question judgements compactly, one line per
// system and question (deterministic, diff-friendly): the first 5 delivered
// ids, the delivered count, the rank of each gold unit (0 = missed; every
// retrieval metric derives from it), tokens and the content checks.
func (r *Report) QuestionsJSONL() ([]byte, error) {
	type line struct {
		System    string   `json:"sys"`
		ID        string   `json:"q"`
		Top       []string `json:"top5"`
		N         int      `json:"n"`
		GoldRanks []int    `json:"gold_ranks,omitempty"`
		Tokens    int      `json:"tokens"`
		Answer    bool     `json:"answer,omitempty"`
		Forbidden bool     `json:"forbidden,omitempty"`
		Over      int      `json:"budget_violations,omitempty"`
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, s := range r.Systems {
		for _, q := range s.Questions {
			l := line{System: s.Name, ID: q.QuestionID, Top: q.Delivered[:min(5, len(q.Delivered))], N: q.DeliveredCount,
				GoldRanks: q.GoldRanks, Tokens: q.Tokens, Answer: q.AnswerHit, Forbidden: q.Forbidden, Over: q.BudgetViolations}
			if err := enc.Encode(l); err != nil {
				return nil, err
			}
		}
	}
	return b.Bytes(), nil
}

// LatencyHeading starts the non-deterministic section of the Markdown report.
const LatencyHeading = "## Latency (not deterministic)"

// Markdown renders the report. Everything above LatencyHeading is
// deterministic given the same inputs.
func (r *Report) Markdown() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# Chanda evaluation report: %s\n\n", r.Name)
	w("| Field | Value |\n| --- | --- |\n")
	w("| Date | %s |\n", r.Date)
	w("| Chanda version | %s |\n", r.ChandaVersion)
	w("| Configuration hash | `%s` |\n", r.ConfigHash)
	w("| Embedding model_id | `%s` |\n", r.EmbeddingModel)
	w("| Reader model | %s |\n", r.ReaderModel)
	w("| Hardware | %s/%s, %d CPUs, %s |\n", r.Hardware.GOOS, r.Hardware.GOARCH, r.Hardware.NumCPU, r.Hardware.CPUModel)
	w("| Go | %s |\n", r.Hardware.GoVersion)
	d := r.Dataset
	w("| Dataset | %s, hash `%s`: %d items (%d documents in %d versions, %d sessions), %d questions |\n", d.Name, d.Hash, d.Items, d.Documents, d.Versions, d.Sessions, d.Questions)
	w("| Context budget | %d tokens per query, same for every system (estimator: %s) |\n", r.BudgetTokens, r.TokenEstimator)
	w("| Report schema | %s |\n\n", r.Schema)

	var tags []string
	for _, t := range sortedKeys(d.Tags) {
		tags = append(tags, fmt.Sprintf("%s %d", t, d.Tags[t]))
	}
	w("Questions by tag: %s.\n\n", strings.Join(tags, ", "))

	w("## Systems\n\n| System | Embedding | Description |\n| --- | --- | --- |\n")
	for _, s := range r.Systems {
		w("| %s | %s | %s |\n", s.Name, s.Info.EmbeddingModel, s.Info.Description)
	}

	w("\n## Overall, side by side\n\nRetrieval metrics are means over answerable questions; the best value of each row is in bold. ")
	w("\"Forbidden in context\" counts questions whose context contains a superseded value or an injected instruction (lower is better).\n\n")
	r.metricTable(&b, func(s SystemResult) Metrics { return s.Overall })
	sizes := make([]string, len(r.Systems))
	for i, s := range r.Systems {
		sizes[i] = "n/a"
		if s.SizeBytes != nil {
			sizes[i] = fmt.Sprintf("%d", *s.SizeBytes)
		}
	}
	w("| index size (bytes) | %s |\n", strings.Join(sizes, " | "))

	for _, sec := range []struct {
		title string
		get   func(Metrics) *float64
	}{
		{"recall@10", func(m Metrics) *float64 {
			if m.Answerable == 0 {
				return nil
			}
			return &m.RecallAt10
		}},
		{"answer in context", func(m Metrics) *float64 { return m.AnswerInContext }},
		{"forbidden in context (lower is better)", func(m Metrics) *float64 { return m.ForbiddenRate }},
		{"empty results", func(m Metrics) *float64 { return &m.EmptyRate }},
	} {
		w("\n## By tag: %s\n\n| Tag (questions) | %s |\n| --- |%s\n", sec.title, r.systemNames(" | "), strings.Repeat(" --- |", len(r.Systems)))
		for _, t := range sortedKeys(d.Tags) {
			cells := make([]string, len(r.Systems))
			for i, s := range r.Systems {
				cells[i] = fmtOpt(sec.get(s.ByTag[t].rounded()))
			}
			w("| %s (%d) | %s |\n", t, d.Tags[t], strings.Join(cells, " | "))
		}
	}

	w("\n## Where each system loses\n\nScopes and metrics where a system is worse than the best compared system.\n\n")
	if len(r.Losses) == 0 {
		w("No losses (a single system, or all systems tie).\n")
	}
	for _, s := range r.Systems {
		var items []string
		for _, l := range r.Losses {
			if l.System == s.Name {
				items = append(items, fmt.Sprintf("%s %s %s vs %s (%s)", l.Scope, l.Metric, fmtNum(l.Value), fmtNum(l.Best), l.BestBy))
			}
		}
		if len(items) > 0 {
			w("- **%s** (%d): %s.\n", s.Name, len(items), strings.Join(items, "; "))
		}
	}

	w("\n## Own suites\n\nTargets from BASE.md; the measured value is published even when the target is not met.\n\n")
	w("| Suite | System | Target | Measured | Result |\n| --- | --- | --- | --- | --- |\n")
	for _, s := range r.Suites {
		var ms []string
		for _, k := range sortedKeys(s.Measured) {
			ms = append(ms, k+" "+fmtNum(s.Measured[k]))
		}
		res := s.Status
		if s.Reason != "" {
			res += ": " + s.Reason
		}
		sys := s.System
		if sys == "" {
			sys = "-"
		}
		w("| %s | %s | %s | %s | %s |\n", s.Suite, sys, s.Target.Text, strings.Join(ms, ", "), res)
	}

	w("\n%s\n\nMeasured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.\n\n", LatencyHeading)
	w("| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Retrieve mean | Retrieve p50 | Retrieve p95 |\n")
	w("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, l := range r.Latency {
		w("| %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f |\n", l.System, l.IngestItems, l.IngestTotalMs, l.IngestP50Ms, l.IngestP95Ms, l.FlushMs, l.RetrieveMeanMs, l.RetrieveP50Ms, l.RetrieveP95Ms)
	}
	return b.String()
}

func (r *Report) systemNames(sep string) string {
	ns := make([]string, len(r.Systems))
	for i, s := range r.Systems {
		ns[i] = s.Name
	}
	return strings.Join(ns, sep)
}

func (r *Report) metricTable(b *strings.Builder, pick func(SystemResult) Metrics) {
	fmt.Fprintf(b, "| Metric | %s |\n| --- |%s\n", r.systemNames(" | "), strings.Repeat(" --- |", len(r.Systems)))
	rows := []struct {
		name   string
		higher bool
		get    func(Metrics) *float64
	}{
		{"recall@5", true, func(m Metrics) *float64 { return &m.RecallAt5 }},
		{"recall@10", true, func(m Metrics) *float64 { return &m.RecallAt10 }},
		{"MRR", true, func(m Metrics) *float64 { return &m.MRR }},
		{"nDCG@10", true, func(m Metrics) *float64 { return &m.NDCGAt10 }},
		{"doc recall@10", true, func(m Metrics) *float64 { return &m.DocRecallAt10 }},
		{"answer in context", true, func(m Metrics) *float64 { return m.AnswerInContext }},
		{"forbidden in context", false, func(m Metrics) *float64 { return m.ForbiddenRate }},
		{"abstention precision", true, func(m Metrics) *float64 { return m.AbstentionPrecision }},
		{"abstention recall", true, func(m Metrics) *float64 { return m.AbstentionRecall }},
		{"empty results", false, nil},
		{"context tokens per query (mean)", false, nil},
		{"context tokens per query (p50 / p95)", false, nil},
		{"budget violations", false, nil},
	}
	for _, row := range rows {
		cells := make([]string, len(r.Systems))
		if row.get != nil {
			vals := make([]*float64, len(r.Systems))
			var best *float64
			for i, s := range r.Systems {
				vals[i] = row.get(pick(s).rounded())
				if v := vals[i]; v != nil && (best == nil || (row.higher && *v > *best) || (!row.higher && *v < *best)) {
					best = v
				}
			}
			for i, v := range vals {
				cells[i] = fmtOpt(v)
				if v != nil && best != nil && *v == *best && len(r.Systems) > 1 {
					cells[i] = "**" + cells[i] + "**"
				}
			}
		} else {
			for i, s := range r.Systems {
				m := pick(s).rounded()
				switch row.name {
				case "empty results":
					cells[i] = fmtNum(m.EmptyRate)
				case "context tokens per query (mean)":
					cells[i] = fmtNum(m.TokensMean)
				case "context tokens per query (p50 / p95)":
					cells[i] = fmt.Sprintf("%d / %d", m.TokensP50, m.TokensP95)
				case "budget violations":
					cells[i] = fmt.Sprintf("%d", m.BudgetViolations)
				}
			}
		}
		fmt.Fprintf(b, "| %s | %s |\n", row.name, strings.Join(cells, " | "))
	}
}

func fmtOpt(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmtNum(*v)
}
