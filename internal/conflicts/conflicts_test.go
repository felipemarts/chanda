package conflicts

import (
	"reflect"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/dedup"
)

var params = Params{MinCosine: 0.8, MaxPerRecord: 3, Threshold: 0.95}

func rec(id, content, validFrom, recordedAt string) Record {
	return Record{ID: id, Content: content, ValidFrom: validFrom, RecordedAt: recordedAt}
}

const (
	day1 = "2024-01-01T00:00:00.000000000Z"
	day2 = "2024-06-01T00:00:00.000000000Z"
)

// Decision 0148, detection: each of the five conditions on and off.
func TestDetectConditions(t *testing.T) {
	newer := rec("evt_2", "The office of Acme is in Lisbon now.", "", day2)
	older := rec("evt_1", "The office of Acme is in Porto.", "", day1)
	for _, tc := range []struct {
		name string
		n    Record
		x    Record
		cos  float64
		want int
	}{
		{"all hold", newer, older, 0.85, 1},
		{"1 similar: below min_cosine", newer, older, 0.79, 0},
		{"1 similar: at min_cosine", newer, older, 0.80, 1},
		{"2 not a duplicate: the guard accepts", rec("evt_2", "The office of Acme is in Porto.", "", day2), older, 0.99, 0},
		{"3 related: nothing shared", rec("evt_2", "Bought 3 apples.", "", day2), rec("evt_1", "Paid 5 euros.", "", day1), 0.85, 0},
		{"4 different: same signature", rec("evt_2", "The office of Acme is large.", "", day2),
			rec("evt_1", "The office of Acme is small.", "", day1), 0.85, 0},
		{"5 different times: same effective time", newer, rec("evt_1", "The office of Acme is in Porto.", "", day2), 0.85, 0},
		{"5 different times: valid_from counts", newer, rec("evt_1", "The office of Acme is in Porto.", day1, day2), 0.85, 1},
	} {
		pairs, _ := Detect(tc.n, []Neighbour{{tc.x, tc.cos}}, params)
		if len(pairs) != tc.want {
			t.Errorf("%s: %d pairs %+v, want %d", tc.name, len(pairs), pairs, tc.want)
		}
	}
	pairs, refusals := Detect(newer, []Neighbour{{older, 0.85}}, params)
	want := Pair{OlderID: "evt_1", NewerID: "evt_2", Cosine: 0.85, Differs: CompEntities, Shared: []string{"acme", "office"}}
	if len(pairs) != 1 || !reflect.DeepEqual(pairs[0], want) || len(refusals) != 0 {
		t.Fatalf("pair %+v refusals %+v", pairs, refusals)
	}
	// Order is by effective time, not by arrival: a new record dated earlier
	// is the older side.
	early := rec("evt_3", "The office of Acme was in Braga.", "2023-01-01", day2)
	pairs, _ = Detect(early, []Neighbour{{older, 0.85}}, params)
	if len(pairs) != 1 || pairs[0].OlderID != "evt_3" || pairs[0].NewerID != "evt_1" {
		t.Fatalf("order by effective time: %+v", pairs)
	}
	// Differing component: numbers first (Sig.Compare order).
	pairs, _ = Detect(rec("evt_2", "The Acme plan costs 12 dollars.", "", day2),
		[]Neighbour{{rec("evt_1", "The Acme plan costs 10 dollars.", "", day1), 0.9}}, params)
	if len(pairs) != 1 || pairs[0].Differs != CompNumbers {
		t.Fatalf("numbers: %+v", pairs)
	}
	pairs, _ = Detect(rec("evt_2", "The Acme plan is not cheap.", "", day2),
		[]Neighbour{{rec("evt_1", "The Acme plan is cheap.", "", day1), 0.9}}, params)
	if len(pairs) != 1 || pairs[0].Differs != CompNegations {
		t.Fatalf("negations: %+v", pairs)
	}
}

