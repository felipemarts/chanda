package write

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/write/normalize"
)

// IngestInput is one document to ingest (write steps 1–7 and 11).
type IngestInput struct {
	Namespace  string          // empty: cfg.Namespace
	URI        string          // document uri, unique per namespace (required)
	Content    string          // raw Markdown or plain text
	SourceURI  string          // source uri; empty: URI
	SourceKind core.SourceKind // empty: document (internal); web for fetched pages (external)
	Actor      string          // required
	DocDate    string          // optional; overrides the front matter "date:"
	MaxZone    core.Zone       // channel ceiling (decision 0090); empty: none
}

// IngestResult reports what one document ingest did. Id lists are in
// document order (Created, Kept) or previous-version order (Retired).
type IngestResult struct {
	DocID      string   `json:"doc_id,omitempty"`
	URI        string   `json:"uri"`
	Version    int      `json:"version,omitempty"`
	Action     string   `json:"action"` // created | updated | unchanged | rejected
	Reason     string   `json:"reason"`
	Created    []string `json:"created,omitempty"`     // new chunk events
	Kept       []string `json:"kept,omitempty"`        // chunk events reused from the previous version
	Retired    []string `json:"retired,omitempty"`     // chunk events that disappeared (retired_at set)
	StaleFacts []string `json:"stale_facts,omitempty"` // facts moved to stale_source
	Flags      []string `json:"flags,omitempty"`       // union of chunk flags
	AuditID    int64    `json:"audit_id,omitempty"`
}

type docRow struct {
	id, sourceID, hash string
	version            int
}

