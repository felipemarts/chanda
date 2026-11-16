package lme

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// Context conditions for phases 3/4.
const (
	CondB = "B" // plain RAG
	CondD = "D" // Chanda context, no sleep
	CondE = "E" // Chanda context after sleep_run (phase 2 with the LLM)
)

// ContextLine is one line of the contexts JSONL: what the reader gets for
// one question under one condition, and how it was obtained. Context is the
// text exactly as it goes into the reader prompt; the reader prompt itself
// (question, question date, instructions) is the LLM stage's.
type ContextLine struct {
	QuestionID          string   `json:"question_id"`
	QuestionType        string   `json:"question_type"`
	Abstention          bool     `json:"abstention"`
	Condition           string   `json:"condition"`
	Question            string   `json:"question"`
	QuestionDate        string   `json:"question_date"`
	Context             string   `json:"context"`
	ContextTokens       int      `json:"context_tokens"` // ceil(runes/4) of Context
	RetrievedSessionIDs []string `json:"retrieved_session_ids"`
	// Gold, for error analysis only: never put it in a prompt.
	GoldEvidenceSessionIDs []string `json:"gold_evidence_session_ids"`
	EvidenceRecall         *float64 `json:"evidence_recall_in_context,omitempty"`
	// Turn-level evidence recall (evidence.go has the matching rule): the
	// has_answer turns, how many reached the block, their ratio (null when
	// the question has none) and each turn's match.
	EvidenceTurns          int            `json:"evidence_turns"`
	EvidenceTurnsInContext int            `json:"evidence_turns_in_context"`
	EvidenceTurnRecall     *float64       `json:"evidence_turn_recall"`
	EvidenceTurnMatches    []EvidenceTurn `json:"evidence_turn_matches,omitempty"`
	// ContextItems: one entry per block item (Chanda) or turn piece (RAG).
	ContextItems     []ContextItem `json:"context_items"`
	IngestSeconds    float64       `json:"ingest_seconds"`
	RetrievalMS      float64       `json:"retrieval_latency_ms"`
	DBBytes          int64         `json:"db_bytes"` // B: chunk text + vectors; D/E: Chanda .db + -wal + -shm
	EmbedModel       string        `json:"embed_model"`
	ChandaTokensUsed int           `json:"chanda_tokens_used,omitempty"`
	Reason           string        `json:"reason,omitempty"`
	Items            int           `json:"items"`
	Sleep            any           `json:"sleep,omitempty"`    // E: the sleep_run output
	SleepMS          float64       `json:"sleep_ms,omitempty"` // E
	// D/E: the -chanda-set overrides (key=value, in order) and Chanda's
	// configuration hash of the chanda.yaml the server ran with.
	ChandaSet        []string `json:"chanda_set,omitempty"`
	ChandaConfigHash string   `json:"chanda_config_hash,omitempty"`
	Err              string   `json:"error,omitempty"`
}

// Contexts produces one ContextLine per selected question for cond.
func Contexts(ctx context.Context, o *RunOptions, emb *Cached, cond string) ([]*ContextLine, error) {
	start := time.Now()
	var mu sync.Mutex
	done := 0
	return ForEachQuestion(ctx, o, func(ctx context.Context, _ int, q *Question) (*ContextLine, error) {
		l := &ContextLine{
			QuestionID: q.QuestionID, QuestionType: q.QuestionType, Abstention: IsAbstention(q.QuestionID),
			Condition: cond, Question: q.Question, QuestionDate: q.QuestionDate,
			GoldEvidenceSessionIDs: q.AnswerSessionIDs, EmbedModel: emb.ModelID(), RetrievedSessionIDs: []string{},
		}
		var err error
		switch cond {
		case CondB:
			err = contextB(ctx, o, emb, q, l)
		case CondD, CondE:
			err = contextChanda(ctx, o, emb, q, l, cond == CondE)
		default:
			err = fmt.Errorf("unknown condition %q (want B, D or E)", cond)
		}
		if err != nil {
			return nil, err
		}
		l.ContextTokens = Tokens(l.Context)
		l.EvidenceTurns, l.EvidenceTurnsInContext, l.EvidenceTurnRecall = EvidenceCounts(l.EvidenceTurnMatches)
		if l.ContextItems == nil {
			l.ContextItems = []ContextItem{}
		}
		if !l.Abstention && len(q.AnswerSessionIDs) > 0 {
			r := recallOf(l.RetrievedSessionIDs, q.AnswerSessionIDs)
			l.EvidenceRecall = &r
		}
		mu.Lock()
		defer mu.Unlock()
		done++
		o.logf("[%d] %s %s %s tokens=%d (elapsed %s)", done, cond, q.QuestionID, q.QuestionType, l.ContextTokens, time.Since(start).Round(time.Second))
		return l, nil
	})
}

