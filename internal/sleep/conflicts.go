package sleep

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/conflicts"
	"github.com/felipemarts/chanda/internal/core"
)

// Conflict verdicts (docs/decisions/0148-conflict-resolution-in-sleep.md).
// With sleep.conflicts.enabled, phase 2 first judges the pending pairs of
// the conflict queue, in priority order, until the token budget or
// sleep.conflicts.max_pairs runs out. The model sees the two records (date,
// author, zone and the neighbouring turns of their sessions) as quoted data
// and answers a fixed schema; conflicts.Decide then decides, without an
// LLM, whether a supersedes verdict takes effect. Applied verdicts are
// supersessions rows, staged and promoted through the gate like the other
// phase 2 writes. The sleep never reads retrieval query text: only which
// records each retrieval delivered, for the priority.

// ConflictCounts is what the conflict verdicts did (Counts.Conflicts; nil,
// and absent from the JSON, when sleep.conflicts is off outside
// maintenance mode).
type ConflictCounts struct {
	Mode           string         `json:"mode"`            // sleep mode of the run: uniform, guided or maintenance
	Enabled        bool           `json:"enabled"`         // sleep.conflicts.enabled
	Pending        int            `json:"pending"`         // pending pairs (both records live) when phase 2 started
	Judged         map[string]int `json:"judged"`          // pairs judged, by verdict after the rules
	ModelVerdicts  map[string]int `json:"model_verdicts"`  // pairs judged, by the model's verdict
	Applied        int            `json:"applied"`         // supersessions written
	Rejected       map[string]int `json:"rejected"`        // supersedes verdicts not applied, by rule reason
	InvalidAnswers int            `json:"invalid_answers"` // answers that did not match the schema: the pair stays pending
	TooLarge       int            `json:"too_large"`       // prompts larger than the whole token budget: the pair stays pending
	Dropped        int            `json:"dropped"`         // the pair or a record changed between the plan and the gate
	Left           int            `json:"left"`            // pending pairs not judged in this run
	LLMCalls       int            `json:"llm_calls"`
	InputTokens    int            `json:"input_tokens"`
	OutputTokens   int            `json:"output_tokens"`
	Supersessions  []string       `json:"supersessions"` // supersession ids written, sorted
}

func newConflictCounts(mode string, enabled bool) *ConflictCounts {
	return &ConflictCounts{Mode: mode, Enabled: enabled, Judged: map[string]int{}, ModelVerdicts: map[string]int{},
		Rejected: map[string]int{}, Supersessions: []string{}}
}

// conflictSide is one record of a pair as the prompt and the rules see it.
type conflictSide struct {
	id, content, zone, role, author, session string
	ordinal                                  int64
	validFrom, recordedAt                    string
	effective                                time.Time
	before, after                            []conflictTurn
}

type conflictTurn struct{ date, author, content string }

// conflictJudgement is one planned verdict.
type conflictJudgement struct {
	pairID string
	a, b   conflictSide // a: older, b: newer
	ans    conflicts.Answer
}

type pendingPair struct {
	id, older, newer string
	newerAt          time.Time
	deliveries       int
}

// collectConflicts plans the verdicts of the pending pairs (no write).
func (s *Sleeper) collectConflicts(ctx context.Context, r *run, p *plan) error {
	k := s.env.Cfg.Sleep.Conflicts
	if !k.On() {
		return nil
	}
	cc := r.res.Counts.Conflicts
	if cc == nil {
		cc = newConflictCounts(s.env.Cfg.Sleep.SleepMode(), true)
		r.res.Counts.Conflicts = cc
	}
	pairs, err := s.pendingPairs(ctx, r.ns)
	if err != nil {
		return err
	}
	cc.Pending = len(pairs)
	planned := 0
	defer func() { cc.Left = cc.Pending - planned }()
	tooLarge := false
	for _, pr := range pairs {
		if k.PairCap() > 0 && planned >= k.PairCap() {
			r.ceiling("max_pairs")
			break
		}
		if r.stopLLM {
			break
		}
		a, err := s.conflictSide(ctx, r.ns, pr.older, k.Turns())
		if err != nil {
			return err
		}
		b, err := s.conflictSide(ctx, r.ns, pr.newer, k.Turns())
		if err != nil {
			return err
		}
		prompt := conflictPrompt(a, b)
		if s.estimate(systemConflict, prompt) > r.budget.Tokens {
			cc.TooLarge++
			if !tooLarge {
				tooLarge = true
				r.res.notice("conflicts: a pair is larger than the whole token budget; it stays pending")
			}
			continue
		}
		invalid, cost := r.res.Counts.InvalidAnswers, r.res.Cost
		var ans conflicts.Answer
		ok, err := s.call(ctx, r, systemConflict, prompt, schemaConflict, &ans)
		cc.LLMCalls += r.res.Cost.LLMCalls - cost.LLMCalls
		cc.InputTokens += r.res.Cost.InputTokens - cost.InputTokens
		cc.OutputTokens += r.res.Cost.OutputTokens - cost.OutputTokens
		cc.InvalidAnswers += r.res.Counts.InvalidAnswers - invalid
		if err != nil {
			return err
		}
		if !ok {
			continue // invalid answer (no retry) or ceiling: the pair stays pending
		}
		p.conflicts = append(p.conflicts, conflictJudgement{pairID: pr.id, a: a, b: b, ans: ans})
		planned++
	}
	return nil
}

