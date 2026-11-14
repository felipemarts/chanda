// Package write implements the synchronous write path (BASE.md "Write path"),
// steps 1 to 7 and 11: normalize, bind the origin, admit, check exact
// duplicates, write the raw event, compare document versions, enqueue for
// indexing, audit and respond. No LLM is involved.
//
// Steps 8 to 10 (entities, facts, near-duplicates) belong to other packages;
// they plug in through EventHook, which runs inside the same transaction for
// every newly created event.
package write

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Actions reported in Result.Action and IngestResult.Action.
const (
	ActionCreated   = "created"   // a new event (or a new document version) was written
	ActionDuplicate = "duplicate" // exact duplicate: the existing id is returned
	ActionRejected  = "rejected"  // refused by an admission rule; see Reason
	ActionUnchanged = "unchanged" // document re-ingested with the same full-text hash
	ActionUpdated   = "updated"   // document changed: a new version was written
	ActionRetired   = "retired"   // document gone from its re-ingested folder: every chunk retired
)

// Reasons for non-rejected results (rejections use the Reason* admission constants).
const (
	ReasonNewEvent       = "new_event"
	ReasonExactDuplicate = "exact_duplicate"
	ReasonNewDocument    = "new_document"
	ReasonNewVersion     = "new_version"
	ReasonSameContent    = "same_content"
	ReasonMissingFromDir = "missing_from_folder"
)

// Audit actions used by this package (registered in internal/audit).
const (
	ActSourceCreate    = audit.ActSourceCreate
	ActDocumentVersion = audit.ActDocumentVersion
)

// EventHook lets later write steps (entity resolution, fact reconciliation,
// near-duplicate marking) run inside the same transaction as the event that
// triggered them. Hooks run in the order given to New, once per newly created
// event, in creation order. An error rolls back the whole write.
type EventHook interface {
	AfterEvent(ctx context.Context, tx *sql.Tx, ev Event) error
}

// EventHookFunc adapts a function to EventHook.
type EventHookFunc func(ctx context.Context, tx *sql.Tx, ev Event) error

// AfterEvent calls f.
func (f EventHookFunc) AfterEvent(ctx context.Context, tx *sql.Tx, ev Event) error {
	return f(ctx, tx, ev)
}

// Event mirrors one row of the events table. Empty strings and nil pointers
// stand for SQL NULL.
type Event struct {
	ID          string
	Namespace   string
	SourceID    string
	Kind        core.EventKind
	Content     string // normalized, secrets masked
	ContentHash string // SHA-256 hex of Content
	TrustZone   core.Zone
	Actor       string
	RecordedAt  string
	ValidFrom   string
	DocID       string
	DocVersion  *int
	Ordinal     *int
	HeadingPath string
	SessionID   string
	Flags       []string // sorted
	RetiredAt   string
}

// Result is the response of a single-event write (write step 11).
type Result struct {
	ID      string   `json:"id,omitempty"`
	Action  string   `json:"action"`
	Reason  string   `json:"reason"`
	Flags   []string `json:"flags,omitempty"`
	AuditID int64    `json:"audit_id,omitempty"`
}

// Writer is the write path. It is safe for sequential use from one goroutine
// per store connection (the store serializes transactions).
type Writer struct {
	e     *env.Env
	adm   *Admitter
	hooks []EventHook
}

// New builds a Writer. Hooks run in order inside each write transaction.
// It fails only if a configured pattern does not compile.
func New(e *env.Env, hooks ...EventHook) (*Writer, error) {
	adm, err := NewAdmitter(e.Cfg.Write)
	if err != nil {
		return nil, err
	}
	return &Writer{e: e, adm: adm, hooks: hooks}, nil
}

// Admitter exposes the configured admission rules (e.g. for previews).
func (w *Writer) Admitter() *Admitter { return w.adm }

// OnEventFunc runs inside a write transaction once the written event exists:
// created is true for a new event (it runs before the Writer's hooks) and
// false when the write found an exact duplicate (ev is the existing event;
// no hook runs then). An error rolls back the whole write.
type OnEventFunc func(ctx context.Context, tx *sql.Tx, ev Event, created bool) error

