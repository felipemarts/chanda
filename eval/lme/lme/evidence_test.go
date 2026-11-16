package lme

import (
	"math"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/pkg/chanda"
)

func TestCoverageRule(t *testing.T) {
	turn := "I graduated with a degree in Business Administration."
	runes := len([]rune(squash(turn)))
	cases := []struct {
		name, text string
		want       float64
		reaches    bool
	}{
		{"full", "x " + turn + " y", 1, true},
		{"whitespace reflowed", strings.ReplaceAll(turn, " ", "\n  "), 1, true},
		{"absent", "nothing to see", 0, false},
	}
	for _, c := range cases {
		got := Coverage(turn, c.text)
		if math.Abs(got-c.want) > 1e-9 || Reaches(got) != c.reaches {
			t.Errorf("%s: coverage %v reaches %v, want %v %v", c.name, got, Reaches(got), c.want, c.reaches)
		}
	}
	// A truncated item: a prefix plus Chanda's ellipsis. Exactly 60% counts,
	// one rune less does not.
	sq := []rune(squash(turn))
	n60 := int(math.Ceil(0.6 * float64(runes)))
	for _, tc := range []struct {
		n       int
		reaches bool
	}{{n60, true}, {n60 - 1, false}} {
		text := "[excerpt evt_1 | zone: internal | truncated]\n" + string(sq[:tc.n]) + "…"
		got := Coverage(turn, text)
		if want := float64(tc.n) / float64(runes); math.Abs(got-want) > 1e-9 || Reaches(got) != tc.reaches {
			t.Errorf("prefix %d/%d: coverage %v reaches %v", tc.n, runes, got, Reaches(got))
		}
	}
	// The prefix must be contiguous: the turn's end alone is not coverage.
	if got := Coverage(turn, "Business Administration."); got != 0 {
		t.Errorf("suffix only: %v", got)
	}
	if Coverage("", "anything") != 0 || Coverage("   ", "anything") != 0 {
		t.Error("empty turn must have coverage 0")
	}
}

func evidenceQuestion() *Question {
	long := strings.Repeat("Filler sentence about nothing in particular. ", 30) + "The answer is 42."
	return &Question{
		QuestionID: "qe", QuestionType: "single-session-user",
		HaystackDates: []string{"2023/05/20 (Sat) 02:21", "2023/05/21 (Sun) 02:21"},
		HaystackSessions: [][]Turn{
			{
				{Role: "user", Content: "My dog is called Rex and he is three years old.", HasAnswer: true},
				{Role: "assistant", Content: "Rex sounds lovely; three is a great age for training."},
			},
			{
				{Role: "user", Content: long, HasAnswer: true},
				{Role: "assistant", Content: "Noted."},
			},
		},
	}
}

func TestMatchEvidenceChanda(t *testing.T) {
	q := evidenceQuestion()
	te := map[TurnKey]string{{0, 0}: "evt_a", {0, 1}: "evt_b", {1, 0}: "evt_c", {1, 1}: "evt_d"}
	rex := q.HaystackSessions[0][0].Content
	long := q.HaystackSessions[1][0].Content
	items := []chanda.Item{
		{ID: "evt_a", Type: chanda.RecEvent, Text: rex},
		{ID: "evt_c", Type: chanda.RecEvent, Text: string([]rune(long)[:200]) + "…"}, // truncated far below 60%
	}
	block := "<chanda-context>\n\n[excerpt evt_a | zone: internal]\n" + items[0].Text + "\n\n[excerpt evt_c | zone: internal | truncated]\n" + items[1].Text + "\n</chanda-context>\n"
	ev := MatchEvidenceChanda(q, items, block, te)
	if len(ev) != 2 || !ev[0].Reached || ev[0].By != MatchByID || ev[0].Coverage != 1 || ev[0].Role != "user" {
		t.Fatalf("rex: %+v", ev)
	}
	if ev[1].Reached || ev[1].Coverage <= 0 || ev[1].Coverage >= 0.6 {
		t.Fatalf("long truncated: %+v", ev[1])
	}
	total, in, r := EvidenceCounts(ev)
	if total != 2 || in != 1 || r == nil || *r != 0.5 {
		t.Fatalf("counts %d %d %v", total, in, r)
	}

	// Delivered under another id (a near-duplicate collapsed onto its
	// original): found by text in the block.
	items2 := []chanda.Item{{ID: "evt_z", Type: chanda.RecEvent, Text: rex}}
	ev = MatchEvidenceChanda(q, items2, "\n[excerpt evt_z | zone: internal]\n"+rex+"\n", te)
	if !ev[0].Reached || ev[0].By != MatchByText {
		t.Fatalf("text fallback: %+v", ev[0])
	}
	// A rejected turn (no event id) can still be found by text, and an
	// absent one is not.
	ev = MatchEvidenceChanda(q, nil, "", map[TurnKey]string{})
	if ev[0].Reached || ev[1].Reached {
		t.Fatalf("empty block: %+v", ev)
	}
	// No evidence turn: recall is nil.
	if _, _, r := EvidenceCounts(nil); r != nil {
		t.Fatal("recall without evidence turns must be nil")
	}
}

