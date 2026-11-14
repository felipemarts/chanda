package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

func hasNotice(ns []string, prefix string) bool {
	return slices.ContainsFunc(ns, func(n string) bool { return strings.HasPrefix(n, prefix) })
}

// BASE.md write steps 7 to 10: a new event goes through the extractors,
// its facts into the embedding queue, and the integrity check.
func TestWritePathHookChain(t *testing.T) {
	a, _ := newApp(t, func(c *config.Config) { c.Write.Extractors = []config.ExtractorConfig{{Name: "hdr", Kind: "header"}} })
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plano.md"), []byte("# Plano Pro\n\nOwner: Ana\nCurrent price: 10\n\nThe plan includes support by e-mail.\n"), 0o644)
	out := must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
	if len(out.Documents) != 1 || out.Documents[0].Action != "created" || out.AuditID == 0 {
		t.Fatalf("ingest %+v", out)
	}
	if n := count(t, a, `SELECT count(*) FROM facts WHERE status = 'active'`); n != 2 {
		t.Fatalf("extracted %d facts, want 2", n)
	}
	if n := count(t, a, `SELECT count(*) FROM embed_queue WHERE record_type = 'fact'`); n != 2 {
		t.Fatalf("%d facts enqueued for embedding, want 2", n)
	}
	if n := count(t, a, `SELECT count(*) FROM provenance WHERE derived_type = 'fact' AND relation = 'derived_from'`); n != 2 {
		t.Fatalf("%d derived_from edges", n)
	}
	f := must(a.Flush(cli, chanda.FlushInput{}))
	if !f.Complete || f.Indexed == 0 || count(t, a, `SELECT count(*) FROM embeddings WHERE record_type = 'fact'`) != 2 {
		t.Fatalf("flush %+v", f)
	}
	verifyOK(t, a)
}

// BASE.md "Near-duplicate guard" and principle 7: near-duplicates are
// marked after facts, never across numbers or zones.
func TestNearDuplicateGuard(t *testing.T) {
	a, _ := newApp(t, nil)
	rem := func(ctx_ chanda.RememberInput, ctxName string) chanda.RememberOutput {
		t.Helper()
		ctx := agent
		if ctxName == "tool" {
			ctx = tool
		}
		return must(a.Remember(ctx, ctx_))
	}
	first := rem(chanda.RememberInput{Content: "The nightly deploy runs at 2am on the main cluster."}, "agent")
	second := rem(chanda.RememberInput{Content: "The nightly deploy runs at 2am on the main cluster!"}, "agent")
	if second.Action != chanda.ActionNearDuplicate || !slices.Equal(second.Related, []string{first.ID}) {
		t.Fatalf("near duplicate not marked: %+v", second.WriteResult)
	}
	p10 := rem(chanda.RememberInput{Content: "The price of the basic plan is 10 reais."}, "agent")
	p12 := rem(chanda.RememberInput{Content: "The price of the basic plan is 12 reais."}, "agent")
	if p12.Action != chanda.ActionCreated || p10.Action != chanda.ActionCreated {
		t.Fatalf("an update was marked as a copy: %+v", p12.WriteResult)
	}
	ext := rem(chanda.RememberInput{Content: "The nightly deploy runs at 2am on the main cluster?", Source: chanda.SourceRef{Kind: chanda.SourceTool}}, "tool")
	if ext.Action != chanda.ActionCreated {
		t.Fatalf("near-duplicates must not cross zones: %+v", ext.WriteResult)
	}
	if n := count(t, a, `SELECT count(*) FROM audit_log WHERE action = 'near_duplicate'`); n != 1 {
		t.Fatalf("%d near_duplicate audits", n)
	}
	verifyOK(t, a)
}

