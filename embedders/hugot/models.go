package hugot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
)

// HugotVersion is the knights-analytics/hugot version this module pins in
// go.mod (a test keeps them in sync). It is part of every model id because a
// different engine may change the vectors.
const HugotVersion = "0.8.6"

// Kind tells what a model computes.
type Kind string

const (
	KindEmbedder Kind = "embedder" // sentence embedding, mean pooling
	KindReranker Kind = "reranker" // cross-encoder, one logit per (query, doc)
)

// Model is a supported Hugging Face model.
type Model struct {
	Name          string // short name, used in model ids and directory names
	Repo          string // Hugging Face repository
	OnnxFile      string // ONNX file inside the repository
	Kind          Kind
	Dim           int    // embedding dimension (embedders)
	MaxTokens     int    // window, including <s> and </s>; longer input is cut at the end
	QueryPrefix   string // prepended to queries (e5: "query: ")
	PassagePrefix string // prepended to stored texts (e5: "passage: ")
}

// Models lists the supported models (docs/research/embeddings.md).
var Models = []Model{
	{Name: "e5-small", Repo: "intfloat/multilingual-e5-small", OnnxFile: "onnx/model.onnx",
		Kind: KindEmbedder, Dim: 384, MaxTokens: 512, QueryPrefix: "query: ", PassagePrefix: "passage: "},
	{Name: "paraphrase-minilm-l12", Repo: "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
		OnnxFile: "onnx/model.onnx", Kind: KindEmbedder, Dim: 384, MaxTokens: 128}, // sentence-transformers max_seq_length
	{Name: "mmarco-minilm-l12", Repo: "cross-encoder/mmarco-mMiniLMv2-L12-H384-v1",
		OnnxFile: "onnx/model.onnx", Kind: KindReranker, MaxTokens: 512},
}

// LookupModel finds a model by short name or by repository.
func LookupModel(name string) (Model, bool) {
	for _, m := range Models {
		if name == m.Name || name == m.Repo {
			return m, true
		}
	}
	return Model{}, false
}

// ModelID is the id stored with every vector: model, engine version and
// build flavour (simd vs stock change vectors by about 1e-7).
func (m Model) ModelID() string {
	return fmt.Sprintf("%s@hugot%s+%s", m.Name, HugotVersion, Flavour)
}

// ManifestFile is written by Pull into the model directory.
const ManifestFile = "chanda-model.json"

// Manifest records which model a directory holds and the SHA-256 of each file,
// checked at every load because the files decide the vectors.
type Manifest struct {
	Model  string            `json:"model"`
	Repo   string            `json:"repo"`
	SHA256 map[string]string `json:"sha256"` // file name -> hex digest
}

// detect finds the model held by dir: from its manifest (verifying the
// checksums), else from the directory name, which is either the short name
// ("e5-small") or hugot's download name ("intfloat_multilingual-e5-small").
func detect(dir string, want Kind) (Model, error) {
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		return Model{}, core.Errf(core.CodeUnavailable,
			"hugot: no model.onnx in %q (fetch it with: chanda-hugot models pull <model> <dir>)", dir)
	}
	man, err := ReadManifest(dir)
	var m Model
	var ok bool
	switch {
	case err == nil:
		if err := VerifyManifest(dir, man); err != nil {
			return Model{}, err
		}
		m, ok = LookupModel(man.Model)
	case errors.Is(err, fs.ErrNotExist):
		base := filepath.Base(filepath.Clean(dir))
		m, ok = LookupModel(base)
		if !ok {
			m, ok = LookupModel(strings.Replace(base, "_", "/", 1))
		}
	default:
		return Model{}, err
	}
	if !ok {
		return Model{}, core.Errf(core.CodeInvalidArgument,
			"hugot: cannot tell which model %q holds: no %s and the directory name is not a known model (%s)",
			dir, ManifestFile, modelNames())
	}
	if m.Kind != want {
		return Model{}, core.Errf(core.CodeInvalidArgument, "hugot: %s is a %s, not an %s", m.Name, m.Kind, want)
	}
	return m, nil
}

// resolve follows a symlinked model directory: hugot walks the directory
// without following a symlink at its root.
func resolve(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}

func modelNames() string {
	var s []string
	for _, m := range Models {
		s = append(s, m.Name)
	}
	return strings.Join(s, ", ")
}

// ReadManifest reads dir/chanda-model.json.
func ReadManifest(dir string) (Manifest, error) {
	var man Manifest
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return man, err
	}
	if err := json.Unmarshal(b, &man); err != nil {
		return man, core.Errf(core.CodeInvalidArgument, "hugot: %s in %q: %v", ManifestFile, dir, err)
	}
	return man, nil
}

// WriteManifest hashes every regular file of dir (except the manifest) and
// writes the manifest for model m.
func WriteManifest(dir string, m Model) (Manifest, error) {
	man := Manifest{Model: m.Name, Repo: m.Repo, SHA256: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return man, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name() == ManifestFile {
			continue
		}
		sum, err := fileSHA256(filepath.Join(dir, e.Name()))
		if err != nil {
			return man, err
		}
		man.SHA256[e.Name()] = sum
	}
	b, err := json.MarshalIndent(man, "", "  ") // map keys are sorted by encoding/json
	if err != nil {
		return man, err
	}
	return man, os.WriteFile(filepath.Join(dir, ManifestFile), append(b, '\n'), 0o644)
}

// VerifyManifest checks that every file listed in man has its recorded digest.
func VerifyManifest(dir string, man Manifest) error {
	if len(man.SHA256) == 0 || man.SHA256["model.onnx"] == "" {
		return core.Errf(core.CodeIntegrity, "hugot: %s in %q lists no model.onnx digest", ManifestFile, dir)
	}
	names := make([]string, 0, len(man.SHA256))
	for n := range man.SHA256 {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		got, err := fileSHA256(filepath.Join(dir, n))
		if err != nil {
			return core.Wrap(core.CodeIntegrity, err, "hugot: model file "+n)
		}
		if got != man.SHA256[n] {
			return core.Errf(core.CodeIntegrity, "hugot: %s in %q has sha256 %s, manifest says %s", n, dir, got, man.SHA256[n])
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
