package embed

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

// Run: CGO_ENABLED=0 go test ./internal/embed -run '^$' -bench Search -benchtime 300x
//
// Each leaf reports p50/p95 latency of one ExactIndex.Search (generation
// check in SQLite + exact scan + merge), k = 50 as in read step 3, plus the
// cold-load time of the cache. BASE.md target: p95 < 150 ms for the whole
// read at 100k records on 4 cores.

func seedVectors(b *testing.B, e *env.Env, model string, n, dim int) {
	b.Helper()
	ctx := context.Background()
	r := splitmix(42)
	err := e.Store.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `INSERT INTO embeddings(namespace, record_type, record_id, model_id, dim, vector) VALUES (?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for i := 0; i < n; i++ {
			rt := []core.RecordType{core.RecEvent, core.RecFact, core.RecPage}[i%3]
			if _, err := st.ExecContext(ctx, "ns", string(rt), fmt.Sprintf("%s_%08d", core.IDPrefix(rt), i), model, dim, Encode(r.unit(dim))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
}

func percentile(ds []time.Duration, p float64) float64 {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(float64(len(s)-1) * p)
	return float64(s[i].Microseconds()) / 1000
}

func BenchmarkSearch(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		for _, dim := range []int{256, 384} {
			b.Run(fmt.Sprintf("n=%d/dim=%d", n, dim), func(b *testing.B) {
				e, _ := env.NewTest(b)
				model := fmt.Sprintf("bench-%d", dim)
				seedVectors(b, e, model, n, dim)
				r := splitmix(99)
				queries := make([][]float32, 64)
				for i := range queries {
					queries[i] = r.unit(dim)
				}
				for _, workers := range []int{1, 4, runtime.GOMAXPROCS(0)} {
					b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
						e.Cfg.Embed.ScanWorkers = workers
						x := NewExactIndex(e)
						ctx := context.Background()
						t0 := time.Now()
						if _, err := x.Search(ctx, "ns", model, queries[0], 50, nil); err != nil {
							b.Fatal(err)
						}
						load := time.Since(t0)
						ds := make([]time.Duration, 0, b.N)
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							t := time.Now()
							hs, err := x.Search(ctx, "ns", model, queries[i%len(queries)], 50, nil)
							ds = append(ds, time.Since(t))
							if err != nil || len(hs) != 50 {
								b.Fatalf("%d hits, %v", len(hs), err)
							}
						}
						b.StopTimer()
						b.ReportMetric(percentile(ds, 0.50), "p50-ms")
						b.ReportMetric(percentile(ds, 0.95), "p95-ms")
						b.ReportMetric(float64(load.Milliseconds()), "coldload-ms")
					})
				}
			})
		}
	}
}

// BenchmarkHashEmbedQuery is the query-side embedding cost of the hash model.
func BenchmarkHashEmbedQuery(b *testing.B) {
	for _, dim := range []int{256, 384} {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			h, _ := NewHash(dim)
			q := []string{"qual é o preço atual do plano empresarial para clientes em São Paulo?"}
			ctx := context.Background()
			for i := 0; i < b.N; i++ {
				if _, err := h.Embed(ctx, q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
