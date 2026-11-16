package facts

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/felipemarts/chanda/internal/core"
)

func at(d string) time.Time {
	t, err := core.ParseTime(d)
	if err != nil {
		panic(err)
	}
	return t
}

func values(fs []Fact) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Value)
	}
	return out
}

func TestTimeQueries_TimelineAsOfCurrent(t *testing.T) {
	f := newFixture(t)
	r1 := f.assert(step{value: "Engineer", validFrom: "2026-01-01"})
	r2 := f.assert(step{value: "Manager", validFrom: "2026-06-01"})
	r3 := f.assert(step{value: "Intern", validFrom: "2025-06-01"}) // history
	f.assert(step{zone: core.ZoneExternal, value: "CEO", validFrom: "2026-07-01"})
	f.assert(step{value: "manager", validFrom: "2026-08-01"}) // confirms r2

	tl, err := f.s.Timeline(f.ctx, testNS, "alice", "Job Title")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range tl {
		got = append(got, e.Value+"/"+string(e.Status))
	}
	want := []string{"Intern/superseded", "Engineer/superseded", "Manager/active", "CEO/quarantined"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("timeline=%v want %v", got, want)
	}
	// Origins of each change: derived_from events, plus confirmed_by and supersedes.
	if len(tl[0].Origins) != 1 || tl[0].Origins[0].Relation != core.RelDerivedFrom || tl[0].Origins[0].Type != core.RecEvent {
		t.Errorf("history origins=%+v", tl[0].Origins)
	}
	rels := map[string]int{}
	for _, o := range tl[2].Origins {
		rels[o.Relation]++
	}
	if rels[core.RelDerivedFrom] != 1 || rels[core.RelConfirmedBy] != 1 || rels[core.RelSupersedes] != 1 {
		t.Errorf("current origins=%+v", tl[2].Origins)
	}
	if tl[0].ID != r3.FactID || tl[1].ID != r1.FactID || tl[2].ID != r2.FactID {
		t.Errorf("timeline ids out of order")
	}

	asOf := []struct {
		date string
		want []string
	}{
		{"2025-01-01", nil},
		{"2025-06-01", []string{"Intern"}},
		{"2025-12-31", []string{"Intern"}},
		{"2026-01-01", []string{"Engineer"}}, // valid_to is exclusive
		{"2026-03-15", []string{"Engineer"}},
		{"2026-06-01", []string{"Manager"}},
		{"2026-07-15", []string{"Manager"}}, // quarantined CEO never holds
		{"2030-01-01", []string{"Manager"}},
	}
	for _, c := range asOf {
		fs, err := f.s.AsOf(f.ctx, testNS, "Alice", "job_title", at(c.date))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(values(fs)) != fmt.Sprint(c.want) {
			t.Errorf("as_of %s = %v want %v", c.date, values(fs), c.want)
		}
	}
	cur, err := f.s.Current(f.ctx, testNS, r2.EntityID, "job_title")
	if err != nil || fmt.Sprint(values(cur)) != "[Manager]" {
		t.Fatalf("current=%v %v", values(cur), err)
	}
	// Unknown subjects return empty, and are not created.
	for _, q := range []string{"Nobody", "ent_01NOTANENTITY0000000000000"} {
		if fs, err := f.s.Current(f.ctx, testNS, q, ""); err != nil || len(fs) != 0 {
			t.Errorf("unknown subject %q: %v %v", q, fs, err)
		}
	}
	if n := f.count(`SELECT count(*) FROM entities`); n != 1 {
		t.Errorf("queries created entities: %d", n)
	}
}

func TestTimeQueries_AsOfExcludesStaleFlaggedRejected(t *testing.T) {
	f := newFixture(t)
	for i, st := range []core.FactStatus{core.FactStaleSource, core.FactFlagged, core.FactRejected, core.FactActive} {
		r := f.assert(step{predicate: "skill", value: fmt.Sprintf("s%d", i), validFrom: "2026-01-01"})
		f.exec(`UPDATE facts SET status=? WHERE id=?`, string(st), r.FactID)
	}
	fs, err := f.s.AsOf(f.ctx, testNS, "Alice", "", at("2026-02-01"))
	if err != nil || fmt.Sprint(values(fs)) != "[s3]" {
		t.Fatalf("as_of=%v %v", values(fs), err)
	}
}

