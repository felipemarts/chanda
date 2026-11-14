package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

func baselines(t *testing.T) []System {
	t.Helper()
	cfg := config.Default()
	var out []System
	for _, n := range []string{"B0", "B1", "B2", "B3"} {
		s, err := BaselineFactories()[n](context.Background(), FactoryEnv{Config: cfg, Embedder: NewLocalEmbedder(cfg.Embed.Dim)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		out = append(out, s)
	}
	return out
}

func runOnce(t *testing.T, ds *Dataset) *Report {
	t.Helper()
	rep, err := Run(context.Background(), Options{
		Dataset: ds, Systems: baselines(t), Name: "test",
		Embedder: NewLocalEmbedder(256), Clock: clock.NewFake(env.TestEpoch, 0), Timer: clock.System{},
		Version: "test", Hardware: &Hardware{GOOS: "os", GOARCH: "arch", NumCPU: 4, CPUModel: "cpu", GoVersion: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func withoutLatency(t *testing.T, r *Report) ([]byte, string) {
	t.Helper()
	js, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(js, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "latency")
	out, _ := json.Marshal(m)
	qs, err := r.QuestionsJSONL()
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, qs...)
	md, _, _ := strings.Cut(r.Markdown(), LatencyHeading)
	return out, md
}

// Principle 11 for the report: same inputs, same output, latency excepted.
func TestReportIsDeterministicApartFromLatency(t *testing.T) {
	ds := GenerateSyntheticPT(SyntheticSeed)
	a, b := runOnce(t, ds), runOnce(t, ds)
	ja, ma := withoutLatency(t, a)
	jb, mb := withoutLatency(t, b)
	if !bytes.Equal(ja, jb) {
		t.Fatal("JSON reports differ outside the latency section")
	}
	if ma != mb {
		t.Fatal("Markdown reports differ outside the latency section")
	}
	if !strings.Contains(a.Markdown(), LatencyHeading) || len(a.Latency) != 4 {
		t.Fatal("latency section missing")
	}
}

// BASE.md "Metrics": mandatory reporting of configuration hash, embedding
// model_id, reader, hardware and Chanda version (plus date and dataset hash).
func TestReportHasMandatoryFields(t *testing.T) {
	ds := GenerateSyntheticPT(SyntheticSeed)
	r := runOnce(t, ds)
	want := map[string]string{
		"config_hash": config.Default().Hash(), "embedding_model_id": "eval-localhash-v1-256",
		"chanda_version": "test", "date": "2026-01-01", "schema": ReportSchema,
	}
	js, _ := r.JSON()
	var m map[string]any
	_ = json.Unmarshal(js, &m)
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if !strings.HasPrefix(m["reader_model"].(string), "n/a") {
		t.Error("reader model must be declared")
	}
	if r.Dataset.Hash != ds.Hash() || r.Hardware.NumCPU != 4 || r.BudgetTokens != 800 {
		t.Errorf("dataset %s hardware %+v budget %d", r.Dataset.Hash, r.Hardware, r.BudgetTokens)
	}
	md := r.Markdown()
	for _, s := range []string{"Configuration hash", "Embedding model_id", "Reader model", "Hardware", "Chanda version", ds.Hash(), "| Metric | B0 | B1 | B2 | B3 |", "Where each system loses"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q", s)
		}
	}
}

// BASE.md "Evaluation": same context budget for all; a system that exceeds
// it is cut by the harness and the excess is counted.
type greedy struct{ NoMemory }

func (greedy) Name() string { return "greedy" }
func (greedy) Retrieve(context.Context, Query, int) ([]Retrieved, error) {
	return []Retrieved{{ID: "a", URI: "file:///d1.md", Text: strings.Repeat("x", 40)}, {ID: "b", URI: "file:///d1.md", Text: strings.Repeat("y", 40)}}, nil
}

func TestRunEnforcesBudgetAndMapsURIs(t *testing.T) {
	ds := validDataset()
	rep, err := Run(context.Background(), Options{Dataset: ds, Systems: []System{greedy{}}, Budget: 10,
		Clock: clock.NewFake(env.TestEpoch, 0), Hardware: &Hardware{}})
	if err != nil {
		t.Fatal(err)
	}
	q := rep.Systems[0].Questions[0]
	if q.Tokens != 10 || q.BudgetViolations != 1 || len(q.Delivered) != 1 {
		t.Fatalf("%+v", q)
	}
	if rep.Systems[0].Overall.BudgetViolations != len(ds.Questions) {
		t.Fatalf("violations %d", rep.Systems[0].Overall.BudgetViolations)
	}
	// URI file:///d1.md resolved to doc d1; gold for q2 has no span match, q1 neither, but doc recall counts.
	if q.DocRecallAt10 != 1 {
		t.Fatalf("URI not mapped to DocID: %+v", q)
	}
	if _, err := Run(context.Background(), Options{Dataset: ds, Systems: []System{greedy{}, greedy{}}}); err == nil {
		t.Fatal("duplicate system names accepted")
	}
}

func TestMainWritesReport(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := Main([]string{"-dataset", committedSynthetic, "-out", dir, "-name", "r", "-date", "2026-11-13", "-version", "v-test", "-systems", "B0,B1"}, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	md, err := os.ReadFile(filepath.Join(dir, "r.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "| Date | 2026-11-13 |") || !strings.Contains(string(md), "v-test") {
		t.Fatal("date or version flag ignored")
	}
	for _, f := range []string{"r.json", "r.questions.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"-systems", "B9"}, {"-date", "13/11/2026"}, {"-nope"}} {
		out.Reset()
		if code := Main(append([]string{"-dataset", committedSynthetic, "-out", dir}, args...), &out); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code := Main([]string{"-dataset", filepath.Join(dir, "missing"), "-out", dir}, &out); code != 1 {
		t.Errorf("missing dataset: exit %d, want 1", code)
	}
}

func TestBuildVersionFallsBack(t *testing.T) {
	if v := buildVersion(); v == "" {
		t.Fatal("empty version")
	}
}

// trainer records what the harness hands Train.
type trainer struct {
	oracle
	got []string
}

func (tr *trainer) Name() string { return "C5" }
func (tr *trainer) Train(_ context.Context, train, gate []Question) error {
	for _, q := range append(train, gate...) {
		tr.got = append(tr.got, q.ID)
	}
	return nil
}

// A learning system trains on the training split only, the split never
// enters the metrics, and a configuration that could not run is reported
// with its reason instead of disappearing.
func TestTrainingSplitAndUnavailableSystems(t *testing.T) {
	ds := validDataset()
	ds.Questions = append(ds.Questions, Question{ID: "t1", Question: "Quanto custa o plano A hoje?",
		Gold: []Gold{{DocID: "d1", Span: "custa R$ 12"}}, Answers: []string{"R$ 12"}, Split: SplitTrain},
		Question{ID: "g1", Question: "Preço do plano A?", Gold: []Gold{{DocID: "d1", Span: "custa R$ 12"}}, Answers: []string{"R$ 12"}, Split: SplitGate})
	tr := &trainer{}
	rep, err := Run(context.Background(), Options{Dataset: ds, Systems: []System{tr}, Clock: clock.NewFake(env.TestEpoch, 0),
		Hardware: &Hardware{}, Unavailable: []UnavailableSystem{{Name: "C2", Reason: "no reranker"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tr.got, ",") != "t1,g1" {
		t.Fatalf("trained on %v", tr.got)
	}
	if rep.Dataset.Questions != 4 || rep.Dataset.Train != 1 || rep.Dataset.Gate != 1 || len(rep.Systems[0].Questions) != 4 {
		t.Fatalf("dataset info %+v, %d results", rep.Dataset, len(rep.Systems[0].Questions))
	}
	md := rep.Markdown()
	if !strings.Contains(md, "| C2 | - | **Not available in this run**: no reranker |") || !strings.Contains(md, "+1 training and 1 gate questions") {
		t.Fatalf("markdown:\n%s", md)
	}
	for _, s := range rep.Suites {
		if s.Suite == SuiteLearning && s.Status != "not_available" {
			t.Fatalf("learning suite without C4: %+v", s)
		}
	}
}
