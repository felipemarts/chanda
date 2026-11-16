package sleep

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func TestScoreEventsSignals(t *testing.T) {
	live := map[string]bool{"e1": true, "e2": true, "e3": true, "e4": true}
	session := map[string]string{"e1": "s1", "e2": "s1", "e3": "s2", "e4": "s3"}
	rets := []guidedRetrieval{
		// 168 hours old: half the demand. e2 is a near miss.
		{ts: t0.Add(-168 * time.Hour), top: []string{"e1", "e2", "gone"}, delivered: []string{"e1", "e3"}},
		// Fresh, failed. e1 is a near miss here.
		{ts: t0, top: []string{"e1"}, delivered: []string{"e4", "e2", "fct_1"}, failed: true},
	}
	w := guidedWeights{demand: 1, nearMiss: 2, failure: 2, fragmentation: 1, halfLifeHours: 168}
	sig := scoreEvents(rets, live, session, t0, w)
	if _, ok := sig["gone"]; ok {
		t.Fatal("an event that is not live gets no signals")
	}
	if _, ok := sig["e3"]; ok {
		t.Fatal("an event in no top k gets no signals")
	}
	e1 := sig["e1"]
	// demand 0.5 + 1; near miss 1 (second retrieval); failure 1; sessions
	// delivered: s1 (own, excluded), s2, s3, s1 -> 2.
	if math.Abs(e1.demand-1.5) > 1e-9 || e1.nearMiss != 1 || e1.failure != 1 || e1.fragmentation != 2 {
		t.Fatalf("e1: %+v", e1)
	}
	if want := 1.5 + 2 + 2 + 2; math.Abs(e1.priority-want) > 1e-9 {
		t.Fatalf("e1 priority %v want %v", e1.priority, want)
	}
	e2 := sig["e2"]
	// first retrieval only: demand 0.5, near miss 1, no failure, sessions s1 (own), s2 -> 1.
	if math.Abs(e2.demand-0.5) > 1e-9 || e2.nearMiss != 1 || e2.failure != 0 || e2.fragmentation != 1 {
		t.Fatalf("e2: %+v", e2)
	}
	// A retrieval in the future counts as age 0.
	sig = scoreEvents([]guidedRetrieval{{ts: t0.Add(time.Hour), top: []string{"e1"}, delivered: []string{"e1"}}}, live, session, t0, w)
	if sig["e1"].demand != 1 || sig["e1"].priority != 1 {
		t.Fatalf("future retrieval: %+v", sig["e1"])
	}
}

func TestBuildGroupsMergeCapAndOrder(t *testing.T) {
	prio := map[string]float64{"a": 5, "b": 4, "c": 3, "d": 1, "x": 2, "y": 2, "z": 0}
	rets := []guidedRetrieval{
		{top: []string{"a", "b", "c"}},
		{top: []string{"b", "a", "c", "d"}}, // Jaccard 3/4 with the first: merged
		{top: []string{"x", "y", "z"}},      // z has priority 0: left out
		{top: []string{"z"}},                // empty after the filter: no candidate
		{top: []string{"a", "x"}},           // overlaps both below 0.5: a group of its own
	}
	groups, ncand := buildGroups(rets, prio, 0.5, 3)
	if ncand != 4 {
		t.Fatalf("candidate groups %d", ncand)
	}
	got := make([][]string, len(groups))
	for i, g := range groups {
		got[i] = g.members
	}
	// {a,b,c,d} capped to the three best {a,b,c} = 12; {a,x} = 7; {x,y} = 4.
	want := [][]string{{"a", "b", "c"}, {"a", "x"}, {"x", "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %v want %v", got, want)
	}
	if groups[0].priority != 12 || groups[1].priority != 7 || groups[2].priority != 4 {
		t.Fatalf("priorities %+v", groups)
	}
	// Threshold 1: only identical sets merge.
	groups, _ = buildGroups(rets[:2], prio, 1, 0)
	if len(groups) != 2 || len(groups[0].members) != 4 {
		t.Fatalf("no merge below 1: %+v", groups)
	}
	// Ties by smallest member id.
	groups, _ = buildGroups([]guidedRetrieval{{top: []string{"y"}}, {top: []string{"x"}}}, prio, 0.5, 0)
	if groups[0].members[0] != "x" {
		t.Fatalf("tie order: %+v", groups)
	}
}