// RememberInput is free text written through the remember tool.
type RememberInput struct {
	Namespace  string          // empty: cfg.Namespace
	Content    string          // raw text
	Source     string          // source uri; empty: "<kind>:<actor>"
	SourceKind core.SourceKind // input channel; decides the trust zone (required)
	Actor      string          // who writes (required)
	ValidFrom  string          // optional date given in the call (core.ParseTime formats)
	SessionID  string          // optional
	// EventKind overrides the event kind derived from SourceKind; only
	// statement, tool_result, web and fact_assertion are accepted.
	EventKind core.EventKind
	// MaxZone is the ceiling of the calling channel (decision 0090); empty
	// means no ceiling. The zone is ZoneFor(SourceKind, MaxZone).
	MaxZone core.Zone
	// OnEvent, when set, runs inside the transaction (see OnEventFunc).
	OnEvent OnEventFunc
}

// ZoneFor binds the zone of a write (step 2): the zone of the input channel
// kind, capped by the channel ceiling max (decision 0090). An empty max
// means no ceiling; an unknown max gives external, the safe default.
func ZoneFor(kind core.SourceKind, max core.Zone) core.Zone {
	if max == "" {
		return core.ZoneForSource(kind)
	}
	return core.MinZone(core.ZoneForSource(kind), max)
}