// IngestDocument writes a document version. If its normalized full text has
// the same hash as the current version, nothing changes. Otherwise a new
// version is written: chunks whose hash exists in the previous version are
// kept (same event id), new chunks become new events, and chunks that
// disappeared are retired; facts whose every event origin is now retired
// become stale_source (BASE.md step 6).
func (w *Writer) IngestDocument(ctx context.Context, in IngestInput) (IngestResult, error) {
	res := IngestResult{URI: in.URI}
	ns := w.namespace(in.Namespace)
	kind := in.SourceKind
	if kind == "" {
		kind = core.SourceDocument
	}
	if err := checkActor(in.Actor); err != nil {
		return res, err
	}
	if err := checkSourceKind(kind); err != nil {
		return res, err
	}
	if strings.TrimSpace(in.URI) == "" {
		return res, core.Errf(core.CodeInvalidArgument, "document uri is required")
	}
	docDate, err := optTime(in.DocDate)
	if err != nil {
		return res, err
	}
	sourceURI := in.SourceURI
	if sourceURI == "" {
		sourceURI = in.URI
	}

	// Steps 1–3 at document level: normalize, length rules, secret masking.
	text := normalize.Text(in.Content)
	if r := w.adm.lengthReason(text); r != "" {
		res.Action, res.Reason = ActionRejected, r
		return res, rejected(r)
	}
	fmDate, body := frontMatter(text)
	if docDate == "" {
		docDate = fmDate
	}
	zone := ZoneFor(kind, in.MaxZone)
	fullHash := normalize.Hash(w.adm.MaskSecrets(text))
	maskedBody := w.adm.MaskSecrets(body)
	docMasked := maskedBody != body
	chunks := ChunkText(maskedBody, w.e.Cfg.Write.MaxChunkChars)

	err = w.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		now := w.now()
		src, err := w.source(ctx, tx, ns, kind, sourceURI, in.Actor, now, zone)
		if err != nil {
			return err
		}
		doc, found, err := loadDocument(ctx, tx, ns, in.URI)
		if err != nil {
			return err
		}
		if found && doc.sourceID != src.id {
			return core.Errf(core.CodeConflict, "document %s belongs to another source", in.URI)
		}
		var prev []prevChunk
		if found {
			if prev, err = versionChunks(ctx, tx, doc.id, doc.version); err != nil {
				return err
			}
		}
		if found && doc.hash == fullHash {
			res.DocID, res.Version, res.Action, res.Reason = doc.id, doc.version, ActionUnchanged, ReasonSameContent
			for _, p := range prev {
				res.Kept = append(res.Kept, p.eventID)
			}
			res.AuditID, err = w.auditDuplicate(ctx, tx, ns, in.Actor, doc.id, src.id)
			return err
		}

		version := 1
		res.Action, res.Reason = ActionCreated, ReasonNewDocument
		if found {
			version = doc.version + 1
			res.Action, res.Reason = ActionUpdated, ReasonNewVersion
			_, err = tx.ExecContext(ctx,
				`UPDATE documents SET current_version = ?, content_hash = ?, doc_date = ?, updated_at = ? WHERE id = ?`,
				version, fullHash, nullStr(docDate), now, doc.id)
		} else {
			doc.id = w.e.IDs.For(core.RecDocument)
			_, err = tx.ExecContext(ctx,
				`INSERT INTO documents(id, namespace, source_id, uri, current_version, doc_date, content_hash, updated_at)
				 VALUES (?,?,?,?,?,?,?,?)`, doc.id, ns, src.id, in.URI, version, nullStr(docDate), fullHash, now)
		}
		if err != nil {
			return err
		}
		res.DocID, res.Version = doc.id, version

		// Match chunks against the previous version by content hash, in order.
		byHash := map[string][]int{} // hash -> indexes into prev, ascending
		for i, p := range prev {
			byHash[p.hash] = append(byHash[p.hash], i)
		}
		used := make([]bool, len(prev))
		flags := map[string]bool{}
		for ord, c := range chunks {
			var eventID string
			if idx := byHash[c.Hash]; len(idx) > 0 {
				byHash[c.Hash] = idx[1:]
				used[idx[0]] = true
				eventID = prev[idx[0]].eventID
				res.Kept = append(res.Kept, eventID)
			} else {
				scr := w.adm.Screen(c.Text, zone)
				if docMasked && strings.Contains(c.Text, Redacted) {
					scr.Flags = addFlag(scr.Flags, core.FlagSecretsMasked)
				}
				ev := Event{
					Namespace: ns, SourceID: src.id, Kind: core.EventChunk, Content: scr.Content, ContentHash: scr.Hash,
					TrustZone: zone, Actor: in.Actor, RecordedAt: now, ValidFrom: docDate, DocID: doc.id, DocVersion: intp(version),
					Ordinal: intp(ord), HeadingPath: c.HeadingPath, Flags: scr.Flags,
				}
				if _, err := w.createEvent(ctx, tx, &ev, scr.InstructionRules, nil); err != nil {
					return err
				}
				eventID = ev.ID
				res.Created = append(res.Created, eventID)
				for _, f := range scr.Flags {
					flags[f] = true
				}
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO document_chunks(namespace, doc_id, version, ordinal, event_id) VALUES (?,?,?,?,?)`,
				ns, doc.id, version, ord, eventID); err != nil {
				return err
			}
		}
		res.Flags = sortedKeys(flags)

		for i, p := range prev {
			if used[i] {
				continue
			}
			if err := w.retire(ctx, tx, ns, in.Actor, doc.id, version, p.eventID, now); err != nil {
				return err
			}
			res.Retired = append(res.Retired, p.eventID)
		}
		if res.StaleFacts, err = w.markStale(ctx, tx, ns, in.Actor, doc.id, res.Retired); err != nil {
			return err
		}

		res.AuditID, err = w.e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: in.Actor, Action: ActDocumentVersion, Target: doc.id,
			Details: struct {
				SourceID   string `json:"source_id"`
				Version    int    `json:"version"`
				Created    int    `json:"created"`
				Kept       int    `json:"kept"`
				Retired    int    `json:"retired"`
				StaleFacts int    `json:"stale_facts"`
			}{src.id, version, len(res.Created), len(res.Kept), len(res.Retired), len(res.StaleFacts)},
		})
		return err
	})
	if err != nil {
		return IngestResult{URI: in.URI}, coreErr(err)
	}
	return res, nil
}

// removedHash is the content_hash of a document version written when the
// document left its folder: no normalized text hashes to it, so the file
// coming back always writes a new version with fresh chunks.
const removedHash = ""

// retireMissing retires, one transaction each, the documents of the folder's
// source (same namespace, kind and source uri, uri under sourceURI) that the
// walk did not see. A retired document gets a new, empty version; every
// chunk of its previous version gets retired_at; facts left without a live
// origin become stale_source. Results are in uri order.
func (w *Writer) retireMissing(ctx context.Context, in PathInput, sourceURI string, seen map[string]bool) ([]IngestResult, error) {
	ns := w.namespace(in.Namespace)
	kind := in.SourceKind
	if kind == "" {
		kind = core.SourceDocument
	}
	var srcID string
	err := w.e.Store.DB().QueryRowContext(ctx,
		`SELECT id FROM sources WHERE namespace = ? AND kind = ? AND uri = ?`, ns, string(kind), sourceURI).Scan(&srcID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, coreErr(err)
	}
	rows, err := w.e.Store.DB().QueryContext(ctx,
		`SELECT uri FROM documents WHERE namespace = ? AND source_id = ? AND content_hash != ? ORDER BY uri, id`,
		ns, srcID, removedHash)
	if err != nil {
		return nil, coreErr(err)
	}
	var gone []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			rows.Close()
			return nil, coreErr(err)
		}
		if strings.HasPrefix(uri, sourceURI+"/") && !seen[uri] {
			gone = append(gone, uri)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, coreErr(err)
	}
	var out []IngestResult
	for _, uri := range gone {
		r, err := w.retireDocument(ctx, ns, in.Actor, uri)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// retireDocument writes an empty version of the document at uri and retires
// the chunks of its previous version.
func (w *Writer) retireDocument(ctx context.Context, ns, actor, uri string) (IngestResult, error) {
	res := IngestResult{URI: uri, Action: ActionRetired, Reason: ReasonMissingFromDir}
	err := w.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		now := w.now()
		doc, found, err := loadDocument(ctx, tx, ns, uri)
		if err != nil || !found {
			return err
		}
		prev, err := versionChunks(ctx, tx, doc.id, doc.version)
		if err != nil {
			return err
		}
		version := doc.version + 1
		if _, err := tx.ExecContext(ctx,
			`UPDATE documents SET current_version = ?, content_hash = ?, updated_at = ? WHERE id = ?`,
			version, removedHash, now, doc.id); err != nil {
			return err
		}
		res.DocID, res.Version = doc.id, version
		for _, p := range prev {
			var retired sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT retired_at FROM events WHERE id = ?`, p.eventID).Scan(&retired); err != nil {
				return err
			}
			if retired.Valid {
				continue
			}
			if err := w.retire(ctx, tx, ns, actor, doc.id, version, p.eventID, now); err != nil {
				return err
			}
			res.Retired = append(res.Retired, p.eventID)
		}
		if res.StaleFacts, err = w.markStale(ctx, tx, ns, actor, doc.id, res.Retired); err != nil {
			return err
		}
		res.AuditID, err = w.e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActDocumentRetire, Target: doc.id,
			Details: struct {
				SourceID   string `json:"source_id"`
				Version    int    `json:"version"`
				Retired    int    `json:"retired"`
				StaleFacts int    `json:"stale_facts"`
			}{doc.sourceID, version, len(res.Retired), len(res.StaleFacts)},
		})
		return err
	})
	if err != nil {
		return IngestResult{URI: uri}, coreErr(err)
	}
	return res, nil
}

