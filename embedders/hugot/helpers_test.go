package hugot

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Model files are large (about 450 MB each) and never fetched by tests. They
// are looked up in $CHANDA_HUGOT_MODELS/<name>, default testdata/models/<name>.
func modelDir(t testing.TB, name string) string {
	t.Helper()
	root := os.Getenv("CHANDA_HUGOT_MODELS")
	if root == "" {
		root = filepath.Join("testdata", "models")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		t.Skipf("model %s not found in %s: fetch it with `go run ./cmd/chanda-hugot models pull %s %s` "+
			"(about 450 MB, network) or set CHANDA_HUGOT_MODELS to a directory holding it", name, dir, name, dir)
	}
	return dir
}

var (
	sharedMu        sync.Mutex
	sharedEmbedders = map[string]*Embedder{}
)

// sharedEmbedder loads each model once per test binary (load is about 1 s
// and 1.5 GB of RAM); tests that need a fresh load call Open themselves.
func sharedEmbedder(t testing.TB, name string) *Embedder {
	t.Helper()
	dir := modelDir(t, name)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if e := sharedEmbedders[name]; e != nil {
		return e
	}
	e, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	sharedEmbedders[name] = e
	return e
}

var registered sync.Once

func registerOnce() { registered.Do(Register) }

type golden struct {
	Model   string      `json:"model"`
	Prefix  string      `json:"prefix"`
	Texts   []string    `json:"texts"`
	IDs     [][]int     `json:"ids"`
	Vectors [][]float64 `json:"vectors"`
}

// readGolden reads testdata/golden_<model>.json, made by testdata/golden.py
// with HF tokenizers and ONNX Runtime (one thread, one text per call).
func readGolden(t testing.TB, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden_"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func cosine(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / math.Sqrt(na*nb)
}

func bitsEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			return false
		}
	}
	return true
}
