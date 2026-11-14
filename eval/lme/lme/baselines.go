package lme

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// BM25: SQLite FTS5 with Chanda's tokenizer ('unicode61 remove_diacritics 2',
// internal/store/migrations/0001_init.sql) and Chanda's MATCH construction
// (QueryTerms + MatchExpr), ranked by bm25(), ties by insertion order.

// BM25 units.
const (
	UnitSession = "session" // one document per haystack session: every turn's content, both roles
	UnitTurn    = "turn"    // one document per turn; sessions ranked by their best turn
)

// BM25Result is one BM25 retrieval.
type BM25Result struct {
	Ranked   []int // haystack session indexes, best first (only sessions that match a term)
	IndexMS  float64
	SearchMS float64
}

// RunBM25 indexes q's haystack in a private in-memory FTS5 table and ranks
// sessions for q.Question.
func RunBM25(ctx context.Context, q *Question, unit string) (*BM25Result, error) {
	if unit != UnitSession && unit != UnitTurn {
		return nil, fmt.Errorf("bm25 unit %q: want session or turn", unit)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	start := time.Now()
	if _, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE docs USING fts5(text, tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		return nil, fmt.Errorf("bm25: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var docSession []int
	insert := func(sess int, text string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO docs(rowid, text) VALUES (?, ?)`, len(docSession), text)
		docSession = append(docSession, sess)
		return err
	}
	for i, s := range q.HaystackSessions {
		if unit == UnitSession {
			parts := make([]string, len(s))
			for j, t := range s {
				parts[j] = t.Content
			}
			err = insert(i, strings.Join(parts, "\n"))
		} else {
			for _, t := range s {
				if err = insert(i, t.Content); err != nil {
					break
				}
			}
		}
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	res := &BM25Result{IndexMS: ms(time.Since(start))}
	start = time.Now()
	expr := MatchExpr(QueryTerms(q.Question, MaxQueryTerms))
	if expr != "" {
		rows, err := db.QueryContext(ctx, `SELECT rowid FROM docs WHERE docs MATCH ? ORDER BY bm25(docs), rowid`, expr)
		if err != nil {
			return nil, err
		}
		seen := map[int]bool{}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if s := docSession[id]; !seen[s] {
				seen[s] = true
				res.Ranked = append(res.Ranked, s)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	res.SearchMS = ms(time.Since(start))
	return res, nil
}

// ---------------------------------------------------------------------------
// Plain RAG: raw session text in chunks (ChunkSession), embedded as passages,
// the question embedded as a query, exact cosine (dot product of
// L2-normalized vectors) over every chunk, ties by chunk order.

// RAGResult is one plain-RAG retrieval.
type RAGResult struct {
	Chunks    []Chunk
	Order     []int     // chunk indexes, best first (all chunks)
	Scores    []float64 // per chunk
	Ranked    []int     // sessions by first appearance in Order
	Selected  []int     // chunk indexes in the budgeted context
	Context   string    // the context text given to the reader
	Tokens    int
	InBudget  []int // sessions with a chunk in the context
	EmbedMS   float64
	SearchMS  float64 // query embedding + scan + sort + budget fill
	NChunks   int
	TopKLimit int
}

// RAGTopK caps the candidates considered for the budget fill (Chanda's
// read.vector_top_k is 50 too).
const RAGTopK = 50

// RunRAG ranks q's chunks and fills the budget.
func RunRAG(ctx context.Context, q *Question, emb *Cached, budget int) (*RAGResult, error) {
	res := &RAGResult{TopKLimit: RAGTopK}
	for i, s := range q.HaystackSessions {
		res.Chunks = append(res.Chunks, ChunkSession(i, s, ChunkChars)...)
	}
	res.NChunks = len(res.Chunks)
	texts := make([]string, len(res.Chunks))
	for i, c := range res.Chunks {
		texts[i] = c.Text
	}
	start := time.Now()
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	res.EmbedMS = ms(time.Since(start))
	start = time.Now()
	qv, err := emb.EmbedQuery(ctx, []string{q.Question})
	if err != nil {
		return nil, err
	}
	res.Scores = make([]float64, len(vecs))
	res.Order = make([]int, len(vecs))
	for i, v := range vecs {
		res.Order[i], res.Scores[i] = i, dot(qv[0], v)
	}
	sort.SliceStable(res.Order, func(a, b int) bool { return res.Scores[res.Order[a]] > res.Scores[res.Order[b]] })
	seen := map[int]bool{}
	for _, ci := range res.Order {
		if s := res.Chunks[ci].Session; !seen[s] {
			seen[s] = true
			res.Ranked = append(res.Ranked, s)
		}
	}
	res.Selected, res.Context = FillBudget(q, res.Chunks, res.Order[:min(RAGTopK, len(res.Order))], budget)
	res.Tokens = Tokens(res.Context)
	in := map[int]bool{}
	for _, ci := range res.Selected {
		if s := res.Chunks[ci].Session; !in[s] {
			in[s] = true
			res.InBudget = append(res.InBudget, s)
		}
	}
	res.SearchMS = ms(time.Since(start))
	return res, nil
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// FillBudget takes candidates in rank order and keeps each one whose
// addition leaves the rendered context within budget tokens (a candidate
// that does not fit is skipped, later smaller ones may still fit). It
// returns the kept chunk indexes and the rendered context.
func FillBudget(q *Question, chunks []Chunk, cands []int, budget int) ([]int, string) {
	var kept []int
	text := ""
	for _, ci := range cands {
		try := append(append([]int(nil), kept...), ci)
		t := RenderChunks(q, chunks, try)
		if Tokens(t) <= budget {
			kept, text = try, t
		}
	}
	return kept, text
}

// RenderChunks renders chunks as the reader sees them, in the official
// LongMemEval history layout (src/generation/run_generation.py, "nl"
// format): sessions sorted by date, each as
//
//	### Session N:
//	Session Date: <haystack date>
//	Session Content:
//	<turn lines>
//
// Chunks of one session are listed in order; non-adjacent chunks of the same
// session are separated by a "..." line.
func RenderChunks(q *Question, chunks []Chunk, sel []int) string {
	s := append([]int(nil), sel...)
	sort.Slice(s, func(a, b int) bool {
		ca, cb := chunks[s[a]], chunks[s[b]]
		if ca.Session != cb.Session {
			da, _ := ParseDate(q.HaystackDates[ca.Session])
			db, _ := ParseDate(q.HaystackDates[cb.Session])
			if !da.Equal(db) {
				return da.Before(db)
			}
			return ca.Session < cb.Session
		}
		return ca.Ordinal < cb.Ordinal
	})
	var b strings.Builder
	n := 0
	for i, ci := range s {
		c := chunks[ci]
		if i == 0 || chunks[s[i-1]].Session != c.Session {
			n++
			fmt.Fprintf(&b, "\n### Session %d:\nSession Date: %s\nSession Content:\n", n, q.HaystackDates[c.Session])
		} else if chunks[s[i-1]].Ordinal+1 != c.Ordinal {
			b.WriteString("...\n")
		}
		b.WriteString(c.Text)
		b.WriteString("\n")
	}
	return b.String()
}
