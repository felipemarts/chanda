package forget

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// tokens splits text into lowercase letter/number runs of at least minRunes
// runes, unique and sorted. It approximates FTS5 unicode61; queries built
// from these tokens are tokenized again by FTS5 itself, so small differences
// (diacritics) do not matter.
func tokens(text string, minRunes int) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		f = strings.ToLower(f)
		if utf8.RuneCountInString(f) >= minRunes && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// ftsPhrase quotes one term as an FTS5 string, so no FTS5 syntax in user
// input (operators, column filters, NEAR, *) is ever interpreted.
func ftsPhrase(term string) string { return `"` + strings.ReplaceAll(term, `"`, `""`) + `"` }

// ftsAllTerms builds an FTS5 query matching rows that contain every token of
// q. It returns "" when q has no token.
func ftsAllTerms(q string) string {
	ts := tokens(q, 1)
	for i, t := range ts {
		ts[i] = ftsPhrase(t)
	}
	return strings.Join(ts, " ")
}

// needles cuts n byte strings of length size (at least minNeedle) from text,
// evenly spaced and aligned to rune boundaries. Texts shorter than minNeedle
// give none.
func needles(text string, n, size int) []string {
	if size < minNeedle {
		size = minNeedle
	}
	if len(text) < minNeedle || n <= 0 {
		return nil
	}
	if size > len(text) {
		size = len(text)
	}
	seen := map[string]bool{}
	var out []string
	for i := 0; i < n; i++ {
		start := 0
		if n > 1 {
			start = i * (len(text) - size) / (n - 1)
		}
		for start > 0 && !utf8.RuneStart(text[start]) {
			start--
		}
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end++
		}
		s := text[start:end]
		if len(s) >= minNeedle && strings.TrimSpace(s) != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// minNeedle is the shortest byte string the file scan accepts: shorter ones
// collide with unrelated text.
const minNeedle = 12
