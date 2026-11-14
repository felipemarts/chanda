package eval

import (
	"context"
	"time"
)

// Query is one evaluation question as a system sees it: no answer key, no tags.
type Query struct {
	ID   string
	Text string
	AsOf time.Time // reference date; systems without temporal support ignore it
}

// Retrieved is one excerpt delivered by a system.
type Retrieved struct {
	ID    string  // the system's own record id
	DocID string  // corpus item id the excerpt comes from; may be left empty if URI is set
	URI   string  // corpus item URI; the harness maps it to DocID when DocID is empty
	Text  string  // exactly what would be placed in the reader's context
	Score float64 // informative only
	// Tokens is recomputed by the harness with CountTokens, so every system's
	// cost and budget are measured with the same estimator.
	Tokens int
}

// System is one compared configuration (B0..B3, C1..C5). The harness calls
// Ingest once per corpus item in corpus order, then Flush if implemented,
// then Retrieve once per question.
type System interface {
	Name() string
	Ingest(ctx context.Context, item Item) error
	// Retrieve returns excerpts in rank order whose total CountTokens fits
	// budgetTokens. Excess items are dropped by the harness and counted as
	// budget violations. An empty result means "no relevant memory".
	Retrieve(ctx context.Context, q Query, budgetTokens int) ([]Retrieved, error)
	Close() error
}

// SystemInfo is reported next to each system's results.
type SystemInfo struct {
	Description    string `json:"description"`
	EmbeddingModel string `json:"embedding_model"` // model_id, or "n/a"
	ConfigHash     string `json:"config_hash,omitempty"`
}

// Describer is optionally implemented by a System to fill the report.
type Describer interface {
	Info() SystemInfo
}

// Flusher is optionally implemented by systems with asynchronous indexing
// (e.g. an embedding queue); Flush is timed as part of ingestion.
type Flusher interface {
	Flush(ctx context.Context) error
}

// Trainer is optionally implemented by a learning system (C5). The harness
// calls Train once, after ingestion and Flush and before any evaluated
// question, with the training split (questions with their gold labels). The
// evaluated questions are never passed to Train.
type Trainer interface {
	Train(ctx context.Context, train []Question) error
}

// UnavailableSystem is a compared configuration that could not be built in
// this environment (e.g. no reranker configured). It is listed in the report
// with the reason, never silently dropped or replaced.
type UnavailableSystem struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Sizer is optionally implemented by systems that can report their storage size.
type Sizer interface {
	SizeBytes(ctx context.Context) (int64, error)
}
