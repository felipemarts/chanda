package sleep

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/core"
)

const savepoint = "sleep_stage"

// applyGated stages the plan and runs the evaluation gate
// (docs/decisions/0061-sleep-staging-and-gate.md):
//
//  1. Take the single database connection (no other reader or writer can
//     interleave until it is released), write the snapshot with VACUUM INTO
//     and BEGIN on that same connection, so the snapshot is exactly the
//     state the transaction starts from.
//  2. Score the gate set and count integrity violations (before).
//  3. SAVEPOINT; write every change of the plan, pages and items as staged;
//     promote staged items so the evaluator measures them; score again
//     (after).
//  4. A drop above sleep.gate_tolerance points, or more integrity
//     violations, rolls back to the savepoint (the database returns to the
//     snapshot state, including the audit rows the staged changes wrote,
//     which were never committed) and appends ActSleepRevert, chained to the
//     last committed audit row. Otherwise the savepoint is released.
//  5. COMMIT. A crash at any point before this leaves nothing staged behind.
func (s *Sleeper) applyGated(ctx context.Context, r *run, p *plan) error {
	conn, err := s.db().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := s.snapshot(ctx, conn, r); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	g := &r.res.Gate
	g.Ran, g.Tolerance = true, s.env.Cfg.Sleep.GateTolerance
	if g.Before, err = s.d.Evaluator.Score(ctx, tx, r.ns); err != nil {
		return err
	}
	if gs, ok := s.d.Evaluator.(GateSizer); ok {
		n, err := gs.GateSize(ctx, tx, r.ns)
		if err != nil {
			return err
		}
		if n == 0 {
			r.res.notice("gate: no approved gate questions; the score measured nothing and only the integrity check guarded this run")
		}
	}
	if s.d.Integrity != nil {
		if g.ViolationsBefore, err = s.d.Integrity.Check(ctx, tx, r.ns); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return err
	}
	touched, err := s.apply(ctx, tx, r, p)
	if err != nil {
		return err
	}
	if err := s.promote(ctx, tx, r, touched); err != nil {
		return err
	}
	if g.After, err = s.d.Evaluator.Score(ctx, tx, r.ns); err != nil {
		return err
	}
	if s.d.Integrity != nil {
		if g.ViolationsAfter, err = s.d.Integrity.Check(ctx, tx, r.ns); err != nil {
			return err
		}
	}
	switch drop := g.Before - g.After; {
	case drop > g.Tolerance:
		g.Reverted, g.Reason = true, fmt.Sprintf("score dropped %.2f points", drop)
	case g.ViolationsAfter > g.ViolationsBefore:
		g.Reverted, g.Reason = true, "integrity violations rose"
	}
	if g.Reverted {
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO "+savepoint); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "RELEASE "+savepoint); err != nil {
		return err
	}
	if g.Reverted {
		c := r.res.Counts
		discarded := map[string]int{
			"facts": sumActions(c.FactActions), "verdicts": sumActions(c.Verdicts),
			"merges": len(c.MergesApplied), "pages": len(c.PagesCreated), "page_items": len(c.PageItemsAdded),
		}
		if c.Conflicts != nil {
			discarded["conflict_verdicts"] = sumActions(c.Conflicts.Judged)
			discarded["supersessions"] = len(c.Conflicts.Supersessions)
		}
		if err := s.audit(ctx, tx, r.ns, audit.ActSleepRevert, r.res.RunID, map[string]any{
			"reason": g.Reason, "score_before": g.Before, "score_after": g.After, "tolerance": g.Tolerance,
			"violations_before": g.ViolationsBefore, "violations_after": g.ViolationsAfter, "snapshot": r.res.Snapshot,
			"discarded": discarded,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	s.checkSnapshot(r)
	return nil
}

// checkSnapshot handles a snapshot file removed while the run was in
// progress (forget deletes every snapshot, also during a run). The gate
// never reads the file — reverting is the savepoint rollback — so the
// outcome stands; the run only stops pointing at a file that is gone.
func (s *Sleeper) checkSnapshot(r *run) {
	if r.snapshotAbs == "" {
		return
	}
	if _, err := os.Stat(r.snapshotAbs); err == nil {
		return
	}
	r.res.Snapshot, r.snapshotAbs = "", ""
	msg := "snapshot file removed during the run (forget); no snapshot is kept for this run"
	if r.res.Gate.Reverted {
		msg += "; the revert did not need it: the staged changes were rolled back in the transaction"
	}
	r.res.notice(msg)
}

// snapshot writes VACUUM INTO {snapshot_dir}/{run id}.db. A relative
// snapshot_dir is relative to the database file; the path recorded in
// sleep_runs.snapshot is the configured (relative) one, so it does not
// depend on where the database lives.
func (s *Sleeper) snapshot(ctx context.Context, conn *sql.Conn, r *run) error {
	dir := s.env.Cfg.Sleep.SnapshotDir
	if dir == "" {
		dir = "snapshots"
	}
	rel := filepath.Join(dir, r.res.RunID+".db")
	abs := rel
	if !filepath.IsAbs(rel) {
		abs = filepath.Join(filepath.Dir(s.env.Store.Path()), rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return core.Wrap(core.CodeInternal, err, "sleep: snapshot directory")
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", abs); err != nil {
		return core.Wrap(core.CodeInternal, err, "sleep: snapshot")
	}
	r.res.Snapshot, r.snapshotAbs = rel, abs
	return nil
}

// ---------------------------------------------------------------------------
// Staging the plan. Everything is re-validated against the current state,
// since the plan was built outside the transaction.

type evState int

const (
	evGone evState = iota
	evRetired
	evLive
)

func eventStates(ctx context.Context, tx *sql.Tx, ns string, ids []string) (map[string]evState, map[string]core.Zone, error) {
	st := map[string]evState{}
	zones := map[string]core.Zone{}
	for _, id := range ids {
		var z string
		var retired sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT trust_zone, retired_at FROM events WHERE id=? AND namespace=?`, id, ns).Scan(&z, &retired)
		switch {
		case err == sql.ErrNoRows:
			st[id] = evGone
		case err != nil:
			return nil, nil, err
		case retired.Valid:
			st[id], zones[id] = evRetired, core.Zone(z)
		default:
			st[id], zones[id] = evLive, core.Zone(z)
		}
	}
	return st, zones, nil
}

func (s *Sleeper) markProcessed(ctx context.Context, tx *sql.Tx, r *run, step string, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO event_processing(namespace, event_id, step, sleep_run_id, processed_at)
			VALUES (?,?,?,?,?)`, r.ns, id, step, r.res.RunID, s.now()); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.audit(ctx, tx, r.ns, ActEventsMarked, r.res.RunID, map[string]any{"step": step, "events": len(ids)})
}

// apply writes the plan inside the savepoint and returns the pages that got
// staged items, sorted.
func (s *Sleeper) apply(ctx context.Context, tx *sql.Tx, r *run, p *plan) ([]string, error) {
	c := &r.res.Counts
	for _, j := range p.conflicts {
		if err := s.applyConflict(ctx, tx, r, j); err != nil {
			return nil, err
		}
	}
	for _, b := range p.batches {
		st, _, err := eventStates(ctx, tx, r.ns, b.events)
		if err != nil {
			return nil, err
		}
		gone := false
		for _, id := range b.events {
			gone = gone || st[id] == evGone
		}
		if gone {
			// An event the model read was forgotten meanwhile: its content may
			// be in any answer of the batch. Drop the batch.
			c.CandidatesDrop += len(b.cands)
			continue
		}
		for _, cand := range b.cands {
			live := true
			for _, id := range cand.OriginEventIDs {
				live = live && st[id] == evLive
			}
			if !live {
				c.CandidatesDrop++
				continue
			}
			action, err := s.assert(ctx, tx, r.ns, cand)
			if err != nil {
				if code := core.CodeOf(err); code == core.CodeRejected || code == core.CodeInvalidArgument {
					c.CandidatesDrop++
					continue
				}
				return nil, err
			}
			c.FactActions[action]++
		}
		var processed []string
		for _, id := range b.events {
			if st[id] != evGone {
				processed = append(processed, id)
			}
		}
		if err := s.markProcessed(ctx, tx, r, StepExtractFacts, processed); err != nil {
			return nil, err
		}
	}
	if len(p.skipped) > 0 {
		st, _, err := eventStates(ctx, tx, r.ns, p.skipped)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, id := range p.skipped {
			if st[id] != evGone {
				ids = append(ids, id)
			}
		}
		if err := s.markProcessed(ctx, tx, r, StepExtractFacts, ids); err != nil {
			return nil, err
		}
	}
	for _, v := range p.verdicts {
		var err error
		if v.kind == "fact" {
			err = s.applyFactVerdict(ctx, tx, r, v)
		} else {
			err = s.applyMergeVerdict(ctx, tx, r, v)
		}
		if err != nil {
			return nil, err
		}
	}
	touched := map[string]bool{}
	for _, l := range p.lessons {
		pid, err := s.stageLesson(ctx, tx, r, l)
		if err != nil {
			return nil, err
		}
		if pid != "" {
			touched[pid] = true
		}
	}
	for _, f := range p.fichas {
		pid, err := s.stageFicha(ctx, tx, r, f)
		if err != nil {
			return nil, err
		}
		if pid != "" {
			touched[pid] = true
		}
	}
	out := make([]string, 0, len(touched))
	for pid := range touched {
		out = append(out, pid)
	}
	sort.Strings(out)
	return out, nil
}

// assert runs the sink for one candidate inside its own savepoint, so a
// rejected candidate leaves no partial writes behind.
func (s *Sleeper) assert(ctx context.Context, tx *sql.Tx, ns string, c Candidate) (string, error) {
	if _, err := tx.ExecContext(ctx, "SAVEPOINT sleep_candidate"); err != nil {
		return "", err
	}
	action, err := s.d.Facts.AssertCandidate(ctx, tx, ns, c)
	if err != nil {
		if _, e := tx.ExecContext(ctx, "ROLLBACK TO sleep_candidate"); e != nil {
			return "", e
		}
	}
	if _, e := tx.ExecContext(ctx, "RELEASE sleep_candidate"); e != nil {
		return "", e
	}
	return action, err
}

// applyFactVerdict applies a verdict on a quarantined fact by rule:
//
//   - keep_pending: nothing changes.
//   - reject: status rejected.
//   - coexist: multi predicate → active next to the others; single
//     predicate → refused (two active values cannot coexist).
//   - supersede: refused while any active fact for the same subject and
//     predicate has a higher zone (only promote raises authority). Otherwise,
//     multi → active; single with no active fact → active; single with a
//     later valid_from than the active fact → closes it (valid_to,
//     superseded_by, supersedes edge) and becomes active; with an earlier or
//     equal valid_from → kept closed as history.
//
// A refused verdict leaves the fact quarantined.
func (s *Sleeper) applyFactVerdict(ctx context.Context, tx *sql.Tx, r *run, v verdict) error {
	c := &r.res.Counts
	var subject, pred, zone, validFrom, status string
	var single int
	err := tx.QueryRowContext(ctx, `SELECT subject, predicate, trust_zone, valid_from, is_single, status FROM facts
		WHERE id=? AND namespace=?`, v.id, r.ns).Scan(&subject, &pred, &zone, &validFrom, &single, &status)
	if err == sql.ErrNoRows || (err == nil && status != string(core.FactQuarantined)) {
		return nil // changed since the plan was made
	}
	if err != nil {
		return err
	}
	c.Verdicts["fact:"+v.verdict]++
	refuse := func(reason string) error {
		c.VerdictsRefused = append(c.VerdictsRefused, v.id)
		r.res.notice(fmt.Sprintf("verdict %s on %s refused: %s", v.verdict, v.id, reason))
		return nil
	}
	setStatus := func(id string, st core.FactStatus) error {
		_, err := tx.ExecContext(ctx, `UPDATE facts SET status=? WHERE id=?`, string(st), id)
		return err
	}
	applied := ""
	switch v.verdict {
	case "keep_pending":
		return nil
	case "reject":
		if err := setStatus(v.id, core.FactRejected); err != nil {
			return err
		}
		applied = "rejected"
		c.FactsRejected = append(c.FactsRejected, v.id)
	case "coexist":
		if single == 1 {
			return refuse("single predicate: values cannot coexist")
		}
		if err := setStatus(v.id, core.FactActive); err != nil {
			return err
		}
		applied = "activated"
		c.FactsActivated = append(c.FactsActivated, v.id)
	case "supersede":
		type act struct{ id, zone, validFrom string }
		rows, err := tx.QueryContext(ctx, `SELECT id, trust_zone, valid_from FROM facts
			WHERE namespace=? AND subject=? AND predicate=? AND status='active' ORDER BY id`, r.ns, subject, pred)
		if err != nil {
			return err
		}
		var actives []act
		for rows.Next() {
			var a act
			if err := rows.Scan(&a.id, &a.zone, &a.validFrom); err != nil {
				rows.Close()
				return err
			}
			actives = append(actives, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, a := range actives {
			if core.Zone(a.zone).Rank() > core.Zone(zone).Rank() {
				return refuse("an active fact from a higher zone; only promote can override it")
			}
		}
		switch {
		case single == 0 || len(actives) == 0:
			if err := setStatus(v.id, core.FactActive); err != nil {
				return err
			}
			applied = "activated"
			c.FactsActivated = append(c.FactsActivated, v.id)
		case validFrom > actives[0].validFrom:
			old := actives[0]
			if _, err := tx.ExecContext(ctx, `UPDATE facts SET status='superseded', valid_to=?, superseded_by=? WHERE id=?`,
				validFrom, v.id, old.id); err != nil {
				return err
			}
			if err := setStatus(v.id, core.FactActive); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
				VALUES (?,'fact',?,'fact',?,?,?)`, r.ns, v.id, old.id, core.RelSupersedes, s.now()); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, r.ns, audit.ActSupersede, old.id, map[string]any{"by": v.id, "run": r.res.RunID}); err != nil {
				return err
			}
			applied = "superseded"
			c.FactsSuperseded = append(c.FactsSuperseded, old.id)
			c.FactsActivated = append(c.FactsActivated, v.id)
		default:
			if _, err := tx.ExecContext(ctx, `UPDATE facts SET status='superseded', valid_to=?, superseded_by=? WHERE id=?`,
				actives[0].validFrom, actives[0].id, v.id); err != nil {
				return err
			}
			applied = "history"
			c.FactsHistory = append(c.FactsHistory, v.id)
		}
	default:
		return refuse("unknown verdict")
	}
	return s.audit(ctx, tx, r.ns, ActFactVerdict, v.id, map[string]any{"verdict": v.verdict, "applied": applied, "run": r.res.RunID})
}

// applyMergeVerdict applies a verdict on a pending merge proposal: supersede
// accepts it (entity_b is merged into entity_a, the older id: merged_into is
// set and its aliases move to entity_a); coexist and reject mark it
// rejected; keep_pending leaves it pending. Facts keep their subject id;
// readers resolve entities.merged_into. An accepted merge then reconciles
// the single predicates of the merged set (reconcileMerge).
func (s *Sleeper) applyMergeVerdict(ctx context.Context, tx *sql.Tx, r *run, v verdict) error {
	c := &r.res.Counts
	var a, b, status string
	err := tx.QueryRowContext(ctx, `SELECT p.entity_a, p.entity_b, p.status FROM entity_merge_proposals p
		JOIN entities ea ON ea.id=p.entity_a JOIN entities eb ON eb.id=p.entity_b
		WHERE p.id=? AND p.namespace=? AND ea.merged_into IS NULL AND eb.merged_into IS NULL`, v.id, r.ns).Scan(&a, &b, &status)
	if err == sql.ErrNoRows || (err == nil && status != "pending") {
		return nil
	}
	if err != nil {
		return err
	}
	c.Verdicts["merge:"+v.verdict]++
	switch v.verdict {
	case "keep_pending":
		return nil
	case "supersede":
		k := pairKey(a, b)
		into, from := k[0], k[1]
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE entities SET merged_into=? WHERE id=?`, []any{into, from}},
			{`UPDATE entity_aliases SET entity_id=? WHERE namespace=? AND entity_id=?`, []any{into, r.ns, from}},
			{`UPDATE entity_merge_proposals SET status='accepted' WHERE id=?`, []any{v.id}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		c.MergesApplied = append(c.MergesApplied, v.id)
		if err := s.audit(ctx, tx, r.ns, audit.ActEntityMerge, from, map[string]any{"into": into, "proposal": v.id, "run": r.res.RunID}); err != nil {
			return err
		}
		return s.reconcileMerge(ctx, tx, r, into, from)
	default: // coexist, reject
		if _, err := tx.ExecContext(ctx, `UPDATE entity_merge_proposals SET status='rejected' WHERE id=?`, v.id); err != nil {
			return err
		}
		c.MergesRejected = append(c.MergesRejected, v.id)
		return s.audit(ctx, tx, r.ns, ActMergeVerdict, v.id, map[string]any{"verdict": v.verdict, "run": r.res.RunID})
	}
}

// stageLesson writes one lesson as a staged page item (and a staged page
// when new). The item cites every event the model was shown and its zone is
// the lowest of theirs; if any of them is no longer live the lesson is
// dropped. Returns the page id, or "" when nothing was written.
func (s *Sleeper) stageLesson(ctx context.Context, tx *sql.Tx, r *run, l lesson) (string, error) {
	c := &r.res.Counts
	st, zones, err := eventStates(ctx, tx, r.ns, l.events)
	if err != nil {
		return "", err
	}
	var zs []core.Zone
	for _, id := range l.events {
		if st[id] != evLive {
			c.LessonsSkipped++
			return "", nil
		}
		zs = append(zs, zones[id])
	}
	if err := s.markProcessed(ctx, tx, r, StepLessonReview, l.events); err != nil {
		return "", err
	}
	if l.skip {
		c.LessonsSkipped++
		return "", nil
	}
	zone := core.MinZone(zs...)
	now := s.now()
	pid := l.pageID
	if pid != "" {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM pages WHERE id=? AND namespace=? AND kind='lesson'`, pid, r.ns).Scan(&status)
		if err == sql.ErrNoRows || (err == nil && status != "active") {
			pid = "" // gone or no longer appendable: start a new page
		} else if err != nil {
			return "", err
		}
	}
	if pid == "" {
		pid = s.env.IDs.For(core.RecPage)
		if _, err := tx.ExecContext(ctx, `INSERT INTO pages(id, namespace, kind, title, body, version, sleep_run_id, status, trust_zone, created_at, updated_at)
			VALUES (?,?,'lesson',?,'',0,?,'staged',?,?,?)`, pid, r.ns, l.title, r.res.RunID, string(zone), now, now); err != nil {
			return "", err
		}
		c.PagesCreated = append(c.PagesCreated, pid)
	}
	var ordinal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM page_items WHERE page_id=?`, pid).Scan(&ordinal); err != nil {
		return "", err
	}
	item := s.env.IDs.For(core.RecPageItem)
	if _, err := tx.ExecContext(ctx, `INSERT INTO page_items(id, namespace, page_id, ordinal, text, status, trust_zone, sleep_run_id, created_at)
		VALUES (?,?,?,?,?,'staged',?,?,?)`, item, r.ns, pid, ordinal, l.text, string(zone), r.res.RunID, now); err != nil {
		return "", err
	}
	for _, id := range l.events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
			VALUES (?,'page_item',?,'event',?,?,?)`, r.ns, item, id, core.RelCites, now); err != nil {
			return "", err
		}
	}
	c.PageItemsAdded = append(c.PageItemsAdded, item)
	return pid, nil
}

// promote turns this run's staged items and pages active inside the
// savepoint (so the gate measures them) and recomputes each touched page.
func (s *Sleeper) promote(ctx context.Context, tx *sql.Tx, r *run, pages []string) error {
	if err := s.promoteSupersessions(ctx, tx, r); err != nil {
		return err
	}
	created := map[string]bool{}
	for _, id := range r.res.Counts.PagesCreated {
		created[id] = true
	}
	for _, pid := range pages {
		res, err := tx.ExecContext(ctx, `UPDATE page_items SET status='active' WHERE page_id=? AND status='staged' AND sleep_run_id=?`, pid, r.res.RunID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		items, err := loadItems(ctx, tx, pid)
		if err != nil {
			return err
		}
		if err := s.refreshPage(ctx, tx, pid, items); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, r.ns, audit.ActPageUpdate, pid, map[string]any{
			"items_added": n, "created": created[pid], "run": r.res.RunID}); err != nil {
			return err
		}
	}
	return nil
}
