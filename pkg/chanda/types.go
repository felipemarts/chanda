// Package chanda is the public Go API of Chanda and, at the same time, the
// wire contract of its MCP tools: every tool has an Input and an Output type
// here, and their JSON form (snake_case tags) is exactly what MCP clients send
// and receive inside the [Envelope].
//
// Embedders call an [API] implementation directly; the MCP server in
// internal/mcp and the chanda command are thin shells over the same interface.
//
// Conventions shared by every type in this package:
//   - Times are strings. Outputs use the fixed-width UTC layout
//     "2006-01-02T15:04:05.000000000Z"; inputs also accept RFC 3339 and a bare
//     date ("2026-03-01").
//   - Every input carries Namespace; empty means the configured default.
//   - Lists in outputs are never null when the call succeeds: implementations
//     return empty slices.
//   - Errors carry a stable [Code] (see docs/errors.md); use [CodeOf].
package chanda

import (
	"context"

	"github.com/felipemarts/chanda/internal/core"
)

// Zone is a trust zone: "external" < "internal" < "user".
type Zone = core.Zone

// Trust zones, lowest to highest.
const (
	ZoneExternal = core.ZoneExternal
	ZoneInternal = core.ZoneInternal
	ZoneUser     = core.ZoneUser
)

// RecordType names the kind of a record; ids carry it as a prefix
// (evt_, fct_, pg_, pgi_, ent_, src_, doc_).
type RecordType = core.RecordType

// Record types.
const (
	RecEvent    = core.RecEvent
	RecFact     = core.RecFact
	RecPage     = core.RecPage
	RecPageItem = core.RecPageItem
	RecEntity   = core.RecEntity
	RecSource   = core.RecSource
	RecDocument = core.RecDocument
)

// SourceKind is the input channel of written content. It binds the trust
// zone at write time (BASE.md write step 2): user_statement -> user,
// document and session -> internal, tool and web -> external.
type SourceKind = core.SourceKind

// Source kinds.
const (
	SourceDocument      = core.SourceDocument
	SourceSession       = core.SourceSession
	SourceTool          = core.SourceTool
	SourceWeb           = core.SourceWeb
	SourceUserStatement = core.SourceUserStatement
)

// Code is a stable error code. The values are part of the public contract and
// are listed in docs/errors.md.
type Code = core.Code

// Error codes (see docs/errors.md for when each is returned).
const (
	CodeInvalidArgument  = core.CodeInvalidArgument
	CodeNotFound         = core.CodeNotFound
	CodeRejected         = core.CodeRejected
	CodePermissionDenied = core.CodePermissionDenied
	CodeConflict         = core.CodeConflict
	CodeImmutable        = core.CodeImmutable
	CodeBudgetExceeded   = core.CodeBudgetExceeded
	CodeRateLimited      = core.CodeRateLimited
	CodeUnavailable      = core.CodeUnavailable
	CodeIntegrity        = core.CodeIntegrity
	CodeInternal         = core.CodeInternal
)

// Codes lists every error code, in documentation order.
func Codes() []Code {
	return []Code{
		CodeInvalidArgument, CodeNotFound, CodeRejected, CodePermissionDenied, CodeConflict,
		CodeImmutable, CodeBudgetExceeded, CodeRateLimited, CodeUnavailable, CodeIntegrity, CodeInternal,
	}
}

// Error is the error type returned by API implementations.
type Error = core.Error

// Errf builds an *Error with code c.
func Errf(c Code, format string, a ...any) *Error { return core.Errf(c, format, a...) }

// CodeOf returns the code carried by err, CodeInternal if it carries none.
func CodeOf(err error) Code { return core.CodeOf(err) }

// ---------------------------------------------------------------------------
// Actor: who is calling. The audit log records Actor.Name for every mutation.

// Actor identifies the caller of an API method. Transports put it in the
// context with [WithActor]; implementations read it with [ActorFrom] and
// record Name in the audit log.
type Actor struct {
	Name  string // identity written to audit_log.actor, e.g. "agent" or "cli"
	Human bool   // true only when a person is in control (required by Promote)
	// Remote marks a caller reached over a protocol (MCP). A remote caller
	// may ingest only under write.ingest_roots.
	Remote bool
	// MaxZone caps the trust zone of every write made by this caller
	// (decision 0090): zone = MinZone(zone of the source kind, MaxZone).
	// Empty means the configured mcp.max_zone. "user" counts only for a
	// Human actor; otherwise it is lowered to "internal".
	MaxZone Zone
}

