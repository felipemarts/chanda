// Package determinism holds the byte-for-byte determinism test (BASE.md
// "Conventions"): the same sequence of operations, run twice with a fixed
// Clock, must produce identical database files. Every feature that mutates
// the database must add its operations to Scenario.
package determinism

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// Scenario is the shared operation sequence. Append new feature operations here.
func Scenario(t *testing.T, e *env.Env) {
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if _, err := e.Audit.Append(ctx, e.Store.DB(), audit.Entry{
			Namespace: "ns", Actor: "det", Action: audit.ActWrite, Target: e.IDs.New("evt"), Details: map[string]int{"i": i},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func run(t *testing.T, dir string) []byte {
	path := filepath.Join(dir, "chanda.db")
	e, _ := env.NewTestAt(t, path, config.Default())
	Scenario(t, e)
	if err := e.Store.Checkpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDatabasesAreByteIdentical(t *testing.T) {
	a := run(t, t.TempDir())
	b := run(t, t.TempDir())
	if !bytes.Equal(a, b) {
		t.Fatalf("databases differ: %x vs %x", sha256.Sum256(a), sha256.Sum256(b))
	}
}
