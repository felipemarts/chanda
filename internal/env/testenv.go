package env

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// TestEpoch is the start instant of every test clock.
var TestEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// NewTest returns an Env over a fresh on-disk database in t.TempDir(), with a
// Fake clock starting at TestEpoch and advancing 1ms per call. Closed on cleanup.
func NewTest(t testing.TB) (*Env, *clock.Fake) {
	t.Helper()
	return NewTestAt(t, filepath.Join(t.TempDir(), "chanda.db"), config.Default())
}

// NewTestAt is NewTest with an explicit path and config.
func NewTestAt(t testing.TB, path string, cfg *config.Config) (*Env, *clock.Fake) {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	fc := clock.NewFake(TestEpoch, time.Millisecond)
	cfg.DBPath = path
	return New(cfg, st, fc), fc
}