type actorKey struct{}

// WithActor returns ctx carrying a.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor carried by ctx and whether one was set.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// ---------------------------------------------------------------------------
// Meta: what an output tells the envelope without being part of data.

// Meta is embedded in every Output. Its fields are not serialized inside
// data; the envelope surfaces them as audit_id and notices.
type Meta struct {
	AuditID int64    `json:"-"` // id of the audit_log entry written by this call, 0 if none
	Notices []string `json:"-"` // human-readable remarks (e.g. "embedding queue lagging")
}

// EnvelopeMeta returns m. Every Output gets it by embedding Meta.
func (m Meta) EnvelopeMeta() Meta { return m }

// MetaCarrier is implemented by every Output type through the embedded Meta.
type MetaCarrier interface{ EnvelopeMeta() Meta }

// ---------------------------------------------------------------------------
// Shared record shapes.

// Origin says where a record came from (principle 4).
type Origin struct {
	SourceURI string   `json:"source_uri,omitempty" jsonschema:"URI of the source (file, session, tool or user channel)"`
	DocURI    string   `json:"doc_uri,omitempty" jsonschema:"URI of the document, for document chunks and facts derived from them"`
	EventIDs  []string `json:"event_ids" jsonschema:"raw events this record derives from, sorted"`
}

// Item is one record as delivered by context, search and get.
type Item struct {
	ID        string     `json:"id" jsonschema:"record id; the prefix encodes the type (evt_, fct_, pg_, pgi_)"`
	Type      RecordType `json:"type" jsonschema:"record type: event, fact, page or page_item"`
	Text      string     `json:"text" jsonschema:"record text"`
	ValidFrom string     `json:"valid_from,omitempty" jsonschema:"start of validity in the world (facts; events with a declared date)"`
	ValidTo   string     `json:"valid_to,omitempty" jsonschema:"end of validity; absent means still current"`
	Status    string     `json:"status,omitempty" jsonschema:"fact or page status, e.g. active or superseded"`
	Origin    Origin     `json:"origin" jsonschema:"where the record came from"`
	Zone      Zone       `json:"zone" jsonschema:"trust zone: external, internal or user"`
	Flags     []string   `json:"flags,omitempty" jsonschema:"marks such as instruction_like or secrets_masked"`
	Score     float64    `json:"score" jsonschema:"final ranking score; higher is more relevant"`
}

// SourceRef names the input channel and source of written content.
type SourceRef struct {
	Kind SourceKind `json:"kind,omitempty" jsonschema:"input channel: user_statement, document, session, tool or web; it fixes the trust zone"`
	URI  string     `json:"uri,omitempty" jsonschema:"stable identifier of the source (file path, URL, session or tool name)"`
}

// Write actions reported in WriteResult.Action.
const (
	ActionCreated       = "created"        // new event or fact written
	ActionDuplicate     = "duplicate"      // exact duplicate; the existing id is returned
	ActionConfirmed     = "confirmed"      // same fact value seen again; confidence raised
	ActionSuperseded    = "superseded"     // new fact closed the previous active one
	ActionHistory       = "history"        // new fact is older than the active one; stored closed
	ActionCoexist       = "coexist"        // multi-valued attribute; both facts active
	ActionQuarantined   = "quarantined"    // lower zone than the active fact; supersedes nothing
	ActionNearDuplicate = "near_duplicate" // stored and linked duplicate_of; collapsed at read time
)

// WriteResult reports what a write did (BASE.md write step 11).
type WriteResult struct {
	ID      string   `json:"id" jsonschema:"id of the written record, or of the existing one for duplicate/confirmed"`
	Action  string   `json:"action" jsonschema:"created, duplicate, confirmed, superseded, history, coexist, quarantined or near_duplicate"`
	Reason  string   `json:"reason" jsonschema:"short machine-readable reason for the action"`
	Flags   []string `json:"flags" jsonschema:"marks applied during admission (instruction_like, secrets_masked)"`
	Related []string `json:"related,omitempty" jsonschema:"ids of existing records affected (the superseded fact, the duplicate original)"`
}
