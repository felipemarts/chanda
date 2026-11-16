package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// Default extractor fallback (decision 0094, 0106): a chunk with no heading
// and no title key takes the document's file name, without extension, as
// its subject.
func TestHeaderExtractorFallsBackToFileName(t *testing.T) {
	a, _ := newApp(t, nil) // default extractors: header
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project-atlas.md"), []byte("Owner: Marina Duarte\nStatus: active\n\nThe atlas project maps every office."), 0o644); err != nil {
		t.Fatal(err)
	}
	must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
	tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: "project-atlas", Predicate: "owner"}))
	if len(tl.Entries) != 1 || tl.Entries[0].Value != "Marina Duarte" || tl.SubjectName != "project-atlas" {
		t.Fatalf("timeline %+v", tl)
	}
	verifyOK(t, a)
}