// TestTemporalSuite is the "Temporal" evaluation suite in miniature: 50
// updates to single predicates across subjects, in and out of date order,
// with repeats and lower-zone noise. Current() must never answer with a
// superseded value, and as_of must agree with a reference model on every
// change date.
func TestTemporalSuite_NoSupersededAnswers(t *testing.T) {
	f := newFixture(t)
	model := map[string][]modelEntry{}
	subjects := []string{"Alice", "Bob", "Carol"}
	preds := []string{"job_title", "address"}
	base := at("2026-01-01")
	var probes []string
	for i := 0; i < 50; i++ {
		subj, pred := subjects[i%3], preds[(i/3)%2]
		key := subj + "|" + pred
		offset := i * 3
		if i%5 == 0 {
			offset -= 40 // out of order: becomes history
		}
		vf := core.FormatTime(base.AddDate(0, 0, offset))
		probes = append(probes, vf)
		hist := model[key]
		switch {
		case i%7 == 3 && len(hist) > 0: // external noise: quarantined, excluded from the model
			r := f.assert(step{zone: core.ZoneExternal, subject: subj, predicate: pred, value: fmt.Sprintf("x%d", i), validFrom: vf})
			if r.Action != ActionQuarantined {
				t.Fatalf("update %d: %s", i, r.Action)
			}
		case i%11 == 4 && len(hist) > 0: // re-assert the current value at its own date
			cur := current(hist)
			r := f.assert(step{subject: subj, predicate: pred, value: cur.value, validFrom: cur.vf})
			if r.Action != ActionConfirmed {
				t.Fatalf("update %d: %s", i, r.Action)
			}
		default:
			v := fmt.Sprintf("v%d", i)
			f.assert(step{subject: subj, predicate: pred, value: v, validFrom: vf})
			model[key] = append(hist, modelEntry{vf, v, i})
		}
		f.checkInvariants()
	}

	stale := 0
	keys := make([]string, 0, len(model))
	for k := range model {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		subj, pred := splitKey(k)
		cur, err := f.s.Current(f.ctx, testNS, subj, pred)
		if err != nil {
			t.Fatal(err)
		}
		if len(cur) != 1 || cur[0].Value != current(model[k]).value {
			stale++
			t.Errorf("%s: current=%v want %s", k, values(cur), current(model[k]).value)
		}
		for _, p := range probes {
			want := heldAt(model[k], p)
			fs, err := f.s.AsOf(f.ctx, testNS, subj, pred, at(p))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(values(fs)) != fmt.Sprint(want) {
				t.Errorf("%s as_of %s = %v want %v", k, p, values(fs), want)
			}
		}
	}
	if stale != 0 {
		t.Fatalf("%d answers with a superseded value", stale)
	}
	// The sequence exercised every row of the decision table.
	for _, a := range []string{"supersede", "quarantine", "fact_confirm"} {
		if f.count(`SELECT count(*) FROM audit_log WHERE action=?`, a) == 0 {
			t.Errorf("no %s in the sequence", a)
		}
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE action='fact_insert' AND details LIKE '%"history"%'`) == 0 {
		t.Errorf("no history insert in the sequence")
	}
}

func splitKey(k string) (string, string) {
	for i := range k {
		if k[i] == '|' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

type modelEntry struct {
	vf    string
	value string
	order int
}

// current is the entry with the latest valid_from; ties go to the later write.
func current(es []modelEntry) modelEntry {
	var best modelEntry
	for i, e := range es {
		if i == 0 || e.vf > best.vf || (e.vf == best.vf && e.order > best.order) {
			best = e
		}
	}
	return best
}

func heldAt(es []modelEntry, t string) []string {
	var cand []modelEntry
	for _, e := range es {
		if e.vf <= t {
			cand = append(cand, e)
		}
	}
	if len(cand) == 0 {
		return nil
	}
	return []string{current(cand).value}
}
