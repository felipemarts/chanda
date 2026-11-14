package app

import (
	"context"
	"database/sql"
	"strings"
	"unicode"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/dedup"
	"github.com/felipemarts/chanda-mcp/internal/facts"
	"github.com/felipemarts/chanda-mcp/internal/rank"
	"github.com/felipemarts/chanda-mcp/internal/read"
	"github.com/felipemarts/chanda-mcp/internal/write"
)

// afterEvent is the write-path hook chain (BASE.md write steps 7 to 10, in
// that order: contradiction before duplication), run inside the write
// transaction for every newly created event:
//
//	(a) deterministic extractors -> facts.Assert with the event as origin;
//	(b) every fact inserted by (a) goes into embed_queue;
//	(c) near-duplicate marking of the event, last;
//	(d) the per-write integrity check of the facts created in (a).
//
// The fact_assertion event written by assert_fact skips (a): its fact is
// asserted by the call itself (AssertFact), before this chain runs. With
// Options.DisableFacts, (a) never runs.
func (a *App) afterEvent(ctx context.Context, tx *sql.Tx, ev write.Event) error {
	var created []string
	if ev.Kind != core.EventFact && !a.opt.DisableFacts {
		var docURI string
		if ev.DocID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT uri FROM documents WHERE id = ?`, ev.DocID).Scan(&docURI); err != nil {
				return err
			}
		}
		res, err := a.facts.AfterEvent(ctx, tx, facts.Event{
			ID: ev.ID, Namespace: ev.Namespace, Content: ev.Content, TrustZone: ev.TrustZone,
			RecordedAt: ev.RecordedAt, ValidFrom: ev.ValidFrom, HeadingPath: ev.HeadingPath, DocURI: docURI,
		})
		if err != nil {
			return err
		}
		for _, r := range res {
			if newFact(r) {
				created = append(created, r.FactID)
			}
		}
	}
	if err := a.enqueueFacts(ctx, tx, ev.Namespace, created); err != nil {
		return err
	}
	if err := a.markNearDuplicate(ctx, tx, ev); err != nil {
		return err
	}
	return a.checkDerived(ctx, tx, ev.Namespace, created)
}

// newFact reports whether Assert wrote a new facts row.
func newFact(r facts.AssertResult) bool {
	return r.FactID != "" && r.Action != facts.ActionConfirmed
}

// enqueueFacts puts facts in the embedding queue (write step 7; the facts
// package does not, since it owns no queue).
func (a *App) enqueueFacts(ctx context.Context, tx *sql.Tx, ns string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := core.FormatTime(a.e.Clock.Now())
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO embed_queue(namespace, record_type, record_id, enqueued_at)
			VALUES (?,?,?,?) ON CONFLICT(record_type, record_id) DO NOTHING`, ns, string(core.RecFact), id, now); err != nil {
			return err
		}
	}
	return nil
}

// maxMatchTerms bounds the OR query that picks near-duplicate candidates.
const maxMatchTerms = 32

// markNearDuplicate is write step 10. Candidates are the top
// write.near_dup_candidates live events of the same namespace and zone by
// FTS5 BM25 over the new event's text, queried through tx (the single
// connection forbids ExactIndex.Search inside a transaction). The new text
// and the candidates are embedded in memory with the configured Embedder;
// the most similar candidate that dedup.IsNearDuplicate accepts (cosine
// above write.near_dup_threshold and identical numbers, dates, entities and
// negations) receives the duplicate_of edge new -> older.
func (a *App) markNearDuplicate(ctx context.Context, tx *sql.Tx, ev write.Event) error {
	n := a.e.Cfg.Write.NearDupCandidates
	expr := matchAny(ev.Content)
	if n <= 0 || expr == "" {
		return nil
	}
	ids, err := read.TopEventMatches(ctx, tx, ev.Namespace, expr, n,
		`b.id <> ? AND b.retired_at IS NULL AND b.trust_zone = ?`, ev.ID, string(ev.TrustZone))
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	texts := make([]string, len(ids))
	for i, id := range ids {
		if err := tx.QueryRowContext(ctx, `SELECT content FROM events WHERE id = ?`, id).Scan(&texts[i]); err != nil {
			return err
		}
	}
	vecs, err := a.emb.Embed(ctx, append([]string{ev.Content}, texts...))
	if err != nil {
		return core.Wrap(core.CodeInternal, err, "near-duplicate embedding")
	}
	best, bestCos := -1, 0.0
	for i := range ids {
		cos := dot(vecs[0], vecs[i+1])
		if ok, _ := dedup.IsNearDuplicate(ev.Content, texts[i], cos, a.e.Cfg.Write.NearDupThreshold); !ok {
			continue
		}
		if best < 0 || cos > bestCos || (cos == bestCos && ids[i] < ids[best]) {
			best, bestCos = i, cos
		}
	}
	if best < 0 {
		return nil
	}
	_, err = a.guard.Mark(ctx, tx, ev.Namespace, core.RecEvent, ev.ID, core.RecEvent, ids[best])
	return err
}

