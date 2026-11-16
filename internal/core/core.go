// Package core holds the domain vocabulary shared by every Chanda package:
// trust zones, record types and id prefixes, statuses, time formatting,
// stable error codes and the interfaces behind external dependencies.
//
// core imports nothing from Chanda. Every other package may import it.
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Trust zones (BASE.md "Trust zones"). Lowest to highest: external < internal < user.

type Zone string

const (
	ZoneExternal Zone = "external"
	ZoneInternal Zone = "internal"
	ZoneUser     Zone = "user"
)

// Rank orders zones; unknown zones rank below external so they never gain trust.
func (z Zone) Rank() int {
	switch z {
	case ZoneExternal:
		return 1
	case ZoneInternal:
		return 2
	case ZoneUser:
		return 3
	}
	return 0
}

func (z Zone) Valid() bool { return z.Rank() > 0 }

// MinZone returns the lowest zone among zs (principle 5). With no input it
// returns ZoneExternal, the safe default.
func MinZone(zs ...Zone) Zone {
	if len(zs) == 0 {
		return ZoneExternal
	}
	m := zs[0]
	for _, z := range zs[1:] {
		if z.Rank() < m.Rank() {
			m = z
		}
	}
	if !m.Valid() {
		return ZoneExternal
	}
	return m
}

// ---------------------------------------------------------------------------
// Sources and events.

type SourceKind string

const (
	SourceDocument      SourceKind = "document"
	SourceSession       SourceKind = "session"
	SourceTool          SourceKind = "tool"
	SourceWeb           SourceKind = "web"
	SourceUserStatement SourceKind = "user_statement"
)

// ZoneForSource binds the origin from the input channel (write step 2).
// Never derive a zone from content.
func ZoneForSource(k SourceKind) Zone {
	switch k {
	case SourceUserStatement:
		return ZoneUser
	case SourceDocument, SourceSession:
		return ZoneInternal
	default:
		return ZoneExternal
	}
}

type EventKind string

const (
	EventChunk      EventKind = "chunk"       // a document chunk
	EventTurn       EventKind = "turn"        // a session turn
	EventStatement  EventKind = "statement"   // free text from remember / a user statement
	EventToolResult EventKind = "tool_result" // a tool output
	EventWeb        EventKind = "web"         // web content
	EventFact       EventKind = "fact_assertion"
)

// Event flags, stored comma-separated in events.flags, sorted.
const (
	FlagInstructionLike = "instruction_like"
	FlagSecretsMasked   = "secrets_masked"
)

// ---------------------------------------------------------------------------
// Record types and ids. Every id is "<prefix>_<ULID>", so get(id) can route.

type RecordType string

const (
	RecEvent    RecordType = "event"
	RecFact     RecordType = "fact"
	RecPage     RecordType = "page"
	RecPageItem RecordType = "page_item"
	RecEntity   RecordType = "entity"
	RecSource   RecordType = "source"
	RecDocument RecordType = "document"
)

// Record types that are not records of the memory: they have no entry in
// the id-prefix table, so get, flag, promote and forget never route to
// them, and the read path never returns them.
const (
	// RecKey is a learned query key (decision 0145): its vector lives in
	// embeddings under this type, and it appears in provenance edges.
	RecKey RecordType = "key"
	// RecRetrieval names a retrievals row in provenance edges (key -> retrieval).
	RecRetrieval RecordType = "retrieval"
)

var idPrefix = map[RecordType]string{
	RecEvent:    "evt",
	RecFact:     "fct",
	RecPage:     "pg",
	RecPageItem: "pgi",
	RecEntity:   "ent",
	RecSource:   "src",
	RecDocument: "doc",
}

// Non-record id prefixes.
const (
	PrefixRetrieval = "ret"
	PrefixOutcome   = "out"
	PrefixSleepRun  = "run"
	PrefixTombstone = "tmb"
	PrefixProposal  = "prp"
	PrefixFlag      = "flg"
	PrefixModel     = "mdl"
	PrefixKey       = "key" // learned query key (decision 0145)
)

func IDPrefix(t RecordType) string { return idPrefix[t] }

