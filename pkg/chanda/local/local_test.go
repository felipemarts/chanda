package local_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/pkg/chanda"
	"github.com/felipemarts/chanda/pkg/chanda/local"
)

// BASE.md: the same core is exposed through an importable Go package.
func TestOpenGivesAWorkingAPI(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "chanda.yaml")
	if err := os.WriteFile(cfg, []byte("db_path: "+filepath.Join(dir, "c.db")+"\nnamespace: team\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, closeFn, err := local.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := chanda.WithActor(context.Background(), chanda.Actor{Name: "go-test"})
	r, err := api.Remember(ctx, chanda.RememberInput{Content: "The cafeteria closes at three on Fridays."})
	if err != nil || r.Action != chanda.ActionCreated {
		t.Fatalf("remember: %+v %v", r, err)
	}
	if _, err := api.Flush(ctx, chanda.FlushInput{}); err != nil {
		t.Fatal(err)
	}
	c, err := api.Context(ctx, chanda.ContextInput{Query: "when does the cafeteria close"})
	if err != nil || len(c.Items) != 1 || c.Items[0].ID != r.ID {
		t.Fatalf("context: %+v %v", c, err)
	}
	st, err := api.Stats(ctx, chanda.StatsInput{})
	if err != nil || st.Namespace != "team" || st.Counts.Events != 1 || st.Counts.Embeddings != 1 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(cfg, []byte("nope: 1\n"), 0o600)
	if _, _, err := local.Open(context.Background(), cfg); chanda.CodeOf(err) != chanda.CodeInvalidArgument {
		t.Fatalf("bad config: %v", err)
	}
}
