package read

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/keys"
)

// key seeds a learned key (and the retrieval it came from) pointing to the
// record (rt, rid) of namespace ns.
func (f *fx) key(ns, id, rt, rid, text string) {
	f.t.Helper()
	f.exec(`INSERT OR IGNORE INTO retrievals(id, namespace, mode, query_text, params, candidates, delivered, ts) VALUES (?,?,?,?,?,?,?,?)`,
		"ret_k_"+id, ns, "context", text, "{}", "[]", "[]", t0)
	f.exec(`INSERT INTO learned_keys(id, namespace, record_type, record_id, retrieval_id, text, created_at) VALUES (?,?,?,?,?,?,?)`,
		id, ns, rt, rid, "ret_k_"+id, text, t0)
}

// keyCorpus: two parking events and an unrelated one; the query words
// ("car permit") appear in no record, only in the key of evt_pass.
func keyCorpus(f *fx, ns string) {
	f.basic(ns)
	f.event(ev{ID: "evt_pass_" + ns, NS: ns, Source: "src_user_" + ns, Kind: "statement", Zone: "user",
		Content: "Parking passes are handed out at the reception desk on Mondays."})
	f.event(ev{ID: "evt_lot_" + ns, NS: ns, Source: "src_user_" + ns, Kind: "statement", Zone: "user",
		Content: "The parking lot closes at nine in the evening."})
	f.event(ev{ID: "evt_wifi_" + ns, NS: ns, Source: "src_user_" + ns, Kind: "statement", Zone: "user",
		Content: "The office wifi password is kept in the blue binder."})
	f.key(ns, "key_pass_"+ns, "event", "evt_pass_"+ns, "where do I get a car permit")
}

const keyQuery = "car permit please"

func loggedCands(t *testing.T, f *fx, ret string) []LoggedCandidate {
	t.Helper()
	var cj string
	if err := f.e.Store.DB().QueryRow(`SELECT candidates FROM retrievals WHERE id = ?`, ret).Scan(&cj); err != nil {
		t.Fatal(err)
	}
	var lc []LoggedCandidate
	if err := json.Unmarshal([]byte(cj), &lc); err != nil {
		t.Fatal(err)
	}
	return lc
}

// Decision 0145 "Read": a record that only a key reaches is a candidate
// with keys on, and absent with keys off or the namespace switch off. The
// key itself is never an item or block text.
func TestKeyOnlyRecordAppearsWhenOn(t *testing.T) {
	f := newFx(t)
	keyCorpus(f, "ns")
	r := New(f.e, Deps{})

	off, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: keyQuery})
	if err != nil {
		t.Fatal(err)
	}
	if len(off.Items) != 0 || off.Reason != ReasonNoRelevantMemory {
		t.Fatalf("keys off: %+v", off.Items)
	}
	for _, lc := range loggedCands(t, f, off.RetrievalID) {
		if _, ok := lc.Features["key_rank"]; ok {
			t.Fatal("keys off must not log key features")
		}
	}

	f.e.Cfg.Read.LearnedKeys = true
	on, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: keyQuery})
	if err != nil {
		t.Fatal(err)
	}
	if len(on.Items) != 1 || on.Items[0].ID != "evt_pass_ns" {
		t.Fatalf("keys on: %+v (%s)", on.Items, on.Reason)
	}
	if strings.Contains(on.Text, "car permit") || strings.Contains(on.Text, "key_") {
		t.Fatalf("a key leaked into the block:\n%s", on.Text)
	}
	lc := loggedCands(t, f, on.RetrievalID)
	if len(lc) != 1 || lc[0].ID != "evt_pass_ns" || lc[0].Features["key_rank"] != 1 || lc[0].Features["key_coverage"] != 1 ||
		lc[0].Features["kw_rank"] != 0 || !lc[0].Delivered() {
		t.Fatalf("logged candidates: %+v", lc)
	}
	sr, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: keyQuery})
	if err != nil || len(sr.Items) != 1 || sr.Items[0].ID != "evt_pass_ns" {
		t.Fatalf("search mode: %+v %v", sr.Items, err)
	}

	if _, err := keys.SetEnabled(f.ctx, f.e, "ns", false, "admin"); err != nil {
		t.Fatal(err)
	}
	dis, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: keyQuery})
	if err != nil || len(dis.Items) != 0 {
		t.Fatalf("switch off: %+v %v", dis.Items, err)
	}
}

