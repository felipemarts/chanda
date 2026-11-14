package eval

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/dedup"
)

// Per-model similarity thresholds of the write and sleep paths (decision
// 0125): write.near_dup_threshold and the vector side of
// sleep.merge_similarity. Both compare a cosine of the embedding model with a
// fixed value, so a value chosen for one model means something else for
// another. They are calibrated here from pairs built from the corpus, never
// from evaluated questions, with one rule:
//
//   - a threshold is acceptable when no negative pair (two different things
//     the threshold alone must keep apart) reaches it, and at most 5% of the
//     positive pairs (real duplicates or variants) miss it (BASE.md: "real
//     duplicates not collapsed: up to 5%");
//   - the current value is kept when it is acceptable; otherwise the middle
//     of the acceptable interval is taken; when no value is acceptable, the
//     lowest value that no negative pair reaches (a false merge or a hidden
//     record is worse than a missed duplicate), and the misses are reported.

// ThresholdCalibration is the result for one threshold and model.
type ThresholdCalibration struct {
	Key       string   `json:"key"`
	ModelID   string   `json:"model_id"`
	Current   float64  `json:"current"`
	Chosen    float64  `json:"chosen"`
	Rule      string   `json:"rule"` // kept | middle | above_negatives
	Positives int      `json:"positives"`
	Negatives int      `json:"negatives"`
	PosMin    float64  `json:"pos_min"`
	PosP05    float64  `json:"pos_p05"` // the lowest value 95% of positives reach
	NegMax    float64  `json:"neg_max"`
	Missed    int      `json:"missed"`     // positives the chosen value does not pass
	FalseHits int      `json:"false_hits"` // negatives it passes
	MissedAt  int      `json:"missed_at_current"`
	FalseAt   int      `json:"false_at_current"`
	TopNeg    []string `json:"top_negatives"` // the closest negative pairs, for review
}

// chooseThreshold applies the rule above. strict: a pair passes when its
// cosine is above t (the near-duplicate guard); otherwise at or above t
// (merge proposals).
func chooseThreshold(key string, pos, neg []float64, current float64, strict bool) ThresholdCalibration {
	passes := func(v, t float64) bool {
		if strict {
			return v > t
		}
		return v >= t
	}
	count := func(t float64) (missed, falseHits int) {
		for _, v := range pos {
			if !passes(v, t) {
				missed++
			}
		}
		for _, v := range neg {
			if passes(v, t) {
				falseHits++
			}
		}
		return
	}
	allowed := int(math.Floor(0.05 * float64(len(pos))))
	ok := func(t float64) bool { m, f := count(t); return f == 0 && m <= allowed }
	sp := append([]float64(nil), pos...)
	sort.Float64s(sp)
	negMax := math.Inf(-1)
	for _, v := range neg {
		negMax = math.Max(negMax, v)
	}
	r := ThresholdCalibration{Key: key, Current: current, Positives: len(pos), Negatives: len(neg)}
	if len(sp) > 0 {
		r.PosMin, r.PosP05 = sp[0], sp[allowed]
	}
	if len(neg) > 0 {
		r.NegMax = negMax
	}
	r.MissedAt, r.FalseAt = count(current)
	switch {
	case ok(current):
		r.Chosen, r.Rule = current, "kept"
	case len(sp) > 0 && ok(round4((math.Max(negMax, 0)+sp[allowed])/2)):
		r.Chosen, r.Rule = round4((math.Max(negMax, 0)+sp[allowed])/2), "middle"
	default:
		t := math.Ceil(negMax*1e4) / 1e4
		for _, f := count(t); f > 0; _, f = count(t) {
			t = round4(t + 1e-4)
		}
		r.Chosen, r.Rule = math.Min(t, 1), "above_negatives"
	}
	r.Missed, r.FalseHits = count(r.Chosen)
	return r
}

var probeNumber = regexp.MustCompile(`\d+`)