// pendingPairs returns the pending pairs of ns whose two records are live,
// in priority order: retrievals that delivered either record (most first),
// then the newer record's effective time (most recent first), then the id.
func (s *Sleeper) pendingPairs(ctx context.Context, ns string) ([]pendingPair, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT c.id, c.older_id, c.newer_id, COALESCE(n.valid_from,''), n.recorded_at
		FROM conflict_candidates c JOIN events o ON o.id = c.older_id JOIN events n ON n.id = c.newer_id
		WHERE c.namespace = ? AND c.status = ? AND o.retired_at IS NULL AND n.retired_at IS NULL ORDER BY c.id`,
		ns, conflicts.StatusPending)
	if err != nil {
		return nil, err
	}
	var out []pendingPair
	want := map[string]bool{}
	for rows.Next() {
		var p pendingPair
		var vf, ra string
		if err := rows.Scan(&p.id, &p.older, &p.newer, &vf, &ra); err != nil {
			rows.Close()
			return nil, err
		}
		p.newerAt, _ = conflicts.EffectiveTime(vf, ra)
		want[p.older], want[p.newer] = true, true
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	// Deliveries: retrievals.delivered only (never query_text or params).
	by := map[string]map[int]bool{}
	rrows, err := s.db().QueryContext(ctx, `SELECT delivered FROM retrievals WHERE namespace = ? ORDER BY id`, ns)
	if err != nil {
		return nil, err
	}
	i := 0
	for rrows.Next() {
		var d string
		if err := rrows.Scan(&d); err != nil {
			rrows.Close()
			return nil, err
		}
		var ids []string
		_ = json.Unmarshal([]byte(d), &ids)
		for _, id := range ids {
			if want[id] {
				if by[id] == nil {
					by[id] = map[int]bool{}
				}
				by[id][i] = true
			}
		}
		i++
	}
	rrows.Close()
	if err := rrows.Err(); err != nil {
		return nil, err
	}
	for k := range out {
		n := len(by[out[k].older])
		for ri := range by[out[k].newer] {
			if !by[out[k].older][ri] {
				n++
			}
		}
		out[k].deliveries = n
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.deliveries != b.deliveries {
			return a.deliveries > b.deliveries
		}
		if !a.newerAt.Equal(b.newerAt) {
			return a.newerAt.After(b.newerAt)
		}
		return a.id < b.id
	})
	return out, nil
}

// conflictSide reads one record of a pair and its n neighbouring turns of
// the same session on each side (by ordinal; live turns, no fact
// assertions).
func (s *Sleeper) conflictSide(ctx context.Context, ns, id string, n int) (conflictSide, error) {
	var x conflictSide
	var actor string
	err := s.db().QueryRowContext(ctx, `SELECT id, content, trust_zone, COALESCE(role,''), actor, COALESCE(session_id,''),
		COALESCE(ordinal,0), COALESCE(valid_from,''), recorded_at FROM events WHERE namespace = ? AND id = ?`, ns, id).
		Scan(&x.id, &x.content, &x.zone, &x.role, &actor, &x.session, &x.ordinal, &x.validFrom, &x.recordedAt)
	if err != nil {
		return x, err
	}
	x.author = x.role
	if x.author == "" {
		x.author = actor
	}
	x.effective, _ = conflicts.EffectiveTime(x.validFrom, x.recordedAt)
	if x.session == "" || n <= 0 {
		return x, nil
	}
	turns := func(q string) ([]conflictTurn, error) {
		rows, err := s.db().QueryContext(ctx, q, ns, x.session, x.ordinal, x.id, n)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []conflictTurn
		for rows.Next() {
			var t conflictTurn
			var role, act, vf, ra string
			if err := rows.Scan(&t.content, &role, &act, &vf, &ra); err != nil {
				return nil, err
			}
			t.date, t.author = eventDate(vf, ra), role
			if t.author == "" {
				t.author = act
			}
			out = append(out, t)
		}
		return out, rows.Err()
	}
	const cols = `SELECT content, COALESCE(role,''), actor, COALESCE(valid_from,''), recorded_at FROM events
		WHERE namespace = ? AND session_id = ? AND retired_at IS NULL AND kind <> 'fact_assertion' AND ordinal IS NOT NULL `
	if x.before, err = turns(cols + `AND ordinal < ? AND id <> ? ORDER BY ordinal DESC, id DESC LIMIT ?`); err != nil {
		return x, err
	}
	for i, j := 0, len(x.before)-1; i < j; i, j = i+1, j-1 {
		x.before[i], x.before[j] = x.before[j], x.before[i]
	}
	x.after, err = turns(cols + `AND ordinal > ? AND id <> ? ORDER BY ordinal, id LIMIT ?`)
	return x, err
}

func (x conflictSide) rulesSide() conflicts.Side {
	return conflicts.Side{ID: x.id, Effective: x.effective, Zone: core.Zone(x.zone), Role: x.role}
}

// applyConflict stores one verdict inside the gate transaction: the pair
// leaves the pending state with the model's answer, the verdict after the
// rules and the reason; an applied verdict writes a staged supersession.
// Each judged pair is audited.
func (s *Sleeper) applyConflict(ctx context.Context, tx *sql.Tx, r *run, j conflictJudgement) error {
	cc := r.res.Counts.Conflicts
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM conflict_candidates WHERE id = ? AND namespace = ?`, j.pairID, r.ns).Scan(&status)
	if err == sql.ErrNoRows || (err == nil && status != conflicts.StatusPending) {
		cc.Dropped++
		return nil
	}
	if err != nil {
		return err
	}
	st, _, err := eventStates(ctx, tx, r.ns, []string{j.a.id, j.b.id})
	if err != nil {
		return err
	}
	if st[j.a.id] != evLive || st[j.b.id] != evLive {
		cc.Dropped++
		return nil
	}
	d := conflicts.Decide(j.ans, j.a.rulesSide(), j.b.rulesSide(), s.env.Cfg.Sleep.Conflicts.Confidence())
	now := s.now()
	supID := ""
	if d.Apply {
		err := tx.QueryRowContext(ctx, `SELECT id FROM supersessions WHERE namespace = ? AND old_id = ? AND new_id = ? AND reverted_at IS NULL
			ORDER BY id LIMIT 1`, r.ns, d.OldID, d.NewID).Scan(&supID)
		if err == sql.ErrNoRows {
			endsAt := j.b.effective
			if d.NewID == j.a.id {
				endsAt = j.a.effective
			}
			supID = s.env.IDs.New(core.PrefixSupersession)
			if _, err := tx.ExecContext(ctx, `INSERT INTO supersessions(id, namespace, old_id, new_id, pair_id, sleep_run_id, ends_at, confidence, status, created_at)
				VALUES (?,?,?,?,?,?,?,?,?,?)`, supID, r.ns, d.OldID, d.NewID, j.pairID, r.res.RunID, core.FormatTime(endsAt),
				j.ans.Confidence, conflicts.SupStaged, now); err != nil {
				return err
			}
			cc.Applied++
			cc.Supersessions = append(cc.Supersessions, supID)
		} else if err != nil {
			return err
		}
	} else if j.ans.Verdict == conflicts.VerdictSupersedes {
		cc.Rejected[d.Reason]++
	}
	cc.Judged[d.Verdict]++
	cc.ModelVerdicts[j.ans.Verdict]++
	if _, err := tx.ExecContext(ctx, `UPDATE conflict_candidates SET status = ?, model_verdict = ?, model_current = ?, confidence = ?,
		verdict = ?, reason = ?, sleep_run_id = ?, judged_at = ? WHERE id = ?`, conflicts.StatusJudged, j.ans.Verdict, j.ans.Current,
		j.ans.Confidence, d.Verdict, d.Reason, r.res.RunID, now, j.pairID); err != nil {
		return err
	}
	details := map[string]any{"verdict": d.Verdict, "model_verdict": j.ans.Verdict, "current": j.ans.Current,
		"confidence": j.ans.Confidence, "reason": d.Reason, "run": r.res.RunID, "older": j.a.id, "newer": j.b.id}
	if supID != "" {
		details["supersession"] = supID
	}
	return s.audit(ctx, tx, r.ns, audit.ActConflictVerdict, j.pairID, details)
}

