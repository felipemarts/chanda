package hugot

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/pkg/chanda/embedreg"
)

func TestModelIDRecordsModelEngineAndFlavour(t *testing.T) {
	cases := map[string]string{
		"e5-small":              "e5-small@hugot0.8.6+" + Flavour,
		"paraphrase-minilm-l12": "paraphrase-minilm-l12@hugot0.8.6+" + Flavour,
		"mmarco-minilm-l12":     "mmarco-minilm-l12@hugot0.8.6+" + Flavour,
	}
	for name, want := range cases {
		m, ok := LookupModel(name)
		if !ok {
			t.Fatalf("model %s not listed", name)
		}
		if m.ModelID() != want {
			t.Errorf("%s: model id %s, want %s", name, m.ModelID(), want)
		}
		if r, _ := LookupModel(m.Repo); r.Name != name {
			t.Errorf("lookup by repo %s gave %q", m.Repo, r.Name)
		}
	}
	if Flavour != "simd" && Flavour != "stock" {
		t.Fatalf("flavour %q", Flavour)
	}
}

// HugotVersion is part of every model id, so it must follow go.mod.
func TestHugotVersionMatchesGoMod(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*github\.com/knights-analytics/hugot v(\S+)`).FindSubmatch(b)
	if m == nil || string(m[1]) != HugotVersion {
		t.Fatalf("go.mod requires hugot %q, HugotVersion is %q", m, HugotVersion)
	}
}

func fakeModelDir(t *testing.T, name string) string {
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{"model.onnx": "onnx bytes", "tokenizer.json": "{}", "config.json": "{}"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectModel(t *testing.T) {
	cases := []struct {
		name string
		dir  string // directory base name
		kind Kind
		want string
		code core.Code
	}{
		{"short name", "e5-small", KindEmbedder, "e5-small", ""},
		{"hugot download name", "sentence-transformers_paraphrase-multilingual-MiniLM-L12-v2", KindEmbedder, "paraphrase-minilm-l12", ""},
		{"reranker", "mmarco-minilm-l12", KindReranker, "mmarco-minilm-l12", ""},
		{"unknown directory name", "my-model", KindEmbedder, "", core.CodeInvalidArgument},
		{"reranker used as embedder", "mmarco-minilm-l12", KindEmbedder, "", core.CodeInvalidArgument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := detect(fakeModelDir(t, c.dir), c.kind)
			if c.code != "" {
				if core.CodeOf(err) != c.code {
					t.Fatalf("err = %v, want %s", err, c.code)
				}
				return
			}
			if err != nil || m.Name != c.want {
				t.Fatalf("detect = %q, %v; want %s", m.Name, err, c.want)
			}
		})
	}
	if _, err := detect(t.TempDir(), KindEmbedder); core.CodeOf(err) != core.CodeUnavailable {
		t.Fatalf("empty dir: err = %v, want unavailable", err)
	}
}

// The manifest names the model whatever the directory is called, and a
// changed model file is refused at load (files decide the vectors).
func TestManifestNamesModelAndCatchesChangedFiles(t *testing.T) {
	dir := fakeModelDir(t, "anything")
	e5, _ := LookupModel("e5-small")
	man, err := WriteManifest(dir, e5)
	if err != nil {
		t.Fatal(err)
	}
	if len(man.SHA256) != 3 || man.SHA256["model.onnx"] == "" {
		t.Fatalf("manifest %+v", man)
	}
	if m, err := detect(dir, KindEmbedder); err != nil || m.Name != "e5-small" {
		t.Fatalf("detect with manifest = %q, %v", m.Name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := detect(dir, KindEmbedder); core.CodeOf(err) != core.CodeIntegrity {
		t.Fatalf("changed file: err = %v, want integrity", err)
	}
	if err := VerifyManifest(dir, Manifest{Model: "e5-small"}); core.CodeOf(err) != core.CodeIntegrity {
		t.Fatalf("manifest without digests: err = %v", err)
	}
}

func TestNewFromConfigRejectsMismatches(t *testing.T) {
	dir := fakeModelDir(t, "e5-small")
	cases := []struct {
		name, path, id string
		dim            int
	}{
		{"empty model path", "", "", 384},
		{"model id of another model", dir, "paraphrase-minilm-l12@hugot0.8.6+" + Flavour, 384},
		{"model id of another build flavour", dir, "e5-small@hugot0.8.6+other", 384},
		{"dim left at the hash default", dir, "", 256},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewFromConfig(embedreg.Config{Provider: ProviderName, ModelPath: c.path, ModelID: c.id, Dim: c.dim})
			if core.CodeOf(err) != core.CodeInvalidArgument {
				t.Fatalf("err = %v, want invalid_argument", err)
			}
		})
	}
}
