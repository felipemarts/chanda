package eval

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
)

// QuestionResult is the judgement of one system's answer to one question.
// It holds only deterministic values (no latency).
type QuestionResult struct {
	QuestionID       string   `json:"id"`
	Tags             []string `json:"tags,omitempty"`
	Answerable       bool     `json:"answerable"`
	Delivered        []string `json:"delivered"`  // ids in rank order, after budget enforcement
	GoldRanks        []int    `json:"gold_ranks"` // per relevance unit: 1-based rank of its first hit, 0 = missed
	Tokens           int      `json:"tokens"`
	RecallAt5        float64  `json:"recall_at_5"`
	RecallAt10       float64  `json:"recall_at_10"`
	MRR              float64  `json:"mrr"`
	NDCGAt10         float64  `json:"ndcg_at_10"`
	DocRecallAt10    float64  `json:"doc_recall_at_10"`
	HasAnswers       bool     `json:"-"`
	AnswerHit        bool     `json:"answer_in_context"` // an expected answer substring is in the context
	HasForbidden     bool     `json:"-"`
	Forbidden        bool     `json:"forbidden_in_context"` // a must_not_contain string is in the context
	Empty            bool     `json:"empty"`
	BudgetViolations int      `json:"budget_violations,omitempty"`
}

// unit is one relevance unit: alternatives that all count as the same evidence.
type unit struct {
	golds []Gold
	docs  map[string]bool
}

func units(gs []Gold) []unit {
	var out []unit
	idx := map[string]int{}
	for i, g := range gs {
		key := g.Unit
		if key == "" {
			key = "\x00" + strconv.Itoa(i)
		}
		j, ok := idx[key]
		if !ok {
			j = len(out)
			idx[key] = j
			out = append(out, unit{docs: map[string]bool{}})
		}
		out[j].golds = append(out[j].golds, g)
		out[j].docs[g.DocID] = true
	}
	return out
}

// hits reports whether a retrieved excerpt is evidence for gold g: same
// corpus item and, when g has a span, the excerpt contains it (normalized).
// Matching by span makes the metric independent of each system's chunking.
func hits(r Retrieved, normText string, g Gold) bool {
	return r.DocID == g.DocID && (g.Span == "" || strings.Contains(normText, Normalize(g.Span)))
}

// Judge scores one ranked, budget-enforced result list against q.
//
//   - recall@k: share of relevance units hit within the first k items.
//   - MRR: 1/rank of the first item that hits any unit.
//   - nDCG@k: gain at rank i = number of units first hit at i; ideal = one
//     new unit per rank; clamped to 1 (one excerpt may cover two units).
//   - doc recall@k: a unit counts if any of its items appears in the first k.
func Judge(q Question, rs []Retrieved) QuestionResult {
	res := QuestionResult{QuestionID: q.ID, Tags: q.Tags, Answerable: q.Answerable(), Empty: len(rs) == 0,
		HasAnswers: len(q.Answers) > 0, HasForbidden: len(q.MustNotContain) > 0, Delivered: []string{}}
	norm := make([]string, len(rs))
	var ctx strings.Builder
	for i, r := range rs {
		norm[i] = Normalize(r.Text)
		ctx.WriteString(norm[i] + "\n")
		res.Tokens += r.Tokens
		res.Delivered = append(res.Delivered, r.ID)
	}
	all := ctx.String()
	for _, a := range q.Answers {
		res.AnswerHit = res.AnswerHit || strings.Contains(all, Normalize(a))
	}
	for _, f := range q.MustNotContain {
		res.Forbidden = res.Forbidden || strings.Contains(all, Normalize(f))
	}
	us := units(q.Gold)
	if len(us) == 0 {
		return res
	}
	res.GoldRanks = make([]int, len(us))
	docRank := make([]int, len(us))
	for i, r := range rs {
		rank := i + 1
		hit := false
		for j, u := range us {
			if docRank[j] == 0 && u.docs[r.DocID] {
				docRank[j] = rank
			}
			for _, g := range u.golds {
				if hits(r, norm[i], g) {
					hit = true
					if res.GoldRanks[j] == 0 {
						res.GoldRanks[j] = rank
					}
				}
			}
		}
		if hit && res.MRR == 0 {
			res.MRR = 1 / float64(rank)
		}
	}
	res.RecallAt5 = recallAt(res.GoldRanks, 5)
	res.RecallAt10 = recallAt(res.GoldRanks, 10)
	res.DocRecallAt10 = recallAt(docRank, 10)
	res.NDCGAt10 = ndcgAt(res.GoldRanks, 10)
	return res
}

