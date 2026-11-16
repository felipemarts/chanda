package write

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func uris(r PathResult) []string {
	var out []string
	for _, d := range r.Documents {
		out = append(out, d.URI+":"+d.Action)
	}
	return out
}

// "Ingest a folder": deterministic walk, default extensions, one document per
// file, uri = source uri + relative path, hidden entries skipped.
func TestIngestPath_Folder(t *testing.T) {
	w, e := newWriter(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"b.md":           "# B\n\nbee text",
		"a.txt":          "plain a text",
		"sub/c.markdown": "c paragraph text",
		"sub/D.MD":       "upper case extension",
		"empty.md":       "  \n ",
		".hidden/x.md":   "hidden folder",
		".dot.md":        "hidden file",
		"image.png":      "not text",
	})
	r, err := w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kb/a.txt:created", "kb/b.md:created", "kb/empty.md:rejected", "kb/sub/D.MD:created", "kb/sub/c.markdown:created"}
	if got := uris(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if r.Created != 4 || r.Rejected != 1 || r.Documents[2].Reason != ReasonEmpty {
		t.Fatalf("counts %+v", r)
	}
	if n := count(t, e, `SELECT count(*) FROM sources WHERE kind = 'document' AND uri = 'kb' AND trust_zone = 'internal'`); n != 1 {
		t.Fatalf("sources %d", n)
	}

	// Re-ingest: unchanged; then change one file: only it gets a new version.
	r2, err := w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil || r2.Unchanged != 4 || r2.Created+r2.Updated != 0 {
		t.Fatalf("second run %+v %v", r2, err)
	}
	writeFiles(t, root, map[string]string{"b.md": "# B\n\nbee text changed"})
	r3, err := w.IngestPath(ctx, "ns", root, "kb", "tester")
	if err != nil || r3.Updated != 1 || r3.Unchanged != 3 || r3.Documents[1].URI != "kb/b.md" || len(r3.Documents[1].Retired) != 1 {
		t.Fatalf("third run %+v %v", r3, err)
	}
}

func TestIngestPath_SingleFileAndDefaults(t *testing.T) {
	w, _ := newWriter(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"notes.md": "single file text"})
	r, err := w.IngestPath(ctx, "ns", filepath.Join(root, "notes.md"), "file-src", "tester")
	if err != nil || len(r.Documents) != 1 || r.Documents[0].URI != "file-src/notes.md" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = w.IngestPath(ctx, "ns", root, "", "tester")
	if err != nil || r.SourceURI != filepath.ToSlash(filepath.Clean(root)) || r.Documents[0].URI != r.SourceURI+"/notes.md" {
		t.Fatalf("default source uri: %+v %v", r, err)
	}
}

func TestIngestPath_Errors(t *testing.T) {
	w, _ := newWriter(t)
	if _, err := w.IngestPath(ctx, "ns", filepath.Join(t.TempDir(), "missing"), "", "a"); core.CodeOf(err) != core.CodeNotFound {
		t.Fatalf("missing path: %v", err)
	}
	if _, err := w.IngestPath(ctx, "ns", t.TempDir(), "", ""); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("missing actor: %v", err)
	}
}