// DedupProbeBases returns the paragraphs the deduplication suite builds its
// probes from: document paragraphs (source "document") with a number, at
// least 8 words and no "key: value" line, the first 40 in corpus order.
func DedupProbeBases(ds *Dataset, maxChunkChars int) []string {
	var bases []string
	seen := map[string]bool{}
	for _, it := range ds.Corpus {
		if it.Type != ItemDocument || it.Source != "document" {
			continue
		}
		for _, c := range ChunkItem(it, maxChunkChars) {
			if len(bases) >= 40 {
				return bases
			}
			t := strings.TrimSpace(c.Text)
			if seen[t] || len(strings.Fields(t)) < 8 || !probeNumber.MatchString(t) || strings.Contains(t, ": ") {
				continue
			}
			seen[t] = true
			bases = append(bases, t)
		}
	}
	return bases
}

// courtesy are sentences appended to make a real duplicate (none is a
// suite probe word).
var courtesy = []string{"Obrigado.", "Atenciosamente.", "Confirmado.", "Até mais."}

// nearDupPairs builds the calibration pairs of the near-duplicate guard
// from the corpus. Paragraphs: every distinct chunk text of at least 8
// words, except the deduplication suite's own bases (so the suite stays a
// held-out measurement). Positives: three real duplicates of each paragraph
// (first letter's case swapped; a courtesy sentence appended; the final
// period removed or added), kept only when the signature is unchanged (the
// threshold is what decides them). Negatives: every pair of different
// paragraphs with identical signatures (the guard would collapse them on
// the cosine alone).
func nearDupPairs(ds *Dataset, maxChunkChars int) (paras []string, pos []dupPair, neg [][2]int) {
	skip := map[string]bool{}
	for _, b := range DedupProbeBases(ds, maxChunkChars) {
		skip[b] = true
	}
	seen := map[string]bool{}
	for _, it := range ds.Corpus {
		for _, c := range ChunkItem(it, maxChunkChars) {
			t := strings.TrimSpace(c.Text)
			if seen[t] || skip[t] || len(strings.Fields(t)) < 8 {
				continue
			}
			seen[t] = true
			paras = append(paras, t)
		}
	}
	sigKey := func(s string) string {
		g := dedup.Signature(s)
		return strings.Join(g.Numbers, ",") + "|" + strings.Join(g.Dates, ",") + "|" + strings.Join(g.Entities, ",") + "|" + strings.Join(g.Negations, ",")
	}
	groups := map[string][]int{}
	for i, p := range paras {
		k := sigKey(p)
		groups[k] = append(groups[k], i)
		rs := []rune(p)
		swapped := unicode.ToLower(rs[0])
		if unicode.IsLower(rs[0]) {
			swapped = unicode.ToUpper(rs[0])
		}
		trimmed := strings.TrimSuffix(p, ".")
		if trimmed == p {
			trimmed = p + "."
		}
		for _, d := range []string{string(swapped) + string(rs[1:]), p + " " + courtesy[i%len(courtesy)], trimmed} {
			if d != p && sigKey(d) == k {
				pos = append(pos, dupPair{i, d})
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k]
		for x := 0; x < len(g); x++ {
			for y := x + 1; y < len(g); y++ {
				neg = append(neg, [2]int{g[x], g[y]})
			}
		}
	}
	return paras, pos, neg
}

// dupPair is a paragraph (by index) and a real duplicate of it.
type dupPair struct {
	para int
	dup  string
}

// CalibrateNearDup chooses write.near_dup_threshold for emb (decision 0125).
func CalibrateNearDup(ctx context.Context, emb core.Embedder, ds *Dataset, maxChunkChars int, current float64) (ThresholdCalibration, error) {
	paras, pos, neg := nearDupPairs(ds, maxChunkChars)
	pv, err := embedAll(ctx, emb, paras)
	if err != nil {
		return ThresholdCalibration{}, err
	}
	dups := make([]string, len(pos))
	for i, p := range pos {
		dups[i] = p.dup
	}
	dv, err := embedAll(ctx, emb, dups)
	if err != nil {
		return ThresholdCalibration{}, err
	}
	var ps, ns []float64
	for i, p := range pos {
		ps = append(ps, cosine32(pv[p.para], dv[i]))
	}
	type scored struct {
		v    float64
		a, b int
	}
	var top []scored
	for _, n := range neg {
		v := cosine32(pv[n[0]], pv[n[1]])
		ns = append(ns, v)
		top = append(top, scored{v, n[0], n[1]})
	}
	r := chooseThreshold("write.near_dup_threshold", ps, ns, current, true)
	r.ModelID = emb.ModelID()
	sort.SliceStable(top, func(i, j int) bool { return top[i].v > top[j].v })
	for _, s := range top[:min(3, len(top))] {
		r.TopNeg = append(r.TopNeg, fmt.Sprintf("%s: %q / %q", fmtNum(s.v), clip(paras[s.a], 70), clip(paras[s.b], 70)))
	}
	return r, nil
}

// EntityName is one entity of an ingested system, for CalibrateMerge.
type EntityName struct{ Name, Type string }

// EntityLister is implemented by a system that extracts entities (C3..C5).
type EntityLister interface {
	EntityNames(ctx context.Context) ([]EntityName, error)
}

// CalibrateMerge chooses the vector threshold of sleep merge proposals
// (sleep.merge_similarities) for emb from the entities a system extracted
// from the corpus (decision 0125). Positives: each name against its
// variants written differently (lowercase without accents; "Last, First"
// for names of two or more words). Negatives: every pair of entities with
// different names (after folding) and compatible types, the pairs sleep
// would compare.
func CalibrateMerge(ctx context.Context, emb core.Embedder, ents []EntityName, current float64) (ThresholdCalibration, error) {
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name
	}
	vs, err := embedAll(ctx, emb, names)
	if err != nil {
		return ThresholdCalibration{}, err
	}
	var ps, ns []float64
	for i, e := range ents {
		var variants []string
		if f := Normalize(e.Name); f != e.Name {
			variants = append(variants, f)
		}
		if w := strings.Fields(e.Name); len(w) >= 2 {
			variants = append(variants, w[len(w)-1]+", "+strings.Join(w[:len(w)-1], " "))
		}
		if len(variants) == 0 {
			continue
		}
		vv, err := embedAll(ctx, emb, variants)
		if err != nil {
			return ThresholdCalibration{}, err
		}
		for _, v := range vv {
			ps = append(ps, cosine32(vs[i], v))
		}
	}
	type scored struct {
		v    float64
		a, b int
	}
	var top []scored
	for i := range ents {
		for j := i + 1; j < len(ents); j++ {
			ti, tj := ents[i].Type, ents[j].Type
			if Normalize(ents[i].Name) == Normalize(ents[j].Name) || (ti != "" && tj != "" && ti != tj) {
				continue
			}
			v := cosine32(vs[i], vs[j])
			ns = append(ns, v)
			top = append(top, scored{v, i, j})
		}
	}
	r := chooseThreshold("sleep.merge_similarities", ps, ns, current, false)
	r.ModelID = emb.ModelID()
	sort.SliceStable(top, func(i, j int) bool { return top[i].v > top[j].v })
	for _, s := range top[:min(3, len(top))] {
		r.TopNeg = append(r.TopNeg, fmt.Sprintf("%s: %q / %q", fmtNum(s.v), ents[s.a].Name, ents[s.b].Name))
	}
	return r, nil
}

