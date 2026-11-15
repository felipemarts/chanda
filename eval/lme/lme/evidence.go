package lme

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Turn-level evidence recall in the context block (eval/selection/PROTOCOL.md,
// primary no-LLM measure): the share of a question's evidence turns
// (has_answer: true, any role) whose text reaches the block.
//
// Matching rule. Texts are compared with every whitespace rune removed (the
// renderers re-flow whitespace: RAG trims and re-joins the parts of a long
// turn, Chanda trims item text). The coverage of a turn in a text is the
// longest prefix of the turn, in runes, that occurs contiguously in that
// text, over the turn's runes. A turn reaches the block when its coverage is
// at least EvidenceMinCoverage (0.6); the full turn present is coverage 1.
// The prefix form matches how both renderers lose text: Chanda truncates an
// item at its end ("…"), RAG drops the later parts of a turn cut across
// chunks.
//
// Where the coverage is measured:
//   - Chanda (D/E): first in the text of each block item whose id is the
//     event id log_session returned for that (session, turn) ("by": "id");
//     failing that, in the whole block ("by": "text"), which catches an
//     evidence turn delivered under another event's id (near-duplicates are
//     collapsed onto their original at read time). Facts and pages derived
//     from the turn do not count: the measure is about the turn's text.
//   - Plain RAG (B): in the rendered text of the selected chunks of the
//     turn's own session ("by": "text").

// EvidenceMinCoverage is the share of an evidence turn's runes (a contiguous
// prefix) that must reach the block for the turn to count.
const EvidenceMinCoverage = 0.6

// Ways an evidence turn was found in the block.
const (
	MatchByID   = "id"
	MatchByText = "text"
)

// EvidenceTurn is one evidence turn of a question and how it fared.
type EvidenceTurn struct {
	Session  int     `json:"session"` // haystack index
	Turn     int     `json:"turn"`    // turn index within the session
	Role     string  `json:"role"`
	Coverage float64 `json:"coverage"` // longest contiguous prefix present / turn runes (3 decimals)
	Reached  bool    `json:"reached"`
	By       string  `json:"by,omitempty"` // id or text, when reached
}

// ContextItem is one item of a context block, for the analysis of the
// budget: Chanda block items, or plain-RAG turn pieces.
type ContextItem struct {
	Session   int    `json:"session"`        // haystack index, -1 when unknown
	Turn      int    `json:"turn"`           // turn index within the session, -1 when unknown
	Role      string `json:"role,omitempty"` // user or assistant, when known
	Tokens    int    `json:"tokens"`         // ceil(runes/4) of the item as rendered
	Truncated bool   `json:"truncated,omitempty"`
	Type      string `json:"type,omitempty"` // Chanda record type when not an event (fact, page)
}

// squash removes every whitespace rune.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// Coverage is the longest prefix of turn (in non-whitespace runes) found
// contiguously in text, over the turn's non-whitespace runes. Both are
// compared with whitespace removed. An empty turn has coverage 0.
func Coverage(turn, text string) float64 {
	return coverageSquashed([]rune(squash(turn)), squash(text))
}

func coverageSquashed(t []rune, text string) float64 {
	if len(t) == 0 {
		return 0
	}
	// Containment of prefixes is monotone: binary search the longest one.
	lo, hi := 0, len(t)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if strings.Contains(text, string(t[:mid])) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return float64(lo) / float64(len(t))
}

// Reaches applies the matching rule to a coverage.
func Reaches(coverage float64) bool { return coverage >= EvidenceMinCoverage }

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// EvidenceTurns lists the has_answer turns of q, in haystack order.
func EvidenceTurns(q *Question) []EvidenceTurn {
	var out []EvidenceTurn
	for s, sess := range q.HaystackSessions {
		for t, tr := range sess {
			if tr.HasAnswer {
				out = append(out, EvidenceTurn{Session: s, Turn: t, Role: tr.Role})
			}
		}
	}
	return out
}

// TurnKey names a haystack turn.
type TurnKey struct{ Session, Turn int }

// MatchEvidenceChanda scores q's evidence turns against a Chanda context
// block: items are the delivered items (their Text as delivered, truncated
// ones included), block the rendered text, turnEvent the event id
// log_session returned for each turn.
func MatchEvidenceChanda(q *Question, items []chanda.Item, block string, turnEvent map[TurnKey]string) []EvidenceTurn {
	ev := EvidenceTurns(q)
	var whole string
	for i := range ev {
		e := &ev[i]
		t := []rune(squash(q.HaystackSessions[e.Session][e.Turn].Content))
		id := turnEvent[TurnKey{e.Session, e.Turn}]
		best := 0.0
		if id != "" {
			for _, it := range items {
				if it.ID == id {
					best = max(best, coverageSquashed(t, squash(it.Text)))
				}
			}
		}
		if Reaches(best) {
			e.Reached, e.By = true, MatchByID
		} else {
			if whole == "" {
				whole = squash(unquote(block))
			}
			if c := coverageSquashed(t, whole); c > best {
				best = c
				if Reaches(c) {
					e.Reached, e.By = true, MatchByText
				}
			}
		}
		e.Coverage = round3(best)
	}
	return ev
}

// unquote drops the "> " prefix Chanda puts on every line of an external
// quotation.
func unquote(block string) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "> ")
	}
	return strings.Join(lines, "\n")
}

