package read

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda/internal/core"
)

// record is the metadata of an event, fact or page needed by the read path.
type record struct {
	id     string
	typ    core.RecordType
	ns     string
	kind   string // event kind or page kind
	title  string // pages
	text   string // event content, fact text, page body
	status string // facts and pages
	zone   core.Zone
	flags  []string
	ts     string // recorded_at (events, facts) or updated_at (pages): recency
	// events
	sourceID, docID, retiredAt, sessionID string
	headingPath                           string // document chunks: "H1 > H2"
	role                                  string // log_session turns: user, assistant, system, tool (decision 0137)
	docVersion, ordinal                   sql.NullInt64
	// facts; validFrom also for events (the date given when written, "" if none)
	subject, predicate, validFrom, validTo string
	// any type: has a row in the flags table
	flagged bool
	// events: the facts extracted from or confirmed by this event
	derived []derivedFact
}

// derivedFact is the state of a fact an event stated (derived_from or
// confirmed_by edge), as the context filter needs it.
type derivedFact struct {
	subject, status, validFrom, validTo string
	replacedLater                       bool // superseded by a fact recorded after it (not a history insert)
}

// outdatedAt reports whether the value this fact holds was replaced or
// judged wrong as seen at as_of: rejected or flagged, or superseded by a
// later-recorded fact, unless an explicit as_of falls inside its validity
// (it held then). A history insert (a value recorded as already past) is
// not outdated without an explicit as_of: the event stated it as history.
// With an explicit as_of, an active or superseded fact whose validity does
// not contain as_of is outdated too: its value did not hold at that date,
// either because it was already replaced or because it only began later
// (decision 0120).
func (d derivedFact) outdatedAt(o filterOpts) bool {
	switch core.FactStatus(d.status) {
	case core.FactRejected, core.FactFlagged:
		return true
	case core.FactSuperseded:
		if o.asOfExplicit {
			return !validAt(d.validFrom, d.validTo, o.asOf)
		}
		return d.replacedLater
	case core.FactActive:
		return o.asOfExplicit && !validAt(d.validFrom, d.validTo, o.asOf)
	}
	return false
}

// searchText is the text matched for evidence and sent to the reranker.
func (rec *record) searchText() string {
	if rec.typ == core.RecPage {
		return rec.title + "\n" + rec.text
	}
	return rec.text
}

func (rec *record) hasFlag(f string) bool {
	for _, x := range rec.flags {
		if x == f {
			return true
		}
	}
	return false
}

