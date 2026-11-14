package read

import (
	"strings"
	"unicode"
)

// Query text handling. User text never reaches FTS5 as syntax: it is split
// into word tokens (letters, digits and combining marks only), folded the way
// the unicode61 tokenizer with remove_diacritics folds, stripped of stopwords,
// and every surviving token is emitted as a double-quoted string joined by OR.
// A quoted token containing only letters and digits cannot be an FTS5
// operator, column filter, prefix query or NEAR group, so no input can cause
// an FTS syntax error.

// stopwords is a deliberately small Portuguese + English list of function
// words that carry no evidence of relevance. Keep it short: every entry here
// is a word that can never make an item relevant on its own. Entries are
// already folded (lowercase, no diacritics). Single-letter tokens are dropped
// separately, so "a", "e", "o", "i" are not listed.
var stopwords = func() map[string]bool {
	words := []string{
		// Portuguese
		"as", "os", "um", "uma", "uns", "umas", "de", "da", "do", "das", "dos",
		"em", "na", "no", "nas", "nos", "por", "para", "pra", "com", "sem", "ou",
		"que", "se", "ao", "aos", "como", "mais", "mas", "qual", "quais", "quando",
		"onde", "quem", "isso", "isto", "este", "esta", "esse", "essa", "ser",
		"foi", "sao", "sobre", "nao", "meu", "minha", "seu", "sua", "ele", "ela",
		"eu", "voce", "nosso", "nossa", "ja", "tem", "ha", "entre", "ate",
		// English
		"an", "the", "of", "to", "in", "on", "at", "for", "by", "with", "from",
		"and", "or", "is", "are", "was", "were", "be", "been", "it", "its",
		"this", "that", "these", "those", "what", "which", "who", "whom", "when",
		"where", "why", "how", "do", "does", "did", "you", "he", "she", "we",
		"they", "me", "my", "our", "your", "as", "about", "into", "than", "then",
		"there", "not", "please", "can", "could", "would", "should", "has", "have",
	}
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

// foldRune maps common Latin letters with diacritics to their base letter,
// matching unicode61 remove_diacritics for the Portuguese and English corpus.
var foldRune = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// tokenize splits s into folded word tokens, in order, with repetitions.
func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			r = unicode.ToLower(r)
			if f, ok := foldRune[r]; ok {
				r = f
			}
			b.WriteRune(r)
		case unicode.Is(unicode.Mn, r):
			// combining mark (decomposed diacritic): dropped, like remove_diacritics
		default:
			flush()
		}
	}
	flush()
	return out
}

// queryTerms returns the distinct evidence-bearing terms of a free-text
// query, in first-occurrence order, at most max of them.
func queryTerms(q string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tokenize(q) {
		if seen[t] || stopwords[t] {
			continue
		}
		if r := []rune(t); len(r) == 1 && !unicode.IsDigit(r[0]) {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if max > 0 && len(out) == max {
			break
		}
	}
	return out
}

// matchExpr builds the FTS5 MATCH expression for terms: each term quoted,
// joined with OR. Returns "" when there is nothing to match.
func matchExpr(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		// Tokens contain only letters and digits; doubling quotes is a
		// second line of defence should tokenize ever change.
		parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " OR ")
}

// coverage is the lexical evidence of text for the query terms: the number of
// distinct terms present in text divided by min(len(terms), cap), clamped to
// 1. The cap keeps long natural-language requests (the usual pre-context
// query) from needing a match on every word.
func coverage(terms []string, text string, cap int) float64 {
	if len(terms) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, t := range tokenize(text) {
		have[t] = true
	}
	n := 0
	for _, t := range terms {
		if have[t] {
			n++
		}
	}
	den := len(terms)
	if cap > 0 && cap < den {
		den = cap
	}
	c := float64(n) / float64(den)
	if c > 1 {
		c = 1
	}
	return c
}

// estimateTokens is the deterministic token estimate used for budgets:
// ceil(runes / 4). It is model-independent on purpose (same input, same
// output); real tokenizers give roughly 3 to 4.5 characters per token for
// Portuguese and English prose.
func estimateTokens(s string) int {
	n := len([]rune(s))
	return (n + 3) / 4
}
