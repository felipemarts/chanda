# 0100 Performance at 100k records: measurement and fixes

BASE.md "Performance targets": with 100 thousand records and four cores,
p95 under 150 ms without the reranker. This record gives the measurement,
what it found and what changed.

## The measurement

`internal/app/perf_test.go`, `TestPerformanceAt100k` (skipped with `-short`;
`CHANDA_PERF_RECORDS`, `CHANDA_PERF_CACHE_MB`, `CHANDA_PERF_MMAP_MB` and
`CHANDA_PERF_CPUPROFILE=<file>` for experiments). It bulk-inserts by SQL:

- 60 000 document chunks (500 documents of 120 chunks, 40 to 100 words),
- 30 000 session turns (15 to 55 words; a tenth external, some
  instruction-like),
- 10 000 facts over 2 000 entities (one `derived_from` edge each; 8 000
  multi-valued, 2 000 single-valued),
- one 256-dimension hash embedding per record (100 000 vectors).

Text is a skewed draw over 4 096 synthetic words plus 20 plain words that
each appear in about 45% of the events. That is a **stress** vocabulary: a
real corpus seldom has non-stopwords in almost half of the records. Ten
queries rotate (rare words, common words, fact-shaped questions, a miss).
Reads go through `App.Search` / `App.Context` (the full read path:
keyword, vectors with `ExactIndex`, rule filter, fusion, utility and shadow
lookups, retrieval logging with its audit entry), `GOMAXPROCS=4`, after a
warm-up pass; 200 searches and 200 contexts. Writes are 200 `remember`
calls of new session text. Then `prov.Check` of the namespace 3 times.

Hardware: Apple M1 (4 performance + 4 efficiency cores), 8 GB, macOS
(Darwin 25.6), Go 1.26.2, `modernc.org/sqlite` v1.62.0 (`CGO_ENABLED=0`),
database file 278 MB.

## Results

| | before | after |
|---|---|---|
| `search` p50 / p95 | 334 / 558 ms | **106 / 172 ms** |
| `context` p50 / p95 | 330 / 557 ms | **107 / 187 ms** |
| `remember` p50 / p95 | 548 / 833 ms | **262 / 429 ms** |
| `prov.Check` (namespace) p50 | 469 ms | **259 ms** |
| `verify` (audit chain + check) | 423 ms | 244 ms |

Per query (search and context together, after): rare words 13 to 53 ms,
the miss 7 ms, queries with one plain word 75 to 128 ms, and the two
fact-shaped questions ("who is the owner of entity …", "what is the status
of entity … project") 180 to 190 ms: each of them has a term in ~45% of the
events and a term in every fact.

**The target is met at p50 and missed at p95 on this stress corpus** (the
slowest 20% of queries). What remains is FTS5 itself: computing `bm25()`
for every row that matches an OR query, about 1.3 µs per matching row in
the pure-Go SQLite (half of it reading the row's size from the docsize
table). No exact top-k exists over FTS5 from SQL without scoring every
match (the auxiliary function recomputes its statistics on every rowid
lookup). The options left, for the architect:

1. A **common-term cutoff** (as Lucene's common-terms query): terms in more
   than N% of rows do not select candidates, only rarer ones do. Changes
   the ranking; needs the evaluation harness to approve it.
2. Measure on a real corpus before acting: on this vocabulary the
   common terms are far more common than in prose.
3. A CGO build of SQLite (several times faster per row) is excluded by
   `CGO_ENABLED=0`.

## What the profile found and what changed

1. **Page cache.** SQLite's default 2 MB cache made every read a `pread`
   syscall: 94% of CPU time was in syscalls. New `store.cache_mb` (default
   256) sets `PRAGMA cache_size` on the single connection (applied by
   `internal/app`, since `internal/store` is foundation code; the
   connection is never recycled). `store.mmap_mb` (default 0) was measured
   and gave nothing on top of the cache. Both are left out of
   `config.Hash`. **Ask:** move the pragma into `store.Open`.
2. **FTS query shape.** The keyword query joined each FTS5 match to the base
   table and read the FTS row's `record_id` and `namespace` (a content-row
   seek) **for every match** before sorting: with a common term, tens of
   thousands of seeks per query. `read.ftsTop` now runs in two phases: the
   index alone ranks matches by `rowid, bm25()` with a limit `n`, then
   only those `n` rowids are resolved and filtered (namespace, status,
   retired, flags, zones). `n` starts at `1.5k + 16` and grows ×4 until
   the k-th eligible row ranks **strictly** before the n-th match or the
   matches run out, so the result is exactly the old query's, ties
   included (`TestKeywordTopKEqualsSingleQuery` compares both on a corpus
   with ties, other namespaces, retired and flagged rows). The
   near-duplicate guard of the write path uses the same function
   (`read.TopEventMatches`).
3. **`namespace = ? AND id IN (…)` scans.** With more than a few dozen ids
   the planner preferred an index on `namespace` (for events
   `events_session`, for facts `facts_sp`) and walked the whole namespace.
   The read path's batched lookups now write `+namespace = ?`, which keeps
   the filter and leaves the primary-key lookups to the planner.
4. **Rule-filter N+1s.** The near-duplicate collapse looked up
   `duplicate_of` edges by id alone, which no index served (a full
   provenance scan per read): migration `0100_perf_indexes.sql` adds two
   partial indexes on `provenance` for `relation = 'duplicate_of'`, and the
   query became a `UNION` with the relation as a literal. The shadow score
   of the learned ranker was asked per candidate (a model lookup and a
   utility query each); `read.LearnedBatch` scores the list in one call.
5. **`prov.Check`.** Listing every vector read every embeddings row with its
   blob (100 MB) and sorted it: a covering index
   `embeddings_ns_key(namespace, record_type, record_id, model_id)` makes
   it an index-only, ordered scan. The check stays whole-namespace (it runs
   in `verify` and twice per sleep run, never per write: decision 0091);
   at 100k it is 0.26 s.

Retrieval logging (the `retrievals` row and its audit entry, synchronous
commit) measured 1.2 ms per read and was left as is; provenance walks for
the delivered items about 1.5 ms.
