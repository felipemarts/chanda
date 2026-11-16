package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/pkg/chanda"
)

func turnsAt(ts string, contents ...string) []chanda.Turn {
	out := make([]chanda.Turn, len(contents))
	for i, c := range contents {
		out[i] = chanda.Turn{Role: "user", Content: c, TS: ts}
	}
	return out
}

// BASE.md "valid_from uses, in this order: the date given in the call, ...",
// through log_session (eval/deviations.md 4, decision 0135): the turn's ts
// becomes the event's valid_from, recorded_at stays the write time, and an
// unparsable ts is refused.
func TestLogSession_TurnTSBecomesValidFrom(t *testing.T) {
	a, _ := newApp(t, nil)
	out := must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "s1", Turns: []chanda.Turn{
		{Role: "user", Content: "I adopted a cat named Miso today.", TS: "2023-05-20T14:03:00Z"},
		{Role: "assistant", Content: "Congratulations on adopting Miso!", TS: "2023-05-20"},
		{Role: "user", Content: "A turn without any timestamp at all."},
	}}))
	want := []string{"2023-05-20T14:03:00.000000000Z", "2023-05-20T00:00:00.000000000Z", ""}
	for i, w := range want {
		id := out.Turns[i].ID
		if got := str(t, a, `SELECT COALESCE(valid_from,'') FROM events WHERE id = ?`, id); got != w {
			t.Errorf("turn %d valid_from %q, want %q", i, got, w)
		}
		if rec := str(t, a, `SELECT recorded_at FROM events WHERE id = ?`, id); rec < "2026" {
			t.Errorf("turn %d recorded_at %q is not the write time", i, rec)
		}
	}
	_, err := a.LogSession(agent, chanda.LogSessionInput{SessionID: "s2", Turns: turnsAt("last tuesday", "Some turn with a bad date.")})
	if core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("bad ts: %v", err)
	}
	if n := count(t, a, `SELECT count(*) FROM events WHERE session_id = 's2'`); n != 0 {
		t.Fatalf("bad ts wrote %d events", n)
	}
}

// BASE.md decision table, "later date": sessions ingested out of date order
// reconcile by valid_from, not by ingest order. A fact extracted from a
// turn (the header extractor) takes the turn's date, and an assert_fact
// without valid_from inside a dated session takes the session's date.
func TestFactsFromDatedSessionsSupersedeByDate(t *testing.T) {
	a, _ := newApp(t, nil)
	// The later session first.
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "late",
		Turns: turnsAt("2023-08-01T10:00:00Z", "Name: Project Atlas\nOwner: Bruna Lima")}))
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "early",
		Turns: turnsAt("2023-03-01T10:00:00Z", "Name: Project Atlas\nOwner: Marina Duarte")}))
	tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Project Atlas", Predicate: "owner"}))
	if len(tl.Entries) != 2 {
		t.Fatalf("timeline %+v", tl.Entries)
	}
	early, late := tl.Entries[0], tl.Entries[1]
	if early.Value != "Marina Duarte" || early.ValidFrom != "2023-03-01T10:00:00.000000000Z" || early.Status != "superseded" ||
		late.Value != "Bruna Lima" || late.ValidFrom != "2023-08-01T10:00:00.000000000Z" || late.Status != "active" {
		t.Fatalf("timeline %+v", tl.Entries)
	}
	held := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Project Atlas", Predicate: "owner", AsOf: "2023-05-01"}))
	if len(held.Entries) != 1 || held.Entries[0].Value != "Marina Duarte" {
		t.Fatalf("as_of: %+v", held.Entries)
	}

	// assert_fact in the same sessions, again later session first.
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "s-jun", Turns: turnsAt("2023-06-10", "We talked about where Ana works now.")}))
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "s-feb", Turns: turnsAt("2023-02-10", "We talked about where Ana worked before.")}))
	jun := must(a.AssertFact(agent, chanda.AssertFactInput{Subject: "Ana", Predicate: "employer", Value: "Globex", SessionID: "s-jun"}))
	feb := must(a.AssertFact(agent, chanda.AssertFactInput{Subject: "Ana", Predicate: "employer", Value: "Initech", SessionID: "s-feb"}))
	if jun.Action != chanda.ActionCreated || feb.Action != chanda.ActionHistory {
		t.Fatalf("jun %+v feb %+v", jun.WriteResult, feb.WriteResult)
	}
	if vf := str(t, a, `SELECT valid_from FROM facts WHERE id = ?`, feb.ID); vf != "2023-02-10T00:00:00.000000000Z" {
		t.Fatalf("feb valid_from %s", vf)
	}
	// Without a dated session, the time of the call.
	now := must(a.AssertFact(agent, chanda.AssertFactInput{Subject: "Ana", Predicate: "employer", Value: "Umbrella", SessionID: "no-turns"}))
	if vf := str(t, a, `SELECT valid_from FROM facts WHERE id = ?`, now.ID); vf < "2026" || now.Action != chanda.ActionSuperseded {
		t.Fatalf("undated assert: %s %+v", vf, now.WriteResult)
	}
	verifyOK(t, a)
}