func TestMakeLineFormat(t *testing.T) {
	by := map[string]guidedMember{
		"evt_1": {id: "evt_1", role: "user", session: "s1", ordinal: 2, date: "2023-05-22"},
		"evt_2": {id: "evt_2", role: "assistant", session: "s1", ordinal: 3, date: "2023-05-22"},
		"evt_3": {id: "evt_3", role: "user", session: "s0", ordinal: 9, date: "2023-05-20"},
		"evt_4": {id: "evt_4", date: "2023-06-01"},
	}
	if l := makeLine([]string{"evt_1"}, "  I moved\n to  Lisbon. ", by); l.text != "2023-05-22 | user | evt_1 | I moved to Lisbon." {
		t.Fatalf("single: %q", l.text)
	}
	if l := makeLine([]string{"evt_1", "evt_3"}, "x", by); l.text != "2023-05-20 | user | evt_3,evt_1 | x" {
		t.Fatalf("common role, earliest date: %q", l.text)
	}
	if l := makeLine([]string{"evt_2", "evt_1"}, "x", by); l.text != "2023-05-22 | mixed | evt_1,evt_2 | x" {
		t.Fatalf("mixed: %q", l.text)
	}
	if l := makeLine([]string{"evt_4"}, "x", by); l.text != "2023-06-01 | - | evt_4 | x" {
		t.Fatalf("no role: %q", l.text)
	}
	if got := eventDate("", "2023-05-22T10:00:00.000000000Z"); got != "2023-05-22" {
		t.Fatalf("recorded_at date: %q", got)
	}
	if got := eventDate("2021-01-02", "2023-05-22T10:00:00.000000000Z"); got != "2021-01-02" {
		t.Fatalf("valid_from first: %q", got)
	}
}

func TestExtractiveLines(t *testing.T) {
	long := "We talked about many unrelated things for a long while, the weather, the traffic and the food at the place. " +
		"Then I said the Fender guitar cost 450 dollars at the shop in Lisbon. " +
		"After that we went on about other topics that do not matter at all for anything here, really, nothing at all."
	members := []guidedMember{
		{id: "evt_1", role: "user", session: "s1", ordinal: 1, date: "2023-05-22", content: "Fender guitar is red."},
		{id: "evt_2", role: "assistant", session: "s1", ordinal: 2, date: "2023-05-22", content: long},
		{id: "evt_3", role: "user", session: "s2", ordinal: 1, date: "2023-05-21", content: "my fender guitar is red today"},
		{id: "evt_4", role: "user", session: "s2", ordinal: 2, date: "2023-05-21", content: "  "},
	}
	disc := map[string]int{}
	lines := extractiveLines(members, 30, disc)
	var texts []string
	for _, l := range lines {
		texts = append(texts, l.text)
	}
	// evt_3 comes first (earlier date); evt_1 is contained in it
	// and is dropped as a repetition; evt_4 is empty.
	if len(lines) != 2 || texts[0] != "2023-05-21 | user | evt_3 | my fender guitar is red today" {
		t.Fatalf("lines %q", texts)
	}
	if !strings.HasPrefix(texts[1], "2023-05-22 | assistant | evt_2 | …") || !strings.Contains(texts[1], "Fender guitar cost 450 dollars") {
		t.Fatalf("window line %q", texts[1])
	}
	if disc[DiscardRepeated] != 1 || disc[DiscardEmpty] != 1 {
		t.Fatalf("discarded %v", disc)
	}
	if got := fichaTitle(sharedTerms(members)); got != "Digest: fender, guitar, red" {
		t.Fatalf("title %q", got)
	}
	if fichaTitle(nil) != "Digest" {
		t.Fatal("title without shared terms")
	}
}

