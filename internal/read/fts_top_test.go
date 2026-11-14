package read

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// BASE.md read step 2 ("FTS5 with BM25, top 50 per record type"): the
// two-phase top-k (decision 0100) returns exactly what the single
// JOIN ... ORDER BY bm25, record_id LIMIT k query returns, with ties,
// other namespaces, retired and flagged rows crowding the best matches.
func TestKeywordTopKEqualsSingleQuery(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.basic("other")
	g := lcg(3)
	err := f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		for i := 0; i < 3000; i++ {
			ns, src := "ns", "src_doc_ns"
			if i%3 == 0 { // the other namespace holds a third of the best matches
				ns, src = "other", "src_doc_other"
			}
			// Few distinct texts: many exact bm25 ties.
			words := []string{"alpha", benchWord(int(g.next() % 6))}
			if i%7 == 0 {
				words = append(words, "alpha", "beta")
			}
			for j := 0; j < int(g.next()%4); j++ {
				words = append(words, benchWord(int(g.next()%50)))
			}
			text := fmt.Sprint(words)
			var retired any
			if i%11 == 0 {
				retired = t0
			}
			if _, err := tx.Exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,flags,retired_at)
				VALUES (?,?,?,'turn',?,?,'internal','t',?,'',?)`, fmt.Sprintf("evt_%05d", (i*7919)%3000), ns, src, text, fmt.Sprint(i), t0, retired); err != nil {
				return err
			}
			if i%13 == 0 {
				if _, err := tx.Exec(`INSERT INTO flags(id,namespace,record_type,record_id,reason,actor,ts) VALUES (?,?,?,?,?,?,?)`,
					fmt.Sprintf("flg_%05d", i), ns, "event", fmt.Sprintf("evt_%05d", (i*7919)%3000), "x", "t", t0); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cond := `b.retired_at IS NULL AND NOT EXISTS (SELECT 1 FROM flags fl WHERE fl.record_id = b.id)`
	for _, expr := range []string{`"alpha"`, `"beta"`, `"alpha" OR "` + benchWord(3) + `"`, `"` + benchWord(40) + `"`, `"absent"`} {
		for _, k := range []int{1, 5, 50, 200, 5000} {
			got, err := ftsTop(f.ctx, f.e.Store.DB(), "events_fts", "events", "bm25(events_fts)", expr, "ns", k, cond, nil)
			if err != nil {
				t.Fatal(err)
			}
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
				t.Fatalf("%s k=%d: got %d hits, want %d\n got %v\nwant %v", expr, k, len(got), len(want), head(got), head(want))
			}
		}
	}
}

func head(h []hit) []hit { return h[:min(5, len(h))] }
