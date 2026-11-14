package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/embedders/hugot"
)

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func fakeModel(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "m")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"model.onnx", "tokenizer.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// None of these touch the network: pull fails on its checks before downloading.
func TestCommands(t *testing.T) {
	dir := fakeModel(t)
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string // substring
		stderr string // substring
	}{
		{"no args is a usage error", nil, 2, "", "usage:"},
		{"version reports engine and flavour", []string{"version"}, 0, `"flavour": "` + hugot.Flavour + `"`, ""},
		{"models list", []string{"models", "list"}, 0, `"ModelID": "e5-small@hugot` + hugot.HugotVersion, ""},
		{"models without subcommand", []string{"models"}, 2, "", "needs a subcommand"},
		{"adopt unknown model", []string{"models", "adopt", "nope", dir}, 2, "", "unknown model"},
		{"adopt writes the manifest", []string{"models", "adopt", "e5-small", dir}, 0, `"model": "e5-small"`, ""},
		{"verify after adopt", []string{"models", "verify", dir}, 0, `"model.onnx"`, ""},
		{"pull unknown model", []string{"models", "pull", "nope", filepath.Join(t.TempDir(), "x")}, 1, "", "unknown model"},
		{"pull into an existing directory", []string{"models", "pull", "paraphrase-minilm-l12", dir}, 1, "", "already exists"},
		{"embed without text", []string{"embed", dir}, 2, "", "embed needs"},
		{"chanda commands run the chanda CLI", []string{"forget"}, 2, "", "usage: chanda forget"},
		{"unknown commands reach the chanda CLI", []string{"frobnicate"}, 2, "", "chanda: unknown command"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errs := runCLI(c.args...)
			if code != c.code || !strings.Contains(out, c.stdout) || !strings.Contains(errs, c.stderr) {
				t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runCLI("models", "verify", dir); code != 1 || !strings.Contains(errs, "integrity") {
		t.Fatalf("verify after change: code %d, %q", code, errs)
	}
}

func TestEmbedCommand(t *testing.T) {
	root := os.Getenv("CHANDA_HUGOT_MODELS")
	if root == "" {
		root = filepath.Join("..", "..", "testdata", "models")
	}
	dir := filepath.Join(root, "paraphrase-minilm-l12")
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		t.Skipf("model not found in %s: go run ./cmd/chanda-hugot models pull paraphrase-minilm-l12 %s", dir, dir)
	}
	code, out, errs := runCLI("embed", "--query", dir, "Quanto custa o plano?")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	var got struct {
		ModelID string      `json:"model_id"`
		Dim     int         `json:"dim"`
		Vectors [][]float32 `json:"vectors"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got.ModelID != "paraphrase-minilm-l12@hugot"+hugot.HugotVersion+"+"+hugot.Flavour || got.Dim != 384 || len(got.Vectors) != 1 {
		t.Fatalf("got %s dim %d, %d vectors", got.ModelID, got.Dim, len(got.Vectors))
	}
}
