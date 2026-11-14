package write

import (
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// BASE.md "valid_from uses, in this order: the date given in the call, ...":
// a turn's ts is the date given in the call, so it becomes the event's
// valid_from; recorded_at stays the write time (decision 0135).
func TestLogSession_ValidFromIsTheDateGivenInTheCall(t *testing.T) {
	w, e := newWriter(t)
	turns := []Turn{
		{Role: RoleUser, Content: "We moved the launch to Friday.", TS: "2023-05-20T14:03:00Z"},
		{Role: RoleAssistant, Content: "Noted, the launch is on Friday.", TS: "2023-05-20"},
		{Role: RoleTool, ToolName: "cal", Content: "Calendar entry created for Friday.", TS: "2023-05-20T16:00:00+02:00"},
		{Role: RoleUser, Content: "No timestamp on this turn."},
	}
	r, err := w.LogSession(ctx, "ns", "s", turns, "agent")
	if err != nil || r.Created != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	now := load(t, e, r.Turns[3].ID).RecordedAt // the write time, one per call
	want := []string{"2023-05-20T14:03:00.000000000Z", "2023-05-20T00:00:00.000000000Z", "2023-05-20T14:00:00.000000000Z", ""}
	for i, vf := range want {
		ev := load(t, e, r.Turns[i].ID)
		if ev.ValidFrom != vf || ev.RecordedAt != now || ev.RecordedAt < "2026" {
			t.Fatalf("turn %d: valid_from %q recorded_at %q, want %q and %q", i, ev.ValidFrom, ev.RecordedAt, vf, now)
		}
	}
	// A re-log with another ts is still a duplicate: the raw log keeps the first.
	turns[0].TS = "2024-01-01"
	r2, err := w.LogSession(ctx, "ns", "s", turns, "agent")
	if err != nil || r2.Duplicates != 4 || load(t, e, r2.Turns[0].ID).ValidFrom != want[0] {
		t.Fatalf("%+v %v", r2, err)
	}
}

// An unparsable ts is refused with invalid_argument, never silently dropped,
// and nothing of the call is written.
func TestLogSession_InvalidTSRejectsTheCall(t *testing.T) {
	w, e := newWriter(t)
	turns := []Turn{
		{Role: RoleUser, Content: "A perfectly fine first turn.", TS: "2023-05-20"},
		{Role: RoleAssistant, Content: "A second turn with a bad date.", TS: "yesterday"},
	}
	_, err := w.LogSession(ctx, "ns", "s", turns, "agent")
	if core.CodeOf(err) != core.CodeInvalidArgument || !strings.Contains(err.Error(), "turn 1") {
		t.Fatalf("got %v", err)
	}
	if n := count(t, e, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("events written: %d", n)
	}
}

// remember's valid_from is the date given in the call; without one the event
// has no valid_from (the read path falls back to recorded_at).
func TestRemember_ValidFromIsTheDateGivenInTheCall(t *testing.T) {
	w, e := newWriter(t)
	tests := []struct{ in, want string }{
		{"2023-05-20", "2023-05-20T00:00:00.000000000Z"},
		{"2023-05-20T10:00:00-03:00", "2023-05-20T13:00:00.000000000Z"},
		{"", ""},
	}
	for i, tc := range tests {
		r, err := w.Remember(ctx, RememberInput{Content: "Statement number " + string(rune('A'+i)) + " here.",
			SourceKind: core.SourceSession, Actor: "a", ValidFrom: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if got := load(t, e, r.ID).ValidFrom; got != tc.want {
			t.Fatalf("%q: valid_from %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := w.Remember(ctx, RememberInput{Content: "Bad date here.", SourceKind: core.SourceSession, Actor: "a",
		ValidFrom: "soon"}); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("got %v", err)
	}
}

// BASE.md "valid_from uses ... the document date": a new chunk carries the
// document date as its valid_from; a chunk kept from an earlier version keeps
// the date of the version it first appeared in (the raw log is immutable).
func TestIngestDocument_ChunkValidFromIsTheDocumentDate(t *testing.T) {
	w, e := newWriter(t)
	r1, err := w.IngestDocument(ctx, IngestInput{URI: "d.md", Content: docV1, Actor: "a"})
	if err != nil || len(r1.Created) == 0 {
		t.Fatalf("%+v %v", r1, err)
	}
	for _, id := range r1.Created {
		if vf := load(t, e, id).ValidFrom; vf != "2025-02-01T00:00:00.000000000Z" {
			t.Fatalf("chunk %s valid_from %q", id, vf)
		}
	}
	v2 := strings.Replace(strings.Replace(docV1, "2025-02-01", "2025-03-01", 1), "Beta price is 10.", "Beta price is 12.", 1)
	r2, err := w.IngestDocument(ctx, IngestInput{URI: "d.md", Content: v2, Actor: "a"})
	if err != nil || len(r2.Created) != 1 || len(r2.Kept) == 0 {
		t.Fatalf("%+v %v", r2, err)
	}
	if vf := load(t, e, r2.Created[0]).ValidFrom; vf != "2025-03-01T00:00:00.000000000Z" {
		t.Fatalf("new chunk valid_from %q", vf)
	}
	if vf := load(t, e, r2.Kept[0]).ValidFrom; vf != "2025-02-01T00:00:00.000000000Z" {
		t.Fatalf("kept chunk valid_from %q", vf)
	}
	// Without a document date, chunks have no valid_from.
	r3, err := w.IngestDocument(ctx, IngestInput{URI: "n.md", Content: "# Notes\n\nNo date anywhere in this file.", Actor: "a"})
	if err != nil || load(t, e, r3.Created[0]).ValidFrom != "" {
		t.Fatalf("%+v %v", r3, err)
	}
}
