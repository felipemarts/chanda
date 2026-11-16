package lme

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const locomoTiny = "testdata/locomo-tiny.json"

func TestParseLocomoDate(t *testing.T) {
	cases := map[string]time.Time{
		"1:56 pm on 8 May, 2023":     time.Date(2023, 5, 8, 13, 56, 0, 0, time.UTC),
		"10:37 am on 27 June, 2023":  time.Date(2023, 6, 27, 10, 37, 0, 0, time.UTC),
		"12:05 am on 1 August, 2023": time.Date(2023, 8, 1, 0, 5, 0, 0, time.UTC),
		"12:30 pm on 31 Dec, 2022":   time.Date(2022, 12, 31, 12, 30, 0, 0, time.UTC),
		" 9:00 PM on 2 January 2024": time.Date(2024, 1, 2, 21, 0, 0, 0, time.UTC),
		"7:15 a.m. on 3 Sept, 2023":  time.Date(2023, 9, 3, 7, 15, 0, 0, time.UTC),
	}
	for s, want := range cases {
		got, err := ParseLocomoDate(s)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("ParseLocomoDate(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	for _, bad := range []string{"", "2023/05/20 (Sat) 02:21", "13:00 pm on 8 May, 2023", "1:60 pm on 8 May, 2023",
		"1:56 pm on 31 June, 2023", "1:56 pm on 8 Mai, 2023", "1:56 on 8 May, 2023"} {
		if _, err := ParseLocomoDate(bad); err == nil {
			t.Errorf("ParseLocomoDate(%q): want an error", bad)
		}
	}
}

func TestParseEvidence(t *testing.T) {
	exists := func(id string) bool {
		switch id {
		case "D8:6", "D9:17", "D11:26", "D30:5", "D9:1", "D4:4", "D4:6", "D22:1", "D22:2", "D9:10", "D9:11", "D2:1", "D2:3":
			return true
		}
		return false
	}
	cases := []struct {
		raw     []string
		valid   []string
		invalid []string
		fixed   int
	}{
		{[]string{"D2:1"}, []string{"D2:1"}, nil, 0},
		// The malformed entries of SOURCE.md.
		{[]string{"D8:6; D9:17"}, []string{"D8:6", "D9:17"}, nil, 1},
		{[]string{"D:11:26"}, []string{"D11:26"}, nil, 1},
		{[]string{"D"}, nil, []string{"D"}, 1},
		{[]string{"D30:05"}, []string{"D30:5"}, nil, 1},
		{[]string{"D9:1 D4:4 D4:6"}, []string{"D9:1", "D4:4", "D4:6"}, nil, 1},
		{[]string{"D22:1 D22:2 D9:10 D9:11"}, []string{"D22:1", "D22:2", "D9:10", "D9:11"}, nil, 1},
		// Dangling ids (D10:19 in conv-42, D4:36 in conv-47) are skipped.
		{[]string{"D10:19", "D2:3"}, []string{"D2:3"}, []string{"D10:19"}, 0},
		{[]string{"D4:36"}, nil, []string{"D4:36"}, 0},
		// Repeats collapse; empty lists stay empty.
		{[]string{"D2:1", "D2:01"}, []string{"D2:1"}, nil, 1},
		{nil, nil, nil, 0},
	}
	for _, c := range cases {
		p := ParseEvidence(c.raw, exists)
		if !reflect.DeepEqual(p.Valid, c.valid) || !reflect.DeepEqual(p.Invalid, c.invalid) || len(p.Fixed) != c.fixed {
			t.Errorf("ParseEvidence(%q) = %+v; want valid %v invalid %v fixed %d", c.raw, p, c.valid, c.invalid, c.fixed)
		}
	}
}

func TestLoadLocomoFixture(t *testing.T) {
	convs, err := LoadLocomo(locomoTiny)
	if err != nil {
		t.Fatal(err)
	}
	c := convs[0]
	if c.SampleID != "conv-t1" || c.SpeakerA != "Ana" || c.SpeakerB != "Bruno" || len(c.Sessions) != 3 || len(c.QA) != 9 {
		t.Fatalf("conversation %+v", c)
	}
	if !reflect.DeepEqual(c.IgnoredDateKeys, []string{"session_4_date_time"}) {
		t.Fatalf("ignored date keys %v", c.IgnoredDateKeys)
	}
	if !c.Sessions[2].Date.Equal(time.Date(2023, 8, 1, 0, 5, 0, 0, time.UTC)) {
		t.Fatalf("session 3 date %v", c.Sessions[2].Date)
	}
	tr, k, ok := c.Turn("D1:4")
	if !ok || k != [2]int{0, 3} || c.Role(tr) != "user" {
		t.Fatalf("D1:4 %v %v %v", tr, k, ok)
	}
	if got := LocomoContent(tr); got != "Ana: Here is Pixel sleeping on my desk. [shares a photo: a photo of a grey cat on a desk]" {
		t.Fatalf("content %q", got)
	}
	b, _, _ := c.Turn("D1:1")
	if c.Role(b) != "assistant" || LocomoContent(b) != "Bruno: Hi Ana! How was your week?" || c.SessionID(1) != "conv-t1-s2" {
		t.Fatalf("speaker_b turn %v / %s", b, c.SessionID(1))
	}
	if c.QA[6].AnswerText() != "2" || c.QA[8].AnswerText() != "" || LocomoCategoryNames[c.QA[5].Category] != "open-domain" {
		t.Fatalf("answers %q %q", c.QA[6].AnswerText(), c.QA[8].AnswerText())
	}
	q := c.AsQuestion("x", "?", []string{"D2:1"})
	if len(q.HaystackSessions) != 3 || !q.HaystackSessions[1][0].HasAnswer || q.HaystackSessions[1][1].HasAnswer ||
		q.HaystackDates[0] != "1:56 pm on 8 May, 2023" || q.HaystackSessions[0][1].Content != "Ana: Great! I adopted a grey cat named Pixel from the shelter on Monday." {
		t.Fatalf("as question %+v", q)
	}
}

func TestLocomoStudyQuestionsFixture(t *testing.T) {
	convs, err := LoadLocomo(locomoTiny)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ReadLocomoSplit("testdata/split-tiny.json")
	if err != nil {
		t.Fatal(err)
	}
	use, err := LocomoStudyQuestions(convs[0], sp, HalfUse)
	if err != nil {
		t.Fatal(err)
	}
	if len(use) != 3 || use[0].ID != "conv-t1-q0" || use[2].ID != "conv-t1-q3" || use[2].Gold != "a bowl" ||
		!reflect.DeepEqual(use[2].Evidence.Valid, []string{"D2:2", "D2:3"}) {
		t.Fatalf("use half %+v", use)
	}
	test, _ := LocomoStudyQuestions(convs[0], sp, HalfTest)
	if len(test) != 5 || test[0].ID != "conv-t1-q2" || !reflect.DeepEqual(test[0].Evidence.Valid, []string{"D2:1"}) {
		t.Fatalf("test half %+v", test)
	}
	// D3:01 -> D3:1; "D2:1 D3:3" split; "D" and the dangling D9:9 skipped.
	if !reflect.DeepEqual(test[1].Evidence.Valid, []string{"D3:1"}) || !reflect.DeepEqual(test[2].Evidence.Valid, []string{"D2:1", "D3:3"}) ||
		!reflect.DeepEqual(test[2].Evidence.Invalid, []string{"D"}) || !reflect.DeepEqual(test[3].Evidence.Invalid, []string{"D9:9"}) {
		t.Fatalf("evidence %+v / %+v / %+v", test[1].Evidence, test[2].Evidence, test[3].Evidence)
	}
	if _, err := LocomoStudyQuestions(convs[0], sp, "both"); err == nil {
		t.Fatal("bad half: want an error")
	}
}

// locomoDataPath is the real locomo10.json: $LME_LOCOMO, else the
// git-ignored eval/locomo/data/locomo10.json of this checkout.
func locomoDataPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("LME_LOCOMO")
	if p == "" {
		p = filepath.Join("..", "..", "locomo", "data", "locomo10.json")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("LoCoMo data not present (%s; fetch it per eval/locomo/SOURCE.md or set LME_LOCOMO)", p)
	}
	return p
}