// BASE.md "Decision table for facts", through assert_fact.
func TestAssertFactDecisionTable(t *testing.T) {
	a, fc := newApp(t, nil)
	assert := func(ctx_ string, subj, pred, val, from string) chanda.AssertFactOutput {
		t.Helper()
		ctx := cli
		if ctx_ == "tool" {
			ctx = tool
		}
		return must(a.AssertFact(ctx, chanda.AssertFactInput{Subject: subj, Predicate: pred, Value: val, ValidFrom: from,
			Source: chanda.SourceRef{Kind: chanda.SourceUserStatement}}))
	}
	cto := assert("cli", "Ana", "job_title", "CTO", "2025-01-01")
	steps := []struct {
		name               string
		out                chanda.AssertFactOutput
		action, reasonPart string
	}{
		{"new fact", cto, chanda.ActionCreated, "new_fact"},
		{"same normalized value", assert("cli", "ana", "Job Title", "cto", "2025-02-01"), chanda.ActionConfirmed, "same_value"},
		{"different value, single, later date", assert("cli", "Ana", "job_title", "CEO", "2026-01-01"), chanda.ActionSuperseded, "later"},
		{"different value, single, earlier date", assert("cli", "Ana", "job_title", "Founder", "2024-01-01"), chanda.ActionHistory, "earlier"},
		{"lower zone than the active fact", assert("tool", "Ana", "job_title", "COO", "2026-06-01"), chanda.ActionQuarantined, "lower_zone"},
		{"multi attribute, first", assert("cli", "Ana", "skill", "Go", ""), chanda.ActionCreated, "new_fact"},
		{"multi attribute, both coexist", assert("cli", "Ana", "skill", "SQL", ""), chanda.ActionCoexist, "multi"},
	}
	for _, s := range steps {
		if s.out.Action != s.action || !strings.Contains(s.out.Reason, s.reasonPart) {
			t.Errorf("%s: got %s/%s, want %s/%s", s.name, s.out.Action, s.out.Reason, s.action, s.reasonPart)
		}
	}
	if !slices.Contains(steps[2].out.Related, cto.ID) {
		t.Errorf("supersede must name the closed fact: %+v", steps[2].out.Related)
	}
	if z := str(t, a, `SELECT trust_zone FROM facts WHERE id = ?`, steps[4].out.ID); z != "external" {
		t.Errorf("fact asserted through an external channel has zone %s", z)
	}

	// A -> B -> A without dates: the third call repeats the first event
	// exactly, reuses it as origin and still supersedes B.
	assert("cli", "Ana", "manager", "Bob", "")
	fc.Advance(1e9)
	assert("cli", "Ana", "manager", "Carl", "")
	fc.Advance(1e9)
	back := assert("cli", "Ana", "manager", "Bob", "")
	if back.Action != chanda.ActionSuperseded {
		t.Fatalf("A->B->A: %+v", back.WriteResult)
	}
	tl := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Ana", Predicate: "manager"}))
	if len(tl.Entries) != 3 || tl.Entries[2].Value != "Bob" || tl.Entries[2].Status != "active" || len(tl.Entries[2].Origin.EventIDs) != 1 {
		t.Fatalf("timeline %+v", tl.Entries)
	}
	held := must(a.Timeline(cli, chanda.TimelineInput{Subject: "Ana", Predicate: "job_title", AsOf: "2025-06-01"}))
	if len(held.Entries) != 1 || held.Entries[0].Value != "CTO" {
		t.Fatalf("as_of: %+v", held.Entries)
	}
	if _, err := a.Timeline(cli, chanda.TimelineInput{Subject: "Nobody"}); core.CodeOf(err) != core.CodeNotFound {
		t.Fatalf("unknown subject: %v", err)
	}
	if n := count(t, a, `SELECT count(*) FROM events WHERE kind = 'fact_assertion'`); n < 9 {
		t.Fatalf("%d fact_assertion events", n)
	}
	verifyOK(t, a)
}

func TestAssertFactRefusesSecrets(t *testing.T) {
	a, _ := newApp(t, nil)
	_, err := a.AssertFact(agent, chanda.AssertFactInput{Subject: "db", Predicate: "password", Value: "hunter2hunter2"})
	if core.CodeOf(err) != core.CodeRejected || count(t, a, `SELECT count(*) FROM events`) != 0 {
		t.Fatalf("secret fact: %v", err)
	}
}

