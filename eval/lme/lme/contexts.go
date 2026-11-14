package lme

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
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
	IngestSeconds          float64  `json:"ingest_seconds"`
	RetrievalMS            float64  `json:"retrieval_latency_ms"`
	DBBytes                int64    `json:"db_bytes"` // B: chunk text + vectors; D/E: Chanda .db + -wal + -shm
	EmbedModel             string   `json:"embed_model"`
	ChandaTokensUsed       int      `json:"chanda_tokens_used,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	Items                  int      `json:"items"`
	Sleep                  any      `json:"sleep,omitempty"`    // E: the sleep_run output
	SleepMS                float64  `json:"sleep_ms,omitempty"` // E
	Err                    string   `json:"error,omitempty"`
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
		s := time.Now()
		var out chanda.SleepRunOutput
		if err := srv.Call(ctx, chanda.ToolSleepRun, chanda.SleepRunInput{Phases: []int{1, 2}, Budget: co.SleepBudget}, &out); err != nil {
			return err
		}
		l.SleepMS = ms(time.Since(s))
		l.Sleep = out
		// sleep may write derived records and their vectors: wait for the index.
		var f chanda.FlushOutput
		if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
			return err
		}
	}
	asOf, err := RFC3339(q.QuestionDate)
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
	srv.Close()
	closed = true
	l.DBBytes = DiskBytes(srv.DBPath)
	return nil
}
