package lme

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tokens estimates the token count of s the way Chanda's budget does
// (internal/read.estimateTokens): ceil(runes / 4). Every condition uses this
// one estimator so the 2,000-token budgets are comparable.
func Tokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }

// SHA256 is the hex SHA-256 of s.
func SHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Query term handling copied from Chanda's internal/read/query.go (the
// functions are unexported there), so the BM25 baseline builds its FTS5
// MATCH expression exactly as Chanda's keyword stage does: folded tokens,
// stopwords and single letters dropped, distinct terms in order, at most
// read.max_query_terms (32), each quoted and joined with OR.

var chandaStopwords = func() map[string]bool {
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

var foldRune = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

func foldTokens(s string) []string {
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
		default:
			flush()
		}
	}
	flush()
	return out
}

// QueryTerms is Chanda's queryTerms.
func QueryTerms(q string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range foldTokens(q) {
		if seen[t] || chandaStopwords[t] {
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

// MatchExpr is Chanda's matchExpr: quoted terms joined by OR ("" if none).
func MatchExpr(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " OR ")
}

// MaxQueryTerms is Chanda's read.max_query_terms default.
const MaxQueryTerms = 32