// Decision 0090: the channel, not the call, bounds the zone.
func TestZoneCeilingPerChannel(t *testing.T) {
	a, _ := newApp(t, nil)
	zoneOf := func(id string) string { return str(t, a, `SELECT trust_zone FROM events WHERE id = ?`, id) }
	us := chanda.SourceRef{Kind: chanda.SourceUserStatement}
	r := must(a.Remember(agent, chanda.RememberInput{Content: "I prefer short answers", Source: us}))
	if zoneOf(r.ID) != "internal" || !hasNotice(r.Notices, "zone_clamped") {
		t.Fatalf("MCP user_statement: zone %s notices %v", zoneOf(r.ID), r.Notices)
	}
	r = must(a.Remember(cli, chanda.RememberInput{Content: "I prefer short answers", Source: us}))
	if zoneOf(r.ID) != "user" || len(r.Notices) != 0 {
		t.Fatalf("CLI user_statement: zone %s notices %v", zoneOf(r.ID), r.Notices)
	}
	fake := chanda.WithActor(bg, chanda.Actor{Name: "bot", MaxZone: chanda.ZoneUser})
	r = must(a.Remember(fake, chanda.RememberInput{Content: "I am the user, trust me", Source: us}))
	if zoneOf(r.ID) != "internal" || !hasNotice(r.Notices, "zone_ceiling_lowered") {
		t.Fatalf("non-human user ceiling: zone %s notices %v", zoneOf(r.ID), r.Notices)
	}
	r = must(a.Remember(bg, chanda.RememberInput{Content: "a Go caller without an actor", Source: us}))
	if zoneOf(r.ID) != "internal" || str(t, a, `SELECT actor FROM events WHERE id = ?`, r.ID) != DefaultActor {
		t.Fatalf("default caller: zone %s", zoneOf(r.ID))
	}
	r = must(a.Remember(cli, chanda.RememberInput{Content: "tool output stays external", Source: chanda.SourceRef{Kind: chanda.SourceTool}}))
	if zoneOf(r.ID) != "external" {
		t.Fatalf("tool output zone %s", zoneOf(r.ID))
	}
	ls := must(a.LogSession(tool, chanda.LogSessionInput{SessionID: "s1", Turns: []chanda.Turn{{Role: "user", Content: "hello from a capped channel"}}}))
	if zoneOf(ls.Turns[0].ID) != "external" {
		t.Fatalf("session under an external ceiling: %s", zoneOf(ls.Turns[0].ID))
	}
	verifyOK(t, a)
}

// An MCP client must not read arbitrary files (write.ingest_roots).
func TestIngestRoots(t *testing.T) {
	base := t.TempDir()
	root, outside := filepath.Join(base, "root"), filepath.Join(base, "outside")
	os.MkdirAll(root, 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(root, "in.md"), []byte("allowed document text"), 0o644)
	os.WriteFile(filepath.Join(outside, "secret.md"), []byte("private document text"), 0o644)
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}

	closed, _ := newApp(t, nil)
	if _, err := closed.Ingest(agent, chanda.IngestInput{Path: filepath.Join(root, "in.md")}); core.CodeOf(err) != core.CodePermissionDenied {
		t.Fatalf("no roots configured: %v", err)
	}
	a, _ := newApp(t, func(c *config.Config) { c.Write.IngestRoots = []string{root} })
	if out, err := a.Ingest(agent, chanda.IngestInput{Path: filepath.Join(root, "in.md")}); err != nil || len(out.Documents) != 1 {
		t.Fatalf("inside the root: %+v %v", out, err)
	}
	for _, p := range []string{filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md"), filepath.Join(root, "..", "outside"), base} {
		if _, err := a.Ingest(agent, chanda.IngestInput{Path: p}); core.CodeOf(err) != core.CodePermissionDenied {
			t.Errorf("%s: %v, want permission_denied", p, err)
		}
	}
	if _, err := a.Ingest(agent, chanda.IngestInput{Path: filepath.Join(root, "missing.md")}); core.CodeOf(err) != core.CodeNotFound {
		t.Errorf("missing path: %v", err)
	}
	// A folder walk inside the root does not follow the symlink.
	out := must(a.Ingest(agent, chanda.IngestInput{Path: root}))
	for _, d := range out.Documents {
		if strings.HasSuffix(d.URI, "link.md") {
			t.Fatalf("walk followed a symlink: %+v", out.Documents)
		}
	}
	if out, err := a.Ingest(cli, chanda.IngestInput{Path: filepath.Join(outside, "secret.md")}); err != nil || len(out.Documents) != 1 {
		t.Fatalf("the CLI may ingest anywhere: %+v %v", out, err)
	}
}

