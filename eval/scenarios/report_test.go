package scenarios

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite results.json and results.md from this run")

func TestMain(m *testing.M) {
	flag.Parse()
	dir, err := os.MkdirTemp("", "chanda-scenarios-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	chandaBin, err = buildChanda(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Report is the content of results.json.
type Report struct {
	Suite         string   `json:"suite"`
	Server        string   `json:"server"`
	Embedder      string   `json:"embedder"`
	LLM           string   `json:"llm"`
	Runs          int      `json:"runs"`
	Deterministic bool     `json:"deterministic"`
	Total         int      `json:"total"`
	Passed        int      `json:"passed"`
	Failed        int      `json:"failed"`
	FailedIDs     []string `json:"failed_ids"`
	Scenarios     []Result `json:"scenarios"`
}

// parallelism bounds how many scenarios (each one chanda serve process at a
// time) run at once.
const parallelism = 4

// runOne runs a scenario in dir and returns its result. An abort (a call
// that had to succeed failed, a protocol error) is a failed check.
func runOne(sc Scenario, dir string) (res Result) {
	res = Result{ID: sc.ID, Area: sc.Area, Description: sc.Description}
	s, err := newS(dir)
	if err != nil {
		res.Checks = []Check{{Name: "scenario setup", Expected: "no error", Measured: err.Error()}}
		res.Status = "FAIL"
		return res
	}
	defer func() {
		if p := recover(); p != nil {
			a, ok := p.(abort)
			if !ok {
				a = abort{fmt.Errorf("panic: %v", p)}
			}
			msg := strings.ReplaceAll(a.err.Error(), dir, "<dir>")
			s.check("scenario ran to completion", "no error", msg, false)
		}
		if s.cs != nil {
			_ = s.cs.Close()
			s.cs = nil
		}
		res.Steps, res.Checks = s.steps, s.checks
		if res.Steps == nil {
			res.Steps = []string{}
		}
		res.Status = "PASS"
		if len(res.Checks) == 0 {
			res.Status = "FAIL"
		}
		for _, c := range res.Checks {
			if !c.Pass {
				res.Status = "FAIL"
			}
		}
	}()
	sc.Run(s)
	return res
}

// runAll runs every scenario on fresh directories under root.
func runAll(root string) []Result {
	out := make([]Result, len(scenarios))
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, sc := range scenarios {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = runOne(sc, filepath.Join(root, sc.ID))
		}()
	}
	wg.Wait()
	return out
}

func TestScenarios(t *testing.T) {
	start := time.Now()
	ids := map[string]bool{}
	for _, sc := range scenarios {
		if ids[sc.ID] {
			t.Fatalf("duplicate scenario id %s", sc.ID)
		}
		ids[sc.ID] = true
	}
	run1 := runAll(filepath.Join(t.TempDir(), "run1"))
	run2 := runAll(filepath.Join(t.TempDir(), "run2"))
	j1, _ := json.MarshalIndent(run1, "", "  ")
	j2, _ := json.MarshalIndent(run2, "", "  ")
	deterministic := bytes.Equal(j1, j2)

	rep := Report{
		Suite:         "phase 5 scenarios (eval/scenarios)",
		Server:        "chanda serve over MCP stdio, built from ./cmd/chanda by the test; chanda CLI for person actions",
		Embedder:      "hash (built-in default, hash-v1-256)",
		LLM:           "none",
		Runs:          2,
		Deterministic: deterministic,
		Total:         len(run1),
		FailedIDs:     []string{},
		Scenarios:     run1,
	}
	for _, r := range run1 {
		if r.Status == "PASS" {
			rep.Passed++
		} else {
			rep.Failed++
			rep.FailedIDs = append(rep.FailedIDs, r.ID)
		}
	}
	js, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	js = append(js, '\n')
	md := []byte(renderMarkdown(rep))
	t.Logf("%d scenarios, %d passed, %d failed, deterministic %v, %s", rep.Total, rep.Passed, rep.Failed, deterministic, time.Since(start).Round(time.Millisecond))

	if *update {
		if err := os.WriteFile("results.json", js, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("results.md", md, 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		for name, want := range map[string][]byte{"results.json": js, "results.md": md} {
			got, err := os.ReadFile(name)
			if err != nil {
				t.Errorf("%s: %v (run with -update)", name, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from this run (run with -update and review the diff)", name)
			}
		}
	}
	if !deterministic {
		t.Errorf("the two runs differ:\nrun1:\n%s\nrun2:\n%s", j1, j2)
	}
	for _, r := range run1 {
		if r.Status != "PASS" {
			for _, c := range r.Checks {
				if !c.Pass {
					t.Errorf("%s FAIL: %s: expected %s, measured %s", r.ID, c.Name, c.Expected, c.Measured)
				}
			}
		}
	}
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func renderMarkdown(rep Report) string {
	var b strings.Builder
	b.WriteString("# Phase 5 scenarios\n\n")
	b.WriteString("Generated by `go test ./eval/scenarios/ -run TestScenarios -update`; ")
	b.WriteString("`go test ./eval/scenarios/` checks this file and results.json without writing.\n\n")
	fmt.Fprintf(&b, "- Server: %s\n", rep.Server)
	fmt.Fprintf(&b, "- Embedder: %s; LLM: %s\n", rep.Embedder, rep.LLM)
	fmt.Fprintf(&b, "- Runs: %d on fresh databases; byte-identical results: %v\n", rep.Runs, rep.Deterministic)
	fmt.Fprintf(&b, "- Result: %d scenarios, %d PASS, %d FAIL", rep.Total, rep.Passed, rep.Failed)
	if len(rep.FailedIDs) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(rep.FailedIDs, ", "))
	}
	b.WriteString("\n\n| id | area | result | description |\n| --- | --- | --- | --- |\n")
	for _, r := range rep.Scenarios {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.ID, r.Area, r.Status, mdCell(r.Description))
	}
	for _, r := range rep.Scenarios {
		fmt.Fprintf(&b, "\n## %s: %s\n\n", r.ID, r.Status)
		fmt.Fprintf(&b, "Area: %s. %s\n\nSteps:\n\n", r.Area, r.Description)
		for i, st := range r.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, st)
		}
		b.WriteString("\n| check | expected | measured | result |\n| --- | --- | --- | --- |\n")
		for _, c := range r.Checks {
			res := "PASS"
			if !c.Pass {
				res = "FAIL"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", mdCell(c.Name), mdCell(c.Expected), mdCell(c.Measured), res)
		}
	}
	return b.String()
}
