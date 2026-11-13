// Package norm holds the deterministic text normalizations shared by fact
// reconciliation and the near-duplicate guard: entity aliases, predicate
// names, fact values, numbers and dates. Every function is pure.
//
// Number rule (docs/decisions/0040-fact-reconciliation.md): a run of digits
// separated by '.' or ',' is rewritten to a plain decimal with '.' as the
// decimal point and no grouping, so "1.000,50", "1,000.50" and "1000.50" all
// become "1000.5". When both separators appear, the last one is the decimal
// point. When one separator appears once, it is a thousands separator if it
// is followed by exactly three digits and preceded by one to three digits
// that do not start with 0 ("1.000" = "1,000" = "1000"); otherwise it is the
// decimal point ("10,5" = "10.5"). A separator that repeats is grouping only
// if every group after the first has three digits; otherwise the token is
// left as is (versions such as "1.2.3", addresses such as "10.0.0.1").
// Trailing zeros of the fraction are dropped; leading zeros are kept.
//
// Date rule: Y-M-D (with '-', '/' or '.') becomes ISO YYYY-MM-DD. D/M/Y and
// M/D/Y are converted only when unambiguous (one of the first two numbers is
// above 12, or both are equal); otherwise the text is left unchanged.
package norm

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NFC returns s in Unicode NFC.
func NFC(s string) string { return norm.NFC.String(s) }

// CollapseSpace trims s and collapses every run of whitespace into one space.
func CollapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// StripAccents removes combining marks after canonical decomposition.
func StripAccents(s string) string {
	d := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(d))
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

// Alias normalizes an entity name (write step 8): lowercase, no accents,
// punctuation and symbols replaced by spaces, whitespace collapsed.
// "José  O'Brien-Silva" -> "jose o brien silva".
func Alias(s string) string {
	s = strings.ToLower(StripAccents(NFC(s)))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return CollapseSpace(b.String())
}

// Predicate normalizes an attribute name to lowercase snake_case without
// accents: "Job Title", "jobTitle", "job-title" -> "job_title";
// "Endereço" -> "endereco".
func Predicate(s string) string {
	s = StripAccents(NFC(strings.TrimSpace(s)))
	var b strings.Builder
	var prev rune
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			b.WriteRune('_')
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune('_')
		}
		prev = r
	}
	parts := strings.FieldsFunc(b.String(), func(r rune) bool { return r == '_' })
	return strings.Join(parts, "_")
}

// Value normalizes a fact value for comparison: NFC, lowercase, collapsed
// whitespace, dates to ISO where unambiguous, numbers to a canonical decimal.
func Value(s string) string {
	s = CollapseSpace(strings.ToLower(NFC(s)))
	s = Dates(s)
	return Numbers(s)
}

var (
	reYMD = regexp.MustCompile(`\b(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
	reXYY = regexp.MustCompile(`\b(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})\b`)
	reNum = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	// ISODate matches a normalized date.
	ISODate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
)

func isoDate(y, m, d int) (string, bool) {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return "", false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// Dates rewrites unambiguous dates in s to ISO YYYY-MM-DD.
func Dates(s string) string {
	s = reYMD.ReplaceAllStringFunc(s, func(m string) string {
		g := reYMD.FindStringSubmatch(m)
		y, _ := strconv.Atoi(g[1])
		mo, _ := strconv.Atoi(g[2])
		d, _ := strconv.Atoi(g[3])
		if iso, ok := isoDate(y, mo, d); ok {
			return iso
		}
		return m
	})
	return reXYY.ReplaceAllStringFunc(s, func(m string) string {
		g := reXYY.FindStringSubmatch(m)
		a, _ := strconv.Atoi(g[1])
		b, _ := strconv.Atoi(g[2])
		y, _ := strconv.Atoi(g[3])
		var mo, d int
		switch {
		case a == b || (a > 12 && b <= 12):
			d, mo = a, b
		case b > 12 && a <= 12:
			mo, d = a, b
		default:
			return m // ambiguous: 01/03/2026 could be 1 March or 3 January
		}
		if iso, ok := isoDate(y, mo, d); ok {
			return iso
		}
		return m
	})
}

// Numbers rewrites every number in s to its canonical decimal form.
func Numbers(s string) string {
	return reNum.ReplaceAllStringFunc(s, Number)
}

// Number canonicalizes one numeric token (digits separated by '.' or ',').
// Tokens that do not follow a recognizable grouping are returned unchanged.
func Number(tok string) string {
	var groups []string
	var seps []byte
	start := 0
	for i := 0; i < len(tok); i++ {
		if tok[i] == '.' || tok[i] == ',' {
			groups = append(groups, tok[start:i])
			seps = append(seps, tok[i])
			start = i + 1
		}
	}
	groups = append(groups, tok[start:])
	if len(seps) == 0 {
		return tok
	}
	grouping := func(gs []string) bool { // gs[0] 1-3 digits, the rest exactly 3
		if len(gs[0]) < 1 || len(gs[0]) > 3 || gs[0][0] == '0' {
			return false
		}
		for _, g := range gs[1:] {
			if len(g) != 3 {
				return false
			}
		}
		return true
	}
	last := seps[len(seps)-1]
	mixed := false
	for _, c := range seps[:len(seps)-1] {
		if c != last {
			mixed = true
		}
	}
	var intPart, frac string
	switch {
	case mixed:
		// Every separator but the last must be the grouping kind.
		for _, c := range seps[:len(seps)-1] {
			if c == last {
				return tok
			}
		}
		if !grouping(groups[:len(groups)-1]) {
			return tok
		}
		intPart, frac = strings.Join(groups[:len(groups)-1], ""), groups[len(groups)-1]
	case len(seps) == 1:
		if grouping(groups) {
			intPart = groups[0] + groups[1]
		} else {
			intPart, frac = groups[0], groups[1]
		}
	default:
		if !grouping(groups) {
			return tok
		}
		intPart = strings.Join(groups, "")
	}
	frac = strings.TrimRight(frac, "0")
	if frac == "" {
		return intPart
	}
	return intPart + "." + frac
}
