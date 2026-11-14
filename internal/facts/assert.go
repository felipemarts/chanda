package facts

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Action is what Assert did with a candidate fact.
type Action string

const (
	ActionInserted    Action = "inserted"    // no active fact: inserted as active
	ActionConfirmed   Action = "confirmed"   // same normalized value: provenance edge + confidence
	ActionSuperseded  Action = "superseded"  // single, later valid_from: closed the active fact
	ActionHistory     Action = "history"     // single, earlier valid_from: inserted already closed
	ActionQuarantined Action = "quarantined" // lower zone than the active fact
	ActionCoexist     Action = "coexist"     // multi predicate: both values active
)

// AssertInput is one candidate fact. Subject is a name (resolved, created if
// needed) or an existing entity id. ValidFrom and DocDate accept any
// core.ParseTime layout. OriginEventIDs must be live events of Namespace.
type AssertInput struct {
	Namespace      string
	Subject        string
	SubjectType    string
	Predicate      string
	Value          string
	ValidFrom      string // date given in the call
	DocDate        string // document date; when empty, documents.doc_date of the origins
	OriginEventIDs []string
	Actor          string
}

// AssertResult reports the decision. AuditID is 0 when nothing changed
// (the same origin confirmed the same value again).
type AssertResult struct {
	FactID   string
	EntityID string
	Action   Action
	Reason   string
	AuditID  int64
	Closed   []string // facts whose validity this call closed or trimmed
}

// Reasons reported in AssertResult.Reason.
const (
	ReasonNewFact         = "new_fact"
	ReasonSameValue       = "same_value"
	ReasonAlreadyRecorded = "already_recorded"
	ReasonLaterValidFrom  = "later_valid_from"
	ReasonEarlierValid    = "earlier_valid_from"
	ReasonLowerZone       = "lower_zone_than_active"
	ReasonMulti           = "multi_predicate"
)

// Fact is one row of the facts table. ValidTo and SupersededBy are "" when null.
type Fact struct {
	ID           string          `json:"id"`
	Namespace    string          `json:"namespace"`
	Subject      string          `json:"subject"`
	Predicate    string          `json:"predicate"`
	Value        string          `json:"value"`
	ValueNorm    string          `json:"value_norm"`
	ValidFrom    string          `json:"valid_from"`
	ValidTo      string          `json:"valid_to,omitempty"`
	RecordedAt   string          `json:"recorded_at"`
	SupersededBy string          `json:"superseded_by,omitempty"`
	Status       core.FactStatus `json:"status"`
	Confidence   float64         `json:"confidence"`
	TrustZone    core.Zone       `json:"trust_zone"`
	Single       bool            `json:"single"`
	Text         string          `json:"text"`
}

const factCols = `id, namespace, subject, predicate, value, value_norm, valid_from, COALESCE(valid_to,''), recorded_at,
	COALESCE(superseded_by,''), status, confidence, trust_zone, is_single, text`

