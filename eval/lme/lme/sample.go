package lme

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
)

// SampleAlgorithm describes Draw; it is written into sample.json.
const SampleAlgorithm = `Stratified draw without replacement, deterministic.
1. Pool: every question of the dataset file, keyed by question_type and by
   abstention (question_id ends with "_abs").
2. Per-type quota: n * count(type) / total by largest remainder (Hamilton):
   the floor of each exact share, then one seat at a time to the type whose
   exact share exceeds its seats the most; ties to the type that sorts first.
3. Abstention quota: max(min_abstention, round(n * abstention / total)),
   spread the same way over the types that have abstention questions, in
   proportion to their abstention counts and never above that type's quota
   or its number of abstention questions; the rest of a type's quota comes
   from its non-abstention questions.
4. Draw: one math/rand/v2 PCG source seeded (seed, seed). Strata are visited
   in sorted order (type, then non-abstention before abstention); each
   stratum's question ids are sorted, shuffled with rand.Shuffle and the
   first k kept.
5. Order: the selected questions are shuffled once more with the same source;
   that order is the order of the file ("the first 10 questions of the
   sample" are the first 10 entries).
The draw reads only question ids and types, never answers or results.`

// SampleEntry is one sampled question.
type SampleEntry struct {
	QuestionID   string `json:"question_id"`
	QuestionType string `json:"question_type"`
	Abstention   bool   `json:"abstention"`
}

// Sample is the content of eval/sample.json.
type Sample struct {
	Dataset        string         `json:"dataset"`
	File           string         `json:"file"`
	Revision       string         `json:"revision"`
	SHA256         string         `json:"sha256"`
	Seed           uint64         `json:"seed"`
	N              int            `json:"n"`
	MinAbstention  int            `json:"min_abstention"`
	Algorithm      string         `json:"algorithm"`
	Generator      string         `json:"generator"`
	PopulationType map[string]int `json:"population_by_type"`
	PopulationAbs  map[string]int `json:"population_abstention_by_type"`
	QuotaType      map[string]int `json:"quota_by_type"`
	QuotaAbs       map[string]int `json:"quota_abstention_by_type"`
	Questions      []SampleEntry  `json:"questions"`
}

// largestRemainder splits n seats proportionally to weights, never giving a
// key more than its cap: each key first gets floor(exact share), capped; then
// seats go one at a time to the uncapped key whose exact share exceeds its
// seats the most (Hamilton's largest remainder; ties to the key that sorts
// first).
func largestRemainder(n int, weights, caps map[string]int) map[string]int {
	keys := make([]string, 0, len(weights))
	total := 0
	for k, w := range weights {
		if w > 0 {
			keys = append(keys, k)
			total += w
		}
	}
	sort.Strings(keys)
	out := map[string]int{}
	if total == 0 || n <= 0 {
		return out
	}
	exact := map[string]float64{}
	given := 0
	for _, k := range keys {
		exact[k] = float64(n) * float64(weights[k]) / float64(total)
		out[k] = int(exact[k])
		if c, ok := caps[k]; ok && out[k] > c {
			out[k] = c
		}
		given += out[k]
	}
	capped := func(k string) bool { c, ok := caps[k]; return ok && out[k] >= c }
	for given < n {
		best := ""
		for _, k := range keys {
			if !capped(k) && (best == "" || exact[k]-float64(out[k]) > exact[best]-float64(out[best])) {
				best = k
			}
		}
		if best == "" {
			break // every key at its cap
		}
		out[best]++
		given++
	}
	return out
}

// Draw selects n questions from pop (id -> type) as SampleAlgorithm says.
func Draw(pop map[string]string, n, minAbs int, seed uint64) (*Sample, error) {
	if n <= 0 || n > len(pop) {
		return nil, fmt.Errorf("sample: n=%d with %d questions", n, len(pop))
	}
	byType := map[string]int{}
	absByType := map[string]int{}
	pools := map[string][]string{} // "type|0" non-abstention, "type|1" abstention
	nAbs := 0
	for id, t := range pop {
		byType[t]++
		key := t + "|0"
		if IsAbstention(id) {
			absByType[t]++
			nAbs++
			key = t + "|1"
		}
		pools[key] = append(pools[key], id)
	}
	quota := largestRemainder(n, byType, nil)
	wantAbs := max(minAbs, int(float64(n)*float64(nAbs)/float64(len(pop))+0.5))
	if wantAbs > nAbs {
		return nil, fmt.Errorf("sample: %d abstention questions wanted, %d exist", wantAbs, nAbs)
	}
	caps := map[string]int{}
	for t, c := range absByType {
		caps[t] = min(c, quota[t])
	}
	absQuota := largestRemainder(wantAbs, absByType, caps)
	got := 0
	for _, v := range absQuota {
		got += v
	}
	if got < wantAbs {
		return nil, fmt.Errorf("sample: only %d abstention seats fit the type quotas, %d wanted", got, wantAbs)
	}
	r := rand.New(rand.NewPCG(seed, seed))
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	var picked []SampleEntry
	for _, t := range types {
		for _, abs := range []bool{false, true} {
			k := quota[t] - absQuota[t]
			key := t + "|0"
			if abs {
				k, key = absQuota[t], t+"|1"
			}
			pool := append([]string(nil), pools[key]...)
			if k > len(pool) {
				return nil, fmt.Errorf("sample: stratum %s needs %d of %d", key, k, len(pool))
			}
			sort.Strings(pool)
			r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			for _, id := range pool[:k] {
				picked = append(picked, SampleEntry{QuestionID: id, QuestionType: t, Abstention: abs})
			}
		}
	}
	r.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
	return &Sample{
		Seed: seed, N: n, MinAbstention: minAbs, Algorithm: SampleAlgorithm,
		PopulationType: byType, PopulationAbs: absByType, QuotaType: quota, QuotaAbs: absQuota, Questions: picked,
	}, nil
}

// ReadSample reads a sample file.
func ReadSample(path string) (*Sample, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Sample
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// IDs returns the sample's question ids in order.
func (s *Sample) IDs() []string {
	out := make([]string, len(s.Questions))
	for i, q := range s.Questions {
		out[i] = q.QuestionID
	}
	return out
}
