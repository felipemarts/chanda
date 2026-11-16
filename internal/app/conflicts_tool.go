package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/conflicts"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/read"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Conflicts is the admin control of conflict resolution (decision 0148):
// list, revert a supersession, reset the queue, scan (offline detection)
// and stats. Every mutating action (revert, reset, scan with write) is
// audited. The output always carries the counts after the action.
func (a *App) Conflicts(ctx context.Context, in chanda.ConflictsInput) (chanda.ConflictsOutput, error) {
	c := a.begin(ctx, in.Namespace)
	action := strings.TrimSpace(in.Action)
	out := chanda.ConflictsOutput{Meta: c.meta(0), Action: action, Enabled: a.e.Cfg.Sleep.Conflicts.On()}
	if !out.Enabled {
		c.notice("conflicts_off: sleep.conflicts.enabled is off, so writes queue no pair, sleep judges none and reads ignore supersessions")
	}
	var auditID int64
	var err error
	switch action {
	case chanda.ConflictsList:
		if out.Pairs, err = a.listPairs(ctx, c.ns); err == nil {
			out.Supersessions, err = a.listSupersessions(ctx, c.ns)
		}
	case chanda.ConflictsRevert:
		auditID, err = a.revertSupersession(ctx, c.ns, strings.TrimSpace(in.ID), c.actor.Name)
		out.RevertedID = strings.TrimSpace(in.ID)
	case chanda.ConflictsReset:
		auditID, out.Removed, err = a.resetConflicts(ctx, c.ns, c.actor.Name)
	case chanda.ConflictsScan:
		if !(in.MinCosine >= 0 && in.MinCosine < 1) {
			return out, core.Errf(core.CodeInvalidArgument, "conflicts: min_cosine must be in [0,1)")
		}
		var sc *chanda.ConflictScan
		sc, auditID, err = a.scanConflicts(ctx, c.ns, in.MinCosine, in.Write, in.Details, c.actor.Name)
		out.Scan = sc
	case chanda.ConflictsStats:
	default:
		return out, core.Errf(core.CodeInvalidArgument, "conflicts: action must be one of %s, not %q",
			strings.Join(chanda.ConflictsActions(), ", "), in.Action)
	}
	if err != nil {
		return out, coded(err)
	}
	if err := a.conflictStats(ctx, c.ns, &out); err != nil {
		return out, coded(err)
	}
	out.Meta = c.meta(auditID)
	return out, nil
}

