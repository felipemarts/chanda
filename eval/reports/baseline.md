# Chanda evaluation report: baseline

| Field | Value |
| --- | --- |
| Date | 2026-11-14 |
| Chanda version | dev |
| Configuration hash | `886e3e3050bef82f` |
| Embedding model_id | `eval-localhash-v1-256` |
| Reader model | n/a (retrieval-only; no LLM reader or judge) |
| Hardware | darwin/arm64, 8 CPUs, Apple M1 |
| Go | go1.26.2 |
| Dataset | synthetic-pt, hash `5c3622e93c017a5c`: 45 items (23 documents in 32 versions, 13 sessions), 147 questions |
| Context budget | 800 tokens per query, same for every system (estimator: ceil(characters / 4)) |
| Report schema | chanda-eval-report/v1 |

Questions by tag: abstention 20, dedup 9, injection 5, plain 73, session 12, temporal 16, update 20.

## Systems

| System | Embedding | Description |
| --- | --- | --- |
| B0 | n/a | No memory: always returns an empty context. |
| B1 | n/a | BM25 (SQLite FTS5, unicode61 remove_diacritics 2) over raw chunks of every item and version. |
| B2 | eval-localhash-v1-256 | Plain RAG: raw chunks of every item and version, exact cosine top-k. |
| B3 | n/a | Full context: all 181 raw chunks (4142 tokens) when they fit the budget, else the most recent chunks that fit; truncated in 147 of 147 queries. |

## Overall, side by side

Retrieval metrics are means over answerable questions; the best value of each row is in bold. "Forbidden in context" counts questions whose context contains a superseded value or an injected instruction (lower is better).

| Metric | B0 | B1 | B2 | B3 |
| --- | --- | --- | --- | --- |
| recall@5 | 0 | **0.9764** | 0.9528 | 0.0157 |
| recall@10 | 0 | **0.9921** | 0.9685 | 0.0394 |
| MRR | 0 | **0.8475** | 0.8046 | 0.0109 |
| nDCG@10 | 0 | **0.8838** | 0.8449 | 0.0144 |
| doc recall@10 | 0 | **1** | 0.9921 | 0.1496 |
| answer in context | 0 | **0.9921** | 0.9843 | 0.1811 |
| forbidden in context | **0** | 1 | 0.95 | 0.3 |
| abstention precision | 0.1361 | **1** | n/a | n/a |
| abstention recall | **1** | 0.15 | 0 | 0 |
| empty results | 1 | 0.0204 | 0 | 0 |
| context tokens per query (mean) | 0 | 559.7823 | 796.3265 | 799 |
| context tokens per query (p50 / p95) | 0 / 0 | 766 / 796 | 798 / 800 | 799 / 799 |
| budget violations | 0 | 0 | 0 | 0 |
| index size (bytes) | n/a | 65536 | 201938 | n/a |

## By tag: recall@10

| Tag (questions) | B0 | B1 | B2 | B3 |
| --- | --- | --- | --- | --- |
| abstention (20) | n/a | n/a | n/a | n/a |
| dedup (9) | 0 | 1 | 0.8889 | 0.5556 |
| injection (5) | 0 | 1 | 1 | 0 |
| plain (73) | 0 | 0.9863 | 0.9863 | 0 |
| session (12) | 0 | 1 | 1 | 0 |
| temporal (16) | 0 | 1 | 0.875 | 0.0625 |
| update (20) | 0 | 1 | 1 | 0.2 |

## By tag: answer in context

| Tag (questions) | B0 | B1 | B2 | B3 |
| --- | --- | --- | --- | --- |
| abstention (20) | n/a | n/a | n/a | n/a |
| dedup (9) | 0 | 1 | 1 | 0.5556 |
| injection (5) | 0 | 1 | 1 | 0.2 |
| plain (73) | 0 | 0.9863 | 0.9863 | 0.0548 |
| session (12) | 0 | 1 | 1 | 0.5 |
| temporal (16) | 0 | 1 | 0.9375 | 0.0625 |
| update (20) | 0 | 1 | 1 | 0.7 |

## By tag: forbidden in context (lower is better)

| Tag (questions) | B0 | B1 | B2 | B3 |
| --- | --- | --- | --- | --- |
| abstention (20) | n/a | n/a | n/a | n/a |
| dedup (9) | 0 | 1 | 1 | 0.2 |
| injection (5) | 0 | 1 | 0.8 | 0.2 |
| plain (73) | n/a | n/a | n/a | n/a |
| session (12) | 0 | 1 | 1 | 0 |
| temporal (16) | 0 | 1 | 0.9375 | 0.625 |
| update (20) | 0 | 1 | 1 | 0.05 |

## By tag: empty results

| Tag (questions) | B0 | B1 | B2 | B3 |
| --- | --- | --- | --- | --- |
| abstention (20) | 1 | 0.15 | 0 | 0 |
| dedup (9) | 1 | 0 | 0 | 0 |
| injection (5) | 1 | 0 | 0 | 0 |
| plain (73) | 1 | 0 | 0 | 0 |
| session (12) | 1 | 0 | 0 | 0 |
| temporal (16) | 1 | 0 | 0 | 0 |
| update (20) | 1 | 0 | 0 | 0 |