func contextB(ctx context.Context, o *RunOptions, emb *Cached, q *Question, l *ContextLine) error {
	g, err := RunRAG(ctx, q, emb, o.Budget)
	if err != nil {
		return err
	}
	l.Context, l.IngestSeconds, l.RetrievalMS, l.Items = g.Context, g.EmbedMS/1000, g.SearchMS, len(g.Selected)
	l.RetrievedSessionIDs = sessionIDs(q, g.InBudget)
	l.EvidenceTurnMatches = MatchEvidenceRAG(q, g.Chunks, g.Selected)
	l.ContextItems = RAGContextItems(q, g.Chunks, g.Selected)
	for _, c := range g.Chunks {
		l.DBBytes += int64(len(c.Text)) + int64(4*emb.Dim())
	}
	return nil
}

func contextChanda(ctx context.Context, o *RunOptions, emb *Cached, q *Question, l *ContextLine, sleep bool) error {
	cond := CondD
	co := o.Chanda
	if sleep {
		cond = CondE
		if co.LLMURL == "" {
			return fmt.Errorf("condition E needs an LLM endpoint (-llm-url)")
		}
		co.Admin = true
	}
	dir := questionDir(o, q, cond)
	_ = os.RemoveAll(dir)
	srv, err := StartChanda(ctx, co, dir, emb.ModelID(), emb.Dim())
	if err != nil {
		return err
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
		return err
	}
	l.IngestSeconds = in.Elapsed.Seconds()
	if sleep {
		// Chanda refuses a sleep run during a write burst (a write in the last
		// sleep.burst_seconds, 10 by default): wait it out, as a real caller
		// would, and retry while it still says so.
		var out chanda.SleepRunOutput
		var s time.Time
		for attempt := 1; ; attempt++ {
			if err := waitCtx(ctx, SleepBurstWait); err != nil {
				return err
			}
			s = time.Now()
			err := srv.Call(ctx, chanda.ToolSleepRun, chanda.SleepRunInput{Phases: []int{1, 2}, Budget: co.SleepBudget}, &out)
			if err == nil {
				break
			}
			if attempt >= 3 || !strings.Contains(err.Error(), "write burst") {
				return err
			}
		}
		l.SleepMS = ms(time.Since(s))
		l.Sleep = out
		// sleep may write derived records and their vectors: wait for the index.
		var f chanda.FlushOutput
		if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
			return err
		}
	}
	asOf, err := AsOfDay(q.QuestionDate)
	if err != nil {
		return err
	}
	start := time.Now()
	var out chanda.ContextOutput
	if err := srv.Call(ctx, chanda.ToolContext, chanda.ContextInput{Query: q.Question, BudgetTokens: o.Budget, AsOf: asOf}, &out); err != nil {
		return err
	}
	l.RetrievalMS = ms(time.Since(start))
	l.Context, l.ChandaTokensUsed, l.Reason, l.Items = out.Text, out.TokensUsed, out.Reason, len(out.Items)
	var per [][]string
	for _, it := range out.Items {
		per = append(per, sessionIDs(q, in.ItemSessions(it)))
	}
	l.RetrievedSessionIDs = RankSessions(per)
	if l.RetrievedSessionIDs == nil {
		l.RetrievedSessionIDs = []string{}
	}
	l.EvidenceTurnMatches = MatchEvidenceChanda(q, out.Items, out.Text, in.TurnEvent)
	l.ContextItems = ChandaContextItems(q, out.Items, out.Text, in.EventTurns)
	l.ChandaSet = OverrideStrings(co.Set)
	if b, err := os.ReadFile(filepath.Join(dir, "chanda.yaml")); err == nil {
		if h, err := CheckChandaConfig(string(b)); err == nil {
			l.ChandaConfigHash = h
		}
	}
	srv.Close()
	closed = true
	l.DBBytes = DiskBytes(srv.DBPath)
	return nil
}

// SleepBurstWait is how long condition E waits after its last write before
// sleep_run: Chanda's sleep.burst_seconds (10) plus a second.
const SleepBurstWait = 11 * time.Second

func waitCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