func scanFacts(rows *sql.Rows) ([]Fact, error) {
	defer rows.Close()
	var out []Fact
	for rows.Next() {
		var f Fact
		var st, z string
		var single int
		if err := rows.Scan(&f.ID, &f.Namespace, &f.Subject, &f.Predicate, &f.Value, &f.ValueNorm, &f.ValidFrom, &f.ValidTo,
			&f.RecordedAt, &f.SupersededBy, &st, &f.Confidence, &z, &single, &f.Text); err != nil {
			return nil, err
		}
		f.Status, f.TrustZone, f.Single = core.FactStatus(st), core.Zone(z), single == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// chainFacts returns every fact of (ns, subject, predicate), oldest first,
// including the facts of entities merged into subject (they keep their
// own subject id).
func chainFacts(ctx context.Context, q store.Queryer, ns, subject, predicate string) ([]Fact, error) {
	set, err := MergeSet(ctx, q, ns, subject)
	if err != nil {
		return nil, err
	}
	cond, args := subjectIn(set)
	rows, err := q.QueryContext(ctx, `SELECT `+factCols+` FROM facts WHERE namespace = ? AND `+cond+` AND predicate = ?
		ORDER BY valid_from, recorded_at, id`, append(append([]any{ns}, args...), predicate)...)
	if err != nil {
		return nil, err
	}
	return scanFacts(rows)
}

type origins struct {
	ids        []string // sorted, unique
	zone       core.Zone
	recordedAt string // latest recorded_at among the origins
	docDate    string // latest documents.doc_date among the origins, normalized
}

func loadOrigins(ctx context.Context, tx *sql.Tx, ns string, ids []string) (origins, error) {
	var o origins
	seen := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			o.ids = append(o.ids, id)
		}
	}
	sort.Strings(o.ids)
	if len(o.ids) == 0 {
		return o, core.Errf(core.CodeInvalidArgument, "a fact needs at least one origin event")
	}
	args := []any{ns}
	for _, id := range o.ids {
		args = append(args, id)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, trust_zone, recorded_at, COALESCE(doc_id,''), retired_at IS NOT NULL
		FROM events WHERE namespace = ? AND id IN (?`+strings.Repeat(",?", len(o.ids)-1)+`) ORDER BY id`, args...)
	if err != nil {
		return o, err
	}
	var zones []core.Zone
	var docIDs []string
	found := 0
	var retired []string
	for rows.Next() {
		var id, z, rec, doc string
		var isRetired bool
		if err := rows.Scan(&id, &z, &rec, &doc, &isRetired); err != nil {
			rows.Close()
			return o, err
		}
		found++
		zones = append(zones, core.Zone(z))
		if rec > o.recordedAt {
			o.recordedAt = rec
		}
		if doc != "" {
			docIDs = append(docIDs, doc)
		}
		if isRetired {
			retired = append(retired, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return o, err
	}
	if found != len(o.ids) {
		return o, core.Errf(core.CodeNotFound, "origin event not found in namespace %s", ns)
	}
	if len(retired) > 0 {
		return o, core.Errf(core.CodeInvalidArgument, "origin event %s is retired", retired[0])
	}
	o.zone = core.MinZone(zones...)
	sort.Strings(docIDs)
	for _, d := range docIDs {
		var dd sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT doc_date FROM documents WHERE id = ?`, d).Scan(&dd); err != nil && err != sql.ErrNoRows {
			return o, err
		}
		if t, err := core.ParseTime(strings.TrimSpace(dd.String)); dd.Valid && err == nil {
			if s := core.FormatTime(t); s > o.docDate {
				o.docDate = s
			}
		}
	}
	return o, nil
}

// pickValidFrom applies the precedence of BASE.md: the date given in the
// call, then the document date, then recorded_at (of the origin events).
func pickValidFrom(given, docDate string, o origins) (string, error) {
	for _, d := range []string{given, docDate} {
		if d = strings.TrimSpace(d); d != "" {
			t, err := core.ParseTime(d)
			if err != nil {
				return "", err
			}
			return core.FormatTime(t), nil
		}
	}
	if o.docDate != "" {
		return o.docDate, nil
	}
	return o.recordedAt, nil
}

// AssertCommit runs Assert in its own transaction.
func (s *Service) AssertCommit(ctx context.Context, in AssertInput) (AssertResult, error) {
	var res AssertResult
	err := s.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		res, err = s.Assert(ctx, tx, in)
		return err
	})
	return res, err
}

// Assert reconciles one candidate fact with the facts of the same subject
// and predicate (write step 9, "Decision table for facts"):
//
//   - same normalized value as an active fact: no new fact; confirmed_by
//     edges to the new origins and confidence + cfg.Facts.ConfirmBoost (capped at 1);
//   - single predicate, lower zone than the active fact: inserted quarantined;
//   - single, valid_from at or after the active fact's: the active fact is
//     closed (valid_to = new valid_from, superseded) and the new one inserted;
//   - single, valid_from before the active fact's: inserted already closed as
//     history, valid_to = valid_from of the next fact in the chain;
//   - multi: both values coexist.
//
// Every new fact gets derived_from edges to its origins and the zone
// core.MinZone of their zones.
func (s *Service) Assert(ctx context.Context, tx *sql.Tx, in AssertInput) (AssertResult, error) {
	var res AssertResult
	ns := strings.TrimSpace(in.Namespace)
	actor := in.Actor
	if actor == "" {
		actor = "system"
	}
	pred := norm.Predicate(in.Predicate)
	value := norm.CollapseSpace(norm.NFC(in.Value))
	vn := norm.Value(in.Value)
	if ns == "" || pred == "" || vn == "" || norm.Alias(in.Subject) == "" && core.RecordTypeOf(in.Subject) != core.RecEntity {
		return res, core.Errf(core.CodeInvalidArgument, "namespace, subject, predicate and value are required")
	}
	o, err := loadOrigins(ctx, tx, ns, in.OriginEventIDs)
	if err != nil {
		return res, err
	}
	vf, err := pickValidFrom(in.ValidFrom, in.DocDate, o)
	if err != nil {
		return res, err
	}
	subj, err := s.subjectFor(ctx, tx, ns, in.Subject, in.SubjectType, actor)
	if err != nil {
		return res, err
	}
	res.EntityID = subj
	card, err := s.Cardinality(ctx, tx, ns, pred, actor)
	if err != nil {
		return res, err
	}
	chain, err := chainFacts(ctx, tx, ns, subj, pred)
	if err != nil {
		return res, err
	}
	name, err := canonicalName(ctx, tx, ns, subj)
	if err != nil {
		return res, err
	}
	nf := Fact{
		Namespace: ns, Subject: subj, Predicate: pred, Value: value, ValueNorm: vn, ValidFrom: vf,
		RecordedAt: s.now(), Status: core.FactActive, Confidence: s.e.Cfg.Facts.InitialConfidence,
		TrustZone: o.zone, Single: card == core.Single, Text: name + " " + pred + ": " + value,
	}
	op := assertOp{s: s, ctx: ctx, tx: tx, actor: actor, o: o}

	var actives []Fact
	for _, f := range chain {
		if f.Status == core.FactActive {
			actives = append(actives, f)
		}
	}
	for _, a := range actives {
		if a.ValueNorm == vn {
			return op.confirm(a)
		}
	}
	if card == core.Single && len(actives) > 0 {
		a := actives[0]
		if o.zone.Rank() < a.TrustZone.Rank() {
			for _, q := range chain {
				if q.Status == core.FactQuarantined && q.ValueNorm == vn {
					return op.confirm(q)
				}
			}
			nf.Status = core.FactQuarantined
			return op.insert(nf, ActionQuarantined, ReasonLowerZone, audit.ActQuarantine, map[string]any{"active": a.ID})
		}
		if vf >= a.ValidFrom {
			return op.supersede(nf, a)
		}
		return op.history(nf, chain)
	}
	if card == core.Multi && len(actives) > 0 {
		return op.insert(nf, ActionCoexist, ReasonMulti, audit.ActFactInsert, map[string]any{"coexisting": len(actives)})
	}
	return op.insert(nf, ActionInserted, ReasonNewFact, audit.ActFactInsert, nil)
}