// dot is the cosine of two L2-normalized vectors, summed in a fixed order.
func dot(x, y []float32) float64 {
	var s float64
	for i := range x {
		if i < len(y) {
			s += float64(x[i]) * float64(y[i])
		}
	}
	return s
}

// matchAny builds a safe FTS5 query: the text's distinct tokens (letters and
// digits, lowercased), each quoted, joined with OR, in order of appearance.
func matchAny(text string) string {
	seen := map[string]bool{}
	var terms []string
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		terms = append(terms, `"`+tok+`"`)
		if len(terms) == maxMatchTerms {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

// checkDerived is the integrity check run at the end of every write
// (BASE.md "Integrity rules"), restricted to the derived records the write
// created: each needs at least one derived_from edge to an event of its
// namespace, and its zone must not exceed the lowest zone of those events.
// A violation fails the whole write with CodeIntegrity. The whole-namespace
// prov.Check stays in verify (and sleep).
func (a *App) checkDerived(ctx context.Context, tx *sql.Tx, ns string, factIDs []string) error {
	for _, id := range factIDs {
		var zone string
		if err := tx.QueryRowContext(ctx, `SELECT trust_zone FROM facts WHERE id = ? AND namespace = ?`, id, ns).Scan(&zone); err != nil {
			if err == sql.ErrNoRows {
				return core.Errf(core.CodeIntegrity, "fact %s written in this call is missing", id)
			}
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT e.trust_zone FROM provenance p JOIN events e ON e.id = p.source_id
			WHERE p.derived_type = ? AND p.derived_id = ? AND p.source_type = ? AND p.relation = ? AND e.namespace = ?
			ORDER BY e.id`, string(core.RecFact), id, string(core.RecEvent), core.RelDerivedFrom, ns)
		if err != nil {
			return err
		}
		var zones []core.Zone
		for rows.Next() {
			var z string
			if err := rows.Scan(&z); err != nil {
				rows.Close()
				return err
			}
			zones = append(zones, core.Zone(z))
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(zones) == 0 {
			return core.Errf(core.CodeIntegrity, "fact %s has no origin event", id)
		}
		if min := core.MinZone(zones...); core.Zone(zone).Rank() > min.Rank() {
			return core.Errf(core.CodeIntegrity, "fact %s has zone %s above its origins (%s)", id, zone, min)
		}
	}
	return nil
}

// rankCandidate is the read path's candidate as internal/rank sees it.
func rankCandidate(c read.Candidate) rank.Candidate {
	val := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	s := c.Scores
	em := c.EntityMatch
	return rank.Candidate{
		ID: c.ID, Type: c.Type, Zone: c.Zone, AgeSeconds: c.AgeSeconds,
		BM25: val(s.BM25), Cosine: val(s.Cosine), Rerank: val(s.Rerank), RRF: val(s.RRF), Utility: val(s.Utility),
		EntityMatch: &em,
	}
}

func rankCandidates(cs []read.Candidate) []rank.Candidate {
	out := make([]rank.Candidate, len(cs))
	for i, c := range cs {
		out[i] = rankCandidate(c)
	}
	return out
}

// learnedAdapter shows the stage-2 ranker to the read path (shadow mode).
type learnedAdapter struct{ l *rank.Learned }

func (l learnedAdapter) Score(ctx context.Context, ns string, c read.Candidate) (float64, bool) {
	v, ok, err := l.l.Score(ctx, ns, rankCandidate(c))
	if err != nil {
		return 0, false
	}
	return v, ok
}

// ScoreAll scores the whole list with one model lookup (read.LearnedBatch).
func (l learnedAdapter) ScoreAll(ctx context.Context, ns string, cs []read.Candidate) (map[string]float64, bool) {
	m, err := l.l.ScoreAll(ctx, ns, rankCandidates(cs))
	if err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// termAdapter is read step 7's learned term: the promoted model's score
// (rank.Terms; decision 0070).
type termAdapter struct{ r *rank.Ranker }

func (t termAdapter) Terms(ctx context.Context, ns string, cs []read.Candidate) (map[string]float64, error) {
	return t.r.Terms(ctx, ns, rankCandidates(cs))
}

// modelTerm scores with the newest non-retired model (the one rank.Promote
// evaluates), for the promotion evaluator.
type modelTerm struct{ l *rank.Learned }

func (t modelTerm) Terms(ctx context.Context, ns string, cs []read.Candidate) (map[string]float64, error) {
	return t.l.ScoreAll(ctx, ns, rankCandidates(cs))
}
