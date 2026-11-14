package lme

import (
	"strings"
	"unicode/utf8"
)

// ChunkChars is the plain-RAG chunk size in runes (about 250 tokens by the
// shared estimator): small enough that the 2,000-token budget holds several
// chunks, and under e5-small's 512-token window so the embedder never clips.
const ChunkChars = 1000

// Chunk is one plain-RAG unit: consecutive turns of one session.
type Chunk struct {
	Session int    // index in the question's haystack
	Ordinal int    // position within the session's chunks
	Text    string // "user: ...\nassistant: ..." lines
}

// TurnLine renders a turn as the chunk text shows it.
func TurnLine(t Turn) string { return t.Role + ": " + strings.TrimSpace(t.Content) }

// ChunkSession splits a session into chunks of at most maxChars runes:
// whole turn lines are packed in order; a line longer than maxChars is cut at
// sentence ends, or hard-cut when a sentence alone is longer. Chunks never
// cross sessions and never overlap.
func ChunkSession(session int, turns []Turn, maxChars int) []Chunk {
	var out []Chunk
	var cur []string
	curLen := 0
	emit := func() {
		if len(cur) > 0 {
			out = append(out, Chunk{Session: session, Ordinal: len(out), Text: strings.Join(cur, "\n")})
			cur, curLen = nil, 0
		}
	}
	for _, t := range turns {
		for _, piece := range splitLong(TurnLine(t), maxChars) {
			n := utf8.RuneCountInString(piece)
			if curLen > 0 && curLen+1+n > maxChars {
				emit()
			}
			if curLen > 0 {
				curLen++
			}
			cur = append(cur, piece)
			curLen += n
		}
	}
	emit()
	return out
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

// sentences splits after ". ", "? ", "! " and newlines, keeping separators.
func sentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(text); i++ {
		if ((text[i] == '.' || text[i] == '?' || text[i] == '!') && (text[i+1] == ' ' || text[i+1] == '\n')) || text[i] == '\n' {
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}
