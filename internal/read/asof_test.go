package read

import (
	"slices"
	"testing"
	"time"
)

// BASE.md "as_of answers what held on that date", for raw events (decision
// 0135): with an explicit as_of, an event dated after it (valid_from, else
// recorded_at) did not exist yet and is delivered by neither context nor
// search. A date-only valid_from is stored as 00:00 UTC of its day. Without
// as_of nothing changes.
func TestAsOfDropsEventsDatedAfterIt(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.source("ns", "src_sess", "session", "sess-1", "internal")
	seed := func(id, content, validFrom, recordedAt string) {
		var vf any
		if validFrom != "" {
			vf = validFrom
		}
		f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,valid_from,session_id,ordinal,flags)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "ns", "src_sess", "turn", content, "h"+id, "internal", "test", recordedAt, vf, "sess-1", 0, "")
	}
	const rec = "2026-01-01T09:30:00.000000000Z"
	seed("evt_may", "The quarterly budget review for the marketing team was moved.", "2023-05-20T00:00:00.000000000Z", rec)
	seed("evt_may_pm", "The quarterly budget review for the marketing team is on Monday.", "2023-05-20T18:00:00.000000000Z", rec)
	seed("evt_jun", "The quarterly budget review for the marketing team went well.", "2023-06-01T00:00:00.000000000Z", rec)
	seed("evt_undated", "The quarterly budget review for the marketing team needs slides.", "", rec)

	all := []string{"evt_may", "evt_may_pm", "evt_jun", "evt_undated"}
	endOfDay := time.Date(2023, 5, 20, 23, 59, 59, 999999999, time.UTC) // a date-only as_of, as the App passes it
	cases := []struct {
		name string
		asOf time.Time
		want []string
	}{
		{"no as_of: every event", time.Time{}, all},
		{"end of the day: both events of that day", endOfDay, []string{"evt_may", "evt_may_pm"}},
		{"midnight: a date-only valid_from holds from the start of its day", time.Date(2023, 5, 20, 0, 0, 0, 0, time.UTC), []string{"evt_may"}},
		{"before every date: nothing", time.Date(2023, 5, 19, 12, 0, 0, 0, time.UTC), nil},
		{"undated event counts from recorded_at", time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), all},
		{"just before recorded_at", time.Date(2026, 1, 1, 9, 29, 0, 0, time.UTC), []string{"evt_may", "evt_may_pm", "evt_jun"}},
	}
	r := New(f.e, Deps{})
	const query = "quarterly budget review marketing team"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: query, AsOf: tc.asOf, BudgetTokens: 1500})
			if err != nil {
				t.Fatal(err)
			}
			s, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: query, AsOf: tc.asOf})
			if err != nil {
				t.Fatal(err)
			}
			for mode, got := range map[string][]string{"context": ids(c.Items), "search": ids(s.Items)} {
				g := slices.Clone(got)
				slices.Sort(g)
				w := slices.Clone(tc.want)
				slices.Sort(w)
				if !slices.Equal(g, w) {
					t.Errorf("%s delivered %v, want %v", mode, g, w)
				}
			}
		})
	}
}

// Delivered events carry their valid_from (the item's date); undated events
// carry recorded_at only.
func TestEventItemsCarryValidFrom(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.source("ns", "src_sess", "session", "sess-1", "internal")
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,valid_from,flags)
		VALUES ('evt_d','ns','src_sess','statement','Lunch with the design team at the harbour cafe.','h1','internal','t',?,?,'')`,
		t0, "2023-05-20T00:00:00.000000000Z")
	r := New(f.e, Deps{})
	s, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "lunch design team harbour cafe"})
	if err != nil || len(s.Items) == 0 {
		t.Fatalf("%v %v", ids(s.Items), err)
	}
	for _, it := range s.Items {
		if it.ID == "evt_d" && (it.ValidFrom != "2023-05-20T00:00:00.000000000Z" || it.RecordedAt != t0) {
			t.Fatalf("item %+v", it)
		}
	}
}