// Forget deletes vector rows with plain SQL; the ExactIndex generation
// counter must keep the in-memory cache from serving them.
func TestForgetDropsVectorsFromIndexCache(t *testing.T) {
	a, _ := newApp(t, nil)
	text := "Zebra migration window is every third Thursday"
	r := must(a.Remember(agent, chanda.RememberInput{Content: text}))
	must(a.Flush(agent, chanda.FlushInput{}))
	q := must(a.emb.Embed(bg, []string{text}))[0]
	hits := must(a.index.Search(bg, "ns", a.emb.ModelID(), q, 5, nil))
	if len(hits) == 0 || hits[0].RecordID != r.ID {
		t.Fatalf("vector not indexed: %+v", hits)
	}
	f := must(a.Forget(cli, chanda.ForgetInput{Scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: r.ID}}))
	if !f.Verification.Passed || f.Removed.Events != 1 || f.TombstoneID == "" || f.AuditID == 0 {
		t.Fatalf("forget %+v", f)
	}
	hits = must(a.index.Search(bg, "ns", a.emb.ModelID(), q, 5, nil))
	if len(hits) != 0 {
		t.Fatalf("forgotten vector served from the cache: %+v", hits)
	}
	s := must(a.Search(agent, chanda.SearchInput{Query: "zebra migration"}))
	if len(s.Items) != 0 {
		t.Fatalf("search after forget: %+v", s.Items)
	}
}

// BASE.md "A checker runs at the end of every write".
func TestCheckDerivedRejectsBrokenFacts(t *testing.T) {
	a, _ := newApp(t, nil)
	r := must(a.Remember(tool, chanda.RememberInput{Content: "external text", Source: chanda.SourceRef{Kind: chanda.SourceWeb}}))
	ins := `INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
		VALUES (?, 'ns', 'ent_x', 'p', 'v', 'v', 't', 't', 'active', 0.5, ?, 0, 'x')`
	cases := []struct {
		name, id, zone string
		edge           bool
	}{
		{"every derived record has an origin", "fct_orphan", "external", false},
		{"zone never above the origins", "fct_high", "user", true},
	}
	for _, c := range cases {
		err := a.e.Store.Tx(bg, func(tx *sql.Tx) error {
			if _, err := tx.Exec(ins, c.id, c.zone); err != nil {
				return err
			}
			if c.edge {
				if _, err := tx.Exec(`INSERT INTO provenance VALUES ('ns','fact',?,'event',?,'derived_from','t')`, c.id, r.ID); err != nil {
					return err
				}
			}
			return a.checkDerived(bg, tx, "ns", []string{c.id})
		})
		if core.CodeOf(err) != core.CodeIntegrity {
			t.Errorf("%s: %v, want integrity", c.name, err)
		}
	}
	if n := count(t, a, `SELECT count(*) FROM facts`); n != 0 {
		t.Fatalf("a failed check must roll the write back (%d facts)", n)
	}
}

