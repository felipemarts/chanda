package keys_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/keys"
	"github.com/felipemarts/chanda-mcp/internal/prov"
	"github.com/felipemarts/chanda-mcp/internal/rank"
)

var bg = context.Background()

type fixture struct {
	t  *testing.T
	e  *env.Env
	r  *rank.Ranker
	ev []string
}

func newFixture(t *testing.T, on bool) *fixture {
	t.Helper()
	cfg := config.Default()
	cfg.Write.LearnedKeys = on
	cfg.Ranking.OutcomeRatePerMinute = 0
	e, _ := env.NewTestAt(t, t.TempDir()+"/chanda.db", cfg)
	f := &fixture{t: t, e: e, r: rank.New(e)}
	src := e.IDs.For(core.RecSource)
	now := core.FormatTime(e.Clock.Now())
	if _, err := e.Store.DB().Exec(`INSERT INTO sources(id, namespace, kind, uri, trust_zone, created_at) VALUES (?,?,?,?,?,?)`,
		src, "ns", "user_statement", "test:src", "user", now); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"O plano empresarial custa 120 reais", "A reunião é na sala azul", "O servidor roda em Frankfurt"} {
		id := e.IDs.For(core.RecEvent)
		h := sha256.Sum256([]byte(c))
		if _, err := e.Store.DB().Exec(`INSERT INTO events(id, namespace, source_id, kind, content, content_hash, trust_zone, actor, recorded_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, "ns", src, "statement", c, hex.EncodeToString(h[:]), "user", "test", now); err != nil {
			t.Fatal(err)
		}
		f.ev = append(f.ev, id)
	}
	return f
}

// retrieval logs a retrieval the way the read path does.
func (f *fixture) retrieval(query string, delivered ...string) string {
	f.t.Helper()
	id := f.e.IDs.New(core.PrefixRetrieval)
	var cands []map[string]any
	for _, d := range delivered {
		cands = append(cands, map[string]any{"id": d, "type": "event", "zone": "internal", "features": map[string]float64{"delivered": 1}})
	}
	cj, _ := json.Marshal(cands)
	dj, _ := json.Marshal(delivered)
	if _, err := f.e.Store.DB().Exec(`INSERT INTO retrievals(id, namespace, mode, query_text, params, candidates, delivered, ts) VALUES (?,?,?,?,?,?,?,?)`,
		id, "ns", "context", query, "{}", string(cj), string(dj), core.FormatTime(f.e.Clock.Now())); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) outcome(ret string, v float64, used ...string) rank.OutcomeResult {
	f.t.Helper()
	res, err := f.r.Outcome(bg, rank.OutcomeInput{Namespace: "ns", RetrievalID: ret, Value: v, UsedIDs: used, Actor: "agent", ActorZone: core.ZoneInternal})
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.e.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

type keyRow struct{ id, recordType, recordID, retrieval, text string }

func (f *fixture) keys() []keyRow {
	f.t.Helper()
	rows, err := f.e.Store.DB().Query(`SELECT id, record_type, record_id, retrieval_id, text FROM learned_keys ORDER BY id`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []keyRow
	for rows.Next() {
		var k keyRow
		if err := rows.Scan(&k.id, &k.recordType, &k.recordID, &k.retrieval, &k.text); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, k)
	}
	return out
}

// Decision 0145 "Creation": a positive outcome gives every used record the
// retrieval delivered one key, normalised, with its two edges, queued for
// embedding, audited with counts only.
func TestPositiveOutcomeCreatesKeys(t *testing.T) {
	f := newFixture(t, true)
	ret := f.retrieval("  quanto   custa o\tplano? ", f.ev[0], f.ev[1])
	res := f.outcome(ret, 1, f.ev[1], f.ev[0], f.ev[2]) // ev[2] was not delivered
	if res.KeysCreated != 2 {
		t.Fatalf("keys created %d", res.KeysCreated)
	}
	ks := f.keys()
	if len(ks) != 2 || ks[0].recordID != f.ev[0] || ks[1].recordID != f.ev[1] {
		t.Fatalf("keys %+v", ks)
	}
	for _, k := range ks {
		if !strings.HasPrefix(k.id, "key_") || k.recordType != "event" || k.retrieval != ret || k.text != "quanto custa o plano?" {
			t.Fatalf("key row %+v", k)
		}
		if n := f.count(`SELECT COUNT(*) FROM provenance WHERE derived_type = 'key' AND derived_id = ? AND
			((source_type = 'retrieval' AND source_id = ? AND relation = 'derived_from') OR
			 (source_type = 'event' AND source_id = ? AND relation = 'points_to'))`, k.id, ret, k.recordID); n != 2 {
			t.Fatalf("edges of %s: %d", k.id, n)
		}
		if n := f.count(`SELECT COUNT(*) FROM embed_queue WHERE record_type = 'key' AND record_id = ?`, k.id); n != 1 {
			t.Fatalf("key %s not queued", k.id)
		}
		if n := f.count(`SELECT COUNT(*) FROM learned_keys_fts WHERE learned_keys_fts MATCH 'custa' AND record_id = ?`, k.id); n != 1 {
			t.Fatalf("key %s not in the FTS index", k.id)
		}
	}
	var details string
	if err := f.e.Store.DB().QueryRow(`SELECT details FROM audit_log WHERE action = ? AND target = ?`, audit.ActKeysCreate, res.ID).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(details, "custa") || !strings.Contains(details, `"created":2`) {
		t.Fatalf("audit details must hold counts only: %s", details)
	}
	if rep, err := prov.Check(bg, f.e.Store.DB(), "ns"); err != nil || !rep.OK {
		t.Fatalf("provenance check: %+v %v", rep.Violations, err)
	}
}

func TestNoKeysWithoutAPositiveDeliveredUse(t *testing.T) {
	f := newFixture(t, true)
	f.outcome(f.retrieval("quanto custa o plano", f.ev[0]), -1, f.ev[0]) // negative
	f.outcome(f.retrieval("quanto custa o plano", f.ev[0]), 0, f.ev[0])  // neutral
	f.outcome(f.retrieval("onde fica o servidor", f.ev[0]), 1, f.ev[2])  // used but not delivered
	if ks := f.keys(); len(ks) != 0 {
		t.Fatalf("keys made: %+v", ks)
	}
	if n := f.count(`SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActKeysCreate); n != 0 {
		t.Fatalf("audit entries %d", n)
	}

	off := newFixture(t, false)
	if res := off.outcome(off.retrieval("quanto custa o plano", off.ev[0]), 1, off.ev[0]); res.KeysCreated != 0 {
		t.Fatal("write.learned_keys off must create nothing")
	}
	if ks := off.keys(); len(ks) != 0 {
		t.Fatalf("keys made with the option off: %+v", ks)
	}
}