## Where each system loses

Scopes and metrics where a system is worse than the best compared system.

- **B0** (14): all recall@10 0 vs 0.9921 (B1); all answer_in_context 0 vs 0.9921 (B1); dedup recall@10 0 vs 1 (B1); dedup answer_in_context 0 vs 1 (B1, B2); injection recall@10 0 vs 1 (B1, B2); injection answer_in_context 0 vs 1 (B1, B2); plain recall@10 0 vs 0.9863 (B1, B2); plain answer_in_context 0 vs 0.9863 (B1, B2); session recall@10 0 vs 1 (B1, B2); session answer_in_context 0 vs 1 (B1, B2); temporal recall@10 0 vs 1 (B1); temporal answer_in_context 0 vs 1 (B1); update recall@10 0 vs 1 (B1, B2); update answer_in_context 0 vs 1 (B1, B2).
- **B1** (8): all forbidden_rate 1 vs 0 (B0); all abstention_recall 0.15 vs 1 (B0); abstention abstention_recall 0.15 vs 1 (B0); dedup forbidden_rate 1 vs 0 (B0); injection forbidden_rate 1 vs 0 (B0); session forbidden_rate 1 vs 0 (B0, B3); temporal forbidden_rate 1 vs 0 (B0); update forbidden_rate 1 vs 0 (B0).
- **B2** (13): all recall@10 0.9685 vs 0.9921 (B1); all answer_in_context 0.9843 vs 0.9921 (B1); all forbidden_rate 0.95 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.8889 vs 1 (B1); dedup forbidden_rate 1 vs 0 (B0); injection forbidden_rate 0.8 vs 0 (B0); session forbidden_rate 1 vs 0 (B0, B3); temporal recall@10 0.875 vs 1 (B1); temporal answer_in_context 0.9375 vs 1 (B1); temporal forbidden_rate 0.9375 vs 0 (B0); update forbidden_rate 1 vs 0 (B0).
- **B3** (21): all recall@10 0.0394 vs 0.9921 (B1); all answer_in_context 0.1811 vs 0.9921 (B1); all forbidden_rate 0.3 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.5556 vs 1 (B1); dedup answer_in_context 0.5556 vs 1 (B1, B2); dedup forbidden_rate 0.2 vs 0 (B0); injection recall@10 0 vs 1 (B1, B2); injection answer_in_context 0.2 vs 1 (B1, B2); injection forbidden_rate 0.2 vs 0 (B0); plain recall@10 0 vs 0.9863 (B1, B2); plain answer_in_context 0.0548 vs 0.9863 (B1, B2); session recall@10 0 vs 1 (B1, B2); session answer_in_context 0.5 vs 1 (B1, B2); temporal recall@10 0.0625 vs 1 (B1); temporal answer_in_context 0.0625 vs 1 (B1); temporal forbidden_rate 0.625 vs 0 (B0); update recall@10 0.2 vs 1 (B1, B2); update answer_in_context 0.7 vs 1 (B1, B2); update forbidden_rate 0.05 vs 0 (B0).

## Own suites

Targets from BASE.md; the measured value is published even when the target is not met.

| Suite | System | Target | Measured | Result |
| --- | --- | --- | --- | --- |
| abstention | B0 | 95% precision | empty 147, precision 0.1361, recall 1, unanswerable 20 | fail: precision = 0.1361, want >= 0.95 |
| abstention | B1 | 95% precision | empty 3, precision 1, recall 0.15, unanswerable 20 | pass |
| abstention | B2 | 95% precision | empty 0, recall 0, unanswerable 20 | fail: precision undefined |
| abstention | B3 | 95% precision | empty 0, recall 0, unanswerable 20 | fail: precision undefined |
| temporal | - | Up to 1% |  | not_implemented |
| deletion | - | 100% |  | not_implemented |
| injection | - | 0% |  | not_implemented |
| document_update | - | 0% |  | not_implemented |
| deduplication | - | 0% and up to 5% |  | not_implemented |
| learning | - | Positive, with no regression at the gate |  | not_implemented |
| determinism | - | 100% |  | not_implemented |

## Latency (not deterministic)

Measured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.

| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Retrieve mean | Retrieve p50 | Retrieve p95 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 45 | 0.013 | 0.000 | 0.001 | 0.000 | 0.000 | 0.000 | 0.001 |
| B1 | 45 | 6.440 | 0.099 | 0.463 | 0.000 | 0.392 | 0.373 | 0.683 |
| B2 | 45 | 1.962 | 0.026 | 0.091 | 0.000 | 0.074 | 0.073 | 0.078 |
| B3 | 45 | 0.491 | 0.008 | 0.021 | 0.000 | 0.013 | 0.012 | 0.015 |
