package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// keysScenario writes three records, asks a question whose words appear
// in none of them (search delivers every record through the vector list),
// and reports that the retreat record answered it.
func keysScenario(t *testing.T, a *App) (retreat string) {
	t.Helper()
	for _, c := range []string{
		"The quarterly offsite will be held at Lakeview Lodge in October.",
		"Expense reports are due on the fifth business day of the month.",
		"The staging cluster is rebuilt every Sunday night.",
	} {
		w := must(a.Remember(agent, chanda.RememberInput{Content: c}))
		if strings.Contains(c, "Lakeview") {
			retreat = w.ID
		}
	}
	must(a.Flush(cli, chanda.FlushInput{}))
	s := must(a.Search(agent, chanda.SearchInput{Query: "where is our team retreat", Limit: 10}))
	if find(s.Items, retreat) == nil {
		t.Fatalf("search did not deliver the retreat record: %+v", s.Items)
	}
	o := must(a.Outcome(agent, chanda.OutcomeInput{RetrievalID: s.RetrievalID, Value: 1, UsedIDs: []string{retreat}}))
	if a.e.Cfg.Write.LearnedKeys && o.KeysCreated != 1 {
		t.Fatalf("outcome made %d keys", o.KeysCreated)
	}
	f := must(a.Flush(cli, chanda.FlushInput{}))
	if !f.Complete {
		t.Fatalf("flush: %+v", f)
	}
	return retreat
}

func find(items []chanda.Item, id string) *chanda.Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

const retreatQuery = "where is our team retreat"

// Decision 0145 end to end with the hash embedder: a positive outcome makes
// a key, flush embeds it, the context block then holds the record the key
// points to (and not without keys); the switch, reset and rebuild work over
// the API and are audited; the key never shows; verify passes; forgetting
// the record removes its key.
func TestLearnedKeysEndToEnd(t *testing.T) {
	a, _ := newApp(t, func(c *config.Config) { c.Write.LearnedKeys, c.Read.LearnedKeys = true, true })
	retreat := keysScenario(t, a)

	st := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysStats}))
	if st.Keys != 1 || st.Records != 1 || st.Embedded != 1 || st.Pending != 0 || !st.Enabled || st.AuditID != 0 {
		t.Fatalf("stats %+v", st)
	}
	on := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
	if find(on.Items, retreat) == nil {
		t.Fatalf("keys on: the retreat record is missing: %+v (%s)", on.Items, on.Reason)
	}
	for _, it := range on.Items {
		if strings.HasPrefix(it.ID, "key_") {
			t.Fatalf("a key was delivered: %+v", it)
		}
	}
	if strings.Contains(on.Text, retreatQuery) {
		t.Fatalf("the key text leaked into the block:\n%s", on.Text)
	}
	cands := str(t, a, `SELECT candidates FROM retrievals WHERE id = ?`, on.RetrievalID)
	if !strings.Contains(cands, `"key_rank":1`) {
		t.Fatalf("features.key_rank not logged: %s", cands)
	}

	off := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysDisable}))
	if off.Enabled || off.AuditID == 0 {
		t.Fatalf("disable %+v", off)
	}
	dis := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
	if find(dis.Items, retreat) != nil {
		t.Fatalf("switch off: the key still acts: %+v", dis.Items)
	}
	must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysEnable}))

	rs := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysReset}))
	if rs.Removed != 1 || rs.Keys != 0 || rs.AuditID == 0 {
		t.Fatalf("reset %+v", rs)
	}
	rb := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysRebuild}))
	if rb.Created != 1 || rb.Outcomes != 1 || rb.Keys != 1 || rb.Pending != 1 || rb.AuditID == 0 {
		t.Fatalf("rebuild %+v", rb)
	}
	rb2 := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysRebuild}))
	if rb2.Created != 0 || rb2.Existing != 1 || rb2.Keys != 1 {
		t.Fatalf("second rebuild %+v", rb2)
	}
	must(a.Flush(cli, chanda.FlushInput{}))
	again := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
	if find(again.Items, retreat) == nil {
		t.Fatalf("after rebuild: %+v", again.Items)
	}
	for _, act := range []string{audit.ActKeysCreate, audit.ActKeysToggle, audit.ActKeysReset, audit.ActKeysRebuild} {
		if count(t, a, `SELECT count(*) FROM audit_log WHERE action = ?`, act) == 0 {
			t.Fatalf("no %s audit entry", act)
		}
	}
	if _, err := a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: "frob"}); chanda.CodeOf(err) != chanda.CodeInvalidArgument {
		t.Fatalf("unknown action: %v", err)
	}
	verifyOK(t, a)

	fg := must(a.Forget(cli, chanda.ForgetInput{Scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: retreat}}))
	if !fg.Verification.Passed || fg.Removed.Keys != 1 {
		t.Fatalf("forget %+v", fg.Removed)
	}
	if n := count(t, a, `SELECT count(*) FROM learned_keys`) + count(t, a, `SELECT count(*) FROM embeddings WHERE record_type = 'key'`); n != 0 {
		t.Fatalf("forget left %d key rows", n)
	}
	verifyOK(t, a)
}

// With the options off, an outcome creates no key, and a read is the same
// whether the database holds keys or not.
func TestLearnedKeysOff(t *testing.T) {
	a, _ := newApp(t, nil)
	retreat := keysScenario(t, a)
	if n := count(t, a, `SELECT count(*) FROM learned_keys`); n != 0 {
		t.Fatalf("write.learned_keys off made %d keys", n)
	}
	before := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
	rb := must(a.LearnedKeys(cli, chanda.LearnedKeysInput{Action: chanda.KeysRebuild}))
	if rb.Created != 1 {
		t.Fatalf("rebuild %+v", rb)
	}
	if len(rb.Notices) == 0 || !strings.Contains(rb.Notices[0], "learned_keys_read_off") {
		t.Fatalf("notices %v", rb.Notices)
	}
	must(a.Flush(cli, chanda.FlushInput{}))
	after := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
	strip := func(o chanda.ContextOutput) string { return strings.ReplaceAll(o.Text, o.RetrievalID, "ret_X") }
	if !reflect.DeepEqual(before.Items, after.Items) || strip(before) != strip(after) {
		t.Fatalf("read.learned_keys off: keys changed the read\n%+v\n%+v", before.Items, after.Items)
	}
	_ = retreat
}

// Determinism: the same database and query give the same result twice,
// and two databases built the same way give the same result.
func TestLearnedKeysDeterministic(t *testing.T) {
	results := func() []chanda.ContextOutput {
		a, _ := newApp(t, func(c *config.Config) { c.Write.LearnedKeys, c.Read.LearnedKeys = true, true })
		keysScenario(t, a)
		x := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
		y := must(a.Context(agent, chanda.ContextInput{Query: retreatQuery}))
		return []chanda.ContextOutput{x, y}
	}
	r1, r2 := results(), results()
	strip := func(o chanda.ContextOutput) string { return strings.ReplaceAll(o.Text, o.RetrievalID, "ret_X") }
	if !reflect.DeepEqual(r1[0].Items, r1[1].Items) || strip(r1[0]) != strip(r1[1]) {
		t.Fatal("same database, same query: results differ")
	}
	for i := range r1 {
		if !reflect.DeepEqual(r1[i], r2[i]) {
			t.Fatalf("two databases built the same way differ at read %d:\n%+v\n%+v", i, r1[i], r2[i])
		}
	}
}
