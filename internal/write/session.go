package write

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
)

// Turn roles accepted by LogSession.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

// Turn is one turn of an agent session.
type Turn struct {
	Role     string // user | assistant | system | tool
	Content  string
	ToolName string // for tool turns; names the external tool source
	// TS is when the turn happened (core.ParseTime formats: RFC 3339 or
	// YYYY-MM-DD). It becomes the event's valid_from, the date given in the
	// call (BASE.md); recorded_at stays the write time. Empty: no valid_from.
	TS string
}

// SessionResult reports one Result per input turn, in input order.
type SessionResult struct {
	SessionID  string   `json:"session_id"`
	Turns      []Result `json:"turns"`
	Created    int      `json:"created"`
	Duplicates int      `json:"duplicates"`
	Rejected   int      `json:"rejected"`
}

// LogSession writes the turns of a session in one transaction. User,
// assistant and system turns come from the session source (kind session,
// uri = sessionID, zone internal) as events of kind turn; tool turns come from
// a tool source (kind tool, uri "tool:<ToolName>", zone external) as events
// of kind tool_result, so instruction-like tool output is marked. Every
// event keeps the turn's role in events.role (decision 0137).
//
// The ordinal of each event is the turn's index in turns. A turn already
// logged for this session at the same index with the same content is a
// duplicate (re-logging a session is idempotent); the same text at another
// index is a new turn. Turns refused by admission are reported with
// ActionRejected and do not fail the call. A turn whose TS does not parse
// fails the whole call with invalid_argument, before anything is written.
// A duplicate turn keeps its first event, whatever TS the new call gives.
func (w *Writer) LogSession(ctx context.Context, ns, sessionID string, turns []Turn, actor string) (SessionResult, error) {
	return w.LogSessionWith(ctx, SessionInput{Namespace: ns, SessionID: sessionID, Turns: turns, Actor: actor})
}

// SessionInput is LogSession with every option.
type SessionInput struct {
	Namespace string
	SessionID string
	Turns     []Turn
	Actor     string
	MaxZone   core.Zone // channel ceiling (decision 0090); empty: none
}

// LogSessionWith is LogSession; each turn's zone is also capped by MaxZone.
func (w *Writer) LogSessionWith(ctx context.Context, in SessionInput) (SessionResult, error) {
	ns, sessionID, turns, actor := in.Namespace, in.SessionID, in.Turns, in.Actor
	res := SessionResult{SessionID: sessionID}
	ns = w.namespace(ns)
	if err := checkActor(actor); err != nil {
		return res, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return res, core.Errf(core.CodeInvalidArgument, "session_id is required")
	}
	type plan struct {
		kind    core.SourceKind
		uri     string
		evKind  core.EventKind
		ts      string // valid_from, core.TimeLayout; "" when the turn has no TS
		adm     Admitted
		rejectR string
	}
	plans := make([]plan, len(turns))
	for i, t := range turns {
		p := &plans[i]
		switch t.Role {
		case RoleUser, RoleAssistant, RoleSystem:
			p.kind, p.uri, p.evKind = core.SourceSession, sessionID, core.EventTurn
		case RoleTool:
			name := strings.TrimSpace(t.ToolName)
			if name == "" {
				name = "unknown"
			}
			p.kind, p.uri, p.evKind = core.SourceTool, "tool:"+name, core.EventToolResult
		default:
			return res, core.Errf(core.CodeInvalidArgument, "turn %d: unknown role %q", i, t.Role)
		}
		ts, err := optTime(t.TS)
		if err != nil {
			return res, core.Errf(core.CodeInvalidArgument, "turn %d: invalid ts %q", i, t.TS)
		}
		p.ts = ts
		adm, err := w.adm.Admit(t.Content, ZoneFor(p.kind, in.MaxZone))
		if err != nil {
			p.rejectR = RejectReason(err)
			continue
		}
		p.adm = adm
	}

	err := w.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		now := w.now()
		res.Turns = make([]Result, len(plans))
		res.Created, res.Duplicates, res.Rejected = 0, 0, 0
		for i, p := range plans {
			if p.rejectR != "" {
				res.Turns[i] = Result{Action: ActionRejected, Reason: p.rejectR}
				res.Rejected++
				continue
			}
			zone := ZoneFor(p.kind, in.MaxZone)
			src, err := w.source(ctx, tx, ns, p.kind, p.uri, actor, now, zone)
			if err != nil {
				return err
			}
			var dup string
			err = tx.QueryRowContext(ctx,
				`SELECT id FROM events WHERE namespace = ? AND source_id = ? AND content_hash = ? AND session_id = ?
				   AND ordinal = ? AND trust_zone = ? AND retired_at IS NULL ORDER BY id LIMIT 1`,
				ns, src.id, p.adm.Hash, sessionID, i, string(zone)).Scan(&dup)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if dup != "" {
				aid, err := w.auditDuplicate(ctx, tx, ns, actor, dup, src.id)
				if err != nil {
					return err
				}
				res.Turns[i] = Result{ID: dup, Action: ActionDuplicate, Reason: ReasonExactDuplicate, Flags: p.adm.Flags, AuditID: aid}
				res.Duplicates++
				continue
			}
			ev := Event{
				Namespace: ns, SourceID: src.id, Kind: p.evKind, Content: p.adm.Content, ContentHash: p.adm.Hash,
				TrustZone: zone, Actor: actor, RecordedAt: now, ValidFrom: p.ts, Ordinal: intp(i),
				SessionID: sessionID, Role: turns[i].Role, Flags: p.adm.Flags,
			}
			aid, err := w.createEvent(ctx, tx, &ev, p.adm.InstructionRules, nil)
			if err != nil {
				return err
			}
			res.Turns[i] = Result{ID: ev.ID, Action: ActionCreated, Reason: ReasonNewEvent, Flags: p.adm.Flags, AuditID: aid}
			res.Created++
		}
		return nil
	})
	if err != nil {
		return SessionResult{SessionID: sessionID}, coreErr(err)
	}
	return res, nil
}