// Remember writes one free-text event (write steps 1–5, 7, 11).
func (w *Writer) Remember(ctx context.Context, in RememberInput) (Result, error) {
	ns := w.namespace(in.Namespace)
	if err := checkActor(in.Actor); err != nil {
		return Result{}, err
	}
	if err := checkSourceKind(in.SourceKind); err != nil {
		return Result{}, err
	}
	validFrom, err := optTime(in.ValidFrom)
	if err != nil {
		return Result{}, err
	}
	evKind := eventKindFor(in.SourceKind)
	if in.EventKind != "" {
		switch in.EventKind {
		case core.EventStatement, core.EventToolResult, core.EventWeb, core.EventFact:
			evKind = in.EventKind
		default:
			return Result{}, core.Errf(core.CodeInvalidArgument, "event kind %q cannot be written by remember", in.EventKind)
		}
	}
	uri := in.Source
	if uri == "" {
		uri = string(in.SourceKind) + ":" + in.Actor
	}
	zone := ZoneFor(in.SourceKind, in.MaxZone) // step 2: from the channel, never the content
	adm, err := w.adm.Admit(in.Content, zone)
	if err != nil {
		return Result{Action: ActionRejected, Reason: RejectReason(err)}, err
	}

	var res Result
	err = w.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		now := w.now()
		src, err := w.source(ctx, tx, ns, in.SourceKind, uri, in.Actor, now, zone)
		if err != nil {
			return err
		}
		if id, err := w.findDuplicate(ctx, tx, ns, src.id, adm.Hash, zone); err != nil {
			return err
		} else if id != "" {
			aid, err := w.auditDuplicate(ctx, tx, ns, in.Actor, id, src.id)
			res = Result{ID: id, Action: ActionDuplicate, Reason: ReasonExactDuplicate, Flags: adm.Flags, AuditID: aid}
			if err != nil || in.OnEvent == nil {
				return err
			}
			ev, err := LoadEvent(ctx, tx, id)
			if err != nil {
				return err
			}
			return in.OnEvent(ctx, tx, ev, false)
		}
		ev := Event{
			Namespace: ns, SourceID: src.id, Kind: evKind,
			Content: adm.Content, ContentHash: adm.Hash, TrustZone: zone, Actor: in.Actor,
			RecordedAt: now, ValidFrom: validFrom, SessionID: in.SessionID, Flags: adm.Flags,
		}
		var first EventHookFunc
		if in.OnEvent != nil {
			first = func(ctx context.Context, tx *sql.Tx, ev Event) error { return in.OnEvent(ctx, tx, ev, true) }
		}
		aid, err := w.createEvent(ctx, tx, &ev, adm.InstructionRules, first)
		res = Result{ID: ev.ID, Action: ActionCreated, Reason: ReasonNewEvent, Flags: adm.Flags, AuditID: aid}
		return err
	})
	if err != nil {
		return Result{}, coreErr(err)
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Shared steps.

type source struct {
	id   string
	zone core.Zone
}

// source upserts the source (namespace, kind, uri) and returns it. A new
// source records zone, the zone bound for the write that created it (step 2,
// capped by the channel ceiling); events carry their own authoritative zone.
func (w *Writer) source(ctx context.Context, tx *sql.Tx, ns string, kind core.SourceKind, uri, actor, now string, zone core.Zone) (source, error) {
	var s source
	err := tx.QueryRowContext(ctx,
		`SELECT id, trust_zone FROM sources WHERE namespace = ? AND kind = ? AND uri = ?`, ns, string(kind), uri).
		Scan(&s.id, &s.zone)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	s = source{id: w.e.IDs.For(core.RecSource), zone: zone}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sources(id, namespace, kind, uri, trust_zone, created_at) VALUES (?,?,?,?,?,?)`,
		s.id, ns, string(kind), uri, string(s.zone), now); err != nil {
		return s, err
	}
	_, err = w.e.Audit.Append(ctx, tx, audit.Entry{
		Namespace: ns, Actor: actor, Action: ActSourceCreate, Target: s.id,
		Details: struct {
			Kind string `json:"kind"`
			Zone string `json:"zone"`
		}{string(kind), string(s.zone)},
	})
	return s, err
}

// findDuplicate returns the id of a live event of the same source with the
// same content hash and zone (step 4), or "". The zone is compared because
// channels with different ceilings may write to the same source: a capped
// copy is a new event, never an alias of a higher-zone one.
func (w *Writer) findDuplicate(ctx context.Context, tx *sql.Tx, ns, sourceID, hash string, zone core.Zone) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM events WHERE namespace = ? AND source_id = ? AND content_hash = ? AND trust_zone = ?
		   AND retired_at IS NULL ORDER BY id LIMIT 1`, ns, sourceID, hash, string(zone)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (w *Writer) auditDuplicate(ctx context.Context, tx *sql.Tx, ns, actor, id, sourceID string) (int64, error) {
	return w.e.Audit.Append(ctx, tx, audit.Entry{
		Namespace: ns, Actor: actor, Action: audit.ActDuplicate, Target: id,
		Details: struct {
			SourceID string `json:"source_id"`
		}{sourceID},
	})
}

// writeAudit is the audit detail of a written event: ids and labels, never content.
type writeAudit struct {
	SourceID         string   `json:"source_id"`
	Kind             string   `json:"kind"`
	Zone             string   `json:"zone"`
	Flags            []string `json:"flags,omitempty"`
	InstructionRules []string `json:"instruction_rules,omitempty"`
	DocID            string   `json:"doc_id,omitempty"`
	DocVersion       *int     `json:"doc_version,omitempty"`
	Ordinal          *int     `json:"ordinal,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
}

// createEvent assigns an id, inserts the raw event (step 5), enqueues it for
// embedding (step 7; FTS is trigger-maintained), audits it, runs first (the
// call's own step, when not nil) and then the hooks.
func (w *Writer) createEvent(ctx context.Context, tx *sql.Tx, ev *Event, rules []string, first EventHookFunc) (int64, error) {
	ev.ID = w.e.IDs.For(core.RecEvent)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(id, namespace, source_id, kind, content, content_hash, trust_zone, actor, recorded_at,
		                    valid_from, doc_id, doc_version, ordinal, heading_path, session_id, flags)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.ID, ev.Namespace, ev.SourceID, string(ev.Kind), ev.Content, ev.ContentHash, string(ev.TrustZone),
		ev.Actor, ev.RecordedAt, nullStr(ev.ValidFrom), nullStr(ev.DocID), nullInt(ev.DocVersion),
		nullInt(ev.Ordinal), nullStr(ev.HeadingPath), nullStr(ev.SessionID), strings.Join(ev.Flags, ",")); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at) VALUES (?,?,?,?)
		 ON CONFLICT(record_type, record_id) DO NOTHING`,
		ev.Namespace, string(core.RecEvent), ev.ID, ev.RecordedAt); err != nil {
		return 0, err
	}
	aid, err := w.e.Audit.Append(ctx, tx, audit.Entry{
		Namespace: ev.Namespace, Actor: ev.Actor, Action: audit.ActWrite, Target: ev.ID,
		Details: writeAudit{
			SourceID: ev.SourceID, Kind: string(ev.Kind), Zone: string(ev.TrustZone), Flags: ev.Flags,
			InstructionRules: rules, DocID: ev.DocID, DocVersion: ev.DocVersion, Ordinal: ev.Ordinal,
			SessionID: ev.SessionID,
		},
	})
	if err != nil {
		return 0, err
	}
	if first != nil {
		if err := first(ctx, tx, *ev); err != nil {
			return 0, err
		}
	}
	for _, h := range w.hooks {
		if err := h.AfterEvent(ctx, tx, *ev); err != nil {
			return 0, err
		}
	}
	return aid, nil
}

// LoadEvent reads one event by id. Missing ids return CodeNotFound.
func LoadEvent(ctx context.Context, q store.Queryer, id string) (Event, error) {
	var ev Event
	var kind, zone, flags string
	var validFrom, docID, heading, session, retired sql.NullString
	var docVersion, ordinal sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT id, namespace, source_id, kind, content, content_hash, trust_zone, actor, recorded_at,
		        valid_from, doc_id, doc_version, ordinal, heading_path, session_id, flags, retired_at
		 FROM events WHERE id = ?`, id).
		Scan(&ev.ID, &ev.Namespace, &ev.SourceID, &kind, &ev.Content, &ev.ContentHash, &zone, &ev.Actor,
			&ev.RecordedAt, &validFrom, &docID, &docVersion, &ordinal, &heading, &session, &flags, &retired)
	if errors.Is(err, sql.ErrNoRows) {
		return ev, core.Errf(core.CodeNotFound, "event %s not found", id)
	}
	if err != nil {
		return ev, coreErr(err)
	}
	ev.Kind, ev.TrustZone = core.EventKind(kind), core.Zone(zone)
	ev.ValidFrom, ev.DocID, ev.HeadingPath, ev.SessionID, ev.RetiredAt =
		validFrom.String, docID.String, heading.String, session.String, retired.String
	if docVersion.Valid {
		v := int(docVersion.Int64)
		ev.DocVersion = &v
	}
	if ordinal.Valid {
		v := int(ordinal.Int64)
		ev.Ordinal = &v
	}
	if flags != "" {
		ev.Flags = strings.Split(flags, ",")
	}
	return ev, nil
}

// ---------------------------------------------------------------------------
// Small helpers.

func (w *Writer) now() string { return core.FormatTime(w.e.Clock.Now()) }

func (w *Writer) namespace(ns string) string {
	if ns == "" {
		return w.e.Cfg.Namespace
	}
	return ns
}

func checkActor(actor string) error {
	if strings.TrimSpace(actor) == "" {
		return core.Errf(core.CodeInvalidArgument, "actor is required")
	}
	return nil
}

func checkSourceKind(k core.SourceKind) error {
	switch k {
	case core.SourceDocument, core.SourceSession, core.SourceTool, core.SourceWeb, core.SourceUserStatement:
		return nil
	}
	return core.Errf(core.CodeInvalidArgument, "unknown source kind %q", string(k))
}

// eventKindFor maps the input channel of free text to the event kind.
func eventKindFor(k core.SourceKind) core.EventKind {
	switch k {
	case core.SourceTool:
		return core.EventToolResult
	case core.SourceWeb:
		return core.EventWeb
	}
	return core.EventStatement
}

// optTime parses an optional time into core.TimeLayout ("" stays "").
func optTime(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	t, err := core.ParseTime(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	return core.FormatTime(t), nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func intp(v int) *int { return &v }

// coreErr gives every error leaving the package a core.Code. Append-only
// trigger violations become CodeImmutable.
func coreErr(err error) error {
	if err == nil {
		return nil
	}
	var ce *core.Error
	if errors.As(err, &ce) {
		return err
	}
	if store.IsImmutableViolation(err) {
		return store.AsCoreError(err)
	}
	return core.Wrap(core.CodeInternal, err, "write")
}