type assertOp struct {
	s     *Service
	ctx   context.Context
	tx    *sql.Tx
	actor string
	o     origins
}

func (op assertOp) audit(ns, action, target string, details map[string]any) (int64, error) {
	if details == nil {
		details = map[string]any{}
	}
	details["origins"] = len(op.o.ids)
	return op.s.e.Audit.Append(op.ctx, op.tx, audit.Entry{Namespace: ns, Actor: op.actor, Action: action, Target: target, Details: details})
}

// write inserts f with a fresh id and its derived_from edges.
func (op assertOp) write(f *Fact) error {
	f.ID = op.s.e.IDs.For(core.RecFact)
	single := 0
	if f.Single {
		single = 1
	}
	_, err := op.tx.ExecContext(op.ctx, `INSERT INTO facts(id, namespace, subject, predicate, value, value_norm, valid_from, valid_to,
		recorded_at, superseded_by, status, confidence, trust_zone, is_single, text)
		VALUES (?,?,?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?,?,?,?)`,
		f.ID, f.Namespace, f.Subject, f.Predicate, f.Value, f.ValueNorm, f.ValidFrom, f.ValidTo, f.RecordedAt, f.SupersededBy,
		string(f.Status), f.Confidence, string(f.TrustZone), single, f.Text)
	if err != nil {
		return err
	}
	for _, id := range op.o.ids {
		if _, err := op.s.addEdge(op.ctx, op.tx, f.Namespace, core.RecFact, f.ID, core.RecEvent, id, core.RelDerivedFrom); err != nil {
			return err
		}
	}
	return nil
}

func (op assertOp) insert(f Fact, act Action, reason, auditAct string, details map[string]any) (AssertResult, error) {
	if err := op.write(&f); err != nil {
		return AssertResult{}, err
	}
	if details == nil {
		details = map[string]any{}
	}
	details["action"] = string(act)
	id, err := op.audit(f.Namespace, auditAct, f.ID, details)
	return AssertResult{FactID: f.ID, EntityID: f.Subject, Action: act, Reason: reason, AuditID: id}, err
}

