package sleep

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// accentFold maps common Latin accented letters to their base letter
// (enough for Portuguese, Spanish, French and German names).
var accentFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// normalize lowercases, removes accents and punctuation and collapses spaces
// (the alias normalization of write step 8).
func normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if f, ok := accentFold[r]; ok {
			r = f
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// snake normalizes a predicate name to snake_case.
func snake(s string) string { return strings.ReplaceAll(normalize(s), " ", "_") }

// tokenSet returns the sorted unique tokens of an already normalized string.
func tokenSet(norm string) []string {
	return uniqueSorted(strings.Fields(norm))
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	s := append([]string(nil), in...)
	sort.Strings(s)
	out := s[:1]
	for _, x := range s[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}

// jaccard of two sorted unique sets.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	i, j, inter := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			inter++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// nameSimilarity of two normalized names: the larger of the normalized edit
// similarity (1 - distance/longer length) and the token Jaccard overlap.
func nameSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	edit := 1 - float64(levenshtein(ra, rb))/float64(max(len(ra), len(rb)))
	return math.Max(edit, jaccard(tokenSet(a), tokenSet(b)))
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// stopwords are dropped from key terms (Portuguese and English).
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a o e de da do das dos em no na nos nas um uma uns umas para por com sem que se
	ao aos as os ou mas mais menos muito muita como quando onde ja nao sim foi ser esta este essa esse isso isto
	sao tem ter seu sua seus suas ele ela eles elas voce nos pelo pela pelos pelas entre sobre ate apos
	the an and or of to in on at for with without from by is are was were be been it its this that these those
	as but not no yes do does did have has had will would can could should we you they he she them his her our your`) {
		stopwords[w] = true
	}
}

// keyTerms are the normalized words of text that carry meaning: three or
// more letters and not a stopword. Sorted, unique. Two events are "similar"
// for lessons when the Jaccard overlap of their key terms reaches
// sleep.lesson_similarity.
func keyTerms(text string) []string {
	var out []string
	for _, w := range strings.Fields(normalize(text)) {
		if len([]rune(w)) >= 3 && !stopwords[w] {
			out = append(out, w)
		}
	}
	return uniqueSorted(out)
}
