package read

import (
	"context"
	"database/sql"
	"strings"

	"github.com/felipemarts/chanda/internal/core"
)

// Record is one stored record as returned by Get. Unlike search, Get shows
// a record whatever its status (retired, superseded, quarantined, flagged):
// it is inspection by id, and the status fields say where it stands.
type Record struct {
	ID           string          `json:"id"`
	Type         core.RecordType `json:"type"`
	Kind         string          `json:"kind,omitempty"`
	Title        string          `json:"title,omitempty"`
	Text         string          `json:"text"`
	Status       string          `json:"status,omitempty"`
	Subject      string          `json:"subject,omitempty"`
	Predicate    string          `json:"predicate,omitempty"`
	Value        string          `json:"value,omitempty"`
	ValidFrom    string          `json:"valid_from,omitempty"`
	ValidTo      string          `json:"valid_to,omitempty"`
	SupersededBy string          `json:"superseded_by,omitempty"`
	RecordedAt   string          `json:"recorded_at,omitempty"`
	RetiredAt    string          `json:"retired_at,omitempty"`
	Zone         core.Zone       `json:"zone"`
	Flags        []string        `json:"flags,omitempty"`
	Flagged      bool            `json:"flagged,omitempty"` // has a row in flags
	SourceID     string          `json:"source_id,omitempty"`
	DocID        string          `json:"doc_id,omitempty"`
	DocVersion   int64           `json:"doc_version,omitempty"`
	Ordinal      int64           `json:"ordinal,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	PageID       string          `json:"page_id,omitempty"` // page items
}

// GetResult is a record with its origins and, on request, its neighbours.
type GetResult struct {
	Record       Record   `json:"record"`
	Origin       Origin   `json:"origin"`
	Edges        []Edge   `json:"edges,omitempty"`         // provenance walked down to events
	OriginEvents []Record `json:"origin_events,omitempty"` // for derived records
	Previous     *Record  `json:"previous,omitempty"`
	Next         *Record  `json:"next,omitempty"`
}

// Get returns one record by id, routed by its id prefix, with its origins
// (provenance edges followed recursively down to events). With
// expandNeighbors, an event also returns the previous and next chunks of the
// same document version (or the previous and next turns of its session).
func (r *Reader) Get(ctx context.Context, ns, id string, expandNeighbors bool) (GetResult, error) {
	if ns == "" {
		ns = r.e.Cfg.Namespace
	}
	typ := core.RecordTypeOf(id)
	var rec Record
	var err error
	switch typ {
	case core.RecEvent, core.RecFact, core.RecPage:
		rec, err = r.getRecord(ctx, ns, typ, id)
	case core.RecPageItem:
		rec, err = r.getPageItem(ctx, ns, id)
	default:
		return GetResult{}, core.Errf(core.CodeInvalidArgument, "unsupported id %q", id)
	}
	if err != nil {
		return GetResult{}, err
	}
	out := GetResult{Record: rec}
	evIDs, edges, err := r.originEvents(ctx, ns, typ, id)
	if err != nil {
		return GetResult{}, err
	}
	out.Edges = edges
	recs, err := r.loadRecords(ctx, ns, map[core.RecordType][]string{core.RecEvent: evIDs})
	if err != nil {
		return GetResult{}, err
	}
	srcs, docs := map[string]bool{}, map[string]bool{}
	for _, eid := range evIDs {
		ev := recs[eid]
		if ev == nil {
			continue
		}
		out.Origin.EventIDs = append(out.Origin.EventIDs, eid)
		srcs[ev.sourceID] = true
		if ev.docID != "" {
			docs[ev.docID] = true
		}
		if typ != core.RecEvent {
			out.OriginEvents = append(out.OriginEvents, toRecord(ev))
		}
	}
	out.Origin.DocIDs = sortedKeys(docs)
	if out.Origin.SourceURIs, err = r.uris(ctx, "sources", ns, sortedKeys(srcs)); err != nil {
		return GetResult{}, err
	}
	if out.Origin.DocURIs, err = r.uris(ctx, "documents", ns, out.Origin.DocIDs); err != nil {
		return GetResult{}, err
	}
	if expandNeighbors && typ == core.RecEvent {
		if out.Previous, out.Next, err = r.neighbors(ctx, ns, rec); err != nil {
			return GetResult{}, err
		}
	}
	return out, nil
}

func (r *Reader) getRecord(ctx context.Context, ns string, typ core.RecordType, id string) (Record, error) {
	recs, err := r.loadRecords(ctx, ns, map[core.RecordType][]string{typ: {id}})
	if err != nil {
		return Record{}, err
	}
	rec := recs[id]
	if rec == nil || rec.ns != ns {
		return Record{}, core.Errf(core.CodeNotFound, "record %s not found", id)
	}
	out := toRecord(rec)
	if typ == core.RecFact {
		var sup sql.NullString
		err := r.e.Store.DB().QueryRowContext(ctx, `SELECT value, superseded_by FROM facts WHERE namespace = ? AND id = ?`, ns, id).
			Scan(&out.Value, &sup)
		if err != nil {
			return Record{}, core.Wrap(core.CodeInternal, err, "fact")
		}
		out.SupersededBy = sup.String
	}
	return out, nil
}

func (r *Reader) getPageItem(ctx context.Context, ns, id string) (Record, error) {
	out := Record{ID: id, Type: core.RecPageItem}
	var zone string
	err := r.e.Store.DB().QueryRowContext(ctx, `SELECT page_id, text, status, trust_zone, created_at FROM page_items WHERE namespace = ? AND id = ?`,
		ns, id).Scan(&out.PageID, &out.Text, &out.Status, &zone, &out.RecordedAt)
	if err == sql.ErrNoRows {
		return Record{}, core.Errf(core.CodeNotFound, "record %s not found", id)
	}
	if err != nil {
		return Record{}, core.Wrap(core.CodeInternal, err, "page item")
	}
	out.Zone = core.Zone(zone)
	return out, nil
}

func toRecord(rec *record) Record {
	out := Record{
		ID: rec.id, Type: rec.typ, Kind: rec.kind, Title: rec.title, Text: rec.text, Status: rec.status,
		Subject: rec.subject, Predicate: rec.predicate, ValidFrom: rec.validFrom, ValidTo: rec.validTo,
		RecordedAt: rec.ts, RetiredAt: rec.retiredAt, Zone: rec.zone, Flags: rec.flags, Flagged: rec.flagged,
		SourceID: rec.sourceID, DocID: rec.docID, DocVersion: rec.docVersion.Int64, Ordinal: rec.ordinal.Int64,
		SessionID: rec.sessionID,
	}
	return out
}

// neighbors finds the previous and next chunk of the event in the same
// document version: the current version if the chunk belongs to it, else the
// latest version that contains it. Session turns use the session order.
func (r *Reader) neighbors(ctx context.Context, ns string, ev Record) (*Record, *Record, error) {
	db := r.e.Store.DB()
	var prevID, nextID string
	switch {
	case ev.DocID != "":
		var version, ordinal int64
		err := db.QueryRowContext(ctx, `SELECT dc.version, dc.ordinal FROM document_chunks dc JOIN documents d ON d.id = dc.doc_id
			WHERE dc.namespace = ? AND dc.doc_id = ? AND dc.event_id = ?
			ORDER BY (dc.version = d.current_version) DESC, dc.version DESC, dc.ordinal LIMIT 1`, ns, ev.DocID, ev.ID).Scan(&version, &ordinal)
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, core.Wrap(core.CodeInternal, err, "document chunks")
		}
		prevID, err = r.first(ctx, `SELECT event_id FROM document_chunks WHERE namespace = ? AND doc_id = ? AND version = ? AND ordinal < ?
			ORDER BY ordinal DESC, event_id LIMIT 1`, ns, ev.DocID, version, ordinal)
		if err != nil {
			return nil, nil, err
		}
		nextID, err = r.first(ctx, `SELECT event_id FROM document_chunks WHERE namespace = ? AND doc_id = ? AND version = ? AND ordinal > ?
			ORDER BY ordinal, event_id LIMIT 1`, ns, ev.DocID, version, ordinal)
		if err != nil {
			return nil, nil, err
		}
	case ev.SessionID != "":
		var err error
		prevID, err = r.first(ctx, `SELECT id FROM events WHERE namespace = ? AND session_id = ? AND (ordinal < ? OR (ordinal = ? AND id < ?))
			ORDER BY ordinal DESC, id DESC LIMIT 1`, ns, ev.SessionID, ev.Ordinal, ev.Ordinal, ev.ID)
		if err != nil {
			return nil, nil, err
		}
		nextID, err = r.first(ctx, `SELECT id FROM events WHERE namespace = ? AND session_id = ? AND (ordinal > ? OR (ordinal = ? AND id > ?))
			ORDER BY ordinal, id LIMIT 1`, ns, ev.SessionID, ev.Ordinal, ev.Ordinal, ev.ID)
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, nil
	}
	recs, err := r.loadRecords(ctx, ns, map[core.RecordType][]string{core.RecEvent: nonEmpty(prevID, nextID)})
	if err != nil {
		return nil, nil, err
	}
	var prev, next *Record
	if rec := recs[prevID]; rec != nil {
		x := toRecord(rec)
		prev = &x
	}
	if rec := recs[nextID]; rec != nil {
		x := toRecord(rec)
		next = &x
	}
	return prev, next, nil
}

func (r *Reader) first(ctx context.Context, q string, args ...any) (string, error) {
	xs, err := r.queryStrings(ctx, q, args...)
	if err != nil || len(xs) == 0 {
		return "", err
	}
	return xs[0], nil
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