func TestMatchEvidenceRAG(t *testing.T) {
	q := evidenceQuestion()
	var chunks []Chunk
	for i, s := range q.HaystackSessions {
		chunks = append(chunks, ChunkSession(i, s, 300)...)
	}
	// Session 1's long user turn is cut over several chunks; select the
	// session 0 chunk and only the first chunk of session 1.
	var sel []int
	firstOf1 := -1
	for i, c := range chunks {
		if c.Session == 0 {
			sel = append(sel, i)
		}
		if c.Session == 1 && firstOf1 < 0 {
			firstOf1 = i
		}
	}
	sel = append(sel, firstOf1)
	ev := MatchEvidenceRAG(q, chunks, sel)
	if !ev[0].Reached || ev[0].Coverage != 1 || ev[0].By != MatchByText {
		t.Fatalf("rex: %+v", ev[0])
	}
	if ev[1].Reached || ev[1].Coverage == 0 {
		t.Fatalf("long, first part only: %+v", ev[1])
	}
	// Every chunk of session 1: the cut parts re-join (whitespace ignored).
	var all []int
	for i := range chunks {
		all = append(all, i)
	}
	if ev := MatchEvidenceRAG(q, chunks, all); !ev[1].Reached || ev[1].Coverage != 1 {
		t.Fatalf("long, all parts: %+v", ev[1])
	}
	items := RAGContextItems(q, chunks, sel)
	if len(items) < 3 || items[0].Session != 0 || items[0].Role != "user" || items[0].Truncated || items[1].Role != "assistant" {
		t.Fatalf("items %+v", items)
	}
	last := items[len(items)-1]
	if last.Session != 1 || last.Turn != 0 || !last.Truncated || last.Tokens == 0 {
		t.Fatalf("partial piece %+v", last)
	}
}

func TestChandaContextItems(t *testing.T) {
	q := evidenceQuestion()
	items := []chanda.Item{
		{ID: "evt_a", Type: chanda.RecEvent, Text: "My dog is called Rex and he is three years old."},
		{ID: "fct_1", Type: chanda.RecFact, Text: "dog name: Rex", Origin: chanda.Origin{EventIDs: []string{"evt_a"}}},
		{ID: "evt_b", Type: chanda.RecEvent, Text: "Rex sounds lovely; [three] is…"},
	}
	segA := "\n[excerpt evt_a | zone: internal | date: 2023-05-20 02:21 | source: s000]\n" + items[0].Text + "\n"
	segF := "\n[fact fct_1 | zone: internal | valid from 2023-05-20]\n" + items[1].Text + "\n"
	segB := "\n[excerpt evt_b | zone: internal | date: 2023-05-20 02:21 | source: s000 | truncated]\n" + items[2].Text + "\n"
	block := "<chanda-context retrieval_id=\"r1\">\nRetrieved information.\n" + segF + segA + segB + "</chanda-context>\n"
	et := map[string][]TurnKey{"evt_a": {{0, 0}}, "evt_b": {{0, 1}}}
	got := ChandaContextItems(q, items, block, et)
	want := []ContextItem{
		{Session: 0, Turn: 0, Role: "user", Tokens: Tokens(segA)},
		{Session: 0, Turn: 0, Tokens: Tokens(segF), Type: "fact"},
		{Session: 0, Turn: 1, Role: "assistant", Tokens: Tokens(segB), Truncated: true},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// An unmapped item.
	got = ChandaContextItems(q, []chanda.Item{{ID: "evt_q", Type: chanda.RecEvent}}, block, et)
	if got[0].Session != -1 || got[0].Turn != -1 || got[0].Role != "" || got[0].Tokens != 0 {
		t.Errorf("unmapped %+v", got[0])
	}
}

// Labels with a speaker role (read.context_show_role) are segmented too.
func TestBlockSegmentsWithRoleLabels(t *testing.T) {
	block := "<chanda-context retrieval_id=\"r\">\nheader\n" +
		"\n[excerpt evt_A | role: user | zone: internal | date: 2023-01-01 | source: s1]\nI planted 12 tomato saplings.\n" +
		"\n[excerpt evt_B | role: assistant | zone: internal | date: 2023-01-01 | source: s1 | truncated]\nGreat, here are tips…\n" +
		"</chanda-context>\n"
	seg, trunc := BlockSegments(block, []chanda.Item{{ID: "evt_A"}, {ID: "evt_B"}})
	if !strings.Contains(seg["evt_A"], "12 tomato saplings") || !strings.Contains(seg["evt_B"], "here are tips") {
		t.Fatalf("segments: %q", seg)
	}
	if trunc["evt_A"] || !trunc["evt_B"] {
		t.Fatalf("truncated: %v", trunc)
	}
}
