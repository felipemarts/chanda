package write

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Decision 0090: the channel, not the call, bounds the zone.
func TestZoneCeilingPerChannel(t *testing.T) {
	cases := []struct {
		kind core.SourceKind
		max  core.Zone
		want core.Zone
	}{
		{core.SourceUserStatement, "", core.ZoneUser},
		{core.SourceUserStatement, core.ZoneUser, core.ZoneUser},
		{core.SourceUserStatement, core.ZoneInternal, core.ZoneInternal},
		{core.SourceUserStatement, core.ZoneExternal, core.ZoneExternal},
		{core.SourceDocument, core.ZoneUser, core.ZoneInternal},
		{core.SourceSession, core.ZoneExternal, core.ZoneExternal},
		{core.SourceTool, core.ZoneUser, core.ZoneExternal},
		{core.SourceWeb, core.ZoneUser, core.ZoneExternal},
		{core.SourceUserStatement, "bogus", core.ZoneExternal},
	}
	for _, c := range cases {
		if got := ZoneFor(c.kind, c.max); got != c.want {
			t.Errorf("ZoneFor(%s, %q) = %s, want %s", c.kind, c.max, got, c.want)
		}
	}

	w, e := newWriter(t)
	in := RememberInput{Namespace: "ns", Content: "my name is Ana", Source: "chat", SourceKind: core.SourceUserStatement,
		Actor: "agent", MaxZone: core.ZoneInternal}
	r, err := w.Remember(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if ev := load(t, e, r.ID); ev.TrustZone != core.ZoneInternal {
		t.Fatalf("capped statement zone %s", ev.TrustZone)
	}
	// The same text from a channel with the user ceiling is a new user-zone
	// event, not a duplicate of the capped one.
	in.MaxZone, in.Actor = core.ZoneUser, "cli"
	r2, err := w.Remember(ctx, in)
	if err != nil || r2.Action != ActionCreated || r2.ID == r.ID {
		t.Fatalf("uncapped copy: %+v %v", r2, err)
	}
	if ev := load(t, e, r2.ID); ev.TrustZone != core.ZoneUser {
		t.Fatalf("uncapped zone %s", ev.TrustZone)
	}

	// Sessions and documents are capped too.
	sr, err := w.LogSessionWith(ctx, SessionInput{Namespace: "ns", SessionID: "s1", Actor: "agent", MaxZone: core.ZoneExternal,
		Turns: []Turn{{Role: RoleUser, Content: "hello there"}}})
	if err != nil || load(t, e, sr.Turns[0].ID).TrustZone != core.ZoneExternal {
		t.Fatalf("session cap: %+v %v", sr, err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nsome document text"), 0o644)
	pr, err := w.IngestPathWith(ctx, PathInput{Namespace: "ns", Path: dir, Actor: "agent", MaxZone: core.ZoneExternal})
	if err != nil || len(pr.Documents) != 1 {
		t.Fatalf("ingest: %+v %v", pr, err)
	}
	if ev := load(t, e, pr.Documents[0].Created[0]); ev.TrustZone != core.ZoneExternal {
		t.Fatalf("document cap zone %s", ev.TrustZone)
	}
	pr, err = w.IngestPathWith(ctx, PathInput{Namespace: "ns2", Path: dir, Actor: "agent", SourceKind: core.SourceWeb})
	if err != nil || load(t, e, pr.Documents[0].Created[0]).TrustZone != core.ZoneExternal {
		t.Fatalf("web kind: %+v %v", pr, err)
	}
	verifyAudit(t, e)
}

func TestRememberEventKindAndOnEvent(t *testing.T) {
	w, e := newWriter(t, EventHookFunc(func(ctx context.Context, tx *sql.Tx, ev Event) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES ('hook', ?)`, ev.ID)
		return err
	}))
	var calls []bool
	in := RememberInput{Namespace: "ns", Content: "Ana job_title: CTO", SourceKind: core.SourceSession, Actor: "agent",
		EventKind: core.EventFact,
		OnEvent: func(ctx context.Context, tx *sql.Tx, ev Event, created bool) error {
			calls = append(calls, created)
			var hooked int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM meta WHERE key = 'hook'`).Scan(&hooked); err != nil {
				return err
			}
			if created && hooked != 0 {
				t.Error("OnEvent must run before the hooks")
			}
			if ev.Kind != core.EventFact || ev.ID == "" {
				t.Errorf("event %+v", ev)
			}
			return nil
		}}
	r, err := w.Remember(ctx, in)
	if err != nil || load(t, e, r.ID).Kind != core.EventFact {
		t.Fatalf("remember: %+v %v", r, err)
	}
	r2, err := w.Remember(ctx, in)
	if err != nil || r2.Action != ActionDuplicate || r2.ID != r.ID {
		t.Fatalf("duplicate: %+v %v", r2, err)
	}
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("OnEvent calls %v", calls)
	}
	in.EventKind = core.EventChunk
	if _, err := w.Remember(ctx, in); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("chunk kind accepted: %v", err)
	}
	// An OnEvent error rolls the write back.
	in.EventKind, in.Content = "", "another statement"
	in.OnEvent = func(context.Context, *sql.Tx, Event, bool) error { return core.Errf(core.CodeIntegrity, "no") }
	if _, err := w.Remember(ctx, in); core.CodeOf(err) != core.CodeIntegrity {
		t.Fatalf("error not propagated: %v", err)
	}
	if n := count(t, e, `SELECT count(*) FROM events`); n != 1 {
		t.Fatalf("rolled back write left %d events", n)
	}
}
