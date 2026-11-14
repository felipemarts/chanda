package eval

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // FTS5-enabled pure-Go SQLite for the BM25 baseline
)

// ---------------------------------------------------------------------------
// B0: no memory.

// NoMemory is B0: it never returns anything.
type NoMemory struct{}

func (NoMemory) Name() string                                              { return "B0" }
func (NoMemory) Ingest(context.Context, Item) error                        { return nil }
func (NoMemory) Retrieve(context.Context, Query, int) ([]Retrieved, error) { return nil, nil }
func (NoMemory) Close() error                                              { return nil }
func (NoMemory) Info() SystemInfo {
	return SystemInfo{Description: "No memory: always returns an empty context.", EmbeddingModel: "n/a"}
}

// ---------------------------------------------------------------------------
// B1: BM25 over the raw history.

// BM25 is B1: an in-memory SQLite FTS5 index (unicode61, diacritics removed)
// over every raw chunk ever ingested, ranked by bm25(). The query is the OR of
// the question's content terms (Portuguese stopwords removed).
type BM25 struct {
	db   *sql.DB
	log  rawLog
	topK int
}

// NewBM25 opens a private in-memory index. topK caps the candidates read
// before the budget fill (BASE.md read step 2 uses 50).
func NewBM25(ctx context.Context, topK, maxChunkChars int) (*BM25, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one connection: ":memory:" is per connection
	db.SetConnMaxLifetime(0)
	if _, err := db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE chunks USING fts5(text, tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		db.Close()
		return nil, fmt.Errorf("bm25: %w", err)
	}
	return &BM25{db: db, log: newRawLog(maxChunkChars), topK: topK}, nil
}

func (b *BM25) Name() string { return "B1" }

func (b *BM25) Info() SystemInfo {
	return SystemInfo{Description: "BM25 (SQLite FTS5, unicode61 remove_diacritics 2) over raw chunks of every item and version.", EmbeddingModel: "n/a"}
}

func (b *BM25) Ingest(ctx context.Context, it Item) error {
	fresh := b.log.add(it)
	if len(fresh) == 0 {
		return nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	base := len(b.log.chunks) - len(fresh)
	for i, c := range fresh {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(rowid, text) VALUES (?, ?)`, base+i, c.Text); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// matchExpr builds an FTS5 OR query of quoted, de-duplicated content terms.
func matchExpr(text string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range contentTerms(text) {
		if !seen[t] {
			seen[t] = true
			parts = append(parts, `"`+t+`"`)
		}
	}
	return strings.Join(parts, " OR ")
}

func (b *BM25) Retrieve(ctx context.Context, q Query, budget int) ([]Retrieved, error) {
	expr := matchExpr(q.Text)
	if expr == "" {
		return nil, nil
	}
	rows, err := b.db.QueryContext(ctx,
		`SELECT rowid, -bm25(chunks) FROM chunks WHERE chunks MATCH ? ORDER BY bm25(chunks), rowid LIMIT ?`, expr, b.topK)
	if err != nil {
		return nil, err
	}
	var ranked []Chunk
	var scores []float64
	for rows.Next() {
		var id int
		var s float64
		if err := rows.Scan(&id, &s); err != nil {
			rows.Close()
			return nil, err
		}
		ranked = append(ranked, b.log.chunks[id])
		scores = append(scores, s)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return fill(ranked, scores, budget), nil
}

func (b *BM25) SizeBytes(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := b.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := b.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size); err != nil {
		return 0, err
	}
	return pages * size, nil
}

func (b *BM25) Close() error { return b.db.Close() }
