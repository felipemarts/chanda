package sleep

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The verifier of guided digest lines (decision 0142). A line the LLM wrote
// is kept only when everything checkable in it comes from the turns it
// cites. It is a pure function: no database, no LLM.

// Reasons a digest line is discarded (report keys).
const (
	DiscardUnknownOrigin     = "unknown_origin"
	DiscardUnsupportedNumber = "unsupported_number"
	DiscardUnsupportedName   = "unsupported_name"
	DiscardEmpty             = "empty"
	DiscardRepeated          = "repeated"        // extractive: equal to or contained in an earlier line
	DiscardOriginNotLive     = "origin_not_live" // an origin was retired or forgotten before the line was written
)

// Kinds of an LLM digest line.
const (
	LineKindLine     = "line"
	LineKindCount    = "count"
	LineKindTimeline = "timeline"
)

// verifyEvent is what the verifier knows of a cited turn: its content and
// the date shown with it (YYYY-MM-DD, may be empty).
type verifyEvent struct {
	content string
	date    string
}

// reNumber matches a number: a digit run, with . or , inside.
var reNumber = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// verifyLine returns "" when the line may be written, else the discard
// reason. members are the group's turns by id; kind is line, count or
// timeline.
func verifyLine(kind, text string, eventIDs []string, members map[string]verifyEvent) string {
	cited := uniqueSorted(eventIDs)
	if len(cited) == 0 {
		return DiscardUnknownOrigin
	}
	for _, id := range cited {
		if _, ok := members[id]; !ok {
			return DiscardUnknownOrigin
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return DiscardEmpty
	}
	numbers := map[string]bool{}
	var contentTokens = map[string]bool{}
	months, weekdays := map[string]bool{}, map[string]bool{}
	for _, id := range cited {
		ev := members[id]
		for _, n := range reNumber.FindAllString(ev.content, -1) {
			numbers[numberKey(n)] = true
		}
		for _, t := range strings.Fields(normalize(ev.content)) {
			contentTokens[t] = true
		}
		if d, err := time.Parse("2006-01-02", ev.date); err == nil {
			for _, n := range []string{strconv.Itoa(d.Year()), strconv.Itoa(int(d.Month())), strconv.Itoa(d.Day())} {
				numbers[numberKey(n)] = true
			}
			m := strings.ToLower(d.Month().String())
			w := strings.ToLower(d.Weekday().String())
			months[m], months[m[:3]] = true, true
			weekdays[w], weekdays[w[:3]] = true, true
		}
	}
	if kind == LineKindCount {
		numbers[strconv.Itoa(len(cited))] = true
	}
	for _, n := range reNumber.FindAllString(text, -1) {
		if !numbers[numberKey(n)] {
			return DiscardUnsupportedNumber
		}
	}
	for _, name := range properNames(text) {
		norm := normalize(name)
		if months[norm] || weekdays[norm] {
			continue
		}
		toks := strings.Fields(norm)
		if len(toks) == 0 {
			continue
		}
		for _, t := range toks {
			if !contentTokens[t] {
				return DiscardUnsupportedName
			}
		}
	}
	return ""
}

// numberKey is the comparable form of a number: a pure digit run loses its
// leading zeros ("05" and "5" are the same day); a number with separators
// is kept as written.
func numberKey(n string) string {
	if strings.ContainsAny(n, ".,") {
		return n
	}
	t := strings.TrimLeft(n, "0")
	if t == "" {
		return "0"
	}
	return t
}

// nameStop are capitalised words that are not proper names.
var nameStop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`I I'm I've I'll I'd The A An And But Or Nor If In On At To Of For With From By As Is Was
	This That These Those It Its He She They We You My Your His Her Their Our There Here Then So Not No Yes Also
	When What Who Whom Which Where Why How All Some Any Each Every Both User Assistant OK AM PM Mr Mrs Ms Dr`) {
		nameStop[w] = true
	}
}

// properNames returns the proper names of text: capitalised words that do
// not start a sentence and are not on the stop list, and all-caps acronyms
// of two or more letters (wherever they are). Surrounding punctuation and a
// possessive 's are removed.
func properNames(text string) []string {
	var out []string
	start := true
	for _, raw := range strings.Fields(text) {
		w := strings.TrimFunc(raw, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		for _, suf := range []string{"'s", "’s"} {
			w = strings.TrimSuffix(w, suf)
		}
		initial := start
		start = endsSentence(raw)
		if w == "" || nameStop[w] {
			continue
		}
		first := []rune(w)[0]
		if !unicode.IsUpper(first) {
			continue
		}
		if isAcronym(w) || !initial {
			out = append(out, w)
		}
	}
	return out
}

// endsSentence reports whether a raw word ends a sentence: its last
// punctuation before closing quotes or brackets is . ! ? or ….
func endsSentence(raw string) bool {
	t := strings.TrimRight(raw, `"')]}»”’`)
	return t != "" && strings.ContainsRune(".!?…", []rune(t)[len([]rune(t))-1])
}

// isAcronym: two or more letters, every letter upper case.
func isAcronym(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLetter(r) {
			if !unicode.IsUpper(r) {
				return false
			}
			letters++
		}
	}
	return letters >= 2
}
