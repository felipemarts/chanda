package lme

import (
	"regexp"
	"strings"
	"unicode"
)

// reRecordRef is a Chanda record id inside a text.
var reRecordRef = regexp.MustCompile(`\b(evt|fct|pg|pgi|ent|src|doc|ret|out|run)_[0-9A-Za-z]+`)

// The used_ids rule of eval/sleep/PROTOCOL.md (procedure, step 2): which
// delivered items an answer draws on, decided mechanically.
//
//   - The answer's tokens are its lower-case alphanumeric words of three
//     letters or more that are not on UsedStopWords, plus its digit runs
//     (any length). Tokens are distinct.
//   - An item counts as used when at least half of those tokens, rounded up,
//     occur in the item's text. "Occur" means: equal to one of the item
//     text's own words (lower-case alphanumeric runs) or digit runs, so
//     "art" does not occur in "start" and "7" occurs in "May 7th".
//   - An answer that says "Not mentioned", or that has no tokens, uses
//     nothing.
//   - Record ids in an item's text (a ficha line names its origin events,
//     "evt_01M7...") are not text: they are removed before the lookup, so
//     the digits of an id never match a number of the answer.

// UsedStopWords is the fixed stop list of the rule: common English function
// words of three letters or more (shorter words never count anyway).
var UsedStopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		the and for are but not you all any can had her was one our out has
		his how its may who did yes she him they them this that with from
		have were been what when where which there their about would could
		should into than then also just some very only over such does will
		your yours these those here because while being each other more most
		much many after before during between both done get got said says
		like well even still yet off per via let lets
	`) {
		m[w] = true
	}
	return m
}()

// words splits s into lower-case alphanumeric runs (letters and digits).
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// digitRuns returns the maximal runs of digits of s.
func digitRuns(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
}

// SaysNotMentioned reports whether an answer says "Not mentioned" (the
// reader's abstention phrase), ignoring case and spacing.
func SaysNotMentioned(answer string) bool {
	return strings.Contains(strings.Join(strings.Fields(strings.ToLower(answer)), " "), "not mentioned")
}

// AnswerTokens returns the distinct tokens of an answer, in order of first
// occurrence: words of three runes or more not on the stop list, then the
// digit runs not already present.
func AnswerTokens(answer string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, w := range words(answer) {
		if len([]rune(w)) >= 3 && !UsedStopWords[w] {
			add(w)
		}
	}
	for _, d := range digitRuns(answer) {
		add(d)
	}
	return out
}

// textTokens is the set an answer token is looked up in: every word and
// every digit run of the text.
func textTokens(text string) map[string]bool {
	text = reRecordRef.ReplaceAllString(text, " ")
	m := map[string]bool{}
	for _, w := range words(text) {
		m[w] = true
	}
	for _, d := range digitRuns(text) {
		m[d] = true
	}
	return m
}

// UsedItem is a delivered item for the rule: its id and its text as
// delivered.
type UsedItem struct {
	ID   string
	Text string
}

// UsedIDs applies the rule to answer and the delivered items, and returns
// the ids of the items used, in delivery order (never nil).
func UsedIDs(answer string, items []UsedItem) []string {
	out := []string{}
	if SaysNotMentioned(answer) {
		return out
	}
	toks := AnswerTokens(answer)
	if len(toks) == 0 {
		return out
	}
	need := (len(toks) + 1) / 2
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			continue
		}
		tt := textTokens(it.Text)
		n := 0
		for _, t := range toks {
			if tt[t] {
				n++
			}
		}
		if n >= need {
			seen[it.ID] = true
			out = append(out, it.ID)
		}
	}
	return out
}
