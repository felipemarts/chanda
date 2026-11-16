package window

import (
	"strings"
	"testing"
)

func runeCost(s string) int { return (len([]rune(s)) + 3) / 4 }

func lowerFields(s string) []string {
	return strings.Fields(strings.ToLower(strings.NewReplacer(".", " ", ",", " ").Replace(s)))
}

func TestBestPicksTheWindowWithTheMostTerms(t *testing.T) {
	text := "We talked about the weather. The flight to Lisbon leaves at nine. Nothing else happened today."
	got, cost, ok := Best(text, 12, runeCost, lowerFields, []string{"flight", "lisbon"})
	if !ok || got != Ellipsis+"The flight to Lisbon leaves at nine."+Ellipsis || cost != runeCost(got) || cost > 12 {
		t.Fatalf("got %q cost %d ok %v", got, cost, ok)
	}
	// No term matches: the earliest window wins.
	got, _, ok = Best(text, 12, runeCost, lowerFields, nil)
	if !ok || !strings.HasPrefix(got, "We talked") {
		t.Fatalf("earliest window: %q", got)
	}
	// Everything fits: the whole text, no ellipsis.
	got, _, ok = Best(text, 1000, runeCost, lowerFields, []string{"flight"})
	if !ok || got != text {
		t.Fatalf("whole text: %q", got)
	}
	if _, _, ok := Best("", 10, runeCost, lowerFields, nil); ok {
		t.Fatal("empty text has no window")
	}
}

func TestUnitsJoinBackIntoTheText(t *testing.T) {
	for _, s := range []string{"One. Two! Three?", "a b  c\nd", "averyveryverylongwordwithoutspaces and more", " lead. trail "} {
		units := Units(s, func(u string) bool { return len([]rune(u)) <= 6 }, 3)
		if strings.Join(units, "") != s {
			t.Errorf("%q: units %q", s, units)
		}
	}
}
