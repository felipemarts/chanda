package eval

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Chunk is one structural unit of an item: a paragraph of a document (under
// its heading path) or a session turn. Baselines index chunks; they never
// import the write path, so their chunking is defined here.
type Chunk struct {
	ID          string // "<doc id>@v<version>#<ordinal>"
	DocID       string
	URI         string
	Version     int
	Ordinal     int
	HeadingPath string
	Text        string
}

// ChunkItem splits an item by structure, without overlap: headings set the
// heading path, blank lines separate paragraphs, and a paragraph longer than
// maxChars is split at sentence ends (or hard-split if a sentence is longer).
// Session turns, and the fact statements asserted in the session, are one
// chunk each.
func ChunkItem(it Item, maxChars int) []Chunk {
	v := max(it.Version, 1)
	var out []Chunk
	emit := func(heading, text string) {
		for _, piece := range splitLong(text, maxChars) {
			out = append(out, Chunk{
				ID: fmt.Sprintf("%s@v%d#%d", it.ID, v, len(out)), DocID: it.ID, URI: it.URI,
				Version: v, Ordinal: len(out), HeadingPath: heading, Text: piece,
			})
		}
	}
	if it.Type == ItemSession {
		for _, t := range it.Turns {
			emit("", strings.TrimSpace(t.Text))
		}
		for _, f := range it.Facts {
			emit("", f.Text())
		}
		return out
	}
	var headings []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			emit(strings.Join(headings, " > "), strings.Join(para, "\n"))
			para = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(it.Text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if level := headingLevel(line); level > 0 {
			flush()
			if level <= len(headings) {
				headings = headings[:level-1]
			}
			for len(headings) < level-1 {
				headings = append(headings, "")
			}
			headings = append(headings, strings.TrimSpace(line[level:]))
			continue
		}
		if line == "" {
			flush()
			continue
		}
		para = append(para, line)
	}
	flush()
	return out
}

func headingLevel(line string) int {
	n := 0
	for n < len(line) && n < 6 && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

func splitLong(text string, maxChars int) []string {
	if maxChars <= 0 || utf8.RuneCountInString(text) <= maxChars {
		return []string{text}
	}
	var out []string
	var cur strings.Builder
	push := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, sent := range sentences(text) {
		if utf8.RuneCountInString(cur.String())+utf8.RuneCountInString(sent) > maxChars {
			push()
		}
		for utf8.RuneCountInString(sent) > maxChars {
			r := []rune(sent)
			cur.WriteString(string(r[:maxChars]))
			push()
			sent = string(r[maxChars:])
		}
		cur.WriteString(sent)
	}
	push()
	return out
}

// sentences splits after ". ", "? " and "! " keeping the separator with the sentence.
func sentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(text); i++ {
		if (text[i] == '.' || text[i] == '?' || text[i] == '!') && (text[i+1] == ' ' || text[i+1] == '\n') {
			out = append(out, text[start:i+2])
			start = i + 2
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// rawLog is the raw history shared by the baselines: every distinct chunk
// ever ingested, never retired. Like write step 4, a chunk whose normalized
// text already exists for the same item is not stored again, so unchanged
// paragraphs of a new document version do not appear twice.
type rawLog struct {
	maxChars int
	chunks   []Chunk
	seen     map[[32]byte]bool
}

func newRawLog(maxChars int) rawLog {
	return rawLog{maxChars: maxChars, seen: map[[32]byte]bool{}}
}

// add chunks it and returns the chunks that are new.
func (l *rawLog) add(it Item) []Chunk {
	var fresh []Chunk
	for _, c := range ChunkItem(it, l.maxChars) {
		k := sha256.Sum256([]byte(c.DocID + "\x00" + Normalize(c.Text)))
		if l.seen[k] {
			continue
		}
		l.seen[k] = true
		l.chunks = append(l.chunks, c)
		fresh = append(fresh, c)
	}
	return fresh
}

// fill takes chunks in rank order and keeps each one that still fits in the
// budget (greedy, skipping items that do not fit).
func fill(ranked []Chunk, scores []float64, budget int) []Retrieved {
	var out []Retrieved
	used := 0
	for i, c := range ranked {
		n := CountTokens(c.Text)
		if used+n > budget {
			continue
		}
		used += n
		r := Retrieved{ID: c.ID, DocID: c.DocID, URI: c.URI, Text: c.Text, Tokens: n}
		if scores != nil {
			r.Score = scores[i]
		}
		out = append(out, r)
	}
	return out
}
