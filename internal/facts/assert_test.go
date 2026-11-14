package facts

import (
	"database/sql"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

type step struct {
	zone      core.Zone
	subject   string
	predicate string
	value     string
	validFrom string
	docDate   string
}

func (f *fixture) assert(st step) AssertResult {
	f.t.Helper()
	if st.zone == "" {
		st.zone = core.ZoneUser
	}
	if st.subject == "" {
		st.subject = "Alice"
	}
	if st.predicate == "" {
		st.predicate = "job_title"
	}
	ev := f.event(st.zone)
	res, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: st.subject, Predicate: st.predicate,
		Value: st.value, ValidFrom: st.validFrom, DocDate: st.docDate, OriginEventIDs: []string{ev}, Actor: "test"})
	if err != nil {
		f.t.Fatalf("assert %+v: %v", st, err)
	}
	return res
}

func (f *fixture) fact(id string) Fact {
	f.t.Helper()
	rows, err := f.e.Store.DB().Query(`SELECT `+factCols+` FROM facts WHERE id = ?`, id)
	if err != nil {
		f.t.Fatal(err)
	}
	fs, err := scanFacts(rows)
	if err != nil || len(fs) != 1 {
		f.t.Fatalf("fact %s: %v %d", id, err, len(fs))
	}
	return fs[0]
}

func (f *fixture) edges(derivedID, rel string) int {
	return f.count(`SELECT count(*) FROM provenance WHERE derived_id = ? AND relation = ?`, derivedID, rel)
}

func day(d string) string {
	t, err := core.ParseTime(d)
	if err != nil {
		panic(err)
	}
	return core.FormatTime(t)
}

