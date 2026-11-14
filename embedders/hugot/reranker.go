package hugot

import (
	"context"
	"sync"

	khugot "github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/backends"
	"github.com/knights-analytics/hugot/pipelines"
	"golang.org/x/text/unicode/norm"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Reranker is a cross-encoder (core.Reranker) over a local ONNX model,
// mmarco-mMiniLMv2 by default.
//
// Score returns the raw logit of each (query, doc) pair, higher is more
// relevant. hugot's CrossEncoderPipeline applies a float32 sigmoid that
// saturates at 1.0 for strong matches (two relevant documents tie), so the
// pair runs through hugot's backend directly and the logit is kept
// (decision 0035). Pairs are scored one per call, in input order, so a
// document's score does not depend on the other candidates; callers break
// equal scores by record id.
type Reranker struct {
	model Model
	id    string

	mu      sync.Mutex
	clip    *clipper
	session *khugot.Session
	pipe    *pipelines.CrossEncoderPipeline
}

var _ core.Reranker = (*Reranker)(nil)

// OpenReranker loads the cross-encoder in dir. It never uses the network.
func OpenReranker(ctx context.Context, dir string) (*Reranker, error) {
	m, err := detect(dir, KindReranker)
	if err != nil {
		return nil, err
	}
	r := &Reranker{model: m, id: m.ModelID()}
	err = stdoutToStderr(func() error {
		s, err := khugot.NewGoSession(ctx)
		if err != nil {
			return err
		}
		r.session = s
		if r.clip, err = newClipper(dir, m.MaxTokens); err != nil {
			return err
		}
		r.pipe, err = khugot.NewPipeline(s, khugot.CrossEncoderConfig{
			ModelPath: resolve(dir), OnnxFilename: "model.onnx", Name: "chanda-rerank"})
		if err != nil {
			return err
		}
		if sep := r.pipe.Model.SeparatorToken; sep != "</s>" {
			return core.Errf(core.CodeInvalidArgument, "separator %q: only XLM-R style (</s>) cross-encoders are supported", sep)
		}
		_, err = r.logit(ctx, "warm-up", "warm-up")
		return err
	})
	if err != nil {
		r.Close()
		return nil, core.Wrap(core.CodeUnavailable, err, "hugot: load "+m.Name+" from "+dir)
	}
	return r, nil
}

// ModelID is, for example, "mmarco-minilm-l12@hugot0.8.6+simd".
func (r *Reranker) ModelID() string { return r.id }

// Score returns one logit per doc, in the order of docs.
func (r *Reranker) Score(ctx context.Context, query string, docs []string) ([]float32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pipe == nil {
		return nil, core.Errf(core.CodeUnavailable, "hugot: reranker %s is closed", r.id)
	}
	q := norm.NFKC.String(query)
	out := make([]float32, len(docs))
	for i, d := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v, err := r.logit(ctx, q, norm.NFKC.String(d))
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "hugot: rerank with "+r.id)
		}
		out[i] = v
	}
	return out, nil
}

// Close releases the model.
func (r *Reranker) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return nil
	}
	err := r.session.Destroy()
	r.session, r.pipe = nil, nil
	return err
}

// logit scores one pair as "<s> query </s></s> doc </s>" (HF's XLM-R pair
// template, which is what hugot's pair tokenizer builds), cutting the doc
// when the pair exceeds the window. r.mu is held (or r is not shared yet).
func (r *Reranker) logit(ctx context.Context, query, doc string) (float32, error) {
	text := r.clip.clip(query + "</s></s>" + doc)
	model := r.pipe.Model
	res, err := backends.RunPipeline(ctx, 1,
		func(b *backends.PipelineBatch) error {
			backends.TokenizeInputs(b, model.Tokenizer, []string{text})
			return backends.CreateInputTensors(b, model)
		},
		func(ctx context.Context, b *backends.PipelineBatch) error {
			return backends.RunSessionOnBatch(ctx, b, r.pipe.BasePipeline)
		},
		func(b *backends.PipelineBatch) (*float32, error) {
			if len(b.OutputValues) > 0 {
				if v, ok := b.OutputValues[0].([][]float32); ok && len(v) == 1 && len(v[0]) >= 1 {
					return &v[0][0], nil
				}
			}
			return nil, core.Errf(core.CodeInternal, "unexpected cross-encoder output %T", b.OutputValues)
		})
	if err != nil {
		return 0, err
	}
	return *res, nil
}
