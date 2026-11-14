package e2e

import (
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Temporal correctness of raw excerpts (decision 0093): a document chunk
// whose extracted fact was later superseded no longer brings the old value
// into context; the new value and the history stay reachable.
func TestSupersededValueNeverComesBackThroughAnExcerpt(t *testing.T) {
	w := newWorld(t, nil)
	path := w.writeDoc("atlas.md", "# Project Atlas\n\nOwner: Marina Duarte\n\nAtlas moves billing to the new cluster.\n")
	w.ok(w.agent, chanda.ToolIngest, map[string]any{"path": path}, nil)
	w.flush()
	if c := w.context(w.agent, "Project Atlas owner", nil); !strings.Contains(c.Text, "Marina") {
		t.Fatalf("before the change:\n%s", c.Text)
	}
	w.ok(w.admin, chanda.ToolAssertFact, map[string]any{"subject": "Project Atlas", "predicate": "owner", "value": "Bruno Lima"}, nil)
	w.flush()
	for _, q := range []string{"Project Atlas owner", "Who owns Project Atlas?", "Marina Duarte Atlas"} {
		c := w.context(w.agent, q, nil)
		if strings.Contains(c.Text, "Marina") {
			t.Fatalf("context(%q) delivered the superseded value:\n%s", q, c.Text)
		}
	}
	if c := w.context(w.agent, "Project Atlas owner", nil); !strings.Contains(c.Text, "Bruno Lima") {
		t.Fatalf("current value missing:\n%s", c.Text)
	}
	var tl chanda.TimelineOutput
	w.ok(w.agent, chanda.ToolTimeline, map[string]any{"subject": "Project Atlas", "predicate": "owner"}, &tl)
	if len(tl.Entries) != 2 || tl.Entries[0].Value != "Marina Duarte" || tl.Entries[0].Status != "superseded" {
		t.Fatalf("timeline %+v", tl.Entries)
	}
	if s := w.search(w.agent, "Marina Duarte", nil); len(s.Items) == 0 {
		t.Fatal("search still finds the document chunk")
	}
	w.verify()
}

// Decision 0094: the default extractor set reads the metadata header of a
// document ("Key: Value" lines under the title) into facts about the
// document's subject; "extractors: []" turns it off.
func TestDefaultExtractorsFeedTheTimeline(t *testing.T) {
	doc := "# Project Atlas\n\nOwner: Marina Duarte\nStatus: green\n\nAtlas moves billing to the new cluster.\n"
	w := newWorld(t, func(c *config.Config) { c.Write.Extractors = config.Default().Write.Extractors })
	w.ok(w.agent, chanda.ToolIngest, map[string]any{"path": w.writeDoc("atlas.md", doc)}, nil)
	var tl chanda.TimelineOutput
	w.ok(w.agent, chanda.ToolTimeline, map[string]any{"subject": "Project Atlas", "predicate": "owner"}, &tl)
	if tl.SubjectName != "Project Atlas" || len(tl.Entries) != 1 || tl.Entries[0].Value != "Marina Duarte" || tl.Entries[0].Origin.DocURI == "" {
		t.Fatalf("timeline %+v", tl)
	}
	w.verify()

	off := newWorld(t, func(c *config.Config) { c.Write.Extractors = []config.ExtractorConfig{} })
	off.ok(off.agent, chanda.ToolIngest, map[string]any{"path": off.writeDoc("atlas.md", doc)}, nil)
	if env := off.call(off.agent, chanda.ToolTimeline, map[string]any{"subject": "Project Atlas"}, nil); env.OK || env.Error.Code != chanda.CodeNotFound {
		t.Fatalf("extractors off still extracted: %+v", env)
	}
}
