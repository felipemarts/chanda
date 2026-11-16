package facts

import (
	"context"
	"strings"
	"time"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/facts/norm"
)

// Origin is one provenance edge leaving a fact.
type Origin struct {
	Type     core.RecordType `json:"type"`
	ID       string          `json:"id"`
	Relation string          `json:"relation"` // derived_from, confirmed_by, supersedes
}

// TimelineEntry is one fact of a timeline with the origins of the change.
type TimelineEntry struct {
	Fact
	Origins []Origin `json:"origins"`
}

// subjectFilter resolves subject (name or entity id, never created; merges
// followed) and builds the WHERE clause shared by the queries: the facts of
// the surviving entity and of every entity merged into it. ok is false when
// the subject is unknown.
func (s *Service) subjectFilter(ctx context.Context, ns, subject, predicate string) (string, []any, bool, error) {
	id, ok, err := s.LookupEntity(ctx, s.e.Store.DB(), ns, subject)
	if err != nil || !ok {
		return "", nil, false, err
	}
	set, err := MergeSet(ctx, s.e.Store.DB(), ns, id)
	if err != nil {
		return "", nil, false, err
	}
	cond, sargs := subjectIn(set)
	where := `namespace = ? AND ` + cond
	args := append([]any{ns}, sargs...)
	if p := strings.TrimSpace(predicate); p != "" {
		where += ` AND predicate = ?`
		args = append(args, norm.Predicate(p))
	}
	return where, args, true, nil
}

func (s *Service) queryFacts(ctx context.Context, ns, subject, predicate, cond string, extra ...any) ([]Fact, error) {
	where, args, ok, err := s.subjectFilter(ctx, ns, subject, predicate)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT `+factCols+` FROM facts WHERE `+where+cond+`
		ORDER BY predicate, valid_from, recorded_at, id`, append(args, extra...)...)
	if err != nil {
		return nil, err
	}
	return scanFacts(rows)
}

// Timeline returns every fact of a subject and predicate, oldest to current
// (ordered by valid_from), whatever its status, each with its provenance
// edges. An empty predicate returns all predicates, grouped by predicate.
// Subject is a name (resolved by alias, never created) or an entity id.
func (s *Service) Timeline(ctx context.Context, ns, subject, predicate string) ([]TimelineEntry, error) {
	fs, err := s.queryFacts(ctx, ns, subject, predicate, "")
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	args := []any{string(core.RecFact)}
	for _, f := range fs {
		args = append(args, f.ID)
	}
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT derived_id, source_type, source_id, relation FROM provenance
		WHERE derived_type = ? AND derived_id IN (?`+strings.Repeat(",?", len(fs)-1)+`)
		ORDER BY derived_id, relation, source_type, source_id`, args...)
	if err != nil {
		return nil, err
	}
	byFact := map[string][]Origin{}
	for rows.Next() {
		var id, st, sid, rel string
		if err := rows.Scan(&id, &st, &sid, &rel); err != nil {
			rows.Close()
			return nil, err
		}
		byFact[id] = append(byFact[id], Origin{Type: core.RecordType(st), ID: sid, Relation: rel})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TimelineEntry, len(fs))
	for i, f := range fs {
		out[i] = TimelineEntry{Fact: f, Origins: byFact[f.ID]}
	}
	return out, nil
}

// AsOf answers "what held on date t": facts with valid_from <= t and
// (valid_to null or valid_to > t) whose status is active or superseded.
// Quarantined, rejected, flagged and stale facts are excluded. An empty
// predicate covers all predicates of the subject.
func (s *Service) AsOf(ctx context.Context, ns, subject, predicate string, t time.Time) ([]Fact, error) {
	at := core.FormatTime(t)
	return s.queryFacts(ctx, ns, subject, predicate,
		` AND valid_from <= ? AND (valid_to IS NULL OR valid_to > ?) AND status IN (?, ?)`,
		at, at, string(core.FactActive), string(core.FactSuperseded))
}

// Current returns the active facts of a subject (and predicate, if given):
// what holds today. Superseded values never appear here.
func (s *Service) Current(ctx context.Context, ns, subject, predicate string) ([]Fact, error) {
	return s.queryFacts(ctx, ns, subject, predicate, ` AND status = ?`, string(core.FactActive))
}