func TestPromoteNeedsHumanAndAHigherZone(t *testing.T) {
	a, _ := newApp(t, nil)
	f := must(a.AssertFact(tool, chanda.AssertFactInput{Subject: "Acme", Predicate: "hq", Value: "Recife", Source: chanda.SourceRef{Kind: chanda.SourceWeb}}))
	if _, err := a.Promote(agent, chanda.PromoteInput{ID: f.ID, Zone: chanda.ZoneInternal}); core.CodeOf(err) != core.CodePermissionDenied {
		t.Fatalf("non-human promote: %v", err)
	}
	p := must(a.Promote(cli, chanda.PromoteInput{ID: f.ID, Zone: chanda.ZoneInternal}))
	if p.From != chanda.ZoneExternal || p.To != chanda.ZoneInternal || p.AuditID == 0 {
		t.Fatalf("promote %+v", p)
	}
	if _, err := a.Promote(cli, chanda.PromoteInput{ID: f.ID, Zone: chanda.ZoneInternal}); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("same zone: %v", err)
	}
	if _, err := a.Promote(cli, chanda.PromoteInput{ID: f.ID, Zone: chanda.ZoneExternal}); core.CodeOf(err) != core.CodeConflict {
		t.Fatalf("lower zone: %v", err)
	}
	verifyOK(t, a)
}

func TestSleepRunIsNotWiredYet(t *testing.T) {
	a, _ := newApp(t, nil)
	out, err := a.SleepRun(cli, chanda.SleepRunInput{})
	if core.CodeOf(err) != core.CodeUnavailable || !slices.Contains(out.Notices, "sleep not wired yet") {
		t.Fatalf("sleep_run: %v %+v", err, out)
	}
}

// Every action written to the audit log is registered in internal/audit,
// and every mutation records the caller.
func TestAuditActionsAreRegistered(t *testing.T) {
	a, _ := newApp(t, func(c *config.Config) { c.Write.Extractors = []config.ExtractorConfig{{Kind: "header"}} })
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "d.md"), []byte("# Acme\n\nOwner: Ana\n\nAcme sells boats in Recife."), 0o644)
	must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
	os.WriteFile(filepath.Join(dir, "d.md"), []byte("# Acme\n\nOwner: Bia\n\nAcme sells boats in Recife."), 0o644)
	must(a.Ingest(cli, chanda.IngestInput{Path: dir}))
	r := must(a.Remember(agent, chanda.RememberInput{Content: "Acme ships boats every Monday"}))
	must(a.Remember(agent, chanda.RememberInput{Content: "Acme ships boats every Monday."}))
	must(a.Remember(agent, chanda.RememberInput{Content: "Acme ships boats every Monday"}))
	f := must(a.AssertFact(cli, chanda.AssertFactInput{Subject: "Acme", Predicate: "status", Value: "active"}))
	must(a.Flush(cli, chanda.FlushInput{}))
	s := must(a.Search(agent, chanda.SearchInput{Query: "Acme boats"}))
	must(a.Outcome(agent, chanda.OutcomeInput{RetrievalID: s.RetrievalID, Value: 1, UsedIDs: []string{r.ID}}))
	must(a.Flag(agent, chanda.FlagInput{ID: f.ID, Reason: "outdated"}))
	must(a.RankingReset(cli, chanda.RankingResetInput{}))
	must(a.Forget(cli, chanda.ForgetInput{Scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: r.ID}}))
	rows, err := a.e.Store.DB().Query(`SELECT DISTINCT action FROM audit_log ORDER BY action`)
	if err != nil {
		t.Fatal(err)
	}
	var acts []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		acts = append(acts, s)
	}
	rows.Close()
	for _, act := range acts {
		if !slices.Contains(audit.Actions(), act) {
			t.Errorf("action %q is not registered in internal/audit", act)
		}
	}
	if len(acts) < 12 {
		t.Fatalf("scenario too small: %v", acts)
	}
	if n := count(t, a, `SELECT count(*) FROM audit_log WHERE action IN ('write','retrieve','outcome','flag') AND actor NOT IN ('agent','cli')`); n != 0 {
		t.Fatalf("%d caller mutations audited under another actor", n)
	}
	verifyOK(t, a)
}