// RecordTypeOf returns the record type encoded in an id, or "" if unknown.
func RecordTypeOf(id string) RecordType {
	p, _, ok := strings.Cut(id, "_")
	if !ok {
		return ""
	}
	for t, pre := range idPrefix {
		if pre == p {
			return t
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Facts.

type FactStatus string

const (
	FactActive      FactStatus = "active"
	FactSuperseded  FactStatus = "superseded"   // closed by a newer fact, or inserted as history
	FactQuarantined FactStatus = "quarantined"  // lower zone than the active fact; supersedes nothing
	FactStaleSource FactStatus = "stale_source" // its origin chunk was retired
	FactRejected    FactStatus = "rejected"     // judged wrong in sleep
	FactFlagged     FactStatus = "flagged"      // marked wrong or outdated via flag
)

type Cardinality string

const (
	Single Cardinality = "single"
	Multi  Cardinality = "multi"
)

// Provenance relations (provenance.relation).
const (
	RelDerivedFrom = "derived_from" // fact/vector/page item produced from an event
	RelConfirmedBy = "confirmed_by" // same normalized value seen again
	RelDuplicateOf = "duplicate_of" // near-duplicate; collapsed at read time
	RelSupersedes  = "supersedes"   // new fact -> closed fact
	RelCites       = "cites"        // page/page item -> event
	RelMentions    = "mentions"     // event/fact -> entity
	RelEmbeddingOf = "embedding_of" // reserved; embeddings are keyed by record directly
	RelPointsTo    = "points_to"    // learned key -> the record it finds (decision 0145)
)

// ---------------------------------------------------------------------------
// Time. All timestamps are stored as fixed-width UTC text so that string
// order equals time order and output is byte-stable.

const TimeLayout = "2006-01-02T15:04:05.000000000Z"

func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime accepts TimeLayout, RFC3339 (with or without fraction) and a bare date.
func ParseTime(s string) (time.Time, error) {
	for _, l := range []string{TimeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, Errf(CodeInvalidArgument, "invalid time %q", s)
}

// Clock is the only source of "now" (replaceable in tests; principle 11).
type Clock interface {
	Now() time.Time
}

// ---------------------------------------------------------------------------
// Errors with stable codes (BASE.md: "Errors use stable, documented codes").

type Code string

const (
	CodeInvalidArgument  Code = "invalid_argument"
	CodeNotFound         Code = "not_found"
	CodeRejected         Code = "rejected"          // admission rule refused the content
	CodePermissionDenied Code = "permission_denied" // tool not in profile, namespace mismatch, non-human promote
	CodeConflict         Code = "conflict"
	CodeImmutable        Code = "immutable" // attempt to edit the raw log
	CodeBudgetExceeded   Code = "budget_exceeded"
	CodeRateLimited      Code = "rate_limited"
	CodeUnavailable      Code = "unavailable" // optional component not configured (LLM, reranker)
	CodeIntegrity        Code = "integrity"   // audit chain or provenance check failed
	CodeInternal         Code = "internal"
)

type Error struct {
	Code    Code
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func Errf(c Code, format string, a ...any) *Error {
	return &Error{Code: c, Message: fmt.Sprintf(format, a...)}
}

func Wrap(c Code, err error, msg string) *Error { return &Error{Code: c, Message: msg, Err: err} }

// CodeOf extracts the code of err, CodeInternal if it carries none.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

// ---------------------------------------------------------------------------
// Interfaces behind external dependencies (BASE.md "Core interfaces").
// Store is concrete (internal/store); the rest are swappable.

type Embedder interface {
	ModelID() string
	Dim() int
	// Embed returns one L2-normalized vector per text. Must be deterministic.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Query/passage asymmetry (e5-style prefixes) is the Embedder's business:
// callers that know the role use this optional interface.
type RoleEmbedder interface {
	Embedder
	EmbedQuery(ctx context.Context, texts []string) ([][]float32, error)
}

type VectorHit struct {
	RecordType RecordType
	RecordID   string
	Score      float32 // cosine similarity
}

type VectorIndex interface {
	Upsert(ctx context.Context, namespace string, rt RecordType, id, modelID string, vec []float32) error
	Remove(ctx context.Context, namespace string, rt RecordType, id string) error
	// Search is exact and deterministic: ties broken by record id ascending.
	Search(ctx context.Context, namespace, modelID string, q []float32, k int, types []RecordType) ([]VectorHit, error)
}

type Reranker interface {
	ModelID() string
	Score(ctx context.Context, query string, docs []string) ([]float32, error)
}

type LLMRequest struct {
	System     string
	Prompt     string
	JSONSchema []byte // fixed output schema; the answer is validated against it
	MaxTokens  int
}

type LLMResponse struct {
	JSON         []byte
	InputTokens  int
	OutputTokens int
}

type LLMProvider interface {
	Name() string
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}
