package read

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// Decision 0122 (BASE.md read step 2, "FTS5 with BM25, top 50 per record
// type"): setting common terms aside returns exactly the full OR query's
// top k (same ids, same scores to the bit, ties by id), with common terms
// in most rows, other namespaces, retired and flagged rows; and the fast
// path is taken when the bounds allow it.
func TestKeywordTopWithCommonTermsEqualsFullQuery(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.basic("other")
	g := lcg(5)
	err := f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		for i := 0; i < 4000; i++ {
			ns, src := "ns", "src_doc_ns"
			if i%4 == 0 {
				ns, src = "other", "src_doc_other"
			}
			var words []string
			for j := 0; j < 3+int(g.next()%8); j++ {
				switch x := g.next() % 10; {
				case x < 4:
					words = append(words, "common") // in most rows
				case x < 6:
					words = append(words, "usual")
				default:
					words = append(words, benchWord(int(g.next()%300)))
				}
			}
			var retired any
			if i%11 == 0 {
				retired = t0
			}
			id := fmt.Sprintf("evt_%05d", (i*7919)%4000)
			if _, err := tx.Exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,flags,retired_at)
				VALUES (?,?,?,'turn',?,?,'internal','t',?,'',?)`, id, ns, src, fmt.Sprint(words), fmt.Sprint(i), t0, retired); err != nil {
				return err
			}
			if i%13 == 0 {
				if _, err := tx.Exec(`INSERT INTO flags(id,namespace,record_type,record_id,reason,actor,ts) VALUES (?,?,?,?,?,?,?)`,
					fmt.Sprintf("flg_%05d", i), ns, "event", id, "x", "t", t0); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := New(f.e, Deps{})
	s := ftsSpecs(false)[0]
	cond := `b.retired_at IS NULL AND NOT EXISTS (SELECT 1 FROM flags fl WHERE fl.record_id = b.id)`
	fast := 0
	for _, terms := range [][]string{
		{"common", benchWord(7)},
		{benchWord(3), "common", "usual", benchWord(250)},
		{"common", "usual"},
		{benchWord(1), benchWord(2)},
		{"common", "absent"},
		{"usual", benchWord(299), benchWord(298), benchWord(297)},
	} {
		for _, k := range []int{1, 5, 50, 300} {
			got, err := r.keywordTop(f.ctx, s, terms, "ns", k, cond, nil)
			if err != nil {
				t.Fatal(err)
			}
			// The reference: the single query over the expression in the
			// order keywordTop uses (common terms first).
			maxRow, df, err := r.fts.termStats(f.ctx, f.e.Store.DB(), s.fts, terms)
			if err != nil {
				t.Fatal(err)
			}
			var common, rare []string
			for i, term := range terms {
				if df[i]*commonShare >= maxRow {
					common = append(common, term)
				} else {
					rare = append(rare, term)
				}
			}
			expr := matchExpr(append(append([]string{}, common...), rare...))
			rows, err := f.e.Store.DB().Query(`SELECT events_fts.record_id, -bm25(events_fts) FROM events_fts
				JOIN events b ON b.id = events_fts.record_id WHERE events_fts MATCH ? AND events_fts.namespace = 'ns'
				AND b.namespace = 'ns' AND `+cond+` ORDER BY bm25(events_fts), events_fts.record_id LIMIT ?`, expr, k)
			if err != nil {
				t.Fatal(err)
			}
			var want []hit
			for rows.Next() {
				var h hit
				rows.Scan(&h.id, &h.score)
				h.rank = len(want) + 1
				want = append(want, h)
			}
			rows.Close()
			if len(got) == 0 && len(want) == 0 {
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%v k=%d: got %d hits, want %d\n got %v\nwant %v", terms, k, len(got), len(want), head(got), head(want))
			}
			if len(common) > 0 && len(rare) > 0 {
				ub := 0.0
				for i, term := range terms {
					if df[i]*commonShare >= maxRow && term != "" {
						ub += idfBound(maxRow, df[i]) * (bm25K1 + 1)
					}
				}
				_, ok, err := splitTop(f.ctx, f.e.Store.DB(), s, "("+matchExpr(common)+") AND ("+matchExpr(rare)+")", matchExpr(rare), ub*(1+1e-9), "ns", k, cond, nil)
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					fast++
				}
			}
		}
	}
	if fast == 0 {
		t.Fatal("the split path never proved its result: the test does not exercise it")
	}
}

// The cached statistics follow writes: a new row changes the structure
// record, so the document frequencies are read again.
func TestFTSStatsFollowWrites(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.event(ev{ID: "evt_1", NS: "ns", Source: "src_doc_ns", Content: "alpha beta"})
	st := newFTSStats()
	_, df, err := st.termStats(f.ctx, f.e.Store.DB(), "events_fts", []string{"alpha", "gamma"})
	if err != nil || df[0] != 1 || df[1] != 0 {
		t.Fatalf("df %v err %v", df, err)
	}
	f.event(ev{ID: "evt_2", NS: "ns", Source: "src_doc_ns", Content: "alpha gamma"})
	maxRow, df, err := st.termStats(f.ctx, f.e.Store.DB(), "events_fts", []string{"alpha", "gamma"})
	if err != nil || df[0] != 2 || df[1] != 1 || maxRow < 2 {
		t.Fatalf("after a write: max %d df %v err %v", maxRow, df, err)
	}
	f.exec(`DELETE FROM events WHERE id = 'evt_1'`)
	_, df, err = st.termStats(f.ctx, f.e.Store.DB(), "events_fts", []string{"alpha"})
	if err != nil || df[0] != 1 {
		t.Fatalf("after a delete: df %v err %v", df, err)
	}
}
