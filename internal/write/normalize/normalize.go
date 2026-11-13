// Package normalize implements write step 1 (BASE.md "Write path"):
// Unicode NFC, standardized line breaks and collapsed whitespace, followed by
// a SHA-256 over the normalized text.
//
// Rules, applied in order:
//  1. Invalid UTF-8 bytes become U+FFFD.
//  2. Unicode NFC.
//  3. "\r\n" and lone "\r" become "\n".
//  4. On every line, runs of horizontal whitespace (spaces, tabs, NBSP and other
//     Unicode spaces except "\n") become one space, and the line is trimmed.
//  5. Runs of blank lines collapse to a single blank line (paragraph breaks are
//     kept); leading and trailing blank lines are removed.
package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Text returns the normalized form of s. It is idempotent: Text(Text(s)) == Text(s).
func Text(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = norm.NFC.String(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	blank := false // a blank line is pending
	wrote := false // at least one non-blank line written
	for _, line := range strings.Split(s, "\n") {
		line = collapseLine(line)
		if line == "" {
			blank = wrote
			continue
		}
		if wrote {
			b.WriteByte('\n')
			if blank {
				b.WriteByte('\n')
			}
		}
		b.WriteString(line)
		wrote, blank = true, false
	}
	return b.String()
}

// collapseLine turns every run of whitespace into one space and trims the ends.
func collapseLine(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	space := false
	for _, r := range line {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Hash returns the lowercase hex SHA-256 of s (callers pass normalized text).
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
