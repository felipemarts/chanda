# 0030 Exact vector index: in-memory cache and generation counter

BASE.md asks for an exact scan in Go over BLOB vectors. `embed.ExactIndex`
keeps the vectors of each (namespace, model_id) in one flat `[]float32`,
loaded lazily from `embeddings`, and scans it with a dot product (vectors are
L2-normalized, so dot = cosine).

**Coherence.** Writes through the index update the cache after their
transaction commits. Any other writer (forget deletes rows with plain SQL)
is caught by migration `0030_embed_generations.sql`: triggers bump
`embedding_generations.gen` on every insert, update and delete, and each
`Search` compares it (one indexed lookup) before scanning; a moved counter
drops the cached shard and reloads. A forgotten vector can therefore never be
served from memory. Cost: a full reload after an out-of-band change.

**Determinism.** Slots are scanned in fixed contiguous ranges, one bounded
top-k per range, merged under the total order score desc, record id asc,
record type asc. Each score is computed with a fixed summation order, so the
result is bit-identical for any worker count (tested).

**Measured** (Apple M1, 8 cores, k=50, random unit vectors; p50/p95 of one
`Search`, including the SQLite generation check; `scan_workers` = 4 is the
BASE.md "four cores" case):

| Vectors | Dim | 1 worker p50/p95 | 4 workers p50/p95 | 8 workers p50/p95 | Cold load |
| --- | --- | --- | --- | --- | --- |
| 10k | 256 | 0.81 / 0.82 ms | 0.38 / 0.40 ms | 0.38 / 0.39 ms | ~45 ms |
| 10k | 384 | 1.24 / 1.29 ms | 0.53 / 0.54 ms | 0.54 / 0.56 ms | ~65 ms |
| 100k | 256 | 7.8 / 10.1 ms | 2.2 / 2.5 ms | 2.2 / 2.6 ms | ~0.5 s |
| 100k | 384 | 11.7 / 15.6 ms | 3.4 / 5.3 ms | 3.2 / 3.7 ms | 0.7-1.2 s |

The vector step uses under 4% of the 150 ms read budget at 100k records.
Query embedding with the hash model costs ~30 µs. Reproduce with
`CGO_ENABLED=0 go test ./internal/embed -run '^$' -bench . -benchtime 300x`.

**Costs and limits.** Memory is 4 × dim bytes per vector (100k × 384 ≈
150 MB). The cold load is paid on first use and after any out-of-band write,
so `Warm` should run at startup and other packages should write vectors
through the index. At 1M vectors the scan extrapolates to ~35-50 ms p95 on 4
cores but needs ~1.5 GB; that is where the BASE.md open question on scale
(exact scan "stops being enough") bites.

`Search` must not be called inside a caller's transaction (one connection).
