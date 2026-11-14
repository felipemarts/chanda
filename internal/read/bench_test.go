package read

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// lcg is a fixed linear congruential generator (no math/rand: determinism).
type lcg uint64

func (l *lcg) next() uint64 {
	*l = *l*6364136223846793005 + 1442695040888963407
	return uint64(*l >> 33)
}

var syll = []string{"ka", "lo", "mi", "ne", "pu", "ra", "si", "to", "ve", "zu", "bri", "cha", "dor", "fen", "gal", "hum"}

// benchWord returns vocabulary word i (3 syllables, 4096 words).
func benchWord(i int) string {
	return syll[i%16] + syll[(i/16)%16] + syll[(i/256)%16]
}

// seedEvents inserts n events of 40 to 80 words with a skewed word
// distribution (common words repeat often, rare words seldom).
func seedEvents(tb testing.TB, f *fx, n int) {
	f.basic("ns")
	f.doc("ns", "doc_bench", "src_doc_ns", "file:///bench.md", 1)
	g := lcg(42)
	err := f.e.Store.Tx(f.ctx, func(tx *sql.Tx) error {
		st, err := tx.Prepare(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,doc_id,doc_version,ordinal,flags)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'')`)
		if err != nil {
			return err
		}
		defer st.Close()
		for i := 0; i < n; i++ {
			words := make([]string, 40+int(g.next()%41))
			for j := range words {
				x := int(g.next() % 4096)
				words[j] = benchWord(x * x / 4096) // skew toward low indices
			}
			id := fmt.Sprintf("evt_%06d", i)
			if _, err := st.Exec(id, "ns", "src_doc_ns", "chunk", strings.Join(words, " "), "h"+id, "internal", "bench", t0, "doc_bench", 1, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
}

var benchQueries = []string{
	benchWord(5) + " " + benchWord(300) + " " + benchWord(1200),
	"como configurar " + benchWord(40) + " e " + benchWord(900),
	benchWord(2000) + " " + benchWord(3000) + " " + benchWord(77) + " " + benchWord(1),
	"something entirely absent from the corpus",
}

// BenchmarkSearchKeyword10k measures the read path, keyword only, over
// 10 000 events (the measurement includes the retrievals log write and its
// audit entry, which commit with synchronous=FULL).
func BenchmarkSearchKeyword10k(b *testing.B) {
	f := newFx(b)
	seedEvents(b, f, 10000)
	r := New(f.e, Deps{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: benchQueries[i%len(benchQueries)]}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContextKeyword10k(b *testing.B) {
	f := newFx(b)
	seedEvents(b, f, 10000)
	r := New(f.e, Deps{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: benchQueries[i%len(benchQueries)]}); err != nil {
			b.Fatal(err)
		}
	}
}

// TestLatency10kKeyword logs p50/p95 for 200 searches over 10 000 events.
// Skipped with -short.
func TestLatency10kKeyword(t *testing.T) {
	if testing.Short() {
		t.Skip("latency measurement")
	}
	f := newFx(t)
	seedEvents(t, f, 10000)
	r := New(f.e, Deps{})
	for _, mode := range []Mode{ModeSearch, ModeContext} {
		var ds []time.Duration
		for i := 0; i < 200; i++ {
			q := benchQueries[i%len(benchQueries)]
			start := time.Now()
			var err error
			if mode == ModeSearch {
				_, err = r.Search(f.ctx, SearchInput{Namespace: "ns", Query: q})
			} else {
				_, err = r.Context(f.ctx, ContextInput{Namespace: "ns", Query: q})
			}
			if err != nil {
				t.Fatal(err)
			}
			ds = append(ds, time.Since(start))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		t.Logf("%s keyword-only, 10k events: p50=%v p95=%v max=%v", mode, ds[len(ds)/2], ds[len(ds)*95/100], ds[len(ds)-1])
	}
}
