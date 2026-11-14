package hugot

import (
	"context"
	"math"
	"strings"
	"testing"

	khugot "github.com/knights-analytics/hugot"
)

type rerankGolden struct {
	Query  string    `json:"query"`
	Docs   []string  `json:"docs"`
	Logits []float64 `json:"logits"`
}

var (
	sharedRR    *Reranker
	sharedRRErr error
)

func sharedReranker(t testing.TB) *Reranker {
	t.Helper()
	dir := modelDir(t, "mmarco-minilm-l12")
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRR == nil && sharedRRErr == nil {
		sharedRR, sharedRRErr = OpenReranker(context.Background(), dir)
	}
	if sharedRRErr != nil {
		t.Fatal(sharedRRErr)
	}
	return sharedRR
}

// Logits match ONNX Runtime, and the relevant documents rank first.
func TestRerankerMatchesONNXRuntime(t *testing.T) {
	r := sharedReranker(t)
	var g rerankGolden
	readGolden(t, "mmarco-minilm-l12", &g)
	got, err := r.Score(context.Background(), g.Query, g.Docs)
	if err != nil {
		t.Fatal(err)
	}
	var maxDiff float64
	for i := range got {
		maxDiff = math.Max(maxDiff, math.Abs(float64(got[i])-g.Logits[i]))
	}
	t.Logf("%s logits %v, max abs diff to ONNX Runtime %.2g", r.ModelID(), got, maxDiff)
	if maxDiff > 1e-4 {
		t.Fatalf("max abs diff %.3g, want <= 1e-4", maxDiff)
	}
	// docs 1 (Portuguese price) and 5 (English price) are the relevant ones
	for i, s := range got {
		if i != 1 && i != 5 && (s >= got[1] || s >= got[5]) {
			t.Errorf("doc %d (%.3f) not below the relevant ones (%.3f, %.3f)", i, s, got[1], got[5])
		}
	}
	if got[1] == got[5] {
		t.Error("relevant documents tie: logits should not saturate")
	}
}

// The logit is the pre-sigmoid value of hugot's own cross-encoder pipeline.
func TestRerankerLogitIsHugotScoreBeforeSigmoid(t *testing.T) {
	r := sharedReranker(t)
	ctx := context.Background()
	q, docs := "Quanto custa o plano premium?", []string{"O gato dorme no sofá da sala.", "A reunião foi remarcada."}
	got, err := r.Score(ctx, q, docs)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := khugot.NewPipeline(r.session, khugot.CrossEncoderConfig{
		ModelPath: resolve(modelDir(t, "mmarco-minilm-l12")), OnnxFilename: "model.onnx", Name: "check"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := pipe.RunPipeline(ctx, q, docs)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range out.Results {
		want := float32(1 / (1 + math.Exp(-float64(got[res.Index]))))
		if math.Abs(float64(want-res.Score)) > 1e-6 {
			t.Errorf("doc %d: sigmoid(logit) %v, hugot score %v", res.Index, want, res.Score)
		}
	}
}

// Determinism: same pairs give bit-identical logits on repeat, and a doc's
// logit does not depend on the other candidates; long docs are cut, not refused.
func TestRerankerDeterministic(t *testing.T) {
	r := sharedReranker(t)
	ctx := context.Background()
	q := "Quem é o responsável pelo contrato?"
	docs := []string{"O responsável pelo contrato é o João Silva.", "Ok.", strings.Repeat("Texto longo sobre outro assunto. ", 300)}
	a, err := r.Score(ctx, q, docs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.Score(ctx, q, docs)
	alone, _ := r.Score(ctx, q, docs[2:])
	if !bitsEqual(a, b) || math.Float32bits(a[2]) != math.Float32bits(alone[0]) {
		t.Fatalf("not deterministic: %v %v %v", a, b, alone)
	}
	if a[0] <= a[1] || a[0] <= a[2] {
		t.Errorf("relevant doc not first: %v", a)
	}
	if empty, err := r.Score(ctx, q, nil); err != nil || len(empty) != 0 {
		t.Fatalf("no docs: %v, %v", empty, err)
	}
}
