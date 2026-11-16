package facts

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

// TestSameInputSameOutput runs the facts operations twice with a fixed
// clock and compares the database files byte for byte (principle 11). The
// integration should add the same operations to internal/determinism.
func TestSameInputSameOutput(t *testing.T) {
	run := func(dir string) []byte {
		path := filepath.Join(dir, "chanda.db")
		cfg := config.Default()
		cfg.Write.Extractors = []config.ExtractorConfig{{Kind: KindHeader}, {Kind: KindKVTable}}
		e, _ := env.NewTestAt(t, path, cfg)
		f := &fixture{t: t, e: e, s: New(e), ctx: t.Context()}
		f.exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_user',?,'user_statement','u','user','t'),
			('src_external',?,'web','w','external','t'),('src_internal',?,'document','d','internal','t')`, testNS, testNS, testNS)
		for _, st := range []step{
			{value: "Engineer", validFrom: "2026-01-01"}, {value: "Manager", validFrom: "2026-05-01"},
			{value: "Intern", validFrom: "2025-01-01"}, {zone: core.ZoneExternal, value: "CEO"},
			{value: "manager"}, {predicate: "skill", value: "Go"}, {predicate: "skill", value: "SQL"},
		} {
			f.assert(st)
		}
		ev := Event{ID: f.event(core.ZoneInternal), Namespace: testNS, Content: "# Bob\nOwner: Ana\n\n| k | v |\n|---|---|\n| phone | 1 |"}
		f.tx(func(tx *sql.Tx) error {
			if _, err := f.s.AfterEvent(f.ctx, tx, ev); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE events SET retired_at='t' WHERE id=?`, ev.ID); err != nil {
				return err
			}
			_, err := f.s.MarkStaleFromRetired(f.ctx, tx, testNS, []string{ev.ID})
			return err
		})
		if err := e.Store.Checkpoint(f.ctx); err != nil {
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
	a, b := run(t.TempDir()), run(t.TempDir())
	if !bytes.Equal(a, b) {
		t.Fatalf("databases differ: %x vs %x", sha256.Sum256(a), sha256.Sum256(b))
	}
}
