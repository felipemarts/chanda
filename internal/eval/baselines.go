package eval

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // FTS5-enabled pure-Go SQLite for the BM25 baseline

	"github.com/felipemarts/chanda-mcp/internal/core"
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

// ---------------------------------------------------------------------------
// B2: plain RAG.

// PlainRAG is B2: the same raw chunks as B1, embedded with the injected
// Embedder, ranked by exact cosine similarity (ties by ingestion order).
type PlainRAG struct {
	emb  core.Embedder
	log  rawLog
	vecs [][]float32
	topK int
}

// NewPlainRAG returns B2 over emb. topK caps the candidates before the
// budget fill (BASE.md read step 3 uses 50).
func NewPlainRAG(emb core.Embedder, topK, maxChunkChars int) *PlainRAG {
	return &PlainRAG{emb: emb, log: newRawLog(maxChunkChars), topK: topK}
}

func (p *PlainRAG) Name() string { return "B2" }

func (p *PlainRAG) Info() SystemInfo {
	return SystemInfo{Description: "Plain RAG: raw chunks of every item and version, exact cosine top-k.", EmbeddingModel: p.emb.ModelID()}
}

func (p *PlainRAG) Ingest(ctx context.Context, it Item) error {
	fresh := p.log.add(it)
	if len(fresh) == 0 {
		return nil
	}
	texts := make([]string, len(fresh))
	for i, c := range fresh {
		texts[i] = c.Text
	}
	vs, err := p.emb.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(vs) != len(texts) {
		return core.Errf(core.CodeInternal, "embedder returned %d vectors for %d texts", len(vs), len(texts))
	}
	p.vecs = append(p.vecs, vs...)
	return nil
}

func (p *PlainRAG) Retrieve(ctx context.Context, q Query, budget int) ([]Retrieved, error) {
	var qv [][]float32
	var err error
	if re, ok := p.emb.(core.RoleEmbedder); ok {
		qv, err = re.EmbedQuery(ctx, []string{q.Text})
	} else {
		qv, err = p.emb.Embed(ctx, []string{q.Text})
	}
	if err != nil {
		return nil, err
	}
	idx := make([]int, len(p.vecs))
	scores := make([]float64, len(p.vecs))
	for i, v := range p.vecs {
		idx[i], scores[i] = i, dot(qv[0], v)
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	idx = idx[:min(p.topK, len(idx))]
	ranked := make([]Chunk, len(idx))
	rs := make([]float64, len(idx))
	for i, j := range idx {
		ranked[i], rs[i] = p.log.chunks[j], scores[j]
	}
	return fill(ranked, rs, budget), nil
}

func (p *PlainRAG) SizeBytes(context.Context) (int64, error) {
	var n int64
	for i, c := range p.log.chunks {
		n += int64(len(c.Text)) + int64(4*len(p.vecs[i]))
	}
	return n, nil
}

func (p *PlainRAG) Close() error { return nil }

// ---------------------------------------------------------------------------
// B3: full context.

// FullContext is B3: the whole raw history in ingestion order when it fits
// in the budget. When it does not fit, it degrades to the most recent chunks
// that fit (recency truncation) and counts the query as truncated.
type FullContext struct {
	log       rawLog
	queries   int
	truncated int
}

// NewFullContext returns B3.
func NewFullContext(maxChunkChars int) *FullContext {
	return &FullContext{log: newRawLog(maxChunkChars)}
}

func (f *FullContext) Name() string { return "B3" }

func (f *FullContext) Info() SystemInfo {
	total := 0
	for _, c := range f.log.chunks {
		total += CountTokens(c.Text)
	}
	return SystemInfo{EmbeddingModel: "n/a", Description: fmt.Sprintf(
		"Full context: all %d raw chunks (%d tokens) when they fit the budget, else the most recent chunks that fit; truncated in %d of %d queries.",
		len(f.log.chunks), total, f.truncated, f.queries)}
}

func (f *FullContext) Ingest(_ context.Context, it Item) error {
	f.log.add(it)
	return nil
}

func (f *FullContext) Retrieve(_ context.Context, _ Query, budget int) ([]Retrieved, error) {
	f.queries++
	var keep []Chunk
	used := 0
	for i := len(f.log.chunks) - 1; i >= 0; i-- {
		c := f.log.chunks[i]
		n := CountTokens(c.Text)
		if used+n > budget {
			continue
		}
		used += n
		keep = append(keep, c)
	}
	if len(keep) < len(f.log.chunks) {
		f.truncated++
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return fill(keep, nil, budget), nil
}

func (f *FullContext) Close() error { return nil }
