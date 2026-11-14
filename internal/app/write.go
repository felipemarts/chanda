package app

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts"
	"github.com/felipemarts/chanda-mcp/internal/write"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Reasons added by the App.
const (
	ReasonSecretInFact = "secret_in_fact" // assert_fact refused: the fact would store a secret
	ReasonNearDup      = "near_duplicate_of_existing"
)

func flagsOrEmpty(f []string) []string {
	if f == nil {
		return []string{}
	}
	return f
}

// Remember writes free text (write path steps 1 to 11).
func (a *App) Remember(ctx context.Context, in chanda.RememberInput) (chanda.RememberOutput, error) {
	c := a.begin(ctx, in.Namespace)
	kind := in.Source.Kind
	if kind == "" {
		kind = core.SourceSession
	}
	uri := in.Source.URI
	if uri == "" && kind == core.SourceSession && in.SessionID != "" {
		uri = in.SessionID
	}
	c.clampNotice(kind)
	r, err := a.writer.Remember(ctx, write.RememberInput{
		Namespace: c.ns, Content: in.Content, Source: uri, SourceKind: kind, Actor: c.actor.Name,
		ValidFrom: in.ValidFrom, SessionID: in.SessionID, MaxZone: c.ceiling,
	})
	out := chanda.RememberOutput{Meta: c.meta(r.AuditID)}
	if err != nil {
		return out, coded(err)
	}
	out.WriteResult = chanda.WriteResult{ID: r.ID, Action: r.Action, Reason: r.Reason, Flags: flagsOrEmpty(r.Flags)}
	if r.Action == write.ActionCreated {
		if orig, err := a.duplicateOf(ctx, r.ID); err != nil {
			return out, coded(err)
		} else if orig != "" {
			out.Action, out.Reason, out.Related = chanda.ActionNearDuplicate, ReasonNearDup, []string{orig}
		}
	}
	return out, nil
}

// duplicateOf returns the record a new event was marked a near-duplicate of.
func (a *App) duplicateOf(ctx context.Context, id string) (string, error) {
	var orig string
	err := a.e.Store.DB().QueryRowContext(ctx, `SELECT source_id FROM provenance WHERE derived_id = ? AND relation = ?
		ORDER BY source_id LIMIT 1`, id, core.RelDuplicateOf).Scan(&orig)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return orig, err
}

var factActions = map[facts.Action]string{
	facts.ActionInserted:    chanda.ActionCreated,
	facts.ActionConfirmed:   chanda.ActionConfirmed,
	facts.ActionSuperseded:  chanda.ActionSuperseded,
	facts.ActionHistory:     chanda.ActionHistory,
	facts.ActionQuarantined: chanda.ActionQuarantined,
	facts.ActionCoexist:     chanda.ActionCoexist,
}

// AssertFact writes a raw event of kind fact_assertion whose readable
// content is "subject predicate: value", then reconciles the fact with that
// event as its origin, in the same transaction (decision 0091). An exact
// duplicate of an earlier assertion from the same source reuses that event
// as the origin, so the fact is still reconciled (with this call's date).
func (a *App) AssertFact(ctx context.Context, in chanda.AssertFactInput) (chanda.AssertFactOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.AssertFactOutput{}
	if a.opt.DisableFacts {
		out.Meta = c.meta(0)
		return out, core.Errf(core.CodeUnavailable, "assert_fact: facts are disabled in this configuration")
	}
	subject, pred, value := strings.TrimSpace(in.Subject), strings.TrimSpace(in.Predicate), strings.TrimSpace(in.Value)
	if subject == "" || pred == "" || value == "" {
		out.Meta = c.meta(0)
		return out, core.Errf(core.CodeInvalidArgument, "subject, predicate and value are required")
	}
	kind := in.Source.Kind
	if kind == "" {
		kind = core.SourceSession
	}
	uri := in.Source.URI
	if uri == "" && kind == core.SourceSession && in.SessionID != "" {
		uri = in.SessionID
	}
	validFrom := strings.TrimSpace(in.ValidFrom)
	if validFrom == "" && in.SessionID != "" {
		var err error
		if validFrom, err = a.sessionDate(ctx, c.ns, in.SessionID); err != nil {
			out.Meta = c.meta(0)
			return out, coded(err)
		}
	}
	if validFrom == "" {
		validFrom = core.FormatTime(a.e.Clock.Now()) // "default the time of the call"
	} else if t, err := core.ParseTime(validFrom); err != nil {
		out.Meta = c.meta(0)
		return out, err
	} else {
		validFrom = core.FormatTime(t)
	}
	content := subject + " " + pred + ": " + value
	if a.writer.Admitter().MaskSecrets(content) != content {
		out.Meta = c.meta(0)
		return out, core.Errf(core.CodeRejected, "%s", ReasonSecretInFact)
	}
	c.clampNotice(kind)

	var fr facts.AssertResult
	r, err := a.writer.Remember(ctx, write.RememberInput{
		Namespace: c.ns, Content: content, Source: uri, SourceKind: kind, Actor: c.actor.Name,
		ValidFrom: validFrom, SessionID: in.SessionID, MaxZone: c.ceiling, EventKind: core.EventFact,
		OnEvent: func(ctx context.Context, tx *sql.Tx, ev write.Event, _ bool) error {
			var err error
			fr, err = a.facts.Assert(ctx, tx, facts.AssertInput{
				Namespace: c.ns, Subject: subject, Predicate: pred, Value: value, ValidFrom: validFrom,
				OriginEventIDs: []string{ev.ID}, Actor: c.actor.Name,
			})
			if err != nil || !newFact(fr) {
				return err
			}
			if err := a.enqueueFacts(ctx, tx, c.ns, []string{fr.FactID}); err != nil {
				return err
			}
			return a.checkDerived(ctx, tx, c.ns, []string{fr.FactID})
		},
	})
	out.Meta = c.meta(max(r.AuditID, fr.AuditID))
	if err != nil {
		return out, coded(err)
	}
	out.WriteResult = chanda.WriteResult{
		ID: fr.FactID, Action: factActions[fr.Action], Reason: fr.Reason, Flags: flagsOrEmpty(r.Flags), Related: fr.Closed,
	}
	if out.Action == "" {
		out.Action = string(fr.Action)
	}
	return out, nil
}