func TestKeysAreDedupedByRecordAndText(t *testing.T) {
	f := newFixture(t, true)
	f.outcome(f.retrieval("Quanto custa o plano", f.ev[0]), 1, f.ev[0])
	res := f.outcome(f.retrieval("Quanto  custa o plano", f.ev[0], f.ev[1]), 0.5, f.ev[0], f.ev[1])
	if res.KeysCreated != 1 {
		t.Fatalf("second outcome: %d keys, want 1 (ev0 already has the text)", res.KeysCreated)
	}
	f.outcome(f.retrieval("quanto custa o plano", f.ev[0]), 1, f.ev[0]) // different case: another text
	if ks := f.keys(); len(ks) != 3 {
		t.Fatalf("keys %+v", ks)
	}
}

// The runtime switch, reset and rebuild are audited; rebuild replays the
// creation rule in outcome order and is idempotent.
func TestSwitchResetRebuild(t *testing.T) {
	f := newFixture(t, true)
	e := f.e
	if on, _ := keys.Enabled(bg, e.Store.DB(), "ns"); !on {
		t.Fatal("the switch starts on")
	}
	id, err := keys.SetEnabled(bg, e, "ns", false, "admin")
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	if on, _ := keys.Enabled(bg, e.Store.DB(), "ns"); on {
		t.Fatal("disable did not stick")
	}
	if on, _ := keys.Enabled(bg, e.Store.DB(), "other"); !on {
		t.Fatal("the switch is per namespace")
	}
	if _, err := keys.SetEnabled(bg, e, "ns", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM audit_log WHERE action = ? AND actor = 'admin'`, audit.ActKeysToggle); n != 2 {
		t.Fatalf("toggle audit entries %d", n)
	}

	f.outcome(f.retrieval("quanto custa o plano", f.ev[0], f.ev[1]), 1, f.ev[0], f.ev[1])
	f.outcome(f.retrieval("onde fica o servidor", f.ev[2]), 1, f.ev[2])
	f.outcome(f.retrieval("sala da reuniao", f.ev[1]), -1, f.ev[1])
	before := f.keys()
	if len(before) != 3 {
		t.Fatalf("keys %+v", before)
	}
	// A key's vector, as the indexer would write it.
	if _, err := e.Store.DB().Exec(`INSERT INTO embeddings(namespace, record_type, record_id, model_id, dim, vector) VALUES ('ns','key',?,'m',1,x'0000803f')`, before[0].id); err != nil {
		t.Fatal(err)
	}
	st, err := keys.StatsOf(bg, e.Store.DB(), "ns")
	if err != nil || st.Keys != 3 || st.Records != 3 || st.Embedded != 1 || st.Pending != 3 || !st.Enabled {
		t.Fatalf("stats %+v %v", st, err)
	}

	rr, err := keys.Reset(bg, e, "ns", "admin")
	if err != nil || rr.Keys != 3 || rr.Embeddings != 1 || rr.Provenance != 6 || rr.AuditID == 0 {
		t.Fatalf("reset %+v %v", rr, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM learned_keys_fts`) + f.count(`SELECT COUNT(*) FROM embed_queue WHERE record_type = 'key'`) +
		f.count(`SELECT COUNT(*) FROM provenance WHERE derived_type = 'key'`); n != 0 {
		t.Fatalf("reset left %d rows", n)
	}

	cfgOff := *e.Cfg
	cfgOff.Write.LearnedKeys = false // rebuild is the explicit admin path: it ignores the write option
	e.Cfg = &cfgOff
	b1, err := keys.Rebuild(bg, e, "ns", "admin")
	if err != nil || b1.Outcomes != 2 || b1.Created.Created != 3 || b1.Existing != 0 {
		t.Fatalf("rebuild %+v %v", b1, err)
	}
	after := f.keys()
	if len(after) != 3 {
		t.Fatalf("rebuilt keys %+v", after)
	}
	for i := range after {
		if after[i].recordID != before[i].recordID || after[i].text != before[i].text || after[i].retrieval != before[i].retrieval {
			t.Fatalf("rebuild differs at %d: %+v vs %+v", i, after[i], before[i])
		}
	}
	b2, err := keys.Rebuild(bg, e, "ns", "admin")
	if err != nil || b2.Created.Created != 0 || b2.Existing != 3 {
		t.Fatalf("second rebuild %+v %v", b2, err)
	}
	if len(f.keys()) != 3 {
		t.Fatal("rebuild is not idempotent")
	}
	for _, act := range []string{audit.ActKeysReset, audit.ActKeysRebuild} {
		var details string
		if err := e.Store.DB().QueryRow(`SELECT details FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, act).Scan(&details); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(details, "plano") {
			t.Fatalf("%s audit holds text: %s", act, details)
		}
	}
	if r, err := audit.Verify(bg, e.Store.DB()); err != nil || !r.OK {
		t.Fatalf("audit chain %+v %v", r, err)
	}
}
