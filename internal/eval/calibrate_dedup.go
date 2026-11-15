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
// threshold is what decides them). Negatives: every pair of paragraphs with
// identical signatures (the guard would collapse them on the cosine alone)
// whose words differ; two paragraphs with the same words in another order
// ("equipes de Financeiro e Suporte" / "de Suporte e Financeiro") say the
// same thing and are not negatives.
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
	bags := make([]string, len(paras))
	for i, p := range paras {
		ws := terms(p)
		sort.Strings(ws)
		bags[i] = strings.Join(ws, " ")
	}
	for _, k := range keys {
		g := groups[k]
		for x := 0; x < len(g); x++ {
			for y := x + 1; y < len(g); y++ {
				if bags[g[x]] == bags[g[y]] {
					continue // the same words in another order: a duplicate, not a negative
				}
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
	r, _, err := CalibrateNearDupRule(ctx, emb, ds, maxChunkChars, current)
	return r, err
}

// LexicalCalibration is the lexical path of the near-duplicate guard
// (write.near_dup_lexical, decision 0138) chosen for one model on the
// calibration pairs of CalibrateNearDup.
type LexicalCalibration struct {
	ModelID       string  `json:"model_id"`
	Threshold     float64 `json:"threshold"` // the plain write.near_dup_threshold it is combined with
	MaxAddedWords int     `json:"max_added_words"`
	MinCosine     float64 `json:"min_cosine"`
	Rule          string  `json:"rule"` // below_positives | above_negatives
	// PosAddedMax is the most words a positive adds to its paragraph (among
	// positives that repeat every word of it); NegAddedMin the fewest words
	// one text of a negative pair adds to the other when it repeats every
	// word of it (-1: no negative pair does).
	PosAddedMax int `json:"pos_added_max"`
	NegAddedMin int `json:"neg_added_min"`
	// On the lexical path at the chosen max_added_words.
	EligiblePos int `json:"eligible_pos"`
	EligibleNeg int `json:"eligible_neg"`
	// The combined rule (plain threshold or lexical path).
	Positives int      `json:"positives"`
	Negatives int      `json:"negatives"`
	Missed    int      `json:"missed"`
	FalseHits int      `json:"false_hits"`
	TopNeg    []string `json:"top_negatives"` // the closest negatives on the lexical path
}

// CalibrateNearDupRule chooses write.near_dup_threshold (decision 0125) and
// the lexical path write.near_dup_lexical (decision 0138) for emb, from the
// same corpus pairs. The lexical path's rule, stated before its values were
// chosen:
//
//   - max_added_words: positives here are paragraphs with words appended or
//     with case or punctuation changed, so each one repeats every word of its
//     paragraph; the cap is the middle (rounded down) between the most words
//     a positive adds and the fewest words a negative adds when one of its
//     texts repeats every word of the other, kept below the latter; with no
//     such negative, the most words a positive adds;
//   - min_cosine: the strictest four-decimal floor that every positive on the
//     lexical path passes (cosine > floor), if no negative on the lexical
//     path (either text taken as the newer) passes it; otherwise the lowest
//     four-decimal floor no such negative passes, and the misses are
//     reported (a hidden distinct record is worse than a missed duplicate).
func CalibrateNearDupRule(ctx context.Context, emb core.Embedder, ds *Dataset, maxChunkChars int, current float64) (ThresholdCalibration, LexicalCalibration, error) {
	paras, pos, neg := nearDupPairs(ds, maxChunkChars)
	pv, err := embedAll(ctx, emb, paras)
	if err != nil {
		return ThresholdCalibration{}, LexicalCalibration{}, err
	}
	dups := make([]string, len(pos))
	for i, p := range pos {
		dups[i] = p.dup
	}
	dv, err := embedAll(ctx, emb, dups)
	if err != nil {
		return ThresholdCalibration{}, LexicalCalibration{}, err
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

	posAdd := make([]int, len(pos)) // -1: does not repeat every word of its paragraph
	for i, p := range pos {
		if n, ok := dedup.Added(paras[p.para], p.dup); ok {
			posAdd[i] = n
		} else {
			posAdd[i] = -1
		}
	}
	negAdd := make([]int, len(neg)) // fewest words added over both orders, -1 if neither order
	for i, n := range neg {
		negAdd[i] = -1
		for _, o := range [][2]int{{n[0], n[1]}, {n[1], n[0]}} {
			if k, ok := dedup.Added(paras[o[0]], paras[o[1]]); ok && (negAdd[i] < 0 || k < negAdd[i]) {
				negAdd[i] = k
			}
		}
	}
	lx := chooseLexical(ps, posAdd, ns, negAdd, r.Chosen)
	lx.ModelID = emb.ModelID()
	var ltop []scored
	for i, n := range neg {
		if negAdd[i] >= 0 && negAdd[i] <= lx.MaxAddedWords {
			ltop = append(ltop, scored{ns[i], n[0], n[1]})
		}
	}
	sort.SliceStable(ltop, func(i, j int) bool { return ltop[i].v > ltop[j].v })
	for _, s := range ltop[:min(3, len(ltop))] {
		lx.TopNeg = append(lx.TopNeg, fmt.Sprintf("%s: %q / %q", fmtNum(s.v), clip(paras[s.a], 70), clip(paras[s.b], 70)))
	}
	return r, lx, nil
}

// chooseLexical applies the lexical rule of CalibrateNearDupRule. ps/ns are
// the cosines of the positive and negative pairs; posAdd/negAdd the words
// added on the lexical path (-1: not on it); threshold the plain threshold.
func chooseLexical(ps []float64, posAdd []int, ns []float64, negAdd []int, threshold float64) LexicalCalibration {
	lx := LexicalCalibration{Threshold: threshold, Positives: len(ps), Negatives: len(ns), NegAddedMin: -1}
	for _, k := range posAdd {
		lx.PosAddedMax = max(lx.PosAddedMax, k)
	}
	for _, k := range negAdd {
		if k >= 0 && (lx.NegAddedMin < 0 || k < lx.NegAddedMin) {
			lx.NegAddedMin = k
		}
	}
	lx.MaxAddedWords = lx.PosAddedMax
	if lx.NegAddedMin >= 0 {
		lx.MaxAddedWords = min((lx.PosAddedMax+lx.NegAddedMin)/2, lx.NegAddedMin-1)
	}
	on := func(k int) bool { return k >= 0 && k <= lx.MaxAddedWords }
	minPos, negMax := math.Inf(1), math.Inf(-1)
	for i, v := range ps {
		if on(posAdd[i]) {
			lx.EligiblePos++
			minPos = math.Min(minPos, v)
		}
	}
	for i, v := range ns {
		if on(negAdd[i]) {
			lx.EligibleNeg++
			negMax = math.Max(negMax, v)
		}
	}
	t := math.Floor(minPos*1e4) / 1e4
	if !(minPos > t) {
		t = round4(t - 1e-4)
	}
	lx.Rule = "below_positives"
	if lx.EligiblePos == 0 {
		t = 1
	}
	// The cap is below the fewest words a nested negative adds, so no
	// negative is on the path by construction; the guard stays in case
	// the cap rule changes.
	if negMax > t {
		t = math.Ceil(negMax*1e4) / 1e4
		lx.Rule = "above_negatives"
	}
	lx.MinCosine = math.Min(t, 1)
	for i, v := range ps {
		if !(v > threshold) && !(on(posAdd[i]) && v > lx.MinCosine) {
			lx.Missed++
		}
	}
	for i, v := range ns {
		if v > threshold || (on(negAdd[i]) && v > lx.MinCosine) {
			lx.FalseHits++
		}
	}
	return lx
}

// WriteLexical prints one lexical-path calibration.
func WriteLexical(w io.Writer, c LexicalCalibration) {
	fmt.Fprintf(w, "write.near_dup_lexical for %s (decision 0138): max_added_words %d, min_cosine %s (%s)\n", c.ModelID, c.MaxAddedWords, fmtNum(c.MinCosine), c.Rule)
	fmt.Fprintf(w, "  words added: positives up to %d, negatives from %d; on the lexical path: %d positives, %d negatives\n", c.PosAddedMax, c.NegAddedMin, c.EligiblePos, c.EligibleNeg)
	fmt.Fprintf(w, "  with near_dup_threshold %s: %d of %d positives missed, %d of %d negatives passing\n", fmtNum(c.Threshold), c.Missed, c.Positives, c.FalseHits, c.Negatives)
	for _, t := range c.TopNeg {
		fmt.Fprintf(w, "  closest negative on the lexical path %s\n", t)
	}
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
