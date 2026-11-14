# 0122 Read latency at 100k: exact top-k with common terms set aside

Decision 0100 left read p95 at 172 ms (search) and 187 ms (context) on the
100k stress corpus, against the 150 ms target, and named FTS5 itself as
the remaining cost: `bm25()` is computed for every row that matches the OR
query, and the slow queries each hold a term found in about half of the
events (or in every fact). It proposed a common-term cutoff, which changes
the ranking. This record takes an exact route instead.

## MaxScore over FTS5's own bm25

FTS5's `bm25()` is a sum over the query's phrases of
`idf · f·(k1+1) / (f + k1·(1 − b + b·D/avgdl))`, each bounded by
`idf · (k1+1)` (k1 = 1.2; FTS5 uses `idf = 1e-6` when it is not positive).
`read.keywordTop` splits the terms of one FTS table into common C (in at
least a quarter of the rows) and rare R, and runs two queries instead of
the full OR:

- `E1 = (C) AND (R)`: every row holding a term of each group, scored by
  FTS5 over the same phrases, so its score is the full query's;
- `E2 = R`, best first, growing ×4 like `ftsTop`: rows holding only rare
  terms, whose full score is their rare-only score (the common phrases add
  exactly 0.0).

Phrases are ordered C then R in the full expression too, so the
floating-point sums are identical to the bit. The candidates go through the
same namespace and eligibility filter as `ftsTop` (refactored into
`eligibleMatches`). The result is returned only if the k-th best eligible
row scores at least `UB(C) = Σ idf_ub(c)·(k1+1)` (no row with only common
terms can reach it) and, when E2 was truncated, strictly more than E2's last
row; otherwise E2 grows or the full query runs. `idf_ub` uses `max(rowid)`
of the FTS table as the row count (an upper bound, so idf is over-, never
under-estimated), and the bound gets a 1e-9 relative margin for the last bit
of `log`. The result is therefore always the full query's, ties by record id
included (`TestKeywordTopWithCommonTermsEqualsFullQuery` compares both on a
corpus with common terms, ties, another namespace, retired and flagged rows,
and checks that the fast path is taken).

The split is tried only when there are both common and rare terms and the
rare terms hold at most a quarter of the query's postings (otherwise E1 and
E2 score about as many rows as the full query: measured, a query of five
mid-frequency words plus a common one went from 113 ms to 245 ms without
this guard). Inside a scoring transaction (`Score` with a queryer, the
sleep gate) the full query always runs: pending writes would make cached
statistics wrong.

Document frequencies (`count(*) … MATCH`) and `max(rowid)` are cached per
FTS table while its FTS5 structure record (`<table>_data` row 10, rewritten
by every write to the index, deletes included) is byte-identical
(`TestFTSStatsFollowWrites`).

## Also examined

- Per-type passes: events, facts and pages are separate FTS tables with
  separate BM25 scales (decision 0020); nothing is computed twice.
- Prepared statements: `database/sql` over modernc re-prepares each text,
  but preparation is microseconds against tens of milliseconds of scoring;
  not worth the API change.
- A common-term cutoff (0100, option 1) is no longer needed: the exact
  route meets the target, so the ranking is unchanged.

## Measurement

`make perf` (Apple M1, GOMAXPROCS=4, 100k records, the 0100 corpus and
queries), same machine and session; "before" is the read path of this
branch with the full query forced (it includes decisions 0120 and 0121).

| | before | after |
|---|---|---|
| `search` p50 / p95 | 105 / 174 ms | **49 / 121 ms** |
| `context` p50 / p95 | 113 / 191 ms | **56 / 128 ms** |
| "who is the owner of entity …" p50 | 188 ms | 79 ms |
| "what is the status of entity … project" p50 | 188 ms | 55 ms |
| "customer invoice policy for …" p50 | 133 ms | 55 ms |
| "deploy cluster backup" (all common) p50 | 127 ms | 127 ms |
| `remember` p50 / p95 | 264 / 384 ms | 264 / 365 ms |

**The 150 ms p95 target is now met** for both modes on the stress corpus.
The slowest queries left are those made only of common words (no rare term
to anchor the bound), now the p95.

The evaluation report is unchanged by this decision except for
floating-point summation order: recall, MRR, nDCG, answer in context and
every suite are identical with and without it on synthetic-pt.
