package read

import (
	"strings"
	"unicode"
)

// Shortening a context item (decision 0137). read.context_truncate picks
// how: "head" keeps the start of the text (truncateToFit, the behaviour
// before the knob); "best_window" keeps the contiguous window of the text
// that best matches the query. Both serve the per-item cap
// (read.context_item_max_tokens) and the last item that only partly fits
// the budget.

const ellipsis = "…"

// shortenItem shortens it.Text so its block costs at most target tokens,
// by head or by best window over the query terms. It refuses (ok=false)
// when fewer than minTokens remain or when nothing of the text would fit.
func shortenItem(it Item, target, minTokens int, bestWindow bool, terms []string) (Item, int, bool) {
	if !bestWindow {
		return truncateToFit(it, target, minTokens)
	}
	return bestWindowFit(it, target, minTokens, terms)
}

// bestWindowFit keeps the contiguous window of it.Text that best matches
// terms and whose block costs at most target tokens. The text is split into
// sentences; a sentence that cannot fit on its own is split into words, and
// a word that cannot fit on its own into fixed-size rune pieces. Among the longest windows
// that fit from each start, the one with the most distinct query terms
// wins, then the one with the most term occurrences, then the earliest. An
// ellipsis marks each end where text was cut, and the item is marked
// truncated. Deterministic: the score is lexical (the read path's own
// tokenizer and query terms) and the cost is estimateTokens of the
// rendered block, as the budget counts it.
func bestWindowFit(it Item, target, minTokens int, terms []string) (Item, int, bool) {
	if target < minTokens || target <= 0 {
		return it, 0, false
	}
	it.Truncated = true
	cost := func(text string) int {
		x := it
		x.Text = text
		return estimateTokens(renderItem(x))
	}
	// A unit is kept whole when it fits with an ellipsis at both ends (the
	// worst case wherever the window lands).
	unitFits := func(u string) bool {
		t := strings.TrimSpace(u)
		return t != "" && cost(ellipsis+t+ellipsis) <= target
	}
	// Room for the text itself, in runes: an oversize word is cut into
	// pieces of an eighth of it (at least one rune), so a long token
	// (a URL, a blob) does not become thousands of one-rune units.
	room := (target - cost(ellipsis+ellipsis)) * 4
	units := windowUnits(it.Text, unitFits, max(1, room/8))
	m := len(units)
	if m == 0 {
		return it, 0, false
	}
	termIdx := make(map[string]int, len(terms))
	for i, t := range terms {
		termIdx[t] = i
	}
	hits := make([][]int, m)
	for k, u := range units {
		for _, tok := range tokenize(u) {
			if i, ok := termIdx[tok]; ok {
				hits[k] = append(hits[k], i)
			}
		}
	}
	text := func(i, j int) string {
		s := strings.TrimSpace(strings.Join(units[i:j], ""))
		if s == "" {
			return ""
		}
		if i > 0 {
			s = ellipsis + s
		}
		if j < m {
			s += ellipsis
		}
		return s
	}
	fits := func(i, j int) bool {
		s := text(i, j)
		return s != "" && cost(s) <= target
	}

	count := make([]int, len(terms))
	distinct, total := 0, 0
	add := func(k int) {
		for _, t := range hits[k] {
			if count[t] == 0 {
				distinct++
			}
			count[t]++
			total++
		}
	}
	remove := func(k int) {
		for _, t := range hits[k] {
			count[t]--
			if count[t] == 0 {
				distinct--
			}
			total--
		}
	}
	bi, bj, bd, bt := -1, -1, -1, -1
	j := 0 // the window is units[i:j]
	for i := 0; i < m; i++ {
		if j < i {
			j = i
		}
		// Moving the start can add a leading ellipsis: shrink until it fits.
		for j > i && !fits(i, j) {
			j--
			remove(j)
		}
		for j < m && fits(i, j+1) {
			add(j)
			j++
		}
		if j > i && (distinct > bd || (distinct == bd && total > bt)) {
			bi, bj, bd, bt = i, j, distinct, total
		}
		if j > i {
			remove(i)
		}
	}
	if bi < 0 {
		return it, 0, false
	}
	it.Text = text(bi, bj)
	return it, cost(it.Text), true
}

// windowUnits splits text into the units of a window: sentences, with a
// sentence that does not fit on its own split into words, and a word that
// does not fit on its own split into pieces of piece runes (one rune when
// even such a piece does not fit). Joining the units gives back the text.
func windowUnits(text string, fits func(string) bool, piece int) []string {
	var out []string
	for _, s := range splitSentences(text) {
		if fits(s) {
			out = append(out, s)
			continue
		}
		for _, w := range splitWords(s) {
			if fits(w) {
				out = append(out, w)
				continue
			}
			rs := []rune(w)
			n := piece
			if !fits(string(rs[:min(n, len(rs))])) {
				n = 1
			}
			for k := 0; k < len(rs); k += n {
				out = append(out, string(rs[k:min(k+n, len(rs))]))
			}
		}
	}
	return out
}

// splitSentences cuts text after each run of whitespace that follows
// sentence-ending punctuation (. ! ? …, optionally followed by closing
// quotes or brackets) or that contains a line break. Each piece keeps its
// trailing whitespace, so the pieces join back into text; leading
// whitespace stays with the first sentence.
func splitSentences(text string) []string {
	var out []string
	rs := []rune(text)
	start := 0
	ended := false   // the last non-space rune ends a sentence
	content := false // the current piece has a non-space rune
	for k := 0; k < len(rs); {
		r := rs[k]
		if !unicode.IsSpace(r) {
			switch {
			case strings.ContainsRune(".!?…", r):
				ended = true
			case ended && strings.ContainsRune(`"')]}»”’`, r):
				// closing mark after the punctuation: still the end
			default:
				ended = false
			}
			content = true
			k++
			continue
		}
		e := k
		newline := false
		for e < len(rs) && unicode.IsSpace(rs[e]) {
			if rs[e] == '\n' {
				newline = true
			}
			e++
		}
		if content && (ended || newline) && e < len(rs) {
			out = append(out, string(rs[start:e]))
			start, content = e, false
		}
		ended = false
		k = e
	}
	if start < len(rs) {
		out = append(out, string(rs[start:]))
	}
	return out
}

// splitWords cuts s after each run of whitespace; the pieces join back into s.
func splitWords(s string) []string {
	var out []string
	rs := []rune(s)
	start := 0
	for k := 0; k < len(rs); k++ {
		if unicode.IsSpace(rs[k]) && (k+1 == len(rs) || !unicode.IsSpace(rs[k+1])) && k+1 < len(rs) {
			out = append(out, string(rs[start:k+1]))
			start = k + 1
		}
	}
	if start < len(rs) {
		out = append(out, string(rs[start:]))
	}
	return out
}