// promoteSupersessions turns this run's staged supersessions active inside
// the savepoint, so the gate measures them.
func (s *Sleeper) promoteSupersessions(ctx context.Context, tx *sql.Tx, r *run) error {
	if r.res.Counts.Conflicts == nil || len(r.res.Counts.Conflicts.Supersessions) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE supersessions SET status = ? WHERE sleep_run_id = ? AND status = ?`,
		conflicts.SupActive, r.res.RunID, conflicts.SupStaged)
	return err
}

// phase2Maintenance is sleep.mode maintenance: phase 2 judges only the
// conflict queue. No extraction, lessons or digest.
func (s *Sleeper) phase2Maintenance(ctx context.Context, r *run) error {
	k := s.env.Cfg.Sleep.Conflicts
	r.res.Counts.Conflicts = newConflictCounts(config.SleepModeMaintenance, k.On())
	switch {
	case !k.On():
		r.res.notice("phase 2: maintenance mode judges only the conflict queue and sleep.conflicts.enabled is off; nothing to do")
		return nil
	case s.d.LLM == nil:
		r.res.notice("phase 2 skipped: conflict verdicts need an LLM provider; phase 1 only")
		return nil
	case s.d.Evaluator == nil:
		r.res.notice("phase 2 skipped: no evaluator configured, so staged changes could never pass the gate")
		return nil
	}
	p := &plan{}
	if err := s.collectConflicts(ctx, r, p); err != nil {
		return err
	}
	if p.empty() {
		r.res.notice("phase 2: no conflict pair to judge")
		return nil
	}
	return s.applyGated(ctx, r, p)
}
