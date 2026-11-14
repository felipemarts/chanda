package coresys

import (
	"context"
	"fmt"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/eval"
)

// TemporalSuite is eval.TemporalSuite plus, for a C system, one diagnostic
// line (Logf) per delivered record that carries a superseded value: the
// question, the record id and type, its kind and source kind, its corpus
// item and the facts it states with their status, which tell why the read
// path let it through (decision 0120). Ids, never content.
func TemporalSuite(ctx context.Context, in eval.SuiteInput) (eval.Measured, error) {
	m, err := eval.TemporalSuite(ctx, in)
	if err != nil {
		return m, err
	}
	s, ok := in.System.(*System)
	if !ok || in.Results == nil {
		return m, nil
	}
	for i, q := range in.Dataset.Evaluated() {
		if i >= len(in.Results) || !in.Results[i].Forbidden {
			continue
		}
		if !(q.HasTag(eval.TagTemporal) || q.HasTag(eval.TagUpdate)) || q.HasTag(eval.TagInjection) {
			continue
		}
		for _, id := range in.Results[i].Delivered {
			line, err := s.staleRecord(ctx, id, q.MustNotContain)
			if err != nil {
				return m, err
			}
			if line != "" {
				Logf("temporal %s: %s as_of %s: %s", s.Name(), q.ID, q.AsOf, line)
			}
		}
	}
	return m, nil
}

// staleRecord describes record id when its text holds one of forbidden,
// else returns "".
func (s *System) staleRecord(ctx context.Context, id string, forbidden []string) (string, error) {
	db := s.e.Store.DB()
	var typ, text, detail string
	switch {
	case strings.HasPrefix(id, "evt_"):
		var kind, src, sess, doc string
		if err := db.QueryRowContext(ctx, `SELECT e.content, e.kind, s.kind, COALESCE(e.session_id,''), COALESCE(e.doc_id,'')
			FROM events e JOIN sources s ON s.id = e.source_id WHERE e.id = ?`, id).Scan(&text, &kind, &src, &sess, &doc); err != nil {
			return "", err
		}
		rows, err := db.QueryContext(ctx, `SELECT p.relation, f.id, f.status FROM provenance p JOIN facts f ON f.id = p.derived_id
			WHERE p.source_type = 'event' AND p.source_id = ? AND p.derived_type = 'fact' ORDER BY f.id, p.relation`, id)
		if err != nil {
			return "", err
		}
		var states []string
		for rows.Next() {
			var rel, fid, st string
			if err := rows.Scan(&rel, &fid, &st); err != nil {
				rows.Close()
				return "", err
			}
			states = append(states, rel+":"+fid+":"+st)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
		why := "states no fact (no provenance edge), and no stale-value rule matched"
		if len(states) > 0 {
			why = "states facts " + strings.Join(states, ",")
		}
		typ, detail = "event", fmt.Sprintf("kind %s, source %s, session %q, document %q; %s", kind, src, sess, doc, why)
	case strings.HasPrefix(id, "fct_"):
		var st, from, to string
		if err := db.QueryRowContext(ctx, `SELECT text, status, valid_from, COALESCE(valid_to,'') FROM facts WHERE id = ?`, id).Scan(&text, &st, &from, &to); err != nil {
			return "", err
		}
		typ, detail = "fact", fmt.Sprintf("status %s, valid [%s, %s)", st, from, to)
	default:
		return "", nil
	}
	norm := eval.Normalize(text)
	for _, f := range forbidden {
		if strings.Contains(norm, eval.Normalize(f)) {
			return fmt.Sprintf("%s %s (%s)", typ, id, detail), nil
		}
	}
	return "", nil
}