// sessionDate is the date of a session as its turns gave it: the latest
// valid_from among the live events of the session, or "" when no turn of the
// session carried a ts. An assert_fact without valid_from inside a dated
// session takes it, so a session replayed out of date order still dates its
// facts by the session, not by the call (decision 0135).
func (a *App) sessionDate(ctx context.Context, ns, sessionID string) (string, error) {
	var d sql.NullString
	err := a.e.Store.DB().QueryRowContext(ctx, `SELECT MAX(valid_from) FROM events
		WHERE namespace = ? AND session_id = ? AND valid_from IS NOT NULL AND retired_at IS NULL`, ns, sessionID).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil
}

// Ingest ingests a file or folder. A remote (MCP) caller may only read
// under write.ingest_roots, with symlinks resolved; the CLI and Go callers
// may read anywhere.
func (a *App) Ingest(ctx context.Context, in chanda.IngestInput) (chanda.IngestOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.IngestOutput{Documents: []chanda.DocumentResult{}}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		out.Meta = c.meta(0)
		return out, core.Errf(core.CodeInvalidArgument, "path is required")
	}
	if c.actor.Remote {
		p, err := a.allowedPath(path)
		if err != nil {
			out.Meta = c.meta(0)
			return out, err
		}
		path = p
	}
	kind := in.Source.Kind
	if kind == "" {
		kind = core.SourceDocument
	}
	c.clampNotice(kind)
	r, err := a.writer.IngestPathWith(ctx, write.PathInput{
		Namespace: c.ns, Path: path, SourceURI: in.Source.URI, SourceKind: kind, Actor: c.actor.Name, MaxZone: c.ceiling,
	})
	var auditID int64
	for _, d := range r.Documents {
		auditID = max(auditID, d.AuditID)
		out.Documents = append(out.Documents, chanda.DocumentResult{
			URI: d.URI, DocID: d.DocID, Version: d.Version, Action: d.Action, Reason: d.Reason,
			ChunksAdded: len(d.Created), ChunksKept: len(d.Kept), ChunksRetired: len(d.Retired), FactsStale: len(d.StaleFacts),
		})
		if d.Action == write.ActionRejected {
			c.notice("document_rejected: " + d.URI + " (" + d.Reason + ")")
		}
	}
	sort.SliceStable(out.Documents, func(i, j int) bool { return out.Documents[i].URI < out.Documents[j].URI })
	out.Meta = c.meta(auditID)
	return out, coded(err)
}

// LogSession writes the turns of a session; tool turns enter the external
// zone under the source tool:<name>. A turn's ts becomes its event's
// valid_from (decision 0135); an unparsable ts fails the call.
func (a *App) LogSession(ctx context.Context, in chanda.LogSessionInput) (chanda.LogSessionOutput, error) {
	c := a.begin(ctx, in.Namespace)
	turns := make([]write.Turn, len(in.Turns))
	for i, t := range in.Turns {
		turns[i] = write.Turn{Role: t.Role, Content: t.Content, ToolName: t.Name, TS: t.TS}
	}
	c.clampNotice(core.SourceSession)
	r, err := a.writer.LogSessionWith(ctx, write.SessionInput{
		Namespace: c.ns, SessionID: in.SessionID, Turns: turns, Actor: c.actor.Name, MaxZone: c.ceiling,
	})
	out := chanda.LogSessionOutput{SessionID: in.SessionID, Turns: []chanda.TurnResult{}}
	var auditID int64
	for i, t := range r.Turns {
		auditID = max(auditID, t.AuditID)
		out.Turns = append(out.Turns, chanda.TurnResult{Ordinal: i, WriteResult: chanda.WriteResult{
			ID: t.ID, Action: t.Action, Reason: t.Reason, Flags: flagsOrEmpty(t.Flags),
		}})
	}
	out.Meta = c.meta(auditID)
	if err != nil {
		return out, coded(err)
	}
	return out, nil
}
