package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/clock"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

var bg = context.Background()

// Callers as the transports set them.
var (
	cli   = chanda.WithActor(bg, chanda.Actor{Name: "cli", Human: true, MaxZone: chanda.ZoneUser})
	agent = chanda.WithActor(bg, chanda.Actor{Name: "agent", Remote: true, MaxZone: chanda.ZoneInternal})
	tool  = chanda.WithActor(bg, chanda.Actor{Name: "reader-bot", Remote: true, MaxZone: chanda.ZoneExternal})
)

func newApp(t *testing.T, mut func(*config.Config)) (*App, *clock.Fake) {
	t.Helper()
	cfg := config.Default()
	cfg.Namespace = "ns"
	if mut != nil {
		mut(cfg)
	}
	e, fc := env.NewTestAt(t, filepath.Join(t.TempDir(), "chanda.db"), cfg)
	a, err := New(e)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, fc
}

// must returns v or panics with err (a panic fails the test with a trace).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func count(t *testing.T, a *App, q string, args ...any) int {
	t.Helper()
	var n int
	if err := a.e.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func str(t *testing.T, a *App, q string, args ...any) string {
	t.Helper()
	var s string
	if err := a.e.Store.DB().QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s
}

func verifyOK(t *testing.T, a *App) {
	t.Helper()
	v := must(a.Verify(cli, chanda.VerifyInput{}))
	if !v.OK {
		t.Fatalf("verify: %+v notices %v", v, v.Notices)
	}
}