func TestDecisionTable(t *testing.T) {
	cases := []struct {
		name  string
		prior []step
		next  step
		want  Action
		check func(f *fixture, prior []AssertResult, got AssertResult)
	}{
		{
			name:  "no active fact: inserted as active",
			next:  step{value: "Engineer", validFrom: "2026-01-01"},
			want:  ActionInserted,
			check: func(f *fixture, _ []AssertResult, got AssertResult) { f.wantStatus(got.FactID, core.FactActive, "") },
		},
		{
			name:  "same normalized value: confirms, adds edge, raises confidence",
			prior: []step{{value: "Senior  Engineer", validFrom: "2026-01-01"}},
			next:  step{value: "senior engineer", validFrom: "2026-02-01"},
			want:  ActionConfirmed,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				if got.FactID != prior[0].FactID {
					f.t.Fatalf("confirm must not create a fact")
				}
				fc := f.fact(got.FactID)
				if fc.Confidence != 0.6 {
					f.t.Errorf("confidence=%v want 0.6", fc.Confidence)
				}
				if f.edges(got.FactID, core.RelConfirmedBy) != 1 || f.edges(got.FactID, core.RelDerivedFrom) != 1 {
					f.t.Errorf("edges: confirmed=%d derived=%d", f.edges(got.FactID, core.RelConfirmedBy), f.edges(got.FactID, core.RelDerivedFrom))
				}
				if n := f.count(`SELECT count(*) FROM facts`); n != 1 {
					f.t.Errorf("facts=%d", n)
				}
				if n := f.count(`SELECT count(*) FROM audit_log WHERE action='fact_confirm' AND target=?`, got.FactID); n != 1 {
					f.t.Errorf("fact_confirm audits=%d", n)
				}
			},
		},
		{
			name:  "same value with numbers in another notation confirms",
			prior: []step{{predicate: "current_price", value: "R$ 1.000,50"}},
			next:  step{predicate: "current_price", value: "r$ 1000.50"},
			want:  ActionConfirmed,
		},
		{
			name:  "different value, single, later date: closes the old one and inserts the new one",
			prior: []step{{value: "Engineer", validFrom: "2026-01-01"}},
			next:  step{value: "Manager", validFrom: "2026-03-01"},
			want:  ActionSuperseded,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				old := f.fact(prior[0].FactID)
				if old.Status != core.FactSuperseded || old.ValidTo != day("2026-03-01") || old.SupersededBy != got.FactID {
					f.t.Errorf("old fact not closed: %+v", old)
				}
				f.wantStatus(got.FactID, core.FactActive, "")
				if n := f.count(`SELECT count(*) FROM provenance WHERE derived_id=? AND source_id=? AND relation='supersedes'`, got.FactID, old.ID); n != 1 {
					f.t.Errorf("supersedes edge missing")
				}
				if n := f.count(`SELECT count(*) FROM audit_log WHERE action='supersede' AND target=?`, got.FactID); n != 1 {
					f.t.Errorf("supersede audit missing")
				}
			},
		},
		{
			name:  "different value, single, same date: a correction supersedes",
			prior: []step{{value: "Engineer", validFrom: "2026-01-01"}},
			next:  step{value: "Manager", validFrom: "2026-01-01"},
			want:  ActionSuperseded,
		},
		{
			name:  "different value, single, earlier date: inserted closed as history, active unchanged",
			prior: []step{{value: "Manager", validFrom: "2026-03-01"}},
			next:  step{value: "Engineer", validFrom: "2026-01-01"},
			want:  ActionHistory,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				h := f.fact(got.FactID)
				if h.Status != core.FactSuperseded || h.ValidTo != day("2026-03-01") || h.SupersededBy != prior[0].FactID {
					f.t.Errorf("history fact: %+v", h)
				}
				f.wantStatus(prior[0].FactID, core.FactActive, "")
			},
		},
		{
			name: "history valid_to is the start of the next fact in the chain, not the active one",
			prior: []step{
				{value: "Intern", validFrom: "2026-01-01"},
				{value: "Engineer", validFrom: "2026-03-01"},
				{value: "Manager", validFrom: "2026-06-01"},
			},
			next: step{value: "Junior", validFrom: "2026-02-01"},
			want: ActionHistory,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				h := f.fact(got.FactID)
				if h.ValidTo != day("2026-03-01") || h.SupersededBy != prior[1].FactID {
					f.t.Errorf("history fact: %+v", h)
				}
				intern := f.fact(prior[0].FactID)
				if intern.ValidTo != day("2026-02-01") || intern.SupersededBy != got.FactID {
					f.t.Errorf("overlapping fact not trimmed: %+v", intern)
				}
				f.wantStatus(prior[2].FactID, core.FactActive, "")
			},
		},
		{
			name:  "different value, multi: both coexist",
			prior: []step{{predicate: "skill", value: "Go"}},
			next:  step{predicate: "skill", value: "SQL"},
			want:  ActionCoexist,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				f.wantStatus(prior[0].FactID, core.FactActive, "")
				f.wantStatus(got.FactID, core.FactActive, "")
			},
		},
		{
			name:  "lower zone than the active fact: quarantined, supersedes nothing",
			prior: []step{{zone: core.ZoneInternal, value: "Engineer", validFrom: "2026-01-01"}},
			next:  step{zone: core.ZoneExternal, value: "CEO", validFrom: "2026-05-01"},
			want:  ActionQuarantined,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				f.wantStatus(prior[0].FactID, core.FactActive, "")
				q := f.fact(got.FactID)
				if q.Status != core.FactQuarantined || q.TrustZone != core.ZoneExternal || q.SupersededBy != "" {
					f.t.Errorf("quarantined fact: %+v", q)
				}
				if f.edges(got.FactID, core.RelSupersedes) != 0 {
					f.t.Errorf("quarantined fact must supersede nothing")
				}
				if n := f.count(`SELECT count(*) FROM audit_log WHERE action='quarantine' AND target=?`, got.FactID); n != 1 {
					f.t.Errorf("quarantine audit missing")
				}
			},
		},
		{
			name:  "lower zone with an earlier date is also quarantined",
			prior: []step{{zone: core.ZoneUser, value: "Engineer", validFrom: "2026-05-01"}},
			next:  step{zone: core.ZoneInternal, value: "Intern", validFrom: "2026-01-01"},
			want:  ActionQuarantined,
		},
		{
			name:  "higher zone supersedes a lower-zone active fact",
			prior: []step{{zone: core.ZoneExternal, value: "Engineer", validFrom: "2026-01-01"}},
			next:  step{zone: core.ZoneUser, value: "Manager", validFrom: "2026-02-01"},
			want:  ActionSuperseded,
		},
		{
			name:  "lower zone, same value: confirms without raising confidence",
			prior: []step{{zone: core.ZoneUser, value: "Engineer"}},
			next:  step{zone: core.ZoneExternal, value: "engineer"},
			want:  ActionConfirmed,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				if c := f.fact(got.FactID).Confidence; c != 0.5 {
					f.t.Errorf("confidence=%v; a lower zone must not raise it", c)
				}
				if z := f.fact(got.FactID).TrustZone; z != core.ZoneUser {
					f.t.Errorf("zone=%s", z)
				}
			},
		},
		{
			name: "the same quarantined value again confirms the quarantined fact",
			prior: []step{
				{zone: core.ZoneUser, value: "Engineer"},
				{zone: core.ZoneExternal, value: "CEO"},
			},
			next: step{zone: core.ZoneExternal, value: "ceo"},
			want: ActionConfirmed,
			check: func(f *fixture, prior []AssertResult, got AssertResult) {
				if got.FactID != prior[1].FactID {
					f.t.Errorf("confirmed %s want quarantined %s", got.FactID, prior[1].FactID)
				}
			},
		},
		{
			name:  "unknown predicate is multi: different values coexist",
			prior: []step{{predicate: "Favourite Color", value: "blue"}},
			next:  step{predicate: "favourite_color", value: "green"},
			want:  ActionCoexist,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			var prior []AssertResult
			for _, p := range c.prior {
				prior = append(prior, f.assert(p))
			}
			got := f.assert(c.next)
			if got.Action != c.want {
				t.Fatalf("action=%s (%s) want %s", got.Action, got.Reason, c.want)
			}
			if c.check != nil {
				c.check(f, prior, got)
			}
			f.checkInvariants()
		})
	}
}

