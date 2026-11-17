package app

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/facts"
	"github.com/felipemarts/chanda/internal/facts/norm"
	"github.com/felipemarts/chanda/internal/read"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// dateOnly is the layout of a bare date.
const dateOnly = "2006-01-02"

// asOfTime parses an optional as_of given by a caller (context, search,
// timeline). A bare date means the end of that day in UTC (the last
// nanosecond): "what held on 2023-05-20" includes everything dated that day,
// and a date-only valid_from (stored as 00:00 UTC) is valid on its own day
// (decision 0135). Zero means no as_of.
func asOfTime(s string) (time.Time, error) {
	if s = strings.TrimSpace(s); s == "" {
		return time.Time{}, nil
	}
	t, err := core.ParseTime(s)
	if err != nil {
		return t, err
	}
	if _, err := time.Parse(dateOnly, s); err == nil {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}

func strs(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func item(it read.Item) chanda.Item {
	text := it.Text
	if it.Type == core.RecPage && it.Title != "" {
		text = it.Title + "\n" + it.Text
	}
	out := chanda.Item{
		ID: it.ID, Type: it.Type, Text: text, ValidFrom: it.ValidFrom, ValidTo: it.ValidTo, Status: it.Status,
		Origin: chanda.Origin{SourceURI: first(it.Origin.SourceURIs), SourceLabel: it.Origin.SourceLabel, DocURI: first(it.Origin.DocURIs), EventIDs: strs(it.Origin.EventIDs)},
		Zone:   it.Zone, Flags: it.Flags, SupersededBy: it.SupersededBy,
	}
	if it.Scores.Final != nil {
		out.Score = *it.Scores.Final
	}
	return out
}

func items(xs []read.Item) []chanda.Item {
	out := make([]chanda.Item, len(xs))
	for i, it := range xs {
		out[i] = item(it)
	}
	return out
}

// Context returns the pre-context block (read path, mode context).
func (a *App) Context(ctx context.Context, in chanda.ContextInput) (chanda.ContextOutput, error) {
	c := a.begin(ctx, in.Namespace)
	asOf, err := asOfTime(in.AsOf)
	if err != nil {
		return chanda.ContextOutput{Meta: c.meta(0)}, err
	}
	if in.BudgetTokens > a.e.Cfg.Read.ContextMaxTokens && a.e.Cfg.Read.ContextMaxTokens > 0 {
		c.notice("budget_capped: budget_tokens lowered to the ceiling read.context_max_tokens")
	}
	r, err := a.reader.Context(ctx, read.ContextInput{
		Namespace: c.ns, Query: a.agenticQuery(ctx, c, in.Query), BudgetTokens: in.BudgetTokens, AsOf: asOf, SessionID: in.SessionID, Actor: c.actor.Name,
	})
	for _, n := range r.Notices {
		c.notice(n)
	}
	out := chanda.ContextOutput{Meta: c.meta(r.AuditID)}
	if err != nil {
		return out, coded(err)
	}
	out.RetrievalID, out.Text, out.Items, out.Reason, out.TokensUsed = r.RetrievalID, r.Text, items(r.Items), r.Reason, r.TokensUsed
	return out, nil
}

// Search returns a ranked list (read path, mode search).
func (a *App) Search(ctx context.Context, in chanda.SearchInput) (chanda.SearchOutput, error) {
	c := a.begin(ctx, in.Namespace)
	asOf, err := asOfTime(in.Filters.AsOf)
	if err != nil {
		return chanda.SearchOutput{Meta: c.meta(0)}, err
	}
	if in.Filters.SourceURI != "" {
		c.notice("source_uri_filter_ignored: filtering by source is not supported yet")
	}
	r, err := a.reader.Search(ctx, read.SearchInput{
		Namespace: c.ns, Query: a.agenticQuery(ctx, c, in.Query), Types: in.Filters.Types, Zones: in.Filters.Zones, AsOf: asOf,
		Limit: in.Limit, IncludeHistory: in.IncludeHistory, SessionID: in.SessionID, Actor: c.actor.Name,
	})
	for _, n := range r.Notices {
		c.notice(n)
	}
	out := chanda.SearchOutput{Meta: c.meta(r.AuditID)}
	if err != nil {
		return out, coded(err)
	}
	out.RetrievalID, out.Items, out.Reason = r.RetrievalID, items(r.Items), r.Reason
	return out, nil
}

func recordItem(r read.Record, o read.Origin) chanda.Item {
	text := r.Text
	if r.Type == core.RecPage && r.Title != "" {
		text = r.Title + "\n" + r.Text
	}
	return chanda.Item{
		ID: r.ID, Type: r.Type, Text: text, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo, Status: r.Status,
		Origin: chanda.Origin{SourceURI: first(o.SourceURIs), SourceLabel: o.SourceLabel, DocURI: first(o.DocURIs), EventIDs: strs(o.EventIDs)},
		Zone:   r.Zone, Flags: r.Flags,
	}
}

// Get returns one record with its origins and its direct provenance edges.
func (a *App) Get(ctx context.Context, in chanda.GetInput) (chanda.GetOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.GetOutput{Meta: c.meta(0), Provenance: []chanda.Edge{}}
	g, err := a.reader.Get(ctx, c.ns, strings.TrimSpace(in.ID), in.ExpandNeighbors)
	if err != nil {
		return out, coded(err)
	}
	out.Item, out.RecordedAt = recordItem(g.Record, g.Origin), g.Record.RecordedAt
	db := a.e.Store.DB()
	rows, err := db.QueryContext(ctx, `SELECT relation, source_type, source_id FROM provenance
		WHERE namespace = ? AND derived_id = ? ORDER BY relation, source_type, source_id`, c.ns, g.Record.ID)
	if err != nil {
		return out, coded(err)
	}
	for rows.Next() {
		var e chanda.Edge
		var st string
		if err := rows.Scan(&e.Relation, &st, &e.SourceID); err != nil {
			rows.Close()
			return out, coded(err)
		}
		e.SourceType = core.RecordType(st)
		out.Provenance = append(out.Provenance, e)
	}
	if err := rows.Close(); err != nil {
		return out, coded(err)
	}
	if g.Record.Type == core.RecFact {
		fd := &chanda.FactDetail{Subject: g.Record.Subject, Predicate: g.Record.Predicate, Value: g.Record.Value,
			SupersededBy: g.Record.SupersededBy}
		// The read path resolved the subject through accepted merges.
		if err := db.QueryRowContext(ctx, `SELECT f.confidence, COALESCE(e.canonical_name, '') FROM facts f
			LEFT JOIN entities e ON e.id = ? WHERE f.id = ?`, g.Record.Subject, g.Record.ID).Scan(&fd.Confidence, &fd.SubjectName); err != nil {
			return out, coded(err)
		}
		out.Fact = fd
	}
	for _, n := range []*read.Record{g.Previous, g.Next} {
		if n != nil {
			out.Neighbors = append(out.Neighbors, recordItem(*n, read.Origin{}))
		}
	}
	return out, nil
}

// Timeline returns the facts of a subject (and predicate) from the oldest to
// the current one, each with the origin of the change. With as_of, only the
// facts that held at that date.
func (a *App) Timeline(ctx context.Context, in chanda.TimelineInput) (chanda.TimelineOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.TimelineOutput{Meta: c.meta(0), Entries: []chanda.TimelineEntry{}}
	asOf, err := asOfTime(in.AsOf)
	if err != nil {
		return out, err
	}
	db := a.e.Store.DB()
	id, ok, err := a.facts.LookupEntity(ctx, db, c.ns, strings.TrimSpace(in.Subject))
	if err != nil {
		return out, coded(err)
	}
	if !ok {
		return out, core.Errf(core.CodeNotFound, "subject %q not found", in.Subject)
	}
	out.Subject = id
	if err := db.QueryRowContext(ctx, `SELECT canonical_name FROM entities WHERE id = ?`, id).Scan(&out.SubjectName); err != nil {
		return out, coded(err)
	}
	entries, err := a.facts.Timeline(ctx, c.ns, id, in.Predicate)
	if err != nil {
		return out, coded(err)
	}
	var keep map[string]bool
	if !asOf.IsZero() {
		held, err := a.facts.AsOf(ctx, c.ns, id, in.Predicate, asOf)
		if err != nil {
			return out, coded(err)
		}
		keep = map[string]bool{}
		for _, f := range held {
			keep[f.ID] = true
		}
	}
	for _, e := range entries {
		if keep != nil && !keep[e.ID] {
			continue
		}
		origin, err := a.originOf(ctx, e.Origins)
		if err != nil {
			return out, coded(err)
		}
		out.Entries = append(out.Entries, chanda.TimelineEntry{
			FactID: e.ID, Predicate: e.Predicate, Value: e.Value, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo,
			RecordedAt: e.RecordedAt, Status: string(e.Status), Zone: e.TrustZone, SupersededBy: e.SupersededBy, Origin: origin,
		})
	}
	if a.e.Cfg.Sleep.Conflicts.On() {
		if out.Supersessions, err = a.subjectSupersessions(ctx, c.ns, id, out.SubjectName, asOf); err != nil {
			return out, coded(err)
		}
	}
	return out, nil
}

// subjectSupersessions lists the active supersessions of ns (decision 0148)
// whose old or new event names the entity: its canonical name or an alias
// as whole words after norm.Alias. With as_of, only the links in effect then
// (end date at or before it). Ordered by end date, then id.
func (a *App) subjectSupersessions(ctx context.Context, ns, entity, name string, asOf time.Time) ([]chanda.TimelineSupersession, error) {
	db := a.e.Store.DB()
	names := map[string]bool{}
	if n := norm.Alias(name); n != "" {
		names[n] = true
	}
	rows, err := db.QueryContext(ctx, `SELECT alias_norm FROM entity_aliases WHERE namespace = ? AND entity_id = ? ORDER BY alias_norm`, ns, entity)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return nil, err
		}
		if n := norm.Alias(s); n != "" {
			names[n] = true
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = db.QueryContext(ctx, `SELECT s.id, s.old_id, s.new_id, s.ends_at, s.confidence, s.created_at, o.content, n.content
		FROM supersessions s JOIN events o ON o.id = s.old_id JOIN events n ON n.id = s.new_id
		WHERE s.namespace = ? AND s.status = 'active' AND s.reverted_at IS NULL AND n.retired_at IS NULL ORDER BY s.ends_at, s.id`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chanda.TimelineSupersession
	for rows.Next() {
		var x chanda.TimelineSupersession
		var oldText, newText string
		if err := rows.Scan(&x.SupersessionID, &x.OldID, &x.NewID, &x.EndsAt, &x.Confidence, &x.CreatedAt, &oldText, &newText); err != nil {
			return nil, err
		}
		if !asOf.IsZero() {
			if t, err := core.ParseTime(x.EndsAt); err == nil && t.After(asOf) {
				continue
			}
		}
		texts := " " + norm.Alias(oldText) + " " + norm.Alias(newText) + " "
		for n := range names {
			if strings.Contains(texts, " "+n+" ") {
				out = append(out, x)
				break
			}
		}
	}
	return out, rows.Err()
}

// originOf names the events behind a fact (derived_from and confirmed_by
// edges) and the source and document of the first one.
func (a *App) originOf(ctx context.Context, os []facts.Origin) (chanda.Origin, error) {
	o := chanda.Origin{EventIDs: []string{}}
	for _, x := range os {
		if x.Type == core.RecEvent && (x.Relation == core.RelDerivedFrom || x.Relation == core.RelConfirmedBy) {
			o.EventIDs = append(o.EventIDs, x.ID)
		}
	}
	if len(o.EventIDs) == 0 {
		return o, nil
	}
	err := a.e.Store.DB().QueryRowContext(ctx, `SELECT s.uri, COALESCE(d.uri, '') FROM events e JOIN sources s ON s.id = e.source_id
		LEFT JOIN documents d ON d.id = e.doc_id WHERE e.id = ?`, o.EventIDs[0]).Scan(&o.SourceURI, &o.DocURI)
	if err == sql.ErrNoRows {
		err = nil
	}
	return o, err
}

// Explain gives the per-stage scores of a record in a logged retrieval.
func (a *App) Explain(ctx context.Context, in chanda.ExplainInput) (chanda.ExplainOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.ExplainOutput{Meta: c.meta(0)}
	ex, err := a.reader.Explain(ctx, c.ns, in.RetrievalID, in.RecordID)
	if err != nil {
		return out, coded(err)
	}
	out.RetrievalID, out.RecordID, out.Mode, out.Query, out.Candidate, out.Rank =
		ex.RetrievalID, ex.RecordID, string(ex.Mode), ex.Query, ex.Found, ex.Rank
	out.Reason = "not_a_candidate"
	if lc := ex.Candidate; lc != nil {
		out.Delivered = lc.Delivered()
		out.Scores = chanda.StageScores{BM25: lc.BM25, Cosine: lc.Cosine, RRF: lc.RRF, Rerank: lc.Rerank,
			Utility: lc.Utility, Learned: lc.Learned, Final: lc.Final}
		out.Features = lc.Features
		switch {
		case out.Delivered:
			out.Reason = "delivered"
		case lc.Dropped != "":
			out.Reason = lc.Dropped
		default:
			out.Reason = "not_delivered"
		}
	}
	return out, nil
}

// Stats summarizes a namespace.
func (a *App) Stats(ctx context.Context, in chanda.StatsInput) (chanda.StatsOutput, error) {
	c := a.begin(ctx, in.Namespace)
	out := chanda.StatsOutput{Meta: c.meta(0), Namespace: c.ns, EmbedModel: a.emb.ModelID()}
	st, err := a.reader.Stats(ctx, c.ns)
	if err != nil {
		return out, coded(err)
	}
	n := st.Counts
	out.Counts = chanda.Counts{
		Sources: n["sources"], Documents: n["documents"], Events: n["events"], RetiredEvents: n["events.retired"],
		Entities: n["entities"], Facts: n["facts"], ActiveFacts: n["facts.active"], Quarantined: n["facts.quarantined"],
		Pages: n["pages"], Embeddings: n["embeddings"], Retrievals: n["retrievals"], Outcomes: n["outcomes"],
		Tombstones: n["tombstones"],
	}
	out.DBSizeBytes, out.EmbedPending = st.DBSizeBytes, st.EmbedQueue
	if s := st.LastSleep; s != nil {
		out.LastSleep = &chanda.SleepSummary{RunID: s.ID, Status: s.Status, StartedAt: s.StartedAt, EndedAt: s.EndedAt}
	}
	out.Ranking.Enabled = a.e.Cfg.Ranking.Enabled && (st.Ranking == nil || st.Ranking.Enabled)
	if st.Ranking != nil {
		out.Ranking.ActiveModel = st.Ranking.ActiveModel
	}
	if out.Ranking.Lambda, err = a.ranker.Lambda(ctx, c.ns); err != nil {
		return out, coded(err)
	}
	err = a.e.Store.DB().QueryRowContext(ctx, `SELECT id FROM ranker_models WHERE namespace = ? AND status = 'shadow'
		ORDER BY created_at DESC, id DESC LIMIT 1`, c.ns).Scan(&out.Ranking.ShadowModel)
	if err != nil && err != sql.ErrNoRows {
		return out, coded(err)
	}
	return out, nil
}
