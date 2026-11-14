package read

import (
	"context"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// scoreRetrievalID names the block of a scoring query in its rendered text.
// No retrieval is ever logged under it.
const scoreRetrievalID = "ret_score"

// ScoreOptions configure a read-only scoring query (Score).
type ScoreOptions struct {
	// Q, when set, receives every query: typically the transaction of the
	// sleep gate, which holds the single database connection. The query is
	// then keyword only (the vector index reads the store on its own), and
	// Utility, Deps.Term and Learned are not consulted (they do too). The
	// reranker, which needs no database, still runs.
	Q store.Queryer
	// Lambda fixes λ instead of reading the namespace's (promotion evaluates
	// a model at the λ it would get). λ only counts together with a Term.
	Lambda *float64
	// Term replaces Deps.Term: the model being evaluated. With Q set it must
	// not query the store.
	Term Term
}

// ScoreInput is one evaluation query.
type ScoreInput struct {
	Namespace    string
	Query        string
	Mode         Mode      // ModeContext (default) or ModeSearch
	AsOf         time.Time // zero = now
	BudgetTokens int       // context: 0 = read.context_default_tokens
	Limit        int       // search: 0 = read.search_default_limit
}

// ScoreResult is what the query would deliver, in delivered order.
type ScoreResult struct {
	Items  []Item
	Text   string // context mode: the rendered block
	Reason string
}

// Score runs the read path without side effects, for evaluation (the sleep
// gate and ranking promotion, decision 0092): no retrieval is logged, no
// audit entry is written, no id is drawn from the generator and the shadow
// score is not computed. Everything else (rule filter, near-duplicate
// collapse, fusion, rerank, step 7, cut, diversity, budget) is the read path
// of Search and Context.
func (r *Reader) Score(ctx context.Context, in ScoreInput, o ScoreOptions) (ScoreResult, error) {
	rr := *r
	rr.scoreOnly, rr.q, rr.lambdaFix = true, o.Q, o.Lambda
	if o.Q != nil {
		rr.deps.Embedder, rr.deps.VectorIndex, rr.deps.Utility, rr.deps.Term, rr.deps.Learned = nil, nil, nil, nil, nil
	}
	if o.Term != nil {
		rr.deps.Term = o.Term
	}
	req := request{mode: in.Mode, ns: in.Namespace, text: in.Query, asOf: in.AsOf}
	switch in.Mode {
	case ModeSearch:
		req.limit = in.Limit
		if req.limit <= 0 {
			req.limit = r.e.Cfg.Read.SearchDefaultLimit
		}
		if m := r.e.Cfg.Read.SearchMaxLimit; m > 0 && req.limit > m {
			req.limit = m
		}
	case ModeContext, "":
		req.mode = ModeContext
		req.budget = in.BudgetTokens
		if req.budget <= 0 {
			req.budget = r.e.Cfg.Read.ContextDefaultTokens
		}
		if m := r.e.Cfg.Read.ContextMaxTokens; m > 0 && req.budget > m {
			req.budget = m
		}
	default:
		return ScoreResult{}, core.Errf(core.CodeInvalidArgument, "unknown read mode %q", in.Mode)
	}
	out, err := rr.run(ctx, req)
	if err != nil {
		return ScoreResult{}, err
	}
	return ScoreResult{Items: out.items, Text: out.text, Reason: out.reason}, nil
}