// as_of on the read path for raw events (decision 0135): a date-only as_of
// is the end of that day in UTC; an excerpt dated after it is delivered by
// neither context nor search; the block shows each excerpt's date.
func TestAsOfOnRawEvents(t *testing.T) {
	a, _ := newApp(t, nil)
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "may", Turns: turnsAt("2023-05-20T18:30:00Z",
		"My sister Clara gave me a sourdough starter for my birthday.")}))
	must(a.LogSession(agent, chanda.LogSessionInput{SessionID: "jun", Turns: turnsAt("2023-06-02",
		"The sourdough starter from my sister Clara finally bubbled.")}))
	must(a.Flush(cli, chanda.FlushInput{}))
	const q = "sourdough starter sister Clara"
	delivered := func(asOf string) (ctxIDs, searchIDs []string, text string) {
		c := must(a.Context(agent, chanda.ContextInput{Query: q, AsOf: asOf}))
		s := must(a.Search(agent, chanda.SearchInput{Query: q, Filters: chanda.SearchFilters{AsOf: asOf}}))
		for _, it := range c.Items {
			ctxIDs = append(ctxIDs, it.ID)
		}
		for _, it := range s.Items {
			searchIDs = append(searchIDs, it.ID)
		}
		return ctxIDs, searchIDs, c.Text
	}
	may := str(t, a, `SELECT id FROM events WHERE session_id = 'may'`)
	jun := str(t, a, `SELECT id FROM events WHERE session_id = 'jun'`)

	c, s, text := delivered("")
	if !slices.Contains(c, may) || !slices.Contains(c, jun) || !slices.Contains(s, may) || !slices.Contains(s, jun) {
		t.Fatalf("no as_of: context %v search %v", c, s)
	}
	if !strings.Contains(text, "| date: 2023-05-20 18:30 |") || !strings.Contains(text, "| date: 2023-06-02 |") {
		t.Fatalf("dates missing from the block:\n%s", text)
	}
	// Date-only as_of on the day of the May turn (18:30): end of that day, so it is in.
	for _, asOf := range []string{"2023-05-20", "2023-06-01T23:59:59Z"} {
		c, s, _ = delivered(asOf)
		if !slices.Contains(c, may) || slices.Contains(c, jun) || !slices.Contains(s, may) || slices.Contains(s, jun) {
			t.Fatalf("as_of %s: context %v search %v", asOf, c, s)
		}
	}
	// An RFC 3339 as_of is exact: before 18:30 that day, the May turn did not exist yet.
	c, s, _ = delivered("2023-05-20T12:00:00Z")
	if len(c) != 0 || slices.Contains(s, may) || slices.Contains(s, jun) {
		t.Fatalf("as_of before both: context %v search %v", c, s)
	}
	// A date-only valid_from holds from 00:00 UTC of its day.
	c, _, _ = delivered("2023-06-02")
	if !slices.Contains(c, jun) {
		t.Fatalf("as_of on the day of a date-only valid_from: %v", c)
	}
	if _, err := a.Context(agent, chanda.ContextInput{Query: q, AsOf: "around june"}); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("bad as_of: %v", err)
	}
}