// Off is byte-identical: the same reads give the same items, scores and
// logged candidates with keys present and read.learned_keys off as with no
// key at all.
func TestKeysOffIsUnchanged(t *testing.T) {
	run := func(withKeys bool) ([]Item, []LoggedCandidate) {
		f := newFx(t)
		keyCorpus(f, "ns")
		if !withKeys {
			f.exec(`DELETE FROM learned_keys`)
		}
		res, err := New(f.e, Deps{}).Context(f.ctx, ContextInput{Namespace: "ns", Query: "where is the parking pass desk"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Items, loggedCands(t, f, res.RetrievalID)
	}
	ia, la := run(true)
	ib, lb := run(false)
	if len(ia) == 0 || !reflect.DeepEqual(ia, ib) || !reflect.DeepEqual(la, lb) {
		t.Fatalf("keys off changed the read:\n%+v\n%+v", ia, ib)
	}
}

// The key list moves a record up by w/(k + key rank), more with a larger
// weight, and leaves the others' scores untouched.
func TestKeyWeightChangesRanking(t *testing.T) {
	order := func(on bool, w float64) ([]string, map[string]float64) {
		f := newFx(t)
		keyCorpus(f, "ns")
		f.key("ns", "key_lot_ns", "event", "evt_lot_ns", "when does parking close")
		f.e.Cfg.Read.LearnedKeys, f.e.Cfg.Read.LearnedKeysWeight = on, w
		res, err := New(f.e, Deps{}).Search(f.ctx, SearchInput{Namespace: "ns", Query: "parking passes desk"})
		if err != nil {
			t.Fatal(err)
		}
		rrf := map[string]float64{}
		for _, it := range res.Items {
			rrf[it.ID] = *it.Scores.RRF
		}
		return ids(res.Items), rrf
	}
	base, baseRRF := order(false, 0)
	if !reflect.DeepEqual(base, []string{"evt_pass_ns", "evt_lot_ns"}) {
		t.Fatalf("baseline order %v", base)
	}
	// "parking" matches the key of evt_lot (keyword rank 1 among keys).
	got, rrf := order(true, 0)
	if !reflect.DeepEqual(got, []string{"evt_lot_ns", "evt_pass_ns"}) {
		t.Fatalf("w=1: %v %v", got, rrf)
	}
	k := 60.0
	if d := rrf["evt_lot_ns"] - baseRRF["evt_lot_ns"]; d < 1/(k+1)-1e-12 || d > 1/(k+1)+1e-12 {
		t.Fatalf("w=1 must add 1/(k+1): %v", d)
	}
	if rrf["evt_pass_ns"] != baseRRF["evt_pass_ns"] {
		t.Fatalf("a record without a key hit changed: %v vs %v", rrf["evt_pass_ns"], baseRRF["evt_pass_ns"])
	}
	_, rrf3 := order(true, 3)
	if d := rrf3["evt_lot_ns"] - baseRRF["evt_lot_ns"]; d < 3/(k+1)-1e-12 || d > 3/(k+1)+1e-12 {
		t.Fatalf("w=3 must add 3/(k+1): %v", d)
	}
}

// A record reached only through a key still passes the rule filter: a
// retired event, a record of another namespace and a type outside the
// requested types are not returned; a key of another namespace is not
// used.
func TestKeyOnlyRecordsPassTheRuleFilter(t *testing.T) {
	f := newFx(t)
	keyCorpus(f, "ns")
	keyCorpus(f, "other")
	f.e.Cfg.Read.LearnedKeys = true
	f.event(ev{ID: "evt_old_ns", NS: "ns", Source: "src_user_ns", Kind: "statement", Zone: "user",
		Content: "Old rule about visitors.", Retired: t0})
	f.key("ns", "key_old_ns", "event", "evt_old_ns", "car permit for visitors")
	f.key("ns", "key_cross", "event", "evt_lot_other", "car permit at night") // points across namespaces
	f.key("other", "key_other_only", "event", "evt_wifi_other", "car permit wifi")
	r := New(f.e, Deps{})
	res, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: keyQuery})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Items); !reflect.DeepEqual(got, []string{"evt_pass_ns"}) {
		t.Fatalf("items %v", got)
	}
	drops := map[string]string{}
	for _, lc := range loggedCands(t, f, res.RetrievalID) {
		drops[lc.ID] = lc.Dropped
	}
	if drops["evt_old_ns"] != dropRetired {
		t.Fatalf("retired key-only record: %q", drops["evt_old_ns"])
	}
	if _, ok := drops["evt_lot_other"]; ok {
		t.Fatal("a record of another namespace must not be logged")
	}
	if _, ok := drops["evt_wifi_other"]; ok {
		t.Fatal("a key of another namespace must not be used")
	}
	typed, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: keyQuery, Types: []core.RecordType{core.RecFact}})
	if err != nil || len(typed.Items) != 0 {
		t.Fatalf("types filter: %+v %v", typed.Items, err)
	}
}

// The vector side: the query vector against key vectors, with the key's
// cosine as evidence for the cut.
func TestKeyVectorHitCountsAsEvidence(t *testing.T) {
	f := newFx(t)
	keyCorpus(f, "ns")
	f.e.Cfg.Read.LearnedKeys = true
	idx := &fakeIndex{hits: map[string][]core.VectorHit{"ns": {
		{RecordType: core.RecKey, RecordID: "key_pass_ns", Score: 0.97},
		{RecordType: core.RecEvent, RecordID: "evt_wifi_ns", Score: 0.10},
	}}}
	r := New(f.e, Deps{Embedder: roleEmb{}, VectorIndex: idx})
	res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "how do visitors leave the vehicle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) == 0 || res.Items[0].ID != "evt_pass_ns" {
		t.Fatalf("items %+v (%s)", res.Items, res.Reason)
	}
	for _, it := range res.Items {
		if strings.HasPrefix(it.ID, "key_") {
			t.Fatalf("a key was returned as an item: %+v", it)
		}
	}
	var pass LoggedCandidate
	for _, lc := range loggedCands(t, f, res.RetrievalID) {
		if lc.ID == "evt_pass_ns" {
			pass = lc
		}
		if lc.ID == "evt_wifi_ns" && lc.Features["key_rank"] != 0 {
			t.Fatalf("no key hit must log key_rank 0: %+v", lc.Features)
		}
	}
	if pass.Features["key_rank"] != 1 || pass.Features["key_cosine"] < 0.969 || pass.Features["vec_rank"] != 0 {
		t.Fatalf("evt_pass features %+v", pass.Features)
	}
}

// Determinism: the same database and query give the same result twice.
func TestKeysReadDeterministic(t *testing.T) {
	f := newFx(t)
	keyCorpus(f, "ns")
	f.key("ns", "key_lot_ns", "event", "evt_lot_ns", "car permit parking hours")
	f.e.Cfg.Read.LearnedKeys = true
	r := New(f.e, Deps{})
	a, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: keyQuery})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: keyQuery})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 2 || !reflect.DeepEqual(a.Items, b.Items) {
		t.Fatalf("not deterministic:\n%+v\n%+v", a.Items, b.Items)
	}
}
