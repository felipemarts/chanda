// Package read is the read path (BASE.md "Read path and pre-context"):
// keyword and vector search, rule filter, near-duplicate collapse, RRF
// fusion, optional rerank, utility adjustment, relevance cut, diversity and
// budget, assembly, the two modes (search and context) and retrieval
// logging. No LLM takes part (principle 2).
//
// Dependencies on other areas are consumer-defined interfaces declared here
// (Utility, Learned) or the core interfaces (Embedder, VectorIndex,
// Reranker). Every dependency may be nil: the read path then degrades to
// keyword search with no rerank and λ = 0.
package read

import (
	"context"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Utility gives the per-record utility in [0,1] and the namespace's λ
// (implemented by the ranking area). Records missing from Scores have no
// utility yet and use NeutralUtility (0.5).
type Utility interface {
	Scores(ctx context.Context, ns string, ids []string) (map[string]float64, error)
	Lambda(ctx context.Context, ns string) (float64, error)
}

// Term gives the learned term of read step 7 for each candidate: the score
// of the promoted model (decision 0070). Ids missing from the map use
// NeutralUtility. It is only asked when λ > 0.
type Term interface {
	Terms(ctx context.Context, ns string, cs []Candidate) (map[string]float64, error)
}

// Learned is the learned ranker in shadow mode: its score is logged with
// the candidate and never changes the order (BASE.md "Shadow mode first").
type Learned interface {
	Score(ctx context.Context, ns string, c Candidate) (float64, bool)
}

// LearnedBatch is optionally implemented by a Learned: the shadow scores of
// a whole candidate list in one call (one model lookup and one utility
// query instead of one per candidate). ok is false when there is no model.
type LearnedBatch interface {
	ScoreAll(ctx context.Context, ns string, cs []Candidate) (scores map[string]float64, ok bool)
}

// Deps are the optional collaborators of the read path. All may be nil.
type Deps struct {
	Embedder    core.Embedder    // query embedding; RoleEmbedder.EmbedQuery used when available
	VectorIndex core.VectorIndex // nearest neighbours; nil → keyword only
	Reranker    core.Reranker    // nil or cfg.Read.RerankEnabled=false → step skipped
	Utility     Utility          // nil → λ = 0 and no utility feature
	Term        Term             // learned term of step 7; nil → λ = 0
	Learned     Learned          // nil → no shadow score
}

// Reader runs queries against one store.
type Reader struct {
	e       *env.Env
	deps    Deps
	cfgHash string
	floors  config.EvidenceFloor // evidence floors of the embedding model (decision 0121)
	fts     *ftsStats            // FTS row counts and term frequencies (decision 0122); shared by copies

	// Read-only scoring (Score): queries go to q when set, nothing is
	// logged and λ may be fixed by the caller.
	q         store.Queryer
	scoreOnly bool
	lambdaFix *float64
}

// db is where the read path queries: the scoring queryer, else the store.
func (r *Reader) db() store.Queryer {
	if r.q != nil {
		return r.q
	}
	return r.e.Store.DB()
}

// New returns a Reader over e with the given optional dependencies.
func New(e *env.Env, deps Deps) *Reader {
	model := ""
	if deps.Embedder != nil {
		model = deps.Embedder.ModelID()
	}
	return &Reader{e: e, deps: deps, cfgHash: e.Cfg.Hash(), floors: e.Cfg.Read.Floors(model), fts: newFTSStats()}
}

// Mode is the read mode (BASE.md "Differences between the modes").
type Mode string

const (
	ModeSearch  Mode = "search"
	ModeContext Mode = "context"
)

// Reasons reported with a result.
const (
	ReasonNoRelevantMemory = "no_relevant_memory" // nothing passed the cut (or nothing matched)
	ReasonBudgetTooSmall   = "budget_too_small"   // items passed the cut but none fit the budget
)

// Scores is the per-stage breakdown of a candidate. A nil pointer means the
// stage did not produce a value for this record (e.g. no vector hit).
type Scores struct {
	KeywordRank int      `json:"kw_rank,omitempty"`  // 1-based rank within its record type's keyword list
	VectorRank  int      `json:"vec_rank,omitempty"` // 1-based rank in the vector list
	BM25        *float64 `json:"bm25,omitempty"`     // -bm25(): higher is better
	Cosine      *float64 `json:"cosine,omitempty"`
	Rerank      *float64 `json:"rerank,omitempty"` // raw reranker score
	RRF         *float64 `json:"rrf,omitempty"`
	Coverage    *float64 `json:"coverage,omitempty"`  // lexical evidence in [0,1]
	Evidence    *float64 `json:"evidence,omitempty"`  // max(lexical, semantic) evidence in [0,1]
	Relevance   *float64 `json:"relevance,omitempty"` // normalized to [0,1]; the cut uses this
	Utility     *float64 `json:"utility,omitempty"`
	Learned     *float64 `json:"learned,omitempty"` // shadow score of the newest model
	Term        *float64 `json:"term,omitempty"`    // learned term of the promoted model (only when λ > 0)
	Final       *float64 `json:"final,omitempty"`   // relevance when λ = 0, else (1−λ)·relevance + λ·term
}

// Candidate is what the learned ranker sees (features of one candidate).
type Candidate struct {
	ID         string          `json:"id"`
	Type       core.RecordType `json:"type"`
	Zone       core.Zone       `json:"zone"`
	AgeSeconds float64         `json:"age_seconds"`
	// EntityMatch: the query names an entity the record is about (an alias
	// of a fact's subject, of the subject of a fact an event states, or
	// named in the record's text).
	EntityMatch bool   `json:"entity_match,omitempty"`
	Scores      Scores `json:"scores"`
}

// Origin points an item back to the raw log.
type Origin struct {
	EventIDs   []string `json:"event_ids,omitempty"`
	SourceURIs []string `json:"source_uris,omitempty"`
	DocIDs     []string `json:"doc_ids,omitempty"`
	DocURIs    []string `json:"doc_uris,omitempty"`
}

// Item is one delivered record.
type Item struct {
	ID         string          `json:"id"`
	Type       core.RecordType `json:"type"`
	Kind       string          `json:"kind,omitempty"`  // event kind or page kind
	Title      string          `json:"title,omitempty"` // pages
	Text       string          `json:"text"`
	Truncated  bool            `json:"truncated,omitempty"`
	Status     string          `json:"status,omitempty"`     // facts and pages
	Subject    string          `json:"subject,omitempty"`    // facts
	Predicate  string          `json:"predicate,omitempty"`  // facts
	ValidFrom  string          `json:"valid_from,omitempty"` // facts
	ValidTo    string          `json:"valid_to,omitempty"`   // facts; empty = current
	RecordedAt string          `json:"recorded_at,omitempty"`
	Zone       core.Zone       `json:"zone"`
	Flags      []string        `json:"flags,omitempty"` // e.g. instruction_like (own or inherited from origins)
	Origin     Origin          `json:"origin"`
	Scores     Scores          `json:"scores"`
	namespace  string
}
