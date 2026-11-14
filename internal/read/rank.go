package read

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// cand is one candidate as it moves through the stages.
type cand struct {
	rec                  *record
	kwRank, vecRank      int
	bm25, cosine, rerank float64
	hasBM25, hasCos      bool
	hasRerank            bool
	rrf, relNorm         float64
	coverage, evidence   float64
	relevance            float64
	utility, learned     float64
	hasUtil, hasLearned  bool
	final                float64
	ageDays              int
	dropped              string // "" while alive; a drop* reason otherwise
	scored               bool   // reached fusion
}

func (c *cand) id() string { return c.rec.id }

// merge folds the list positions of a near-duplicate into its representative.
func (c *cand) merge(o *cand) {
	if o.kwRank > 0 && (c.kwRank == 0 || o.kwRank < c.kwRank) {
		c.kwRank = o.kwRank
	}
	if o.vecRank > 0 && (c.vecRank == 0 || o.vecRank < c.vecRank) {
		c.vecRank = o.vecRank
	}
	if o.hasBM25 && (!c.hasBM25 || o.bm25 > c.bm25) {
		c.bm25, c.hasBM25 = o.bm25, true
	}
	if o.hasCos && (!c.hasCos || o.cosine > c.cosine) {
		c.cosine, c.hasCos = o.cosine, true
	}
}

// fuse computes Reciprocal Rank Fusion and the normalized relevance.
//
//	rrf     = Σ over lists of 1 / (k + rank)
//	relNorm = rrf / (L / (k + 1))
//
// The keyword side is one list per record type (BM25 scales differ between
// the FTS tables, so ranks are compared within a type); the vector side is
// one list. L is the number of list kinds that ran (1 keyword-only, 2
// hybrid), so relNorm is in [0,1] and equals 1 only for an item ranked first
// in every list. The normalization is query-independent.
func fuse(cs []*cand, k int, lists int) {
	if k < 0 {
		k = 0
	}
	maxRRF := float64(lists) / float64(k+1)
	for _, c := range cs {
		c.rrf = 0
		if c.kwRank > 0 {
			c.rrf += 1 / float64(k+c.kwRank)
		}
		if c.vecRank > 0 {
			c.rrf += 1 / float64(k+c.vecRank)
		}
		c.relNorm = 0
		if maxRRF > 0 {
			c.relNorm = math.Min(1, c.rrf/maxRRF)
		}
		c.relevance = c.relNorm
		c.scored = true
	}
}

// computeEvidence sets the lexical coverage and the evidence of each
// candidate (decision 0020). evidence = max(coverage, cosine clamped to
// [0,1]); a candidate has enough evidence for the context cut when
// coverage ≥ MinTermCoverage or cosine ≥ MinCosine.
func (r *Reader) computeEvidence(cs []*cand, terms []string) {
	for _, c := range cs {
		c.coverage = coverage(terms, c.rec.searchText(), r.e.Cfg.Read.CoverageTermCap)
		sem := 0.0
		if c.hasCos {
			sem = math.Max(0, math.Min(1, c.cosine))
		}
		c.evidence = math.Max(c.coverage, sem)
	}
}

