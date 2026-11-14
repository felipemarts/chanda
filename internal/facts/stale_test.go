package facts

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

func TestDocumentUpdate_FactsFromRetiredChunksBecomeStale(t *testing.T) {
	f := newFixture(t)
	assertFrom := func(pred, value string, evs ...string) string {
		res, err := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: "Acme", Predicate: pred, Value: value, OriginEventIDs: evs})
		if err != nil {
			t.Fatal(err)
		}
		return res.FactID
	}
	e1, e2, e3 := f.event(core.ZoneInternal), f.event(core.ZoneInternal), f.event(core.ZoneInternal)
	onlyE1 := assertFrom("owner", "Maria", e1)
	both := assertFrom("address", "Rua A", e1, e2)
	confirmed := assertFrom("skill", "Go", e1)
	if r, _ := f.s.AssertCommit(f.ctx, AssertInput{Namespace: testNS, Subject: "Acme", Predicate: "skill", Value: "go", OriginEventIDs: []string{e3}}); r.Action != ActionConfirmed {
		t.Fatalf("expected confirm, got %s", r.Action)
	}
	untouched := assertFrom("phone", "123", e2)

	f.exec(`UPDATE events SET retired_at='t' WHERE id=?`, e1)
	var marked []string
	f.tx(func(tx *sql.Tx) (err error) {
		marked, err = f.s.MarkStaleFromRetired(f.ctx, tx, testNS, []string{e1})
		return err
	})
	if fmt.Sprint(marked) != fmt.Sprint([]string{onlyE1}) {
		t.Fatalf("marked=%v want [%s]", marked, onlyE1)
	}
	cases := []struct {
		name string
		id   string
		want core.FactStatus
	}{
		{"only origin retired: stale_source", onlyE1, core.FactStaleSource},
		{"another live derived_from origin: survives", both, core.FactActive},
		{"a live confirmation: survives", confirmed, core.FactActive},
		{"unrelated fact: untouched", untouched, core.FactActive},
	}
	for _, c := range cases {
		if got := f.fact(c.id).Status; got != c.want {
			t.Errorf("%s: status=%s want %s", c.name, got, c.want)
		}
	}
	// Retiring the last live origin of a fact makes it stale; already stale facts are not re-audited.
	f.exec(`UPDATE events SET retired_at='t' WHERE id IN (?, ?)`, e2, e3)
	f.tx(func(tx *sql.Tx) (err error) {
		marked, err = f.s.MarkStaleFromRetired(f.ctx, tx, testNS, []string{e1, e2, e3})
		return err
	})
	if len(marked) != 3 {
		t.Fatalf("second pass marked=%v", marked)
	}
	if n := f.count(`SELECT count(*) FROM facts WHERE status='active'`); n != 0 {
		t.Fatalf("facts from retired chunks still active: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action='stale_source'`); n != 4 {
		t.Fatalf("stale_source audits=%d want 4", n)
	}
	if cur, _ := f.s.Current(f.ctx, testNS, "Acme", ""); len(cur) != 0 {
		t.Fatalf("current still returns stale facts: %v", values(cur))
	}
	// A passed id that is not retired keeps its facts alive.
	e4 := f.event(core.ZoneInternal)
	live := assertFrom("email", "a@b.c", e4)
	f.tx(func(tx *sql.Tx) (err error) {
		marked, err = f.s.MarkStaleFromRetired(f.ctx, tx, testNS, []string{e4})
		return err
	})
	if len(marked) != 0 || f.fact(live).Status != core.FactActive {
		t.Fatalf("live origin must keep the fact: %v", marked)
	}
	f.verifyAudit()
}