// embedAll embeds texts in batches of 32.
func embedAll(ctx context.Context, emb core.Embedder, texts []string) ([][]float32, error) {
	var out [][]float32
	for i := 0; i < len(texts); i += 32 {
		vs, err := emb.Embed(ctx, texts[i:min(i+32, len(texts))])
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

// cosine32 is the dot product of two L2-normalised vectors, in float64.
func cosine32(a, b []float32) float64 {
	var s float64
	for i := range a {
		if i < len(b) {
			s += float64(a[i]) * float64(b[i])
		}
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// WriteThreshold prints one threshold calibration.
func WriteThreshold(w io.Writer, c ThresholdCalibration) {
	fmt.Fprintf(w, "%s for %s (decision 0125): %s (%s; current %s)\n", c.Key, c.ModelID, fmtNum(c.Chosen), c.Rule, fmtNum(c.Current))
	fmt.Fprintf(w, "  %d positive pairs (min %s, 5th percentile %s), %d negative pairs (max %s)\n",
		c.Positives, fmtNum(c.PosMin), fmtNum(c.PosP05), c.Negatives, fmtNum(c.NegMax))
	fmt.Fprintf(w, "  chosen: %d positives missed, %d negatives passing; current: %d missed, %d passing\n", c.Missed, c.FalseHits, c.MissedAt, c.FalseAt)
	for _, t := range c.TopNeg {
		fmt.Fprintf(w, "  closest negative %s\n", t)
	}
}