func (r *Reader) hasEvidence(c *cand) bool {
	return c.coverage >= r.e.Cfg.Read.MinTermCoverage || (c.hasCos && c.cosine >= r.e.Cfg.Read.MinCosine)
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// rerank scores the top cfg.Read.RerankTopN candidates by relevance with the
// cross-encoder. With the reranker on, relevance becomes
//
//	(relNorm + σ(rerank)) / 2   for reranked candidates
//	relNorm / 2                 for the rest (no reranker evidence)
//
// which stays in [0,1] and keeps the unreranked tail below comparable
// reranked items. Returns false when the step is skipped.
func (r *Reader) rerankStep(ctx context.Context, query string, cs []*cand) (bool, error) {
	if r.deps.Reranker == nil || !r.e.Cfg.Read.RerankEnabled || r.e.Cfg.Read.RerankTopN <= 0 || len(cs) == 0 {
		return false, nil
	}
	sorted := append([]*cand(nil), cs...)
	sortByRelevance(sorted)
	top := sorted[:min(len(sorted), r.e.Cfg.Read.RerankTopN)]
	docs := make([]string, len(top))
	for i, c := range top {
		docs[i] = c.rec.searchText()
	}
	scores, err := r.deps.Reranker.Score(ctx, query, docs)
	if err != nil {
		return false, err
	}
	if len(scores) != len(top) {
		return false, core.Errf(core.CodeInternal, "reranker returned %d scores for %d docs", len(scores), len(top))
	}
	for _, c := range cs {
		c.relevance = c.relNorm / 2
	}
	for i, c := range top {
		c.rerank, c.hasRerank = float64(scores[i]), true
		c.relevance = (c.relNorm + sigmoid(c.rerank)) / 2
	}
	return true, nil
}

// lambda returns λ clamped to [0, ranking.lambda_max]; 0 when learning is
// off or no Utility is wired.
func (r *Reader) lambda(ctx context.Context, ns string) (float64, error) {
	if r.deps.Utility == nil || !r.e.Cfg.Ranking.Enabled {
		return 0, nil
	}
	l, err := r.deps.Utility.Lambda(ctx, ns)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(l) || l < 0 {
		l = 0
	}
	return math.Min(l, r.e.Cfg.Ranking.LambdaMax), nil
}

// adjust applies read step 7: final = (1 − λ)·relevance + λ·utility. A
// candidate with no utility yet keeps final = relevance. Utility scores are
// logged even when λ = 0 (they are ranking features).
func (r *Reader) adjust(ctx context.Context, ns string, cs []*cand, lambda float64) error {
	if r.deps.Utility != nil && len(cs) > 0 {
		ids := make([]string, len(cs))
		for i, c := range cs {
			ids[i] = c.id()
		}
		sort.Strings(ids)
		us, err := r.deps.Utility.Scores(ctx, ns, ids)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if u, ok := us[c.id()]; ok && !math.IsNaN(u) {
				c.utility, c.hasUtil = math.Max(0, math.Min(1, u)), true
			}
		}
	}
	for _, c := range cs {
		c.final = c.relevance
		if c.hasUtil {
			c.final = (1-lambda)*c.relevance + lambda*c.utility
		}
	}
	return nil
}

// shadow logs the learned score of every candidate without using it.
func (r *Reader) shadow(ctx context.Context, ns string, cs []*cand) {
	if r.deps.Learned == nil {
		return
	}
	for _, c := range cs {
		if v, ok := r.deps.Learned.Score(ctx, ns, c.candidate()); ok && !math.IsNaN(v) {
			c.learned, c.hasLearned = v, true
		}
	}
}

func (c *cand) candidate() Candidate {
	return Candidate{ID: c.id(), Type: c.rec.typ, Zone: c.rec.zone, AgeDays: c.ageDays, Scores: c.scores()}
}

func ptr(v float64) *float64 { return &v }

func (c *cand) scores() Scores {
	s := Scores{KeywordRank: c.kwRank, VectorRank: c.vecRank}
	if c.hasBM25 {
		s.BM25 = ptr(c.bm25)
	}
	if c.hasCos {
		s.Cosine = ptr(c.cosine)
	}
	if c.hasRerank {
		s.Rerank = ptr(c.rerank)
	}
	if c.scored {
		s.RRF, s.Coverage, s.Evidence, s.Relevance, s.Final = ptr(c.rrf), ptr(c.coverage), ptr(c.evidence), ptr(c.relevance), ptr(c.final)
	}
	if c.hasUtil {
		s.Utility = ptr(c.utility)
	}
	if c.hasLearned {
		s.Learned = ptr(c.learned)
	}
	return s
}

// ageDays is whole days between the record's time and now (floor, ≥ 0).
// Whole days keep logged features identical across repeated queries.
func ageDays(ts string, now time.Time) int {
	t, err := core.ParseTime(ts)
	if err != nil || t.After(now) {
		return 0
	}
	return int(now.Sub(t) / (24 * time.Hour))
}

// sortByFinal orders by final desc, relevance desc, id asc (total order).
func sortByFinal(cs []*cand) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.final != b.final {
			return a.final > b.final
		}
		if a.relevance != b.relevance {
			return a.relevance > b.relevance
		}
		return a.id() < b.id()
	})
}

func sortByRelevance(cs []*cand) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].relevance != cs[j].relevance {
			return cs[i].relevance > cs[j].relevance
		}
		return cs[i].id() < cs[j].id()
	})
}