type prevChunk struct {
	eventID, hash string
}

func loadDocument(ctx context.Context, tx *sql.Tx, ns, uri string) (docRow, bool, error) {
	var d docRow
	err := tx.QueryRowContext(ctx,
		`SELECT id, source_id, current_version, content_hash FROM documents WHERE namespace = ? AND uri = ?`, ns, uri).
		Scan(&d.id, &d.sourceID, &d.version, &d.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	return d, err == nil, err
}

// versionChunks lists the chunk events of a document version in ordinal order.
func versionChunks(ctx context.Context, tx *sql.Tx, docID string, version int) ([]prevChunk, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT e.id, e.content_hash FROM document_chunks dc JOIN events e ON e.id = dc.event_id
		 WHERE dc.doc_id = ? AND dc.version = ? ORDER BY dc.ordinal, e.id`, docID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []prevChunk
	for rows.Next() {
		var p prevChunk
		if err := rows.Scan(&p.eventID, &p.hash); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// retire sets retired_at on a chunk event that left the document (the only
// column the raw log lets change) and audits it.
func (w *Writer) retire(ctx context.Context, tx *sql.Tx, ns, actor, docID string, version int, eventID, now string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE events SET retired_at = ? WHERE id = ? AND retired_at IS NULL`, now, eventID); err != nil {
		return err
	}
	_, err := w.e.Audit.Append(ctx, tx, audit.Entry{
		Namespace: ns, Actor: actor, Action: audit.ActRetire, Target: eventID,
		Details: struct {
			DocID   string `json:"doc_id"`
			Version int    `json:"version"`
		}{docID, version},
	})
	return err
}

// markStale moves to stale_source every active or quarantined fact that has a
// provenance edge to one of the retired events and no edge left to a live
// event. Returns the fact ids, sorted.
func (w *Writer) markStale(ctx context.Context, tx *sql.Tx, ns, actor, docID string, retired []string) ([]string, error) {
	cand := map[string]bool{}
	for _, id := range retired {
		ids, err := queryStrings(ctx, tx,
			`SELECT DISTINCT derived_id FROM provenance
			 WHERE namespace = ? AND derived_type = ? AND source_type = ? AND source_id = ? ORDER BY derived_id`,
			ns, string(core.RecFact), string(core.RecEvent), id)
		if err != nil {
			return nil, err
		}
		for _, f := range ids {
			cand[f] = true
		}
	}
	var stale []string
	for _, factID := range sortedKeys(cand) {
		var live int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM provenance p JOIN events e ON e.id = p.source_id
			 WHERE p.derived_type = ? AND p.derived_id = ? AND p.source_type = ? AND e.retired_at IS NULL`,
			string(core.RecFact), factID, string(core.RecEvent)).Scan(&live); err != nil {
			return nil, err
		}
		if live > 0 {
			continue
		}
		r, err := tx.ExecContext(ctx,
			`UPDATE facts SET status = ? WHERE id = ? AND namespace = ? AND status IN (?, ?)`,
			string(core.FactStaleSource), factID, ns, string(core.FactActive), string(core.FactQuarantined))
		if err != nil {
			return nil, err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			continue
		}
		if _, err := w.e.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: actor, Action: audit.ActStaleSource, Target: factID,
			Details: struct {
				DocID string `json:"doc_id"`
			}{docID},
		}); err != nil {
			return nil, err
		}
		stale = append(stale, factID)
	}
	return stale, nil
}

func queryStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func addFlag(flags []string, f string) []string {
	for _, x := range flags {
		if x == f {
			return flags
		}
	}
	out := append(append([]string{}, flags...), f)
	sort.Strings(out)
	return out
}
