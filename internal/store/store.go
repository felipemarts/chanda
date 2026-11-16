// Package store owns the SQLite file: opening, pragmas, migrations and
// transactions. Domain queries live in the packages that own each table.
//
// Concurrency model: a single connection (SetMaxOpenConns(1)). SQLite has one
// writer anyway, and a single connection keeps reads and writes serialized
// and deterministic. Consequence for callers: never issue a second query
// while a *sql.Rows is still open — collect rows into a slice, Close, then
// query again; inside a transaction, use only the *sql.Tx.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/felipemarts/chanda/internal/core"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies all
// pending migrations. path ":memory:" gives a private in-memory database.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		// Escape the path: an unescaped '#' or '?' would be read as a URI
		// fragment or query and silently point at a different file.
		dsn = "file:" + (&url.URL{Path: path}).EscapedPath()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL", // write returns only once durable (write step 5)
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA secure_delete=ON", // forget: overwrite deleted content
		"PRAGMA auto_vacuum=FULL",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	s := &Store{db: db, path: path}
	if err := s.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }
func (s *Store) Close() error { return s.db.Close() }

// Tx runs fn in a transaction, committing if fn returns nil.
func (s *Store) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Checkpoint folds the WAL into the main file and truncates the WAL.
func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(name, "_")
		if !ok || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("bad migration name %q", name)
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("bad migration number %q", name)
		}
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{v, name, string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i := 1; i < len(ms); i++ {
		if ms[i].version == ms[i-1].version {
			return nil, fmt.Errorf("duplicate migration number %d", ms[i].version)
		}
	}
	return ms, nil
}

// Migrate applies pending migrations in order, each in its own transaction.
// Forward-only: there are no down migrations.
func (s *Store) Migrate(ctx context.Context) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	var cur int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&cur); err != nil {
		return err
	}
	for _, m := range ms {
		if m.version <= cur {
			continue
		}
		err := s.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("migration %s: %w", m.name, err)
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Version returns the applied schema version.
func (s *Store) Version(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// IsImmutableViolation reports whether err came from an append-only trigger.
func IsImmutableViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "immutable:")
}

// AsCoreError maps trigger violations to core.CodeImmutable.
func AsCoreError(err error) error {
	if IsImmutableViolation(err) {
		return core.Wrap(core.CodeImmutable, err, "raw log and audit log are append-only")
	}
	return err
}

// Queryer is satisfied by *sql.DB and *sql.Tx so helpers work in and out of transactions.
type Queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
