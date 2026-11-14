package write

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/env"
)

// "Chunks that disappeared receive retired_at, and the facts derived from them
// become stale_source", at folder level: a re-ingested folder retires the
// documents that left it, only within the same source and root.
func TestIngestPath_RetiresDocumentsGoneFromFolder(t *testing.T) {
	w, e := newWriter(t)
	root, other := t.TempDir(), t.TempDir()
	writeFiles(t, root, map[string]string{
		"a.md":     "# A\n\nalpha text stays",
		"b.md":     "# B\n\nbeta text\n\nsecond beta paragraph",
		"sub/c.md": "gamma text in a subfolder",
	})
	writeFiles(t, other, map[string]string{"b.md": "# B\n\nanother source's beta"})
	if _, err := w.IngestPath(ctx, "ns", root, "kb", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.IngestPath(ctx, "ns", other, "kb-other", "tester"); err != nil {
		t.Fatal(err)
	}
	evq := `SELECT e.id FROM events e JOIN documents d ON d.id = e.doc_id WHERE d.uri = ? ORDER BY e.ordinal, e.id`
	bEvents, aEvents, otherEvents := queryCol(t, e, evq, "kb/b.md"), queryCol(t, e, evq, "kb/a.md"), queryCol(t, e, evq, "kb-other/b.md")
	if len(bEvents) != 2 || len(aEvents) != 1 || len(otherEvents) != 1 {
		t.Fatalf("events b %v a %v other %v", bEvents, aEvents, otherEvents)
	}
	seedFact(t, e, "fct_only_b", "active", bEvents[0])
	seedFact(t, e, "fct_b_and_a", "active", bEvents[1], aEvents[0])
	seedFact(t, e, "fct_other", "active", otherEvents[0])

	// b.md and sub/c.md leave the folder.
	for _, f := range []string{"b.md", "sub/c.md"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kb/a.md:unchanged", "kb/b.md:retired", "kb/sub/c.md:retired"}
	if got := uris(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if r.Removed != 2 || r.Unchanged != 1 {
		t.Fatalf("counts %+v", r)
	}
	b := r.Documents[1]
	if !reflect.DeepEqual(b.Retired, bEvents) || !reflect.DeepEqual(b.StaleFacts, []string{"fct_only_b"}) ||
		b.Reason != ReasonMissingFromDir || b.Version != 2 || b.AuditID == 0 {
		t.Fatalf("b result %+v", b)
	}
	if n := count(t, e, `SELECT count(*) FROM events e JOIN documents d ON d.id = e.doc_id
		WHERE d.uri IN ('kb/b.md','kb/sub/c.md') AND e.retired_at IS NULL`); n != 0 {
		t.Fatalf("%d live chunks left in removed documents", n)
	}
	if n := count(t, e, `SELECT count(*) FROM events WHERE retired_at IS NOT NULL`); n != 3 {
		t.Fatalf("retired %d events, want 3", n)
	}
	for id, st := range map[string]string{"fct_only_b": "stale_source", "fct_b_and_a": "active", "fct_other": "active"} {
		if got := factStatus(t, e, id); got != st {
			t.Errorf("%s: %s, want %s", id, got, st)
		}
	}
	for action, want := range map[string]int{"document_retire": 2, "retire": 3, "stale_source": 1} {
		if n := count(t, e, `SELECT count(*) FROM audit_log WHERE action = ?`, action); n != want {
			t.Errorf("%s audits %d, want %d", action, n, want)
		}
	}

	// A second run retires nothing more; a single-file ingest never retires.
	r, err = w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil || r.Removed != 0 {
		t.Fatalf("second run %+v %v", r, err)
	}
	if r, err = w.IngestPath(ctx, "ns", filepath.Join(root, "a.md"), "kb", "tester"); err != nil || r.Removed != 0 {
		t.Fatalf("single file %+v %v", r, err)
	}

	// The file coming back is a new version with fresh, live chunks.
	writeFiles(t, root, map[string]string{"b.md": "# B\n\nbeta text\n\nsecond beta paragraph"})
	r, err = w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil {
		t.Fatal(err)
	}
	back := r.Documents[1]
	if back.URI != "kb/b.md" || back.Action != ActionUpdated || len(back.Created) != 2 || len(back.Kept) != 0 || back.Version != 3 {
		t.Fatalf("returned b %+v", back)
	}
}

func queryCol(t *testing.T, e *env.Env, query string, args ...any) []string {
	t.Helper()
	rows, err := e.Store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}