func recallAt(firstHit []int, k int) float64 {
	n := 0
	for _, r := range firstHit {
		if r > 0 && r <= k {
			n++
		}
	}
	return float64(n) / float64(len(firstHit))
}

func ndcgAt(firstHit []int, k int) float64 {
	var dcg, idcg float64
	for _, r := range firstHit {
		if r > 0 && r <= k {
			dcg += 1 / math.Log2(float64(r)+1)
		}
	}
	for i := 1; i <= min(len(firstHit), k); i++ {
		idcg += 1 / math.Log2(float64(i)+1)
	}
	return min(1, dcg/idcg)
}

// Metrics aggregates QuestionResults. Retrieval means are over answerable
// questions; nil pointers mean "undefined" (e.g. a system that never abstains
// has no abstention precision).
type Metrics struct {
	Questions           int      `json:"questions"`
	Answerable          int      `json:"answerable"`
	RecallAt5           float64  `json:"recall_at_5"`
	RecallAt10          float64  `json:"recall_at_10"`
	MRR                 float64  `json:"mrr"`
	NDCGAt10            float64  `json:"ndcg_at_10"`
	DocRecallAt10       float64  `json:"doc_recall_at_10"`
	AnswerInContext     *float64 `json:"answer_in_context"`    // over questions with expected answers
	ForbiddenRate       *float64 `json:"forbidden_rate"`       // over questions with must_not_contain
	AbstentionPrecision *float64 `json:"abstention_precision"` // empty results that were truly unanswerable
	AbstentionRecall    *float64 `json:"abstention_recall"`    // unanswerable questions answered with empty
	EmptyRate           float64  `json:"empty_rate"`
	TokensMean          float64  `json:"tokens_mean"`
	TokensP50           int      `json:"tokens_p50"`
	TokensP95           int      `json:"tokens_p95"`
	BudgetViolations    int      `json:"budget_violations"`
}

// Aggregate computes Metrics over rs.
func Aggregate(rs []QuestionResult) Metrics {
	m := Metrics{Questions: len(rs)}
	if len(rs) == 0 {
		return m
	}
	var answers, answerHits, forb, forbHits, empty, unans, tp int
	tokens := make([]int, 0, len(rs))
	for _, r := range rs {
		tokens = append(tokens, r.Tokens)
		m.TokensMean += float64(r.Tokens)
		m.BudgetViolations += r.BudgetViolations
		if r.Empty {
			empty++
		}
		if !r.Answerable {
			unans++
			if r.Empty {
				tp++
			}
		} else {
			m.Answerable++
			m.RecallAt5 += r.RecallAt5
			m.RecallAt10 += r.RecallAt10
			m.MRR += r.MRR
			m.NDCGAt10 += r.NDCGAt10
			m.DocRecallAt10 += r.DocRecallAt10
		}
		if r.HasAnswers {
			answers++
			if r.AnswerHit {
				answerHits++
			}
		}
		if r.HasForbidden {
			forb++
			if r.Forbidden {
				forbHits++
			}
		}
	}
	if m.Answerable > 0 {
		n := float64(m.Answerable)
		m.RecallAt5, m.RecallAt10, m.MRR, m.NDCGAt10, m.DocRecallAt10 = m.RecallAt5/n, m.RecallAt10/n, m.MRR/n, m.NDCGAt10/n, m.DocRecallAt10/n
	}
	m.AnswerInContext = ratio(answerHits, answers)
	m.ForbiddenRate = ratio(forbHits, forb)
	m.AbstentionPrecision = ratio(tp, empty)
	m.AbstentionRecall = ratio(tp, unans)
	m.EmptyRate = float64(empty) / float64(len(rs))
	m.TokensMean /= float64(len(rs))
	m.TokensP50, m.TokensP95 = Percentile(tokens, 50), Percentile(tokens, 95)
	return m
}

func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}

// Percentile is the nearest-rank percentile (p in (0, 100]) of xs; zero if empty.
func Percentile[T cmp.Ordered](xs []T, p float64) T {
	var zero T
	if len(xs) == 0 {
		return zero
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(math.Ceil(p/100*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

// EnforceBudget recomputes every item's tokens with CountTokens and keeps,
// in rank order, the items that fit in budget. It returns the kept items and
// how many were dropped (a system that respects the budget drops none).
func EnforceBudget(rs []Retrieved, budget int) ([]Retrieved, int) {
	var out []Retrieved
	used, dropped := 0, 0
	for _, r := range rs {
		r.Tokens = CountTokens(r.Text)
		if used+r.Tokens > budget {
			dropped++
			continue
		}
		used += r.Tokens
		out = append(out, r)
	}
	return out, dropped
}