// MatchEvidenceRAG scores q's evidence turns against a plain-RAG context:
// the coverage of each turn is measured in the rendered selected chunks of
// its own session.
func MatchEvidenceRAG(q *Question, chunks []Chunk, selected []int) []EvidenceTurn {
	ev := EvidenceTurns(q)
	perSession := map[int]string{}
	for i := range ev {
		e := &ev[i]
		text, ok := perSession[e.Session]
		if !ok {
			var sel []int
			for _, ci := range selected {
				if chunks[ci].Session == e.Session {
					sel = append(sel, ci)
				}
			}
			if len(sel) > 0 {
				text = squash(RenderChunks(q, chunks, sel))
			}
			perSession[e.Session] = text
		}
		c := coverageSquashed([]rune(squash(q.HaystackSessions[e.Session][e.Turn].Content)), text)
		e.Coverage = round3(c)
		if Reaches(c) {
			e.Reached, e.By = true, MatchByText
		}
	}
	return ev
}

// RAGContextItems lists the turn pieces of the selected chunks, chunks in
// the order the context renders them (sessions by date), pieces in order.
// A piece that is only part of a turn is marked truncated.
func RAGContextItems(q *Question, chunks []Chunk, selected []int) []ContextItem {
	order := renderOrder(q, chunks, selected)
	out := []ContextItem{}
	for _, ci := range order {
		c := chunks[ci]
		for _, p := range c.Pieces {
			out = append(out, ContextItem{Session: c.Session, Turn: p.Turn, Role: q.HaystackSessions[c.Session][p.Turn].Role,
				Tokens: Tokens(p.Text), Truncated: !p.Whole})
		}
	}
	return out
}

// BlockSegments splits a rendered Chanda context block into the rendered
// text of each item (label line, content, the newlines around them),
// keyed by item id, and reports which labels say "truncated". An item
// renders as "\n[<kind> <id> | (role: ... | )zone: ...]\n<content>\n"; its segment runs
// from that leading newline to the next item's, or to the block footer.
func BlockSegments(block string, items []chanda.Item) (seg map[string]string, truncated map[string]bool) {
	seg, truncated = map[string]string{}, map[string]bool{}
	type pos struct {
		id    string
		start int
	}
	var ps []pos
	from := 0
	for _, it := range items {
		// Items are rendered in delivery order: search forward, then fall back
		// to the whole block.
		i := labelIndex(block[from:], it.ID)
		if i >= 0 {
			i += from
		} else if i = labelIndex(block, it.ID); i < 0 {
			continue
		}
		ps = append(ps, pos{it.ID, i})
		from = i + 1
	}
	sort.Slice(ps, func(a, b int) bool { return ps[a].start < ps[b].start })
	end := strings.LastIndex(block, "</chanda-context>")
	if end < 0 {
		end = len(block)
	}
	for k, p := range ps {
		stop := end
		if k+1 < len(ps) {
			stop = ps[k+1].start
		}
		if stop < p.start {
			stop = p.start
		}
		s := block[p.start:stop]
		seg[p.id] = s
		label, _, _ := strings.Cut(strings.TrimPrefix(s, "\n"), "\n")
		truncated[p.id] = strings.HasSuffix(label, "| truncated]")
	}
	return seg, truncated
}

// labelIndex finds the "\n[" that opens the label line of item id.
func labelIndex(block, id string) int {
	needle := " " + id + " | " // "| zone: " or, with read.context_show_role, "| role: "
	off := 0
	for {
		j := strings.Index(block[off:], needle)
		if j < 0 {
			return -1
		}
		j += off
		ls := strings.LastIndex(block[:j], "\n")
		if ls >= 0 && ls+1 < len(block) && block[ls+1] == '[' && !strings.Contains(block[ls+1:j], "]") {
			return ls
		}
		off = j + len(needle)
	}
}

// ChandaContextItems describes the delivered items: the (session, turn)
// of an event item through the ids log_session returned (for a fact or a
// page, its first origin event), the role of that turn, the rendered token
// cost and the truncation mark of the label.
func ChandaContextItems(q *Question, items []chanda.Item, block string, eventTurns map[string][]TurnKey) []ContextItem {
	seg, trunc := BlockSegments(block, items)
	out := make([]ContextItem, 0, len(items))
	for _, it := range items {
		ci := ContextItem{Session: -1, Turn: -1, Tokens: Tokens(seg[it.ID]), Truncated: trunc[it.ID]}
		if it.Type != chanda.RecEvent {
			ci.Type = string(it.Type)
		}
		ids := []string{it.ID}
		if it.Type != chanda.RecEvent {
			ids = it.Origin.EventIDs
		}
		for _, id := range ids {
			if ks := eventTurns[id]; len(ks) > 0 {
				ci.Session, ci.Turn = ks[0].Session, ks[0].Turn
				if it.Type == chanda.RecEvent {
					ci.Role = q.HaystackSessions[ci.Session][ci.Turn].Role
				}
				break
			}
		}
		out = append(out, ci)
	}
	return out
}

// EvidenceCounts sets the three summary numbers of a matched list: total,
// reached and recall (nil when the question has no evidence turn).
func EvidenceCounts(ev []EvidenceTurn) (total, in int, recall *float64) {
	for _, e := range ev {
		total++
		if e.Reached {
			in++
		}
	}
	if total > 0 {
		r := float64(in) / float64(total)
		recall = &r
	}
	return total, in, recall
}