func TestVerifyLine(t *testing.T) {
	members := map[string]verifyEvent{
		"evt_1": {content: "I paid 4.2 million for the Quetzal project with Ana from NASA.", date: "2023-05-22"}, // a Monday
		"evt_2": {content: "We met again and Ana brought 3 friends.", date: "2023-06-05"},
		"evt_3": {content: "Lunch was fine.", date: ""},
	}
	for _, tc := range []struct {
		name, kind, text string
		ids              []string
		want             string
	}{
		{"good", "line", "Paid 4.2 million for the Quetzal project with Ana.", []string{"evt_1"}, ""},
		{"empty", "line", "   ", []string{"evt_1"}, DiscardEmpty},
		{"no ids", "line", "Something.", nil, DiscardUnknownOrigin},
		{"unknown id", "line", "Something.", []string{"evt_1", "evt_9"}, DiscardUnknownOrigin},
		{"unsupported integer", "line", "Paid 7 million.", []string{"evt_1"}, DiscardUnsupportedNumber},
		{"unsupported decimal", "line", "Paid 4.3 million.", []string{"evt_1"}, DiscardUnsupportedNumber},
		{"decimal comma differs", "line", "Paid 4,2 million.", []string{"evt_1"}, DiscardUnsupportedNumber},
		{"number from another member", "line", "Ana brought 3 friends.", []string{"evt_1"}, DiscardUnsupportedNumber},
		{"number in the cited member", "line", "Ana brought 3 friends.", []string{"evt_2"}, ""},
		{"year from date", "line", "In 2023 they paid.", []string{"evt_1"}, ""},
		{"day from date", "line", "On the 22nd... no, on day 22 they paid.", []string{"evt_1"}, ""},
		{"month number with leading zero", "line", "Month 05 and month 5.", []string{"evt_1"}, ""},
		{"date not cited", "line", "On day 5 they met.", []string{"evt_3"}, DiscardUnsupportedNumber},
		{"month name from date", "line", "They paid in May.", []string{"evt_1"}, ""},
		{"wrong month name", "line", "They paid in June.", []string{"evt_1"}, DiscardUnsupportedName},
		{"weekday from date", "line", "They paid on Monday.", []string{"evt_1"}, ""},
		{"wrong weekday", "line", "They paid on Friday.", []string{"evt_1"}, DiscardUnsupportedName},
		{"month abbreviation", "line", "They met in Jun.", []string{"evt_2"}, ""},
		{"no date metadata", "line", "Lunch in May was fine.", []string{"evt_3"}, DiscardUnsupportedName},
		{"proper name in content", "line", "They met Ana again.", []string{"evt_2"}, ""},
		{"name case-insensitive", "line", "the work with Quetzal.", []string{"evt_1"}, ""},
		{"unsupported name", "line", "They met Bruno.", []string{"evt_2"}, DiscardUnsupportedName},
		{"possessive", "line", "It was Ana's project.", []string{"evt_1"}, ""},
		{"sentence-initial capital is not a name", "line", "Lunch happened. Fine it was.", []string{"evt_3"}, ""},
		{"stop word", "line", "they said I would.", []string{"evt_3"}, ""},
		{"acronym supported", "line", "Ana works at NASA.", []string{"evt_1"}, ""},
		{"acronym unsupported", "line", "Ana works at ESA.", []string{"evt_1"}, DiscardUnsupportedName},
		{"sentence-initial acronym", "line", "ESA was mentioned.", []string{"evt_1"}, DiscardUnsupportedName},
		{"count line", "count", "Ana appears 2 times.", []string{"evt_1", "evt_2"}, ""},
		{"count line wrong count", "count", "Ana appears 4 times.", []string{"evt_1", "evt_2"}, DiscardUnsupportedNumber},
		{"count only for count kind", "line", "Ana appears 2 times.", []string{"evt_1", "evt_2"}, DiscardUnsupportedNumber},
		{"duplicate ids count once", "count", "Ana appears 1 time.", []string{"evt_2", "evt_2"}, ""},
		{"timeline", "timeline", "First the Quetzal project, then Ana brought 3 friends.", []string{"evt_1", "evt_2"}, ""},
	} {
		if got := verifyLine(tc.kind, tc.text, tc.ids, members); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