func (f *fixture) wantStatus(id string, st core.FactStatus, validTo string) {
	f.t.Helper()
	got := f.fact(id)
	if got.Status != st || got.ValidTo != validTo {
		f.t.Errorf("fact %s: status=%s valid_to=%q want %s %q", id, got.Status, got.ValidTo, st, validTo)
	}
}

// checkInvariants asserts the rules every state must satisfy: at most one
// active single fact per subject and predicate, every fact reaches an event,
// and the audit chain verifies.
func (f *fixture) checkInvariants() {
	f.t.Helper()
	if n := f.count(`SELECT count(*) FROM (SELECT 1 FROM facts WHERE status='active' AND is_single=1
		GROUP BY namespace, subject, predicate HAVING count(*) > 1)`); n != 0 {
		f.t.Errorf("%d subject/predicate pairs with two active single facts", n)
	}
	if n := f.count(`SELECT count(*) FROM facts WHERE id NOT IN (SELECT derived_id FROM provenance
		WHERE derived_type='fact' AND source_type='event' AND relation='derived_from')`); n != 0 {
		f.t.Errorf("%d facts without a derived_from edge", n)
	}
	f.verifyAudit()
}

func TestValidFromPrecedence(t *testing.T) {
	cases := []struct {
		name      string
		given     string
		docDate   string
		dbDocDate string
		evDate    string // events.valid_from of the origin: the date given when it was written
		want      string
	}{
		{"date given in the call wins", "2026-02-02", "2025-05-05", "2024-04-04", "2023-03-03", "2026-02-02"},
		{"then the document date passed in", "", "2025-05-05", "2024-04-04", "2023-03-03", "2025-05-05"},
		{"then the date given when the origin was written", "", "", "2024-04-04", "2023-03-03", "2023-03-03"},
		{"then the document date of the origin", "", "", "2024-04-04", "", "2024-04-04"},
		{"then recorded_at of the origin", "", "", "", "", "rec"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.exec(`INSERT INTO documents(id,namespace,source_id,uri,current_version,doc_date,content_hash,updated_at)
				VALUES('doc_1',?, 'src_internal','file:a',1,NULLIF(?,''),'h','t')`, testNS, c.dbDocDate)
			rec := core.FormatTime(f.e.Clock.Now())
			evDate := c.evDate
			if evDate != "" {
				evDate = day(evDate)
			}
			f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,valid_from,doc_id,doc_version,ordinal)
				VALUES('evt_doc',?,'src_internal','chunk','x','h','internal','t',?,NULLIF(?,''),'doc_1',1,0)`, testNS, rec, evDate)
			res, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: "Alice", Predicate: "job_title", Value: "x",
				ValidFrom: c.given, DocDate: c.docDate, OriginEventIDs: []string{"evt_doc"}})
			if err != nil {
				t.Fatal(err)
			}
			want := rec
			if c.want != "rec" {
				want = day(c.want)
			}
			if got := f.fact(res.FactID).ValidFrom; got != want {
				t.Fatalf("valid_from=%s want %s", got, want)
			}
		})
	}
}

func TestAssertFactShape(t *testing.T) {
	f := newFixture(t)
	evU, evE := f.event(core.ZoneUser), f.event(core.ZoneExternal)
	res, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: "  Alice   Smith ", Predicate: "Job Title",
		Value: " Senior   Engineer ", OriginEventIDs: []string{evU, evE, evU}, Actor: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	fc := f.fact(res.FactID)
	if fc.Text != "Alice Smith job_title: Senior Engineer" {
		t.Errorf("text=%q", fc.Text)
	}
	if fc.TrustZone != core.ZoneExternal {
		t.Errorf("zone=%s; must be the lowest of the origins", fc.TrustZone)
	}
	if fc.Predicate != "job_title" || !fc.Single || fc.ValueNorm != "senior engineer" || fc.Confidence != 0.5 {
		t.Errorf("fact=%+v", fc)
	}
	if n := f.edges(res.FactID, core.RelDerivedFrom); n != 2 {
		t.Errorf("derived_from edges=%d want 2", n)
	}
	var details string
	f.e.Store.DB().QueryRow(`SELECT details FROM audit_log WHERE id=?`, res.AuditID).Scan(&details)
	if details != `{"action":"inserted","origins":2}` {
		t.Errorf("audit details=%s", details)
	}
	// Subject by entity id.
	res2, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: res.EntityID, Predicate: "job_title",
		Value: "senior engineer", OriginEventIDs: []string{f.event(core.ZoneUser)}})
	if err != nil || res2.FactID != res.FactID || res2.Action != ActionConfirmed {
		t.Fatalf("by id: %+v %v", res2, err)
	}
	// Same origin again: nothing changes, nothing audited.
	res3, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: "alice smith", Predicate: "job_title",
		Value: "Senior Engineer", OriginEventIDs: []string{evU}})
	if err != nil || res3.Reason != ReasonAlreadyRecorded || res3.AuditID != 0 {
		t.Fatalf("idempotent re-assert: %+v %v", res3, err)
	}
}

func TestAssertRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	ev := f.event(core.ZoneUser)
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,retired_at)
		VALUES('evt_ret',?,'src_user','statement','x','h','user','t','t','t')`, testNS)
	cases := []struct {
		name string
		in   AssertInput
		code core.Code
	}{
		{"no origin", AssertInput{Namespace: testNS, Subject: "a", Predicate: "p", Value: "v"}, core.CodeInvalidArgument},
		{"empty value", AssertInput{Namespace: testNS, Subject: "a", Predicate: "p", Value: "  ", OriginEventIDs: []string{ev}}, core.CodeInvalidArgument},
		{"empty predicate", AssertInput{Namespace: testNS, Subject: "a", Predicate: "!", Value: "v", OriginEventIDs: []string{ev}}, core.CodeInvalidArgument},
		{"empty subject", AssertInput{Namespace: testNS, Subject: " ", Predicate: "p", Value: "v", OriginEventIDs: []string{ev}}, core.CodeInvalidArgument},
		{"unknown origin", AssertInput{Namespace: testNS, Subject: "a", Predicate: "p", Value: "v", OriginEventIDs: []string{"evt_nope"}}, core.CodeNotFound},
		{"origin from another namespace", AssertInput{Namespace: "other", Subject: "a", Predicate: "p", Value: "v", OriginEventIDs: []string{ev}}, core.CodeNotFound},
		{"retired origin", AssertInput{Namespace: testNS, Subject: "a", Predicate: "p", Value: "v", OriginEventIDs: []string{"evt_ret"}}, core.CodeInvalidArgument},
		{"bad date", AssertInput{Namespace: testNS, Subject: "a", Predicate: "p", Value: "v", ValidFrom: "yesterday", OriginEventIDs: []string{ev}}, core.CodeInvalidArgument},
	}
	for _, c := range cases {
		_, err := f.s.AssertCommit(f.ctx, c.in)
		if core.CodeOf(err) != c.code {
			t.Errorf("%s: err=%v want %s", c.name, err, c.code)
		}
	}
	if n := f.count(`SELECT count(*) FROM facts`); n != 0 {
		t.Fatalf("facts written on error: %d", n)
	}
}

// TestPartialUniqueIndexSafety drives many supersessions, histories and
// quarantines through one subject and checks that there is never more than
// one active single fact, even when the index would reject a bad order.
func TestPartialUniqueIndexSafety(t *testing.T) {
	f := newFixture(t)
	dates := []string{"2026-05-01", "2026-01-01", "2026-07-01", "2026-03-01", "2026-07-01", "2026-02-01", "2026-12-01"}
	zones := []core.Zone{core.ZoneInternal, core.ZoneUser, core.ZoneExternal, core.ZoneUser, core.ZoneUser, core.ZoneInternal, core.ZoneUser}
	for i, d := range dates {
		f.assert(step{zone: zones[i], value: "v" + d + string(zones[i]), validFrom: d})
		f.checkInvariants()
	}
	if n := f.count(`SELECT count(*) FROM facts WHERE status='active'`); n != 1 {
		t.Fatalf("active=%d want 1", n)
	}
	// The raw index still rejects a second active fact written behind the service's back.
	err := f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE facts SET status='active' WHERE status='superseded'`)
		return err
	})
	if err == nil {
		t.Fatal("partial unique index must reject two active single facts")
	}
}