// max_per_record keeps the highest cosines, ties by id; the guard's
// refusals are reported whatever min_cosine says.
func TestDetectCapAndRefusals(t *testing.T) {
	n := rec("evt_9", "The office of Acme is in Lisbon now.", "", day2)
	nb := []Neighbour{
		{rec("evt_4", "The office of Acme is in Porto.", "", day1), 0.85},
		{rec("evt_3", "The office of Acme is in Braga.", "", day1), 0.90},
		{rec("evt_2", "The office of Acme is in Faro.", "", day1), 0.90},
		{rec("evt_1", "The office of Acme is in Évora.", "", day1), 0.97}, // above the threshold: refused, still a pair
	}
	p := params
	p.MaxPerRecord = 2
	pairs, refusals := Detect(n, nb, p)
	var got []string
	for _, x := range pairs {
		got = append(got, x.OlderID)
	}
	if !reflect.DeepEqual(got, []string{"evt_1", "evt_2"}) {
		t.Fatalf("cap: %v", got)
	}
	if len(refusals) != 1 || refusals[0] != (Refusal{OlderID: "evt_1", NewerID: "evt_9", Cosine: 0.97, Reason: dedup.ReasonEntitiesDiffer}) {
		t.Fatalf("refusals %+v", refusals)
	}
	p.MaxPerRecord = 0
	if pairs, _ := Detect(n, nb, p); len(pairs) != 4 {
		t.Fatalf("no cap: %d", len(pairs))
	}
	// The lexical path's floor counts as the cosine stage of the guard.
	p.Lexical = dedup.Lexical{MaxAddedWords: 3, MinCosine: 0.5}
	_, refusals = Detect(rec("evt_9", "Acme costs 12.", "", day2), []Neighbour{{rec("evt_1", "Acme costs 10.", "", day1), 0.6}}, p)
	if len(refusals) != 0 {
		t.Fatalf("a substitution never takes the lexical path: %+v", refusals)
	}
	_, refusals = Detect(rec("evt_9", "Acme costs 10 or 12.", "", day2), []Neighbour{{rec("evt_1", "Acme costs 10.", "", day1), 0.6}}, p)
	if len(refusals) != 1 || refusals[0].Reason != dedup.ReasonNumbersDiffer {
		t.Fatalf("lexical path refusal: %+v", refusals)
	}
	// Determinism: the same input gives the same output whatever the order.
	rev := []Neighbour{nb[3], nb[2], nb[1], nb[0]}
	p.Lexical = dedup.Lexical{}
	a, _ := Detect(n, nb, p)
	b, _ := Detect(n, rev, p)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("order changed the result:\n%+v\n%+v", a, b)
	}
}

func TestTerms(t *testing.T) {
	got := Terms("Ana's NEW office: that is where 2024 things happen, in São Paulo.")
	if want := []string{"happen", "office", "paulo", "things"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terms %v, want %v", got, want)
	}
	if !reflect.DeepEqual(Terms("Escritório em São Paulo"), []string{"escritorio", "paulo"}) {
		t.Fatalf("accents: %v", Terms("Escritório em São Paulo"))
	}
}

// Decision 0148, rules after the LLM: one row per rule.
func TestDecideRules(t *testing.T) {
	t1, t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	a := Side{ID: "evt_a", Effective: t1, Zone: core.ZoneInternal, Role: "user"}
	b := Side{ID: "evt_b", Effective: t2, Zone: core.ZoneInternal, Role: "user"}
	for _, tc := range []struct {
		name string
		ans  Answer
		a, b Side
		want Decision
	}{
		{"applied", Answer{"supersedes", "B", 0.9}, a, b,
			Decision{Verdict: "supersedes", Reason: ReasonApplied, Apply: true, OldID: "evt_a", NewID: "evt_b"}},
		{"current is the older", Answer{"supersedes", "A", 0.9}, a, b, Decision{Verdict: "uncertain", Reason: ReasonNotLater}},
		{"same time", Answer{"supersedes", "B", 0.9}, a, Side{ID: "evt_b", Effective: t1, Zone: core.ZoneInternal}, Decision{Verdict: "uncertain", Reason: ReasonNotLater}},
		{"lower zone", Answer{"supersedes", "B", 0.9}, Side{ID: "evt_a", Effective: t1, Zone: core.ZoneUser}, Side{ID: "evt_b", Effective: t2, Zone: core.ZoneExternal},
			Decision{Verdict: "supersedes", Reason: ReasonLowerZone}},
		{"higher zone", Answer{"supersedes", "B", 0.9}, Side{ID: "evt_a", Effective: t1, Zone: core.ZoneExternal}, Side{ID: "evt_b", Effective: t2, Zone: core.ZoneUser},
			Decision{Verdict: "supersedes", Reason: ReasonApplied, Apply: true, OldID: "evt_a", NewID: "evt_b"}},
		{"assistant over user", Answer{"supersedes", "B", 0.9}, a, Side{ID: "evt_b", Effective: t2, Zone: core.ZoneInternal, Role: "assistant"},
			Decision{Verdict: "supersedes", Reason: ReasonAssistantOverUser}},
		{"user over assistant", Answer{"supersedes", "B", 0.9}, Side{ID: "evt_a", Effective: t1, Zone: core.ZoneInternal, Role: "assistant"}, b,
			Decision{Verdict: "supersedes", Reason: ReasonApplied, Apply: true, OldID: "evt_a", NewID: "evt_b"}},
		{"low confidence", Answer{"supersedes", "B", 0.69}, a, b, Decision{Verdict: "uncertain", Reason: ReasonLowConfidence}},
		{"confidence at the floor", Answer{"supersedes", "B", 0.7}, a, b,
			Decision{Verdict: "supersedes", Reason: ReasonApplied, Apply: true, OldID: "evt_a", NewID: "evt_b"}},
		{"later-date rule first", Answer{"supersedes", "A", 0.1}, a, b, Decision{Verdict: "uncertain", Reason: ReasonNotLater}},
		{"coexist", Answer{"coexist", "B", 1}, a, b, Decision{Verdict: "coexist", Reason: "coexist"}},
		{"unrelated", Answer{"unrelated", "A", 1}, a, b, Decision{Verdict: "unrelated", Reason: "unrelated"}},
		{"uncertain", Answer{"uncertain", "B", 1}, a, b, Decision{Verdict: "uncertain", Reason: "uncertain"}},
	} {
		if got := Decide(tc.ans, tc.a, tc.b, 0.7); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
