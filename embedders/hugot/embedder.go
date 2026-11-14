package hugot

import (
	"context"
	"math"
	"sync"

	khugot "github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
	"golang.org/x/text/unicode/norm"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Embedder is a sentence embedder (core.RoleEmbedder) over a local ONNX model.
//
// Determinism (decision 0034): input is NFKC-normalised (the patched
// tokenizer still lacks HF's Precompiled normalizer), every text is embedded
// in its own call (vectors are not batch-invariant at 1e-7), one warm-up call
// runs after load (e5 switches graph path on its first call), calls are
// serialised, and the mean-pooled vector is L2-normalised here in float64.
type Embedder struct {
	model Model
	id    string
	dir   string

	mu      sync.Mutex
	clip    *clipper
	session *khugot.Session
	pipe    *pipelines.FeatureExtractionPipeline
	calls   int // model calls since the session was loaded (see recycleEvery)
}

var _ core.RoleEmbedder = (*Embedder)(nil)

// Open loads the embedding model in dir. It never uses the network. Model
// load and warm-up run with stdout redirected to stderr.
func Open(ctx context.Context, dir string) (*Embedder, error) {
	m, err := detect(dir, KindEmbedder)
	if err != nil {
		return nil, err
	}
	return open(ctx, dir, m)
}

func open(ctx context.Context, dir string, m Model) (*Embedder, error) {
	e := &Embedder{model: m, id: m.ModelID(), dir: dir}
	var err error
	if e.clip, err = newClipper(dir, m.MaxTokens); err == nil {
		err = e.load(ctx)
	}
	if err != nil {
		e.Close()
		return nil, core.Wrap(core.CodeUnavailable, err, "hugot: load "+m.Name+" from "+dir)
	}
	return e, nil
}

// load creates the session and the pipeline and runs the warm-up call, with
// stdout redirected to stderr; e.mu is held (or e is not shared yet).
func (e *Embedder) load(ctx context.Context) error {
	return stdoutToStderr(func() error {
		s, err := khugot.NewGoSession(ctx)
		if err != nil {
			return err
		}
		e.session, e.calls = s, 0
		e.pipe, err = khugot.NewPipeline(s, khugot.FeatureExtractionConfig{
			ModelPath: resolve(e.dir), OnnxFilename: "model.onnx", Name: "chanda-embed"})
		if err != nil {
			return err
		}
		_, err = e.embedOne(ctx, e.model.PassagePrefix, "warm-up")
		return err
	})
}

// recycle reloads the session once it has served recycleEvery calls (decision
// 0136); e.mu is held. A failed reload leaves the embedder closed.
func (e *Embedder) recycle(ctx context.Context) error {
	if e.calls < recycleEvery {
		return nil
	}
	err := e.session.Destroy()
	e.session, e.pipe = nil, nil
	releaseMemory()
	if err == nil {
		// The session outlives this call: hugot derives the session's
		// context from the one given here, so it must not be cancelled
		// when the call that triggered the reload ends.
		err = e.load(context.WithoutCancel(ctx))
	}
	if err != nil {
		if e.session != nil {
			_ = e.session.Destroy()
		}
		e.session, e.pipe = nil, nil
		return core.Wrap(core.CodeUnavailable, err, "hugot: reload "+e.model.Name+" from "+e.dir)
	}
	return nil
}

// Model returns the model description.
func (e *Embedder) Model() Model { return e.model }

// ModelID is, for example, "e5-small@hugot0.8.6+simd".
func (e *Embedder) ModelID() string { return e.id }

// Dim is the vector dimension.
func (e *Embedder) Dim() int { return e.model.Dim }

// Embed embeds stored texts (passages): e5 gets the "passage: " prefix.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, e.model.PassagePrefix, texts)
}

// EmbedQuery embeds search queries: e5 gets the "query: " prefix.
func (e *Embedder) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, e.model.QueryPrefix, texts)
}

// Close releases the model.
func (e *Embedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session == nil {
		return nil
	}
	err := e.session.Destroy()
	e.session, e.pipe = nil, nil
	return err
}

func (e *Embedder) embed(ctx context.Context, prefix string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pipe == nil {
		return nil, core.Errf(core.CodeUnavailable, "hugot: embedder %s is closed", e.id)
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := e.recycle(ctx); err != nil {
			return nil, err
		}
		v, err := e.embedOne(ctx, prefix, t)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "hugot: embed with "+e.id)
		}
		out[i] = v
	}
	return out, nil
}

// embedOne runs one text through the model; e.mu is held (or e is not shared yet).
func (e *Embedder) embedOne(ctx context.Context, prefix, text string) ([]float32, error) {
	e.calls++
	res, err := e.pipe.RunPipeline(ctx, []string{e.clip.clip(prefix + norm.NFKC.String(text))})
	if err != nil {
		return nil, err
	}
	if len(res.Embeddings) != 1 || len(res.Embeddings[0]) != e.model.Dim {
		return nil, core.Errf(core.CodeInternal, "hugot: %s returned %d vectors, want 1 of dim %d", e.id, len(res.Embeddings), e.model.Dim)
	}
	return l2Normalize(res.Embeddings[0]), nil
}

// l2Normalize returns v scaled to unit length (accumulated in float64); the
// zero vector stays zero.
func l2Normalize(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	out := make([]float32, len(v))
	if s == 0 {
		return out
	}
	inv := 1 / math.Sqrt(s)
	for i, x := range v {
		out[i] = float32(float64(x) * inv)
	}
	return out
}