// The real file: the counts of eval/locomo/SOURCE.md, the date strings,
// every evidence entry, and the split's per-half counts of PROTOCOL.md.
func TestLoadLocomoReal(t *testing.T) {
	p := locomoDataPath(t)
	sum, _, err := FileSHA256(p)
	if err != nil || sum != LocomoSHA256 {
		t.Fatalf("%s: sha256 %s %v", p, sum, err)
	}
	convs, err := LoadLocomo(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{ // sessions, turns
		"conv-26": {19, 419}, "conv-30": {19, 369}, "conv-41": {32, 663}, "conv-42": {29, 629}, "conv-43": {29, 680},
		"conv-44": {28, 675}, "conv-47": {31, 689}, "conv-48": {30, 681}, "conv-49": {25, 509}, "conv-50": {30, 568},
	}
	if len(convs) != 10 {
		t.Fatalf("%d conversations", len(convs))
	}
	totalQA, invalid, fixed := 0, 0, 0
	for _, c := range convs {
		turns := 0
		for _, s := range c.Sessions {
			turns += len(s.Turns)
		}
		if w := want[c.SampleID]; w != [2]int{len(c.Sessions), turns} {
			t.Errorf("%s: %d sessions %d turns, want %v", c.SampleID, len(c.Sessions), turns, w)
		}
		if c.SampleID == "conv-26" && len(c.IgnoredDateKeys) != 16 {
			t.Errorf("conv-26 ignored date keys %v", c.IgnoredDateKeys)
		} else if c.SampleID != "conv-26" && len(c.IgnoredDateKeys) != 0 {
			t.Errorf("%s ignored date keys %v", c.SampleID, c.IgnoredDateKeys)
		}
		totalQA += len(c.QA)
		for _, q := range c.QA {
			e := ParseEvidence(q.Evidence, c.HasDia)
			invalid += len(e.Invalid)
			fixed += len(e.Fixed)
		}
	}
	// 1,986 QA items. The nine entries of SOURCE.md that do not resolve as
	// given: seven malformed (fixed), two dangling; of the pieces, "D" and
	// the two dangling ids are skipped.
	if totalQA != 1986 || invalid != 3 || fixed != 7 {
		t.Errorf("qa %d, invalid pieces %d, fixed entries %d", totalQA, invalid, fixed)
	}
	sp, err := ReadLocomoSplit(filepath.Join("..", "..", "sleep", "split-locomo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.SHA256 != LocomoSHA256 {
		t.Fatalf("split sha256 %s", sp.SHA256)
	}
	halves := map[string][2]int{"conv-30": {40, 41}, "conv-47": {74, 76}, "conv-49": {76, 80}}
	for id, w := range halves {
		c, err := FindLocomo(convs, id)
		if err != nil {
			t.Fatal(err)
		}
		u, err := LocomoStudyQuestions(c, sp, HalfUse)
		if err != nil {
			t.Fatal(err)
		}
		te, _ := LocomoStudyQuestions(c, sp, HalfTest)
		if len(u) != w[0] || len(te) != w[1] {
			t.Errorf("%s: use %d test %d, want %v", id, len(u), len(te), w)
		}
	}
}
