package rank

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/felipemarts/chanda/internal/clock"
	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

var bg = context.Background()

func newTest(t *testing.T, mut func(*config.RankingConfig)) (*Ranker, *env.Env, *clock.Fake) {
	t.Helper()
	cfg := config.Default()
	if mut != nil {
		mut(&cfg.Ranking)
	}
	e, fc := env.NewTestAt(t, t.TempDir()+"/chanda.db", cfg)
	return New(e), e, fc
}

// seedRetrieval writes a retrievals row the way the read path does.
func seedRetrieval(t *testing.T, e *env.Env, ns string, cands []Candidate, delivered []string) string {
	t.Helper()
	id := e.IDs.New(core.PrefixRetrieval)
	cj, _ := json.Marshal(cands)
	dj, _ := json.Marshal(delivered)
	_, err := e.Store.DB().ExecContext(bg,
		`INSERT INTO retrievals(id, namespace, mode, query_text, params, candidates, delivered, ts) VALUES (?,?,?,?,?,?,?,?)`,
		id, ns, "search", "q", "{}", string(cj), string(dj), core.FormatTime(e.Clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// cands builds minimal candidates for ids.
func cands(ids ...string) []Candidate {
	out := make([]Candidate, len(ids))
	for i, id := range ids {
		out[i] = Candidate{ID: id, Zone: core.ZoneInternal}
	}
	return out
}

func mustOutcome(t *testing.T, r *Ranker, ns, ret string, v float64, used ...string) OutcomeResult {
	t.Helper()
	res, err := r.Outcome(bg, OutcomeInput{Namespace: ns, RetrievalID: ret, Value: v, UsedIDs: used, Actor: "agent", ActorZone: core.ZoneInternal})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func utilRow(t *testing.T, e *env.Env, id string) *urow {
	t.Helper()
	r, err := e.Store.DB().QueryContext(bg, `SELECT `+utilityCols+` FROM utility WHERE record_id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	us, err := scanURows(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		return nil
	}
	return us[0]
}

func countRows(t *testing.T, e *env.Env, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.Store.DB().QueryRowContext(bg, q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