// confirm adds confirmed_by edges from f to the origins it does not already
// cite and raises its confidence. A confirmation from a zone lower than the
// fact's adds the edges but does not raise confidence
// (docs/decisions/0040-fact-reconciliation.md).
func (op assertOp) confirm(f Fact) (AssertResult, error) {
	res := AssertResult{FactID: f.ID, EntityID: f.Subject, Action: ActionConfirmed, Reason: ReasonSameValue}
	added := 0
	for _, id := range op.o.ids {
		var n int
		if err := op.tx.QueryRowContext(op.ctx, `SELECT count(*) FROM provenance WHERE derived_type = ? AND derived_id = ?
			AND source_type = ? AND source_id = ?`, string(core.RecFact), f.ID, string(core.RecEvent), id).Scan(&n); err != nil {
			return res, err
		}
		if n > 0 {
			continue
		}
		if _, err := op.s.addEdge(op.ctx, op.tx, f.Namespace, core.RecFact, f.ID, core.RecEvent, id, core.RelConfirmedBy); err != nil {
			return res, err
		}
		added++
	}
	if added == 0 {
		res.Reason = ReasonAlreadyRecorded
		return res, nil
	}
	boosted := op.o.zone.Rank() >= f.TrustZone.Rank()
	if boosted {
		c := math.Min(1, f.Confidence+op.s.e.Cfg.Facts.ConfirmBoost)
		c = math.Round(c*1e9) / 1e9
		if _, err := op.tx.ExecContext(op.ctx, `UPDATE facts SET confidence = ? WHERE id = ?`, c, f.ID); err != nil {
			return res, err
		}
	}
	var err error
	res.AuditID, err = op.audit(f.Namespace, audit.ActFactConfirm, f.ID, map[string]any{"new_edges": added, "boosted": boosted})
	return res, err
}

// supersede closes the active fact a at nf.ValidFrom and inserts nf as active.
// The old fact is closed before the insert so the partial unique index never
// sees two active single facts.
func (op assertOp) supersede(nf Fact, a Fact) (AssertResult, error) {
	res := AssertResult{EntityID: nf.Subject, Action: ActionSuperseded, Reason: ReasonLaterValidFrom, Closed: []string{a.ID}}
	if _, err := op.tx.ExecContext(op.ctx, `UPDATE facts SET status = ?, valid_to = ? WHERE id = ? AND status = ?`,
		string(core.FactSuperseded), nf.ValidFrom, a.ID, string(core.FactActive)); err != nil {
		return res, err
	}
	if err := op.write(&nf); err != nil {
		return res, err
	}
	if _, err := op.tx.ExecContext(op.ctx, `UPDATE facts SET superseded_by = ? WHERE id = ?`, nf.ID, a.ID); err != nil {
		return res, err
	}
	if _, err := op.s.addEdge(op.ctx, op.tx, nf.Namespace, core.RecFact, nf.ID, core.RecFact, a.ID, core.RelSupersedes); err != nil {
		return res, err
	}
	res.FactID = nf.ID
	var err error
	res.AuditID, err = op.audit(nf.Namespace, audit.ActSupersede, nf.ID, map[string]any{"closed": a.ID})
	return res, err
}

// history inserts nf (valid_from earlier than the active fact) already
// closed: valid_to is the valid_from of the earliest chain fact that starts
// after it, superseded_by that fact. The active fact does not change. A
// closed fact whose period covered nf.ValidFrom is trimmed to end there, so
// as_of never sees two values of a single predicate at once; if that fact
// already holds the same value, nf only confirms it.
func (op assertOp) history(nf Fact, chain []Fact) (AssertResult, error) {
	var next *Fact
	var overlapping []Fact
	for i := range chain {
		f := chain[i]
		if f.Status != core.FactActive && f.Status != core.FactSuperseded {
			continue
		}
		if f.ValidFrom > nf.ValidFrom {
			if next == nil || f.ValidFrom < next.ValidFrom {
				next = &chain[i]
			}
			continue
		}
		if f.Status == core.FactSuperseded && f.ValidTo > nf.ValidFrom {
			if f.ValueNorm == nf.ValueNorm {
				return op.confirm(f)
			}
			overlapping = append(overlapping, f)
		}
	}
	if next == nil { // cannot happen: the active fact starts after nf
		return AssertResult{}, core.Errf(core.CodeInternal, "history insert without a later fact")
	}
	nf.Status, nf.ValidTo, nf.SupersededBy = core.FactSuperseded, next.ValidFrom, next.ID
	if err := op.write(&nf); err != nil {
		return AssertResult{}, err
	}
	res := AssertResult{FactID: nf.ID, EntityID: nf.Subject, Action: ActionHistory, Reason: ReasonEarlierValid}
	for _, f := range overlapping {
		if _, err := op.tx.ExecContext(op.ctx, `UPDATE facts SET valid_to = ?, superseded_by = ? WHERE id = ?`, nf.ValidFrom, nf.ID, f.ID); err != nil {
			return res, err
		}
		if _, err := op.audit(nf.Namespace, ActFactAdjust, f.ID, map[string]any{"trimmed_by": nf.ID}); err != nil {
			return res, err
		}
		res.Closed = append(res.Closed, f.ID)
	}
	var err error
	res.AuditID, err = op.audit(nf.Namespace, audit.ActFactInsert, nf.ID, map[string]any{"action": string(ActionHistory), "next": next.ID})
	return res, err
}
