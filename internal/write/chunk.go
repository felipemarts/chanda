package write

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/write/normalize"
)

// Chunk is one structural piece of a document (write step 6).
type Chunk struct {
	Text        string // normalized chunk text
	HeadingPath string // "H1 > H2", "" before the first heading
	Hash        string // SHA-256 hex of Text
}

var atxHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)(?:\s+#+)?$`)

// ChunkText splits normalized Markdown or plain text by structure: ATX
// headings ("# ..") set the heading path, and each paragraph (a run of
// non-blank lines; a fenced code block counts as one paragraph) becomes one
// chunk. There is no overlap. A paragraph longer than maxChars runes is split
// at sentence boundaries (code blocks at line boundaries), and a single unit
// still too long is split at the last space before the limit.
//
// A text with content but no paragraph (only headings) becomes one chunk.
func ChunkText(text string, maxChars int) []Chunk {
	var (
		out      []Chunk
		headings []string
		para     []string
		fence    string // opening fence marker while inside a fenced block
	)
	path := func() string {
		var parts []string
		for _, h := range headings {
			if h != "" {
				parts = append(parts, h)
			}
		}
		return strings.Join(parts, " > ")
	}
	flush := func() {
		if len(para) == 0 {
			return
		}
		p := path()
		for _, piece := range splitLong(strings.Join(para, "\n"), maxChars) {
			out = append(out, Chunk{Text: piece, HeadingPath: p, Hash: normalize.Hash(piece)})
		}
		para = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if fence != "" {
			para = append(para, line)
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if m := fenceMarker(line); m != "" {
			fence = m
			para = append(para, line)
			continue
		}
		if m := atxHeading.FindStringSubmatch(line); m != nil {
			flush()
			level := len(m[1])
			for len(headings) < level {
				headings = append(headings, "")
			}
			headings = headings[:level]
			headings[level-1] = strings.TrimSpace(m[2])
			continue
		}
		if line == "" {
			flush()
			continue
		}
		para = append(para, line)
	}
	flush()
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		t := strings.TrimSpace(text)
		for _, piece := range splitLong(t, maxChars) {
			out = append(out, Chunk{Text: piece, Hash: normalize.Hash(piece)})
		}
	}
	return out
}

func fenceMarker(line string) string {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, m) {
			return m
		}
	}
	return ""
}

// splitLong packs units (sentences, or lines for code) into pieces of at most
// max runes. max <= 0 disables splitting.
func splitLong(s string, max int) []string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return []string{s}
	}
	var units []string
	if fenceMarker(s) != "" {
		units = splitAfter(s, func(prev, next rune) bool { return prev == '\n' })
	} else {
		units = splitAfter(s, sentenceBoundary)
	}
	var out []string
	var cur strings.Builder
	curN := 0
	emit := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
		curN = 0
	}
	for _, u := range units {
		n := utf8.RuneCountInString(strings.TrimSpace(u))
		if curN+n > max && curN > 0 {
			emit()
		}
		if n > max {
			out = append(out, hardSplit(strings.TrimSpace(u), max)...)
			continue
		}
		cur.WriteString(u)
		curN = utf8.RuneCountInString(strings.TrimSpace(cur.String()))
	}
	emit()
	return out
}

// sentenceBoundary reports a boundary between prev and next: after sentence
// punctuation followed by whitespace, or after a newline.
func sentenceBoundary(prev, next rune) bool {
	if prev == '\n' {
		return true
	}
	return unicode.IsSpace(next) && strings.ContainsRune(".!?…", prev)
}

// splitAfter cuts s into consecutive pieces; a cut happens before rune i when
// boundary(s[i-1], s[i]) is true and s[i] is whitespace or follows a newline.
func splitAfter(s string, boundary func(prev, next rune) bool) []string {
	var out []string
	start := 0
	var prev rune
	for i, r := range s {
		if i > 0 && boundary(prev, r) {
			// keep the separator with the previous unit
			end := i
			if r == ' ' {
				end = i + 1
			}
			if end > start {
				out = append(out, s[start:end])
				start = end
			}
		}
		prev = r
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// hardSplit cuts s into pieces of at most max runes, preferring the last space.
func hardSplit(s string, max int) []string {
	var out []string
	for utf8.RuneCountInString(s) > max {
		r := []rune(s)
		cut := max
		for i := max; i > max/2; i-- {
			if r[i] == ' ' || r[i] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimSpace(string(r[:cut])))
		s = strings.TrimSpace(string(r[cut:]))
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// frontMatter splits an optional YAML front matter block ("---" ... "---" or
// "...") off normalized text. It returns the declared date ("date:" key, in
// core.TimeLayout; "" if absent or unparseable) and the body. Other keys are
// ignored: in particular no key can set the trust zone.
func frontMatter(text string) (date, body string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text
	}
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "---" && lines[i] != "..." {
			continue
		}
		for _, l := range lines[1:i] {
			k, v, ok := strings.Cut(l, ":")
			if !ok || strings.TrimSpace(k) != "date" {
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if t, err := core.ParseTime(v); err == nil {
				date = core.FormatTime(t)
			}
		}
		return date, strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
	}
	return "", text
}
