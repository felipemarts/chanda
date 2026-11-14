package lme

import (
	"math"
	"sort"
	"strconv"
)

// Ks are the cutoffs reported by phase 1.
var Ks = []int{5, 10}

// QMetrics are the retrieval metrics of one question under one condition.
// Ranked is the condition's session ranking: distinct haystack session ids
// in order of first appearance in its result list.
type QMetrics struct {
	// Battery metrics over answer_session_ids.
	Recall map[int]float64 `json:"recall"` // |evidence in top k| / |evidence|
	MRR    float64         `json:"mrr"`    // 1 / rank of the first evidence session, 0 if none ranked
	// Official LongMemEval session-level metrics (eval_utils.evaluate_retrieval)
	// over the official targets (OfficialTargets).
	RecallAny map[int]float64 `json:"recall_any"`
	RecallAll map[int]float64 `json:"recall_all"`
	NDCGAny   map[int]float64 `json:"ndcg_any"`
}

// Score computes QMetrics for ranked sessions against evidence (battery
// metrics) and official targets.
func Score(ranked, evidence, official []string) QMetrics {
	m := QMetrics{Recall: map[int]float64{}, RecallAny: map[int]float64{}, RecallAll: map[int]float64{}, NDCGAny: map[int]float64{}}
	ev := set(evidence)
	for i, s := range ranked {
		if ev[s] {
			m.MRR = 1 / float64(i+1)
			break
		}
	}
	for _, k := range Ks {
		top := set(ranked[:min(k, len(ranked))])
		if len(ev) > 0 {
			hit := 0
			for s := range ev {
				if top[s] {
					hit++
				}
			}
			m.Recall[k] = float64(hit) / float64(len(ev))
		}
		m.RecallAny[k], m.RecallAll[k], m.NDCGAny[k] = official3(ranked, official, k)
	}
	return m
}

// official3 is eval_utils.evaluate_retrieval at session granularity:
// recall_any = any target in the top k, recall_all = every target in the
// top k, ndcg_any = DCG of binary relevances of the top k over the ideal DCG
// (all targets first). DCG follows eval_utils.dcg: rel[0] + sum rel[i]/log2(i+1)
// for i >= 1 (1-based position i+1).
func official3(ranked, targets []string, k int) (any, all, ndcg float64) {
	if len(targets) == 0 {
		return 0, 0, 0
	}
	tg := set(targets)
	top := ranked[:min(k, len(ranked))]
	ts := set(top)
	any, all = 0, 1
	for t := range tg {
		if ts[t] {
			any = 1
		} else {
			all = 0
		}
	}
	rel := make([]float64, len(top))
	for i, s := range top {
		if tg[s] {
			rel[i] = 1
		}
	}
	ideal := make([]float64, min(k, len(tg)))
	for i := range ideal {
		ideal[i] = 1
	}
	if d := dcg(ideal); d > 0 {
		ndcg = dcg(rel) / d
	}
	return any, all, ndcg
}

func dcg(rel []float64) float64 {
	if len(rel) == 0 {
		return 0
	}
	s := rel[0]
	for i := 1; i < len(rel); i++ {
		s += rel[i] / math.Log2(float64(i+1))
	}
	return s
}

func set(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Agg is the mean of QMetrics over a group of questions.
type Agg struct {
	N         int                `json:"n"`
	Recall    map[string]float64 `json:"recall"` // keys "@5", "@10"
	MRR       float64            `json:"mrr"`
	NOfficial int                `json:"n_official"` // questions with official targets
	RecallAny map[string]float64 `json:"recall_any"`
	RecallAll map[string]float64 `json:"recall_all"`
	NDCGAny   map[string]float64 `json:"ndcg_any"`
}

// QResult is what an aggregate needs from one question.
type QResult struct {
	Type        string
	Abstention  bool
	HasOfficial bool
	M           QMetrics
}

// Aggregate averages results by group: "all" (every non-abstention
// question), each question_type (non-abstention only) and Abstention
// (count only: abstention questions have no evidence location and the
// official retrieval evaluation skips them). Official metrics average only
// the questions with an official target, as run_retrieval.py does.
func Aggregate(rs []QResult) map[string]*Agg {
	out := map[string]*Agg{}
	get := func(g string) *Agg {
		a := out[g]
		if a == nil {
			a = &Agg{Recall: map[string]float64{}, RecallAny: map[string]float64{}, RecallAll: map[string]float64{}, NDCGAny: map[string]float64{}}
			out[g] = a
		}
		return a
	}
	for _, r := range rs {
		if r.Abstention {
			get(Abstention).N++
			continue
		}
		for _, g := range []string{"all", r.Type} {
			a := get(g)
			a.N++
			a.MRR += r.M.MRR
			for _, k := range Ks {
				a.Recall[kname(k)] += r.M.Recall[k]
			}
			if r.HasOfficial {
				a.NOfficial++
				for _, k := range Ks {
					a.RecallAny[kname(k)] += r.M.RecallAny[k]
					a.RecallAll[kname(k)] += r.M.RecallAll[k]
					a.NDCGAny[kname(k)] += r.M.NDCGAny[k]
				}
			}
		}
	}
	for _, a := range out {
		if a.N > 0 {
			a.MRR /= float64(a.N)
			for k := range a.Recall {
				a.Recall[k] /= float64(a.N)
			}
		}
		if a.NOfficial > 0 {
			for k := range a.RecallAny {
				a.RecallAny[k] /= float64(a.NOfficial)
				a.RecallAll[k] /= float64(a.NOfficial)
				a.NDCGAny[k] /= float64(a.NOfficial)
			}
		}
	}
	return out
}

func kname(k int) string { return "@" + strconv.Itoa(k) }

// Groups returns the aggregate keys in report order.
func Groups(m map[string]*Agg) []string {
	var ks []string
	for k := range m {
		if k != "all" && k != Abstention {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	out := []string{}
	if m["all"] != nil {
		out = append(out, "all")
	}
	out = append(out, ks...)
	if m[Abstention] != nil {
		out = append(out, Abstention)
	}
	return out
}

// RankSessions turns a ranked list of items, each mapped to zero or more
// session ids, into distinct sessions by first appearance.
func RankSessions(items [][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ss := range items {
		for _, s := range ss {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}
