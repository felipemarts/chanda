package lme

import (
	"reflect"
	"testing"
)

func TestAnswerTokens(t *testing.T) {
	cases := map[string][]string{
		"Pixel":                               {"pixel"},
		"The grey cat named Pixel":            {"grey", "cat", "named", "pixel"},
		"On 7 May 2023, at 3pm":               {"2023", "3pm", "7", "3"},
		"by dancing":                          {"dancing"}, // "by" is too short
		"It was in Paris, and then in Paris.": {"paris"},   // stop words; distinct
		"Yes":                                 nil,
		"":                                    nil,
		"Café Müller":                         {"café", "müller"},
		"Lisbon (2 hours, 5 minutes)":         {"lisbon", "hours", "minutes", "2", "5"},
		"they have been there with her":       nil,
	}
	for in, want := range cases {
		if got := AnswerTokens(in); !reflect.DeepEqual(got, want) {
			t.Errorf("AnswerTokens(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsedIDs(t *testing.T) {
	items := []UsedItem{
		{ID: "evt_1", Text: "Ana: Great! I adopted a grey cat named Pixel from the shelter on Monday."},
		{ID: "evt_2", Text: "Ana: I ran my first half marathon in Lisbon last Sunday, 2 hours 5 minutes."},
		{ID: "pg_1", Text: "2023-06-27 | user | evt_2 | half marathon in Lisbon\n2023-05-08 | user | evt_01M7GTM8B9HE7V5DEXX76SRNN3 | grey cat Pixel"},
		{ID: "evt_3", Text: "Bruno: I started pottery classes downtown, every Thursday evening."},
		{ID: "evt_1", Text: "duplicate id"},
	}
	cases := []struct {
		answer string
		want   []string
	}{
		{"Pixel", []string{"evt_1", "pg_1"}},
		// 3 tokens (grey, cat, shelter; "from" is a stop word): 2 needed.
		{"a grey cat from a shelter", []string{"evt_1", "pg_1"}},
		// 4 tokens (lisbon, hours, 2, 5): evt_2 has all; pg_1 only lisbon,
		// since the record ids in its lines are not text ("evt_2" gives no "2").
		{"Lisbon, 2 hours 5", []string{"evt_2"}},
		{"Not mentioned", []string{}},
		{"not  MENTIONED in the memories", []string{}},
		{"Yes", []string{}},
		{"Pottery on Thursdays", []string{"evt_3"}}, // 2 tokens, 1 needed (thursdays != thursday)
		// The digits of a ULID are not digit runs of the text.
		{"7 May", []string{}},
		// "art" must not match inside "started".
		{"art", []string{}},
		// 3 tokens, 2 needed: "pottery" and "classes" in evt_3.
		{"pottery classes weekly", []string{"evt_3"}},
	}
	for _, c := range cases {
		if got := UsedIDs(c.answer, items); !reflect.DeepEqual(got, c.want) {
			t.Errorf("UsedIDs(%q) = %v, want %v", c.answer, got, c.want)
		}
	}
	if got := UsedIDs("Pixel", nil); got == nil || len(got) != 0 {
		t.Errorf("no items: %v", got)
	}
}
