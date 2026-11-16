package rank

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/store"
)

// Kinds of replayed signal, in tie order at equal timestamps.
const (
	evRetrieval = iota // a retrieval that received an outcome (snapshot point for training)
	evSignal           // an outcome or a flag
)

// signalEvent is one entry of the chronological signal log of a namespace.
type signalEvent struct {
	TS   string
	Kind int
	ID   string // outcome, flag or retrieval id

	// outcome and retrieval
	RetrievalID string
	Cands       []Candidate
	Delivered   []string
	Gate        bool // retrieval logged by the gate set (params.gate = true)
	Value       float64
	Used        []string

	// flag
	IsFlag   bool
	FlagType core.RecordType
	FlagID   string
}

// loadSignals reads, after the reset watermark, every outcome (with its
// retrieval) and every flag of ns, in replay order: (ts, kind, id). With
// snapshots, each outcome's retrieval also appears at its own ts.
func loadSignals(ctx context.Context, q store.Queryer, ns, resetAt string, snapshots bool) ([]signalEvent, error) {
	var evs []signalEvent
	rows, err := q.QueryContext(ctx, `
SELECT o.id, o.ts, o.value, o.used_ids, r.id, r.ts, r.candidates, r.delivered, r.params
FROM outcomes o JOIN retrievals r ON r.id = o.retrieval_id
WHERE o.namespace = ? AND o.ts > ? ORDER BY o.ts, o.id`, ns, resetAt)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ev signalEvent
		var used, cj, dj, params, rts string
		if err := rows.Scan(&ev.ID, &ev.TS, &ev.Value, &used, &ev.RetrievalID, &rts, &cj, &dj, &params); err != nil {
			rows.Close()
			return nil, err
		}
		ev.Kind, ev.Used, ev.Cands, ev.Delivered = evSignal, uniqueSorted(ParseIDs(used)), ParseCandidates(cj), ParseIDs(dj)
		var p struct {
			Gate bool `json:"gate"`
		}
		_ = json.Unmarshal([]byte(params), &p)
		ev.Gate = p.Gate
		evs = append(evs, ev)
		if snapshots {
			snap := ev
			snap.Kind, snap.TS, snap.ID = evRetrieval, rts, ev.RetrievalID
			evs = append(evs, snap)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT id, ts, record_type, record_id FROM flags WHERE namespace = ? AND ts > ? ORDER BY ts, id`, ns, resetAt)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		ev := signalEvent{Kind: evSignal, IsFlag: true}
		if err := rows.Scan(&ev.ID, &ev.TS, &ev.FlagType, &ev.FlagID); err != nil {
			rows.Close()
			return nil, err
		}
		evs = append(evs, ev)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(evs, func(i, j int) bool {
		a, b := evs[i], evs[j]
		if a.TS != b.TS {
			return a.TS < b.TS
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	return evs, nil
}

// replay applies signal events to an empty utility table and a fresh moving
// average, calling onSnapshot before each retrieval's outcome is applied and
// onOutcome after it.
func replay(evs []signalEvent, cfg config.RankingConfig, st *rankState, rows rowSet,
	onSnapshot func(ev signalEvent), onOutcome func(ev signalEvent, eff outcomeEffect)) {
	for _, ev := range evs {
		switch {
		case ev.Kind == evRetrieval:
			if onSnapshot != nil {
				onSnapshot(ev)
			}
		case ev.IsFlag:
			applyFlag(rows, cfg, ev.FlagType, ev.FlagID, ev.TS)
		default:
			eff := applyOutcome(st, rows, cfg, ev.Cands, ev.Delivered, ev.Used, ev.Value, ev.TS)
			if onOutcome != nil {
				onOutcome(ev, eff)
			}
		}
	}
}

// RecomputeResult reports a utility rebuild.
type RecomputeResult struct {
	AuditID  int64 `json:"audit_id"`
	Rows     int   `json:"rows"`
	Outcomes int   `json:"outcomes"`
	Flags    int   `json:"flags"`
}

// RecomputeActor is the audit actor of RecomputeUtility (sleep phase 1).
const RecomputeActor = "sleep"

// RecomputeUtility rebuilds the namespace's utility table and moving average
// from retrievals, outcomes and flags after the reset watermark (sleep phase
// 1). It replays the signals in (ts, id) order through the same rules as the
// incremental Outcome and Flag, so the rebuilt table equals the incremental
// one exactly, given the same configuration. λ, the toggle and the active
// model are kept. Audited as ActRankAdjust.
func (r *Ranker) RecomputeUtility(ctx context.Context, ns string) (RecomputeResult, error) {
	var res RecomputeResult
	if err := requireNS(ns); err != nil {
		return res, err
	}
	now := r.now()
	err := r.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		st, err := loadState(ctx, tx, ns)
		if err != nil {
			return err
		}
		evs, err := loadSignals(ctx, tx, ns, st.ResetAt, false)
		if err != nil {
			return err
		}
		st.Mean, st.N = 0, 0
		rows := rowSet{}
		replay(evs, r.env.Cfg.Ranking, &st, rows, nil, nil)
		for _, ev := range evs {
			if ev.IsFlag {
				res.Flags++
			} else {
				res.Outcomes++
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM utility WHERE namespace = ?`, ns); err != nil {
			return err
		}
		if err := writeURows(ctx, tx, ns, rows); err != nil {
			return err
		}
		if err := saveState(ctx, tx, ns, st, now); err != nil {
			return err
		}
		res.Rows = len(rows)
		res.AuditID, err = r.env.Audit.Append(ctx, tx, audit.Entry{
			Namespace: ns, Actor: RecomputeActor, Action: audit.ActRankAdjust, Target: ns,
			Details: map[string]any{"op": "recompute_utility", "rows": res.Rows, "outcomes": res.Outcomes, "flags": res.Flags},
		})
		return err
	})
	if err != nil {
		return RecomputeResult{}, err
	}
	return res, nil
}
