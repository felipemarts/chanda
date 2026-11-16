package sleep

import (
	"fmt"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
)

// Decision table for facts, applied when an entity merge is accepted: two
// active values of a single predicate (one per old entity id) are reconciled
// — the later valid_from wins (ties by recorded_at, then id), a lower zone is
// quarantined, multi predicates coexist.
func TestAcceptedMergeReconcilesSinglePredicates(t *testing.T) {
	f := newFixture(t)
	f.event("evt_1", "internal", "origin")
	f.entity("ent_a", "Marina Duarte", "person")
	f.entity("ent_b", "M. Duarte", "person")
	f.entity("ent_c", "Marina D.", "person")
	f.exec(`UPDATE entities SET merged_into='ent_b' WHERE id='ent_c'`) // an older merge into the merged entity

	type row struct{ id, subject, pred, zone, validFrom, recordedAt string }
	for _, r := range []row{
		// later valid_from wins
		{"fct_j1", "ent_a", "job_title", "internal", "2025-01-01", "2025-01-01"},
		{"fct_j2", "ent_b", "job_title", "internal", "2025-06-01", "2025-06-01"},
		// equal valid_from: later recorded_at wins
		{"fct_l1", "ent_b", "location", "internal", "2025-03-01", "2025-03-02"},
		{"fct_l2", "ent_a", "location", "internal", "2025-03-01", "2025-03-05"},
		// full tie: the larger id wins
		{"fct_s2", "ent_a", "status", "internal", "2025-03-01", "2025-03-01"},
		{"fct_s1", "ent_b", "status", "internal", "2025-03-01", "2025-03-01"},
		// lower zone is quarantined even though it is later
		{"fct_e1", "ent_a", "email", "user", "2025-01-01", "2025-01-01"},
		{"fct_e2", "ent_b", "email", "external", "2026-01-01", "2026-01-01"},
		// three entities: a chain, each closed by the next
		{"fct_m1", "ent_a", "manager", "internal", "2025-01-01", "2025-01-01"},
		{"fct_m2", "ent_b", "manager", "internal", "2025-03-01", "2025-03-01"},
		{"fct_m3", "ent_c", "manager", "internal", "2025-02-01", "2025-02-01"},
	} {
		f.fact(r.id, r.subject, r.pred, "v-"+r.id, "active", r.zone, true, "evt_1")
		f.exec(`UPDATE facts SET valid_from=?, recorded_at=? WHERE id=?`, r.validFrom+"T00:00:00.000000000Z", r.recordedAt+"T00:00:00.000000000Z", r.id)
	}
	f.fact("fct_k1", "ent_a", "skill", "Go", "active", "internal", false, "evt_1")
	f.fact("fct_k2", "ent_b", "skill", "SQL", "active", "internal", false, "evt_1")
	f.fact("fct_o1", "ent_b", "phone", "123", "active", "internal", true, "evt_1") // alone: unchanged
	f.exec(`INSERT INTO entity_merge_proposals(id,namespace,entity_a,entity_b,score,reason,status,created_at) VALUES
		('prp_1',?,'ent_a','ent_b',0.95,'name_similarity','pending',?)`, ns, f.now())

	fake := script{judge: func(ids []string) string {
		var vs []string
		for _, id := range ids {
			if id == "prp_1" {
				vs = append(vs, fmt.Sprintf(`{"id":%q,"verdict":"supersede"}`, id))
			}
		}
		return `{"verdicts":[` + strings.Join(vs, ",") + `]}`
	}}.fake()
	s, _ := f.sleeper(fake)
	res := f.run(s, RunInput{})
	if res.Status != StatusCompleted {
		t.Fatalf("status %s %v", res.Status, res.Notices)
	}
	if got := f.str(`SELECT merged_into FROM entities WHERE id='ent_b'`); got != "ent_a" {
		t.Fatalf("merged_into %q", got)
	}
	cases := []struct{ id, status, validTo, by string }{
		{"fct_j1", "superseded", "2025-06-01", "fct_j2"}, {"fct_j2", "active", "", ""},
		{"fct_l1", "superseded", "2025-03-01", "fct_l2"}, {"fct_l2", "active", "", ""},
		{"fct_s1", "superseded", "2025-03-01", "fct_s2"}, {"fct_s2", "active", "", ""},
		{"fct_e1", "active", "", ""}, {"fct_e2", "quarantined", "", ""},
		{"fct_m1", "superseded", "2025-02-01", "fct_m3"}, {"fct_m3", "superseded", "2025-03-01", "fct_m2"}, {"fct_m2", "active", "", ""},
		{"fct_k1", "active", "", ""}, {"fct_k2", "active", "", ""},
		{"fct_o1", "active", "", ""},
	}
	for _, c := range cases {
		st := f.str(`SELECT status FROM facts WHERE id=?`, c.id)
		vt := f.str(`SELECT substr(COALESCE(valid_to,''),1,10) FROM facts WHERE id=?`, c.id)
		by := f.str(`SELECT COALESCE(superseded_by,'') FROM facts WHERE id=?`, c.id)
		if st != c.status || vt != c.validTo || by != c.by {
			t.Errorf("%s: %s %q %q, want %s %q %q", c.id, st, vt, by, c.status, c.validTo, c.by)
		}
		if c.by != "" && f.count(`SELECT count(*) FROM provenance WHERE derived_id=? AND source_id=? AND relation='supersedes'`, c.by, c.id) != 1 {
			t.Errorf("%s: supersedes edge from %s missing", c.id, c.by)
		}
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action=? AND details LIKE '%entity_merge%'`, audit.ActSupersede); n != 5 {
		t.Errorf("supersede audits %d, want 5", n)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action=? AND target='fct_e2'`, audit.ActQuarantine); n != 1 {
		t.Errorf("quarantine audits %d", n)
	}
	if len(res.Counts.FactsSuperseded) != 5 || len(res.Counts.FactsQuarantined) != 1 {
		t.Errorf("counts superseded %v quarantined %v", res.Counts.FactsSuperseded, res.Counts.FactsQuarantined)
	}
	if !strings.Contains(res.Report, "Quarantined by a merge") {
		t.Errorf("report:\n%s", res.Report)
	}
	f.verifyAudit()
}