// conflictStats fills the counts of ns.
func (a *App) conflictStats(ctx context.Context, ns string, out *chanda.ConflictsOutput) error {
	db := a.e.Store.DB()
	out.Verdicts, out.Reasons = map[string]int64{}, map[string]int64{}
	rows, err := db.QueryContext(ctx, `SELECT status, COALESCE(verdict,''), COALESCE(reason,''), count(*) FROM conflict_candidates
		WHERE namespace = ? GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, ns)
	if err != nil {
		return err
	}
	for rows.Next() {
		var st, v, r string
		var n int64
		if err := rows.Scan(&st, &v, &r, &n); err != nil {
			rows.Close()
			return err
		}
		if st == conflicts.StatusPending {
			out.Pending += n
			continue
		}
		out.Judged += n
		out.Verdicts[v] += n
		out.Reasons[r] += n
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return db.QueryRowContext(ctx, `SELECT COALESCE(SUM(reverted_at IS NULL AND status = 'active'), 0), COALESCE(SUM(reverted_at IS NOT NULL), 0)
		FROM supersessions WHERE namespace = ?`, ns).Scan(&out.Active, &out.Reverted)
}

func (a *App) listPairs(ctx context.Context, ns string) ([]chanda.ConflictPair, error) {
	rows, err := a.e.Store.DB().QueryContext(ctx, `SELECT c.id, c.older_id, c.newer_id, c.cosine, c.differs, c.shared, c.status,
		COALESCE(c.model_verdict,''), COALESCE(c.model_current,''), c.confidence, COALESCE(c.verdict,''), COALESCE(c.reason,''),
		COALESCE(c.sleep_run_id,''), c.created_at, COALESCE(c.judged_at,''),
		COALESCE((SELECT s.id FROM supersessions s WHERE s.pair_id = c.id ORDER BY s.id LIMIT 1), '')
		FROM conflict_candidates c WHERE c.namespace = ? ORDER BY c.id`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chanda.ConflictPair{}
	for rows.Next() {
		var p chanda.ConflictPair
		var shared string
		var conf sql.NullFloat64
		if err := rows.Scan(&p.ID, &p.OlderID, &p.NewerID, &p.Cosine, &p.Differs, &shared, &p.Status, &p.ModelVerdict, &p.ModelCurrent,
			&conf, &p.Verdict, &p.Reason, &p.RunID, &p.CreatedAt, &p.JudgedAt, &p.Supersession); err != nil {
			return nil, err
		}
		if conf.Valid {
			p.Confidence = &conf.Float64
		}
		p.Shared = []string{}
		_ = json.Unmarshal([]byte(shared), &p.Shared)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (a *App) listSupersessions(ctx context.Context, ns string) ([]chanda.Supersession, error) {
	rows, err := a.e.Store.DB().QueryContext(ctx, `SELECT id, old_id, new_id, COALESCE(pair_id,''), sleep_run_id, ends_at, confidence, status,
		created_at, COALESCE(reverted_at,'') FROM supersessions WHERE namespace = ? ORDER BY id`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chanda.Supersession{}
	for rows.Next() {
		var s chanda.Supersession
		if err := rows.Scan(&s.ID, &s.OldID, &s.NewID, &s.PairID, &s.RunID, &s.EndsAt, &s.Confidence, &s.Status, &s.CreatedAt, &s.RevertedAt); err != nil {
			return nil, err
		}
		if s.RevertedAt != "" {
			s.Status = "reverted"
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// revertSupersession ends a supersession's effect: the row stays with
// reverted_at (the judged pair keeps its verdict). Audited.
func (a *App) revertSupersession(ctx context.Context, ns, id, actor string) (int64, error) {
	if id == "" {
		return 0, core.Errf(core.CodeInvalidArgument, "conflicts revert: id (a supersession id) is required")
	}
	var auditID int64
	err := a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		var old, nw string
		var reverted sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT old_id, new_id, reverted_at FROM supersessions WHERE namespace = ? AND id = ?`, ns, id).
			Scan(&old, &nw, &reverted)
		if err == sql.ErrNoRows {
			return core.Errf(core.CodeNotFound, "supersession %s not found in namespace %s", id, ns)
		}
		if err != nil {
			return err
		}
		if reverted.Valid {
			return core.Errf(core.CodeConflict, "supersession %s was already reverted", id)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE supersessions SET reverted_at = ? WHERE id = ?`, core.FormatTime(a.e.Clock.Now()), id); err != nil {
			return err
		}
		auditID, err = a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: actor, Action: audit.ActConflictsRevert, Target: id,
			Details: map[string]any{"old_id": old, "new_id": nw}})
		return err
	})
	return auditID, err
}

// resetConflicts deletes the queue and its verdicts; applied and reverted
// supersessions stay (their pair_id is cleared). Audited.
func (a *App) resetConflicts(ctx context.Context, ns, actor string) (int64, int64, error) {
	var auditID, removed int64
	err := a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE supersessions SET pair_id = NULL WHERE namespace = ? AND pair_id IS NOT NULL`, ns); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM conflict_candidates WHERE namespace = ?`, ns)
		if err != nil {
			return err
		}
		removed, _ = res.RowsAffected()
		auditID, err = a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: actor, Action: audit.ActConflictsReset, Target: ns,
			Details: map[string]any{"pairs": removed}})
		return err
	})
	return auditID, removed, err
}

type scanEvent struct {
	id, content, zone string
	validFrom, rec    string
	role              string
	rowid             int64
}

// scanConflicts recomputes write-time detection over the live events of ns
// (fact assertions excluded), in write order: each event against the
// write.near_dup_candidates BM25 neighbours written before it, of its zone,
// with in-memory cosines from the configured embedder (each text embedded
// once). It counts the guard's refusals by reason and the candidate pairs
// by component; with write it queues the pairs (skipping those already
// queued or with a supersession row) and is audited. minCosine > 0
// overrides sleep.conflicts.min_cosine.
func (a *App) scanConflicts(ctx context.Context, ns string, minCosine float64, writeQueue, details bool, actor string) (*chanda.ConflictScan, int64, error) {
	p := a.conflictParams(minCosine)
	sc := &chanda.ConflictScan{MinCosine: p.MinCosine, RefusalsByReason: map[string]int{}, PairsByComponent: map[string]int{}}
	for _, r := range conflicts.RefusalReasons() {
		sc.RefusalsByReason[r] = 0
	}
	for _, c := range conflicts.Components() {
		sc.PairsByComponent[c] = 0
	}
	if details {
		sc.Candidates, sc.NearDupRefusals = []chanda.ScanPair{}, []chanda.ScanRefusal{}
	}
	db := a.e.Store.DB()
	rows, err := db.QueryContext(ctx, `SELECT rowid, id, content, trust_zone, COALESCE(valid_from,''), recorded_at, COALESCE(role,'') FROM events
		WHERE namespace = ? AND retired_at IS NULL AND kind <> 'fact_assertion' ORDER BY rowid`, ns)
	if err != nil {
		return nil, 0, err
	}
	var evs []scanEvent
	byID := map[string]int{}
	for rows.Next() {
		var e scanEvent
		if err := rows.Scan(&e.rowid, &e.id, &e.content, &e.zone, &e.validFrom, &e.rec, &e.role); err != nil {
			rows.Close()
			return nil, 0, err
		}
		byID[e.id] = len(evs)
		evs = append(evs, e)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	n := a.e.Cfg.Write.NearDupCandidates
	vecs := map[string][]float32{}
	var all []conflicts.Pair
	for _, e := range evs {
		sc.EventsScanned++
		expr := matchAny(e.content)
		if n <= 0 || expr == "" {
			continue
		}
		ids, err := read.TopEventMatches(ctx, db, ns, expr, n,
			`b.id <> ? AND b.retired_at IS NULL AND b.trust_zone = ? AND b.kind <> 'fact_assertion' AND b.rowid < ?`, e.id, e.zone, e.rowid)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			continue
		}
		var missing []string
		for _, id := range append([]string{e.id}, ids...) {
			if _, ok := vecs[id]; !ok && !contains(missing, id) {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			texts := make([]string, len(missing))
			for i, id := range missing {
				texts[i] = evs[byID[id]].content
			}
			vs, err := a.emb.Embed(ctx, texts)
			if err != nil {
				return nil, 0, core.Wrap(core.CodeInternal, err, "conflicts scan: embedding")
			}
			for i, id := range missing {
				vecs[id] = vs[i]
			}
		}
		nb := make([]conflicts.Neighbour, len(ids))
		for i, id := range ids {
			x := evs[byID[id]]
			nb[i] = conflicts.Neighbour{Record: conflicts.Record{ID: x.id, Content: x.content, ValidFrom: x.validFrom, RecordedAt: x.rec, Role: x.role},
				Cosine: dot(vecs[e.id], vecs[id])}
		}
		sc.NeighbourPairs += len(nb)
		pairs, refusals := conflicts.Detect(conflicts.Record{ID: e.id, Content: e.content, ValidFrom: e.validFrom, RecordedAt: e.rec, Role: e.role}, nb, p)
		for _, r := range refusals {
			sc.RefusalsByReason[r.Reason]++
			if details {
				sc.NearDupRefusals = append(sc.NearDupRefusals, chanda.ScanRefusal{OlderID: r.OlderID, NewerID: r.NewerID, Cosine: r.Cosine, Reason: r.Reason})
			}
		}
		for _, pr := range pairs {
			sc.CandidatePairs++
			sc.PairsByComponent[pr.Differs]++
			if details {
				sc.Candidates = append(sc.Candidates, chanda.ScanPair{OlderID: pr.OlderID, NewerID: pr.NewerID, Cosine: pr.Cosine,
					Differs: pr.Differs, Shared: pr.Shared})
			}
		}
		all = append(all, pairs...)
	}
	if !writeQueue {
		return sc, 0, nil
	}
	var auditID int64
	err = a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		var fresh []conflicts.Pair
		for _, pr := range all {
			var k int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM supersessions WHERE namespace = ? AND old_id IN (?, ?) AND new_id IN (?, ?)`,
				ns, pr.OlderID, pr.NewerID, pr.OlderID, pr.NewerID).Scan(&k); err != nil {
				return err
			}
			if k > 0 {
				sc.AlreadySuperseded++
				continue
			}
			fresh = append(fresh, pr)
		}
		q, err := conflicts.Enqueue(ctx, tx, a.e.IDs, ns, core.FormatTime(a.e.Clock.Now()), fresh)
		if err != nil {
			return err
		}
		sc.Queued, sc.AlreadyQueued = q, len(fresh)-q
		auditID, err = a.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: actor, Action: audit.ActConflictsScan, Target: ns,
			Details: map[string]any{"events": sc.EventsScanned, "candidates": sc.CandidatePairs, "queued": sc.Queued,
				"already_queued": sc.AlreadyQueued, "already_superseded": sc.AlreadySuperseded, "min_cosine": sc.MinCosine}})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return sc, auditID, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
