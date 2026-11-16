// Package window keeps the contiguous window of a text that best matches a
// set of terms within a cost (decision 0137, "best_window"). The read path
// uses it to shorten context items; the guided sleep (decision 0142) uses it
// to keep the literal line of each turn of a ficha. The scoring and the
// splitting are the same for both; only the cost and the tokenizer differ.
package window

import (
	"strings"
	"unicode"
)

// Ellipsis marks each end of a window where text was cut.
const Ellipsis = "…"

// Best returns the contiguous window of text that best matches terms and
// whose cost (cost of the window text, ellipses included) is at most
// target, together with that cost. The text is split into sentences; a
// sentence that cannot fit on its own is split into words, and a word that
// cannot fit on its own into fixed-size rune pieces. Among the longest
// windows that fit from each start, the one with the most distinct terms
// wins, then the one with the most term occurrences, then the earliest. An
// ellipsis marks each end where text was cut. tokenize turns a unit into
// the tokens compared with terms. ok is false when nothing of the text fits.
func Best(text string, target int, cost func(string) int, tokenize func(string) []string, terms []string) (string, int, bool) {
	// A unit is kept whole when it fits with an ellipsis at both ends (the
	// worst case wherever the window lands).
	unitFits := func(u string) bool {
		t := strings.TrimSpace(u)
		return t != "" && cost(Ellipsis+t+Ellipsis) <= target
	}
	// Room for the text itself, in runes: an oversize word is cut into
	// pieces of an eighth of it (at least one rune), so a long token
	// (a URL, a blob) does not become thousands of one-rune units.
	room := (target - cost(Ellipsis+Ellipsis)) * 4
	units := Units(text, unitFits, max(1, room/8))
	m := len(units)
	if m == 0 {
		return "", 0, false
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
	span := func(i, j int) string {
		s := strings.TrimSpace(strings.Join(units[i:j], ""))
		if s == "" {
			return ""
		}
		if i > 0 {
			s = Ellipsis + s
		}
		if j < m {
			s += Ellipsis
		}
		return s
	}
	fits := func(i, j int) bool {
		s := span(i, j)
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
		// Moving the start can add a leading Ellipsis: shrink until it fits.
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
		return "", 0, false
	}
	out := span(bi, bj)
	return out, cost(out), true
}

// Units splits text into the units of a window: sentences, with a
// sentence that does not fit on its own split into words, and a word that
// does not fit on its own split into pieces of piece runes (one rune when
// even such a piece does not fit). Joining the units gives back the text.
func Units(text string, fits func(string) bool, piece int) []string {
	var out []string
	for _, s := range SplitSentences(text) {
		if fits(s) {
			out = append(out, s)
			continue
		}
		for _, w := range SplitWords(s) {
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

// SplitSentences cuts text after each run of whitespace that follows
// sentence-ending punctuation (. ! ? …, optionally followed by closing
// quotes or brackets) or that contains a line break. Each piece keeps its
// trailing whitespace, so the pieces join back into text; leading
// whitespace stays with the first sentence.
func SplitSentences(text string) []string {
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

// SplitWords cuts s after each run of whitespace; the pieces join back into s.
func SplitWords(s string) []string {
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
