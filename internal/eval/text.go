package eval

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// fold maps accented Latin letters (after lowercasing) to their base letter.
var fold = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// Normalize lowercases s, removes diacritics and collapses whitespace. It is
// the matching function for gold spans, expected answers and forbidden
// strings, so that a system's own normalization (NFC, whitespace) never
// changes whether an item counts as a hit.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) { // combining marks (NFD input)
			continue
		}
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		r = unicode.ToLower(r)
		if f, ok := fold[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CountTokens is the harness token estimator: one token per four characters,
// rounded up. It is applied to every system's output, so budgets and cost are
// comparable regardless of how each system counts internally.
func CountTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// terms splits normalized text into letter/digit runs.
func terms(s string) []string {
	return strings.FieldsFunc(Normalize(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// stopwords is a small Portuguese list used by the keyword baseline and the
// local embedder. Plain BM25 baselines conventionally drop these.
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a ao aos as com como da das de do dos e em entre era
		esta este eu foi ha isso la lhe mais mas me meu minha na nas no nos o os ou para
		pela pelo por qual quais quando que quem se sem ser seu sua sao ta tem um uma voce`) {
		m[w] = true
	}
	return m
}()

// contentTerms returns terms without stopwords, in order, with duplicates.
func contentTerms(s string) []string {
	var out []string
	for _, t := range terms(s) {
		if !stopwords[t] {
			out = append(out, t)
		}
	}
	return out
}