func splitFlags(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

const inBatch = 400

// loadRecords loads the metadata of ids (grouped by type) in namespace ns.
// Ids from another namespace are simply not returned: the namespace filter
// is part of every query.
func (r *Reader) loadRecords(ctx context.Context, ns string, byType map[core.RecordType][]string) (map[string]*record, error) {
	out := map[string]*record{}
	db := r.db()
	for _, typ := range []core.RecordType{core.RecEvent, core.RecFact, core.RecPage} {
		ids := uniqSorted(byType[typ])
		for start := 0; start < len(ids); start += inBatch {
			batch := ids[start:min(start+inBatch, len(ids))]
			args := []any{ns}
			for _, id := range batch {
				args = append(args, id)
			}
			var q string
			switch typ {
			case core.RecEvent:
				q = `SELECT id, namespace, kind, content, trust_zone, flags, source_id, COALESCE(doc_id,''), doc_version, ordinal,
					COALESCE(retired_at,''), COALESCE(session_id,''), recorded_at, COALESCE(heading_path,''), COALESCE(valid_from,''),
					COALESCE(role,'') FROM events`
			case core.RecFact:
				q = `SELECT id, namespace, subject, predicate, text, status, trust_zone, valid_from, COALESCE(valid_to,''), recorded_at FROM facts`
			case core.RecPage:
				q = `SELECT id, namespace, kind, title, body, status, trust_zone, updated_at FROM pages`
			}
			q += ` WHERE +namespace = ? AND id IN (` + placeholders(len(batch)) + `) ORDER BY id`
			rows, err := db.QueryContext(ctx, q, args...)
			if err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "load "+string(typ))
			}
			for rows.Next() {
				rec := &record{typ: typ}
				var zone, flags string
				switch typ {
				case core.RecEvent:
					err = rows.Scan(&rec.id, &rec.ns, &rec.kind, &rec.text, &zone, &flags, &rec.sourceID, &rec.docID,
						&rec.docVersion, &rec.ordinal, &rec.retiredAt, &rec.sessionID, &rec.ts, &rec.headingPath, &rec.validFrom,
						&rec.role)
				case core.RecFact:
					err = rows.Scan(&rec.id, &rec.ns, &rec.subject, &rec.predicate, &rec.text, &rec.status, &zone,
						&rec.validFrom, &rec.validTo, &rec.ts)
				case core.RecPage:
					err = rows.Scan(&rec.id, &rec.ns, &rec.kind, &rec.title, &rec.text, &rec.status, &zone, &rec.ts)
				}
				if err != nil {
					rows.Close()
					return nil, err
				}
				rec.zone, rec.flags = core.Zone(zone), splitFlags(flags)
				out[rec.id] = rec
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if err := r.markFlagged(ctx, ns, out); err != nil {
		return nil, err
	}
	if err := r.loadDerived(ctx, ns, out); err != nil {
		return nil, err
	}
	if err := r.resolveSubjects(ctx, ns, out); err != nil {
		return nil, err
	}
	return out, nil
}

// maxMergeHops bounds a merged_into chain walk (as internal/facts does).
const maxMergeHops = 16

// resolveSubjects replaces each fact's subject with the surviving entity of
// its merged_into chain: an accepted merge leaves facts on the old subject
// id, but they describe the surviving entity.
func (r *Reader) resolveSubjects(ctx context.Context, ns string, recs map[string]*record) error {
	seen := map[string]bool{}
	for _, rec := range recs {
		if rec.typ == core.RecFact && rec.subject != "" {
			seen[rec.subject] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	into, err := r.mergeTargets(ctx, ns, sortedKeys(seen))
	if err != nil || len(into) == 0 {
		return err
	}
	for _, rec := range recs {
		if rec.typ != core.RecFact {
			continue
		}
		for hop := 0; hop < maxMergeHops; hop++ {
			to, ok := into[rec.subject]
			if !ok {
				break
			}
			rec.subject = to
		}
	}
	return nil
}

// loadDerived fills rec.derived for the events of recs.
func (r *Reader) loadDerived(ctx context.Context, ns string, recs map[string]*record) error {
	var evs []string
	for id, rec := range recs {
		if rec.typ == core.RecEvent {
			evs = append(evs, id)
		}
	}
	sort.Strings(evs)
	for start := 0; start < len(evs); start += inBatch {
		batch := evs[start:min(start+inBatch, len(evs))]
		args := []any{ns}
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := r.db().QueryContext(ctx, `SELECT p.source_id, f.subject, f.status, f.valid_from, COALESCE(f.valid_to,''),
			COALESCE(s.recorded_at > f.recorded_at, 0)
			FROM provenance p JOIN facts f ON f.id = p.derived_id LEFT JOIN facts s ON s.id = f.superseded_by
			WHERE p.namespace = ? AND p.derived_type = 'fact' AND p.source_type = 'event'
			  AND p.relation IN ('derived_from','confirmed_by') AND p.source_id IN (`+placeholders(len(batch))+`)
			ORDER BY p.source_id, f.id`, args...)
		if err != nil {
			return core.Wrap(core.CodeInternal, err, "derived facts")
		}
		for rows.Next() {
			var id string
			var d derivedFact
			if err := rows.Scan(&id, &d.subject, &d.status, &d.validFrom, &d.validTo, &d.replacedLater); err != nil {
				rows.Close()
				return err
			}
			recs[id].derived = append(recs[id].derived, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// markFlagged sets rec.flagged for records with a row in the flags table
// (BASE.md "Marked as wrong or outdated ... removes the record from reads").
func (r *Reader) markFlagged(ctx context.Context, ns string, recs map[string]*record) error {
	ids := make([]string, 0, len(recs))
	for id := range recs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += inBatch {
		batch := ids[start:min(start+inBatch, len(ids))]
		args := []any{}
		for _, id := range batch {
			args = append(args, id)
		}
		// Ids are globally unique (prefix + ULID), so record_id alone identifies
		// the record whatever record_type string the flag writer used.
		flagged, err := r.queryStrings(ctx, `SELECT DISTINCT record_id FROM flags WHERE record_id IN (`+
			placeholders(len(batch))+`) ORDER BY record_id`, args...)
		if err != nil {
			return err
		}
		for _, id := range flagged {
			recs[id].flagged = true
		}
	}
	return nil
}

// queryStrings runs a one-column query and collects the result.
func (r *Reader) queryStrings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := r.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "query")
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

// filterOpts are the rule-filter inputs (BASE.md read step 4).
type filterOpts struct {
	ns             string
	asOf           time.Time
	types          map[core.RecordType]bool
	zones          map[core.Zone]bool
	includeHistory bool // search mode only
	asOfExplicit   bool // the caller gave as_of (time travel)
	mode           Mode
}

// Reasons a record leaves the candidate list. Logged with the candidate.
const (
	dropNotFound    = "not_found_in_namespace"
	dropNamespace   = "namespace"
	dropType        = "type"
	dropZone        = "zone"
	dropFlagged     = "flagged"
	dropRetired     = "retired"
	dropAssertion   = "fact_assertion"   // the raw event behind assert_fact; its fact is the readable record
	dropOutdated    = "outdated_excerpt" // context: a fact this event stated was replaced or judged wrong
	dropStatus      = "status"
	dropNotValid    = "not_valid_at_as_of"
	dropAfterAsOf   = "after_as_of" // an event dated after an explicit as_of (decision 0135)
	dropInstruction = "instruction_like"
	dropDuplicate   = "duplicate_of"
	dropCut         = "below_relevance"
	dropEvidence    = "no_evidence"
	dropDiversity   = "max_per_document"
	dropSession     = "max_per_session" // context: read.context_max_per_session (decision 0137)
	dropBudget      = "budget"
	dropLimit       = "limit"
	// context: a fact or page that did not fit under read.context_derived_max_share
	// or the room kept for the protected raw excerpts (decision 0143)
	dropDerivedShare = "derived_share"
)

// ruleFilter applies BASE.md read step 4 to one record and returns "" when
// it stays, or the reason it is dropped:
//   - facts: status active and valid at as_of; with an explicit as_of, also
//     superseded facts valid at that date (they held then); with
//     include_history (search mode only) also superseded facts, without the
//     validity window. Never quarantined, rejected, flagged or stale_source;
//   - events: no retired_at, and not the fact_assertion event behind an
//     assert_fact call (the fact itself is the readable record, so a
//     superseded value never comes back through its raw event); in context
//     mode, not an event that stated a fact later replaced or judged wrong
//     (decision 0093: its excerpt would deliver the old value); with an
//     explicit as_of, not an event dated after it (decision 0135);
//   - pages: status active;
//   - any record with a row in flags is dropped; type and zone filters apply;
//   - context mode never includes instruction_like events.
func ruleFilter(rec *record, o filterOpts) string {
	switch {
	case rec == nil:
		return dropNotFound
	case rec.ns != o.ns:
		return dropNamespace
	case len(o.types) > 0 && !o.types[rec.typ]:
		return dropType
	case len(o.zones) > 0 && !o.zones[rec.zone]:
		return dropZone
	case rec.flagged:
		return dropFlagged
	}
	switch rec.typ {
	case core.RecEvent:
		if rec.retiredAt != "" {
			return dropRetired
		}
		if rec.kind == string(core.EventFact) {
			return dropAssertion
		}
		if o.asOfExplicit && eventAfter(rec, o.asOf) {
			return dropAfterAsOf
		}
		if o.mode == ModeContext && rec.hasFlag(core.FlagInstructionLike) {
			return dropInstruction
		}
		if o.mode == ModeContext {
			for _, d := range rec.derived {
				if d.outdatedAt(o) {
					return dropOutdated
				}
			}
		}
	case core.RecFact:
		st := core.FactStatus(rec.status)
		if o.includeHistory && o.mode == ModeSearch {
			if st != core.FactActive && st != core.FactSuperseded {
				return dropStatus
			}
			return ""
		}
		// A superseded fact was the current one during its validity window:
		// an explicit as_of inside that window selects it (decision 0021).
		if st != core.FactActive && !(st == core.FactSuperseded && o.asOfExplicit) {
			return dropStatus
		}
		if !validAt(rec.validFrom, rec.validTo, o.asOf) {
			return dropNotValid
		}
	case core.RecPage:
		if rec.status != "active" {
			return dropStatus
		}
	default:
		return dropType
	}
	return ""
}

// eventAfter reports whether an event is dated after asOf: its valid_from
// (the date given when it was written) when set, else its recorded_at. It
// did not exist yet "on that date". A date-only valid_from is stored as
// 00:00 UTC of its day.
func eventAfter(rec *record, asOf time.Time) bool {
	d := rec.validFrom
	if d == "" {
		d = rec.ts
	}
	t, err := core.ParseTime(d)
	return err == nil && t.After(asOf)
}

// validAt reports valid_from <= asOf < valid_to (a null valid_to is open).
func validAt(from, to string, asOf time.Time) bool {
	f, err := core.ParseTime(from)
	if err != nil || f.After(asOf) {
		return false
	}
	if to == "" {
		return true
	}
	t, err := core.ParseTime(to)
	return err == nil && asOf.Before(t)
}

// collapseDuplicates implements "near-duplicates are collapsed into the most
// recent record": records joined by provenance 'duplicate_of' edges form a
// group (transitively); among the group members that pass the rule filter,
// the newest (ts desc, id desc) represents the group. It returns, for each
// eligible candidate, its representative, and the records of representatives
// that were not candidates.
func (r *Reader) collapseDuplicates(ctx context.Context, eligible map[string]*record, o filterOpts) (map[string]string, map[string]*record, error) {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			if ra < rb {
				parent[rb] = ra
			} else {
				parent[ra] = rb
			}
		}
	}
	types := map[string]core.RecordType{}
	seen := map[string]bool{}
	var frontier []string
	for id, rec := range eligible {
		seen[id], types[id] = true, rec.typ
		frontier = append(frontier, id)
	}
	sort.Strings(frontier)
	for round := 0; len(frontier) > 0 && round < 8 && len(seen) < 5000; round++ {
		var next []string
		for start := 0; start < len(frontier); start += inBatch {
			batch := frontier[start:min(start+inBatch, len(frontier))]
			args := []any{o.ns}
			for _, id := range batch {
				args = append(args, id)
			}
			args = append(args, o.ns)
			for _, id := range batch {
				args = append(args, id)
			}
			ph := placeholders(len(batch))
			// relation is a literal so the partial indexes of migration 0100 apply.
			rows, err := r.db().QueryContext(ctx, `SELECT derived_type, derived_id, source_type, source_id FROM provenance
				WHERE namespace = ? AND relation = '`+core.RelDuplicateOf+`' AND derived_id IN (`+ph+`)
				UNION SELECT derived_type, derived_id, source_type, source_id FROM provenance
				WHERE namespace = ? AND relation = '`+core.RelDuplicateOf+`' AND source_id IN (`+ph+`)
				ORDER BY 1, 2, 3, 4`, args...)
			if err != nil {
				return nil, nil, core.Wrap(core.CodeInternal, err, "duplicate edges")
			}
			type dupEdge struct{ dt, did, st, sid string }
			var edges []dupEdge
			for rows.Next() {
				var e dupEdge
				if err := rows.Scan(&e.dt, &e.did, &e.st, &e.sid); err != nil {
					rows.Close()
					return nil, nil, err
				}
				edges = append(edges, e)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
			for _, e := range edges {
				union(e.did, e.sid)
				for _, n := range [][2]string{{e.dt, e.did}, {e.st, e.sid}} {
					if !seen[n[1]] {
						seen[n[1]], types[n[1]] = true, core.RecordType(n[0])
						next = append(next, n[1])
					}
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}
	// Load and filter members that were not candidates.
	byType := map[core.RecordType][]string{}
	for id := range seen {
		if eligible[id] == nil && searchable(types[id]) {
			byType[types[id]] = append(byType[types[id]], id)
		}
	}
	others, err := r.loadRecords(ctx, o.ns, byType)
	if err != nil {
		return nil, nil, err
	}
	best := map[string]*record{} // group root -> newest eligible member
	consider := func(rec *record) {
		root := find(rec.id)
		b := best[root]
		if b == nil || rec.ts > b.ts || (rec.ts == b.ts && rec.id > b.id) {
			best[root] = rec
		}
	}
	for _, rec := range eligible {
		consider(rec)
	}
	for _, rec := range others {
		if ruleFilter(rec, o) == "" {
			consider(rec)
		}
	}
	rep := map[string]string{}
	extra := map[string]*record{}
	for id := range eligible {
		b := best[find(id)]
		rep[id] = b.id
		if eligible[b.id] == nil {
			extra[b.id] = b
		}
	}
	return rep, extra, nil
}

// Edge is one provenance edge (derived → source).
type Edge struct {
	DerivedType string `json:"derived_type"`
	DerivedID   string `json:"derived_id"`
	SourceType  string `json:"source_type"`
	SourceID    string `json:"source_id"`
	Relation    string `json:"relation"`
}

// originEvents walks the provenance graph from (typ, id) down to events,
// following every relation except mentions, supersedes and duplicate_of
// (those do not describe where the content came from). Pages also reach
// their page items. Returns the origin event ids (sorted) and the edges.
func (r *Reader) originEvents(ctx context.Context, ns string, typ core.RecordType, id string) ([]string, []Edge, error) {
	if typ == core.RecEvent {
		return []string{id}, nil, nil
	}
	type node struct {
		typ core.RecordType
		id  string
	}
	seen := map[node]bool{{typ, id}: true}
	frontier := []node{{typ, id}}
	evs := map[string]bool{}
	var edges []Edge
	for depth := 0; len(frontier) > 0 && depth < 16 && len(seen) < 10000; depth++ {
		var next []node
		for _, n := range frontier {
			rows, err := r.db().QueryContext(ctx, `SELECT derived_type, derived_id, source_type, source_id, relation
				FROM provenance WHERE namespace = ? AND derived_type = ? AND derived_id = ?
				AND relation NOT IN (?,?,?) ORDER BY source_type, source_id, relation`,
				ns, string(n.typ), n.id, core.RelMentions, core.RelSupersedes, core.RelDuplicateOf)
			if err != nil {
				return nil, nil, core.Wrap(core.CodeInternal, err, "provenance")
			}
			var es []Edge
			for rows.Next() {
				var e Edge
				if err := rows.Scan(&e.DerivedType, &e.DerivedID, &e.SourceType, &e.SourceID, &e.Relation); err != nil {
					rows.Close()
					return nil, nil, err
				}
				es = append(es, e)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
			if n.typ == core.RecPage {
				items, err := r.queryStrings(ctx, `SELECT id FROM page_items WHERE namespace = ? AND page_id = ? AND status = 'active' ORDER BY id`, ns, n.id)
				if err != nil {
					return nil, nil, err
				}
				for _, it := range items {
					es = append(es, Edge{string(core.RecPage), n.id, string(core.RecPageItem), it, "item"})
				}
			}
			for _, e := range es {
				edges = append(edges, e)
				m := node{core.RecordType(e.SourceType), e.SourceID}
				if m.typ == core.RecEvent {
					evs[m.id] = true
					continue
				}
				if seen[m] || (m.typ != core.RecFact && m.typ != core.RecPage && m.typ != core.RecPageItem) {
					continue
				}
				seen[m] = true
				next = append(next, m)
			}
		}
		frontier = next
	}
	ids := make([]string, 0, len(evs))
	for id := range evs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, edges, nil
}

func uniqSorted(xs []string) []string {
	out := append([]string(nil), xs...)
	sort.Strings(out)
	n := 0
	for i, x := range out {
		if i == 0 || x != out[n-1] {
			out[n] = x
			n++
		}
	}
	return out[:n]
}
