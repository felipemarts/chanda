package lme

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// Phase 1 condition names.
const (
	CondBM25   = "bm25"
	CondRAG    = "rag"
	CondChanda = "chanda"
)

// RunOptions configures phase 1 and the context generator.
type RunOptions struct {
	Dataset     string
	IDs         []string // questions to run, in order; nil = the whole file
	Conds       []string
	BM25Unit    string
	Embed       EmbedOptions
	Chanda      ChandaOptions
	Budget      int
	SearchLimit int // Chanda search limit (items)
	Workers     int
	WorkDir     string // per-question databases (deleted after measuring unless Keep)
	Keep        bool
	Log         func(format string, a ...any)
}

// P1Cond is one condition's result for one question.
type P1Cond struct {
	Ranked      []string `json:"ranked_top20"` // dataset session ids, best first (first 20)
	NRanked     int      `json:"n_ranked"`     // distinct sessions ranked
	Metrics     QMetrics `json:"metrics"`
	RetrievalMS float64  `json:"retrieval_ms"`
	IngestMS    float64  `json:"ingest_ms"`
	DBBytes     int64    `json:"db_bytes,omitempty"`   // Chanda stats.db_size_bytes
	DiskBytes   int64    `json:"disk_bytes,omitempty"` // Chanda .db + -wal + -shm after the run
	// RAG only: evidence recall within the 2,000-token context.
	BudgetRecall  *float64       `json:"budget_recall,omitempty"`
	ContextTokens int            `json:"context_tokens,omitempty"`
	Items         int            `json:"items,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
	Err           string         `json:"error,omitempty"`
}

// P1Question is one line of phase1.questions.jsonl.
type P1Question struct {
	QuestionID   string             `json:"question_id"`
	QuestionType string             `json:"question_type"`
	Abstention   bool               `json:"abstention"`
	Evidence     []string           `json:"answer_session_ids"`
	Official     []string           `json:"official_targets"`
	Sessions     int                `json:"sessions"`
	Turns        int                `json:"turns"`
	Conds        map[string]*P1Cond `json:"conditions"`
	WallMS       float64            `json:"wall_ms"`
}

func (o *RunOptions) logf(format string, a ...any) {
	if o.Log != nil {
		o.Log(format, a...)
	}
}

// ForEachQuestion runs fn over the selected questions with o.Workers
// goroutines and returns results in selection order.
func ForEachQuestion[T any](ctx context.Context, o *RunOptions, fn func(ctx context.Context, idx int, q *Question) (T, error)) ([]T, error) {
	var ids map[string]bool
	order := map[string]int{}
	if o.IDs != nil {
		ids = map[string]bool{}
		for i, id := range o.IDs {
			ids[id] = true
			order[id] = i
		}
	}
	type job struct {
		idx int
		q   *Question
	}
	jobs := make(chan job, max(1, o.Workers))
	var mu sync.Mutex
	results := map[int]T{}
	var firstErr error
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for w := 0; w < max(1, o.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r, err := fn(ctx, j.idx, j.q)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("question %s: %w", j.q.QuestionID, err)
					cancel()
				}
				results[j.idx] = r
				mu.Unlock()
			}
		}()
	}
	n := 0
	readErr := Select(o.Dataset, ids, func(q *Question) error {
		idx := n
		if ids != nil {
			idx = order[q.QuestionID]
		}
		n++
		select {
		case jobs <- job{idx, q}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if readErr != nil {
		return nil, readErr
	}
	if ids != nil && n != len(ids) {
		return nil, fmt.Errorf("%d of %d selected question ids found in %s", n, len(ids), o.Dataset)
	}
	keys := make([]int, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, results[k])
	}
	return out, nil
}

func sessionIDs(q *Question, idx []int) []string {
	out := make([]string, len(idx))
	for i, s := range idx {
		out[i] = q.HaystackSessionIDs[s]
	}
	return out
}

func top(xs []string, n int) []string { return xs[:min(n, len(xs))] }

// Phase1 runs the retrieval conditions over the selected questions.
func Phase1(ctx context.Context, o *RunOptions, emb *Cached) ([]*P1Question, error) {
	done := 0
	var mu sync.Mutex
	start := time.Now()
	return ForEachQuestion(ctx, o, func(ctx context.Context, idx int, q *Question) (*P1Question, error) {
		qs := time.Now()
		r := &P1Question{
			QuestionID: q.QuestionID, QuestionType: q.QuestionType, Abstention: IsAbstention(q.QuestionID),
			Evidence: q.AnswerSessionIDs, Official: OfficialTargets(q), Sessions: len(q.HaystackSessions),
			Conds: map[string]*P1Cond{},
		}
		for _, s := range q.HaystackSessions {
			r.Turns += len(s)
		}
		for _, c := range o.Conds {
			pc, err := phase1Cond(ctx, o, emb, q, c)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", c, err)
			}
			pc.Metrics = Score(pc.Ranked, r.Evidence, r.Official)
			pc.Ranked = top(pc.Ranked, 20)
			r.Conds[c] = pc
		}
		r.WallMS = ms(time.Since(qs))
		mu.Lock()
		done++
		o.logf("[%d/%d] %s %s %.1fs (elapsed %s)", done, max(len(o.IDs), done), q.QuestionID, q.QuestionType, r.WallMS/1000, time.Since(start).Round(time.Second))
		mu.Unlock()
		return r, nil
	})
}

func phase1Cond(ctx context.Context, o *RunOptions, emb *Cached, q *Question, cond string) (*P1Cond, error) {
	switch cond {
	case CondBM25:
		b, err := RunBM25(ctx, q, o.BM25Unit)
		if err != nil {
			return nil, err
		}
		ranked := sessionIDs(q, b.Ranked)
		return &P1Cond{Ranked: ranked, NRanked: len(ranked), RetrievalMS: b.SearchMS, IngestMS: b.IndexMS,
			Extra: map[string]any{"unit": o.BM25Unit}}, nil
	case CondRAG:
		g, err := RunRAG(ctx, q, emb, o.Budget)
		if err != nil {
			return nil, err
		}
		ranked := sessionIDs(q, g.Ranked)
		pc := &P1Cond{Ranked: ranked, NRanked: len(ranked), RetrievalMS: g.SearchMS, IngestMS: g.EmbedMS,
			ContextTokens: g.Tokens, Items: len(g.Selected), Extra: map[string]any{"chunks": g.NChunks}}
		if !IsAbstention(q.QuestionID) && len(q.AnswerSessionIDs) > 0 {
			br := recallOf(sessionIDs(q, g.InBudget), q.AnswerSessionIDs)
			pc.BudgetRecall = &br
		}
		return pc, nil
	case CondChanda:
		return chandaSearch(ctx, o, emb, q)
	}
	return nil, fmt.Errorf("unknown condition %q", cond)
}

func recallOf(got, evidence []string) float64 {
	if len(evidence) == 0 {
		return 0
	}
	g := set(got)
	n := 0
	for _, e := range evidence {
		if g[e] {
			n++
		}
	}
	return float64(n) / float64(len(evidence))
}

func questionDir(o *RunOptions, q *Question, cond string) string {
	return filepath.Join(o.WorkDir, q.QuestionID, cond)
}

func chandaSearch(ctx context.Context, o *RunOptions, emb *Cached, q *Question) (*P1Cond, error) {
	dir := questionDir(o, q, CondChanda)
	_ = os.RemoveAll(dir)
	srv, err := StartChanda(ctx, o.Chanda, dir, emb.ModelID(), emb.Dim())
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			srv.Close()
		}
		if !o.Keep {
			_ = os.RemoveAll(dir)
		}
	}()
	in, err := srv.IngestQuestion(ctx, q)
	if err != nil {
		return nil, err
	}
	asOf, err := AsOfDay(q.QuestionDate)
	if err != nil {
		return nil, err
	}
	limit := o.SearchLimit
	if limit <= 0 {
		limit = 100
	}
	start := time.Now()
	var out chanda.SearchOutput
	if err := srv.Call(ctx, chanda.ToolSearch, chanda.SearchInput{Query: q.Question, Limit: limit, Filters: chanda.SearchFilters{AsOf: asOf}}, &out); err != nil {
		return nil, err
	}
	lat := ms(time.Since(start))
	var per [][]string
	types := map[string]int{}
	unmapped := 0
	for _, it := range out.Items {
		types[string(it.Type)]++
		ss := in.ItemSessions(it)
		if len(ss) == 0 {
			unmapped++
		}
		per = append(per, sessionIDs(q, ss))
	}
	var st chanda.StatsOutput
	if err := srv.Call(ctx, chanda.ToolStats, chanda.StatsInput{}, &st); err != nil {
		return nil, err
	}
	srv.Close()
	closed = true
	ranked := RankSessions(per)
	return &P1Cond{
		Ranked: ranked, NRanked: len(ranked), RetrievalMS: lat, IngestMS: ms(in.Elapsed),
		DBBytes: st.DBSizeBytes, DiskBytes: DiskBytes(srv.DBPath), Items: len(out.Items),
		Extra: map[string]any{
			"turns": in.Turns, "created": in.Created, "duplicates": in.Duplicates, "near_duplicates": in.NearDups,
			"rejected": in.Rejected, "reject_reasons": in.RejectReasons, "flush_ms": in.FlushMS, "item_types": types, "unmapped_items": unmapped,
			"reason": out.Reason, "events": st.Counts.Events, "embeddings": st.Counts.Embeddings, "facts": st.Counts.Facts,
			"embed_model": st.EmbedModel,
		},
	}, nil
}
