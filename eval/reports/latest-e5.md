# Chanda evaluation report: latest-e5

| Field | Value |
| --- | --- |
| Date | 2026-11-14 |
| Chanda version | v0.0.0-20261114045918-e42b77512857+dirty |
| Configuration hash | `890dd251e7510223` |
| Embedding model_id | `e5-small@hugot0.8.6+simd` |
| Reader model | n/a (retrieval-only; no LLM reader or judge) |
| Hardware | darwin/arm64, 8 CPUs, Apple M1 |
| Go | go1.27.0-X:simd |
| Dataset | synthetic-pt, hash `025a822399d1b1e6`: 197 items (101 documents in 133 versions, 64 sessions), 305 evaluated questions (+149 training and 33 gate questions for C5, never evaluated) |
| Context budget | 800 tokens per query, same for every system (estimator: ceil(characters / 4)) |
| Report schema | chanda-eval-report/v1 |

Questions by tag: abstention 35, dedup 23, injection 7, paraphrase 66, plain 86, session 30, temporal 53, update 64.

## Systems

| System | Embedding | Description |
| --- | --- | --- |
| B0 | n/a | No memory: always returns an empty context. |
| B1 | n/a | BM25 (SQLite FTS5, unicode61 remove_diacritics 2) over raw chunks of every item and version. |
| B2 | e5-small@hugot0.8.6+simd | Plain RAG: raw chunks of every item and version, exact cosine top-k. |
| B3 | n/a | Full context: all 1006 raw chunks (17627 tokens) when they fit the budget, else the most recent chunks that fit; truncated in 305 of 305 queries. |
| C1 | e5-small@hugot0.8.6+simd | Chanda: hybrid fusion (FTS5 BM25 + vectors, RRF), no reranker, no structured facts. Config hash `2ea4ac3d6c3602ec`. |
| C3 | e5-small@hugot0.8.6+simd | C2 + structured facts with temporal validity (header and kv_table extractors, assert_fact). Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. Config hash `f0c6106c44504094`. |
| C4 | e5-small@hugot0.8.6+simd | C3 + a sleep run after ingestion. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Sleep run: phases [1], status completed. Config hash `f0c6106c44504094`. |
| C5 | e5-small@hugot0.8.6+simd | C4 + learned ranking from a simulated task sequence. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Learning: the gate split is approved as the gate set, then 3 passes over the training split (outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none), then a sleep run trains and may promote a ranker. Sleep run: phases [1], status completed. Sleep run: phases [1], status completed. After 447 tasks (291 positive) and 33 gate questions, no ranker model was promoted (lambda 0; Ranker: trained mdl_01M5GFG8GWYGPSSN6JWCCKYH8A, evaluated mdl_01M5GFG8GWYGPSSN6JWCCKYH8A, passed false, promoted false, baseline 63.72, learned 63.23, lambda 0.00 (gate_failed)): ranking equals C4. Config hash `f0c6106c44504094`. |
| C2 | - | **Not available in this run**: C2 not run: no cross-encoder reranker is configured in this binary (the chanda binary has none; chanda-hugot loads one from embed.reranker_model_path), so the reranking stage cannot run |

## Overall, side by side

Retrieval metrics are means over answerable questions; the best value of each row is in bold. "Forbidden in context" counts questions whose context contains a superseded value or an injected instruction (lower is better).

| Metric | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| recall@5 | 0 | 0.5593 | 0.5111 | 0.0037 | 0.5667 | **0.6185** | **0.6185** | **0.6185** |
| recall@10 | 0 | 0.6185 | 0.563 | 0.0074 | 0.6296 | **0.6963** | **0.6963** | **0.6963** |
| MRR | 0 | 0.4477 | 0.3552 | 0.0025 | 0.4205 | **0.5258** | **0.5258** | **0.5258** |
| nDCG@10 | 0 | 0.4844 | 0.4002 | 0.0036 | 0.47 | **0.5629** | **0.5629** | **0.5629** |
| doc recall@10 | 0 | 0.7481 | 0.8037 | 0.0111 | 0.8778 | **0.9407** | **0.9407** | **0.9407** |
| answer in context | 0 | 0.7222 | 0.7556 | 0.0185 | 0.6667 | **0.8148** | **0.8148** | **0.8148** |
| forbidden in context | **0** | 0.8136 | 0.839 | 0.0085 | 0.6356 | 0.0508 | 0.0508 | 0.0508 |
| abstention precision | 0.1148 | **0.125** | n/a | n/a | n/a | n/a | n/a | n/a |
| abstention recall | **1** | 0.0286 | 0 | 0 | 0 | 0 | 0 | 0 |
| empty results | 1 | 0.0262 | 0 | 0 | 0 | 0 | 0 | 0 |
| context tokens per query (mean) | 0 | 620.5049 | 786.3508 | 798 | 296.577 | 244.0492 | 244.0492 | 244.0492 |
| context tokens per query (p50 / p95) | 0 / 0 | 785 / 799 | 795 / 800 | 798 / 798 | 302 / 335 | 247 / 306 | 247 / 306 | 247 / 306 |
| budget violations | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| index size (bytes) | n/a | 192512 | 1616249 | n/a | 13250560 | 16867328 | 16871424 | 34750464 |

## By tag: recall@10

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.4348 | 0.5217 | 0 | 0.6957 | 0.6957 | 0.6957 | 0.6957 |
| injection (7) | 0 | 0.4286 | 0.2857 | 0 | 0.4286 | 0.8571 | 0.8571 | 0.8571 |
| paraphrase (66) | 0 | 0.1212 | 0.2121 | 0 | 0.3182 | 0.2273 | 0.2273 | 0.2273 |
| plain (86) | 0 | 0.8953 | 0.9186 | 0 | 0.9302 | 0.7791 | 0.7791 | 0.7791 |
| session (30) | 0 | 1 | 0.9667 | 0.0667 | 0.9667 | 1 | 1 | 1 |
| temporal (53) | 0 | 0.6226 | 0.2453 | 0 | 0.3019 | 0.8491 | 0.8491 | 0.8491 |
| update (64) | 0 | 0.5938 | 0.5938 | 0.0313 | 0.7188 | 0.8125 | 0.8125 | 0.8125 |

## By tag: answer in context

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.8261 | 0.913 | 0 | 0.7826 | 0.6957 | 0.6957 | 0.6957 |
| injection (7) | 0 | 0.8571 | 0.5714 | 0 | 0.7143 | 1 | 1 | 1 |
| paraphrase (66) | 0 | 0.2424 | 0.5152 | 0.0303 | 0.3939 | 0.3939 | 0.3939 | 0.3939 |
| plain (86) | 0 | 0.9535 | 0.9651 | 0 | 0.9419 | 0.9535 | 0.9535 | 0.9535 |
| session (30) | 0 | 1 | 0.9667 | 0.1 | 1 | 1 | 1 | 1 |
| temporal (53) | 0 | 0.7925 | 0.6415 | 0 | 0.3774 | 0.9245 | 0.9245 | 0.9245 |
| update (64) | 0 | 0.7344 | 0.8125 | 0.0469 | 0.7344 | 0.8281 | 0.8281 | 0.8281 |

## By tag: forbidden in context (lower is better)

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.6818 | 0.7273 | 0 | 0.5455 | 0 | 0 | 0 |
| injection (7) | 0 | 0.8571 | 1 | 0 | 0.7143 | 0.7143 | 0.7143 | 0.7143 |
| paraphrase (66) | 0 | 0.5625 | 0.75 | 0 | 0.25 | 0 | 0 | 0 |
| plain (86) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| session (30) | 0 | 1 | 1 | 0.05 | 1 | 0 | 0 | 0 |
| temporal (53) | 0 | 0.8679 | 0.7925 | 0.0189 | 0.8868 | 0 | 0 | 0 |
| update (64) | 0 | 0.7627 | 0.8644 | 0 | 0.4068 | 0.0339 | 0.0339 | 0.0339 |

## By tag: empty results

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | 1 | 0.0286 | 0 | 0 | 0 | 0 | 0 | 0 |
| dedup (23) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| injection (7) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| paraphrase (66) | 1 | 0.1061 | 0 | 0 | 0 | 0 | 0 | 0 |
| plain (86) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| session (30) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| temporal (53) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| update (64) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

## Where each system loses

Scopes and metrics where a system is worse than the best compared system.

- **B0** (16): all recall@10 0 vs 0.6963 (C3, C4, C5); all answer_in_context 0 vs 0.8148 (C3, C4, C5); dedup recall@10 0 vs 0.6957 (C1, C3, C4, C5); dedup answer_in_context 0 vs 0.913 (B2); injection recall@10 0 vs 0.8571 (C3, C4, C5); injection answer_in_context 0 vs 1 (C3, C4, C5); paraphrase recall@10 0 vs 0.3182 (C1); paraphrase answer_in_context 0 vs 0.5152 (B2); plain recall@10 0 vs 0.9302 (C1); plain answer_in_context 0 vs 0.9651 (B2); session recall@10 0 vs 1 (B1, C3, C4, C5); session answer_in_context 0 vs 1 (B1, C1, C3, C4, C5); temporal recall@10 0 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0 vs 0.9245 (C3, C4, C5); update recall@10 0 vs 0.8125 (C3, C4, C5); update answer_in_context 0 vs 0.8281 (C3, C4, C5).
- **B1** (23): all recall@10 0.6185 vs 0.6963 (C3, C4, C5); all answer_in_context 0.7222 vs 0.8148 (C3, C4, C5); all forbidden_rate 0.8136 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup recall@10 0.4348 vs 0.6957 (C1, C3, C4, C5); dedup answer_in_context 0.8261 vs 0.913 (B2); dedup forbidden_rate 0.6818 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.4286 vs 0.8571 (C3, C4, C5); injection answer_in_context 0.8571 vs 1 (C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase recall@10 0.1212 vs 0.3182 (C1); paraphrase answer_in_context 0.2424 vs 0.5152 (B2); paraphrase forbidden_rate 0.5625 vs 0 (B0, B3, C3, C4, C5); plain recall@10 0.8953 vs 0.9302 (C1); plain answer_in_context 0.9535 vs 0.9651 (B2); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.6226 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.7925 vs 0.9245 (C3, C4, C5); temporal forbidden_rate 0.8679 vs 0 (B0, C3, C4, C5); update recall@10 0.5938 vs 0.8125 (C3, C4, C5); update answer_in_context 0.7344 vs 0.8281 (C3, C4, C5); update forbidden_rate 0.7627 vs 0 (B0, B3).
- **B2** (22): all recall@10 0.563 vs 0.6963 (C3, C4, C5); all answer_in_context 0.7556 vs 0.8148 (C3, C4, C5); all forbidden_rate 0.839 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.5217 vs 0.6957 (C1, C3, C4, C5); dedup forbidden_rate 0.7273 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.2857 vs 0.8571 (C3, C4, C5); injection answer_in_context 0.5714 vs 1 (C3, C4, C5); injection forbidden_rate 1 vs 0 (B0, B3); paraphrase recall@10 0.2121 vs 0.3182 (C1); paraphrase forbidden_rate 0.75 vs 0 (B0, B3, C3, C4, C5); plain recall@10 0.9186 vs 0.9302 (C1); session recall@10 0.9667 vs 1 (B1, C3, C4, C5); session answer_in_context 0.9667 vs 1 (B1, C1, C3, C4, C5); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.2453 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.6415 vs 0.9245 (C3, C4, C5); temporal forbidden_rate 0.7925 vs 0 (B0, C3, C4, C5); update recall@10 0.5938 vs 0.8125 (C3, C4, C5); update answer_in_context 0.8125 vs 0.8281 (C3, C4, C5); update forbidden_rate 0.8644 vs 0 (B0, B3).
- **B3** (21): all recall@10 0.0074 vs 0.6963 (C3, C4, C5); all answer_in_context 0.0185 vs 0.8148 (C3, C4, C5); all forbidden_rate 0.0085 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0 vs 0.6957 (C1, C3, C4, C5); dedup answer_in_context 0 vs 0.913 (B2); injection recall@10 0 vs 0.8571 (C3, C4, C5); injection answer_in_context 0 vs 1 (C3, C4, C5); paraphrase recall@10 0 vs 0.3182 (C1); paraphrase answer_in_context 0.0303 vs 0.5152 (B2); plain recall@10 0 vs 0.9302 (C1); plain answer_in_context 0 vs 0.9651 (B2); session recall@10 0.0667 vs 1 (B1, C3, C4, C5); session answer_in_context 0.1 vs 1 (B1, C1, C3, C4, C5); session forbidden_rate 0.05 vs 0 (B0, C3, C4, C5); temporal recall@10 0 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0 vs 0.9245 (C3, C4, C5); temporal forbidden_rate 0.0189 vs 0 (B0, C3, C4, C5); update recall@10 0.0313 vs 0.8125 (C3, C4, C5); update answer_in_context 0.0469 vs 0.8281 (C3, C4, C5).
- **C1** (21): all recall@10 0.6296 vs 0.6963 (C3, C4, C5); all answer_in_context 0.6667 vs 0.8148 (C3, C4, C5); all forbidden_rate 0.6356 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.7826 vs 0.913 (B2); dedup forbidden_rate 0.5455 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.4286 vs 0.8571 (C3, C4, C5); injection answer_in_context 0.7143 vs 1 (C3, C4, C5); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase answer_in_context 0.3939 vs 0.5152 (B2); paraphrase forbidden_rate 0.25 vs 0 (B0, B3, C3, C4, C5); plain answer_in_context 0.9419 vs 0.9651 (B2); session recall@10 0.9667 vs 1 (B1, C3, C4, C5); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.3019 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.3774 vs 0.9245 (C3, C4, C5); temporal forbidden_rate 0.8868 vs 0 (B0, C3, C4, C5); update recall@10 0.7188 vs 0.8125 (C3, C4, C5); update answer_in_context 0.7344 vs 0.8281 (C3, C4, C5); update forbidden_rate 0.4068 vs 0 (B0, B3).
- **C3** (10): all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.6957 vs 0.913 (B2); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.2273 vs 0.3182 (C1); paraphrase answer_in_context 0.3939 vs 0.5152 (B2); plain recall@10 0.7791 vs 0.9302 (C1); plain answer_in_context 0.9535 vs 0.9651 (B2); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C4** (10): all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.6957 vs 0.913 (B2); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.2273 vs 0.3182 (C1); paraphrase answer_in_context 0.3939 vs 0.5152 (B2); plain recall@10 0.7791 vs 0.9302 (C1); plain answer_in_context 0.9535 vs 0.9651 (B2); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C5** (10): all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.6957 vs 0.913 (B2); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.2273 vs 0.3182 (C1); paraphrase answer_in_context 0.3939 vs 0.5152 (B2); plain recall@10 0.7791 vs 0.9302 (C1); plain answer_in_context 0.9535 vs 0.9651 (B2); update forbidden_rate 0.0339 vs 0 (B0, B3).

## Own suites

Targets from BASE.md; the measured value is published even when the target is not met.

| Suite | System | Target | Measured | Result |
| --- | --- | --- | --- | --- |
| temporal | B0 | Up to 1% | current_answer_rate 0, questions 111, stale_answer_rate 0, stale_answers 0 | pass |
| abstention | B0 | 95% precision | empty 305, precision 0.1148, recall 1, unanswerable 35 | fail: precision = 0.1148, want >= 0.95 |
| temporal | B1 | Up to 1% | current_answer_rate 0.7838, questions 111, stale_answer_rate 0.8108, stale_answers 90 | fail: stale_answer_rate = 0.8108, want <= 0.01 |
| abstention | B1 | 95% precision | empty 8, precision 0.125, recall 0.0286, unanswerable 35 | fail: precision = 0.125, want >= 0.95 |
| temporal | B2 | Up to 1% | current_answer_rate 0.7658, questions 111, stale_answer_rate 0.8288, stale_answers 92 | fail: stale_answer_rate = 0.8288, want <= 0.01 |
| abstention | B2 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | B3 | Up to 1% | current_answer_rate 0.027, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | B3 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | C1 | Up to 1% | current_answer_rate 0.5946, questions 111, stale_answer_rate 0.6306, stale_answers 70 | fail: stale_answer_rate = 0.6306, want <= 0.01 |
| abstention | C1 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | C3 | Up to 1% | current_answer_rate 0.8649, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C3 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | C4 | Up to 1% | current_answer_rate 0.8649, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C4 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | C5 | Up to 1% | current_answer_rate 0.8649, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C5 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| deletion | - | 100% | core_probe_failures 0, forget_pass_rate 1, independent_probe_failures 0, passed 16, requests 16, requests_date_range 1, requests_document 3, requests_entity 2, requests_record 1, requests_search 7, requests_source 2 | pass |
| injection | - | 0% | attempts 28, context_guidance 0, context_queries 9, external_events 14, external_events_not_external 0, facts_from_external 2, facts_from_external_not_external 0, forged_assertions 3, forged_assertions_accepted 0, injection_success_rate 0, successes 0 | pass |
| document_update | - | 0% | facts_from_retired_chunks 136, facts_stale_source 21, stale_fact_active_rate 0, stale_facts_active 0, update_questions 58, update_questions_with_stale_fact_in_context 0 | pass |
| deduplication | - | 0% and up to 5% | base_paragraphs 40, duplicates 80, duplicates_missed 27, duplicates_missed_rate 0.3375, updates 40, updates_discarded 0, updates_discarded_rate 0 | fail: duplicates_missed_rate = 0.3375, want <= 0.05 |
| learning | - | Positive, with no regression at the gate | gain_c5_over_c4 0, gate_c4 72.1311, gate_c5 72.1311, gate_regression 0, ndcg10_c4 0.5629, ndcg10_c5 0.5629, train_questions 149 | fail: gain_c5_over_c4 = 0, want > 0 |
| determinism | - | 100% | identical 2, identical_rate 1, pairs 2 | pass |

## Latency (not deterministic)

Measured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.

| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Train | Retrieve mean | Retrieve p50 | Retrieve p95 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 197 | 0.024 | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 | 0.001 |
| B1 | 197 | 14.801 | 0.055 | 0.200 | 0.000 | 0.000 | 0.494 | 0.365 | 1.225 |
| B2 | 197 | 17174.262 | 69.825 | 252.492 | 0.000 | 0.000 | 15.068 | 13.952 | 22.861 |
| B3 | 197 | 3.285 | 0.011 | 0.047 | 0.000 | 0.000 | 0.063 | 0.059 | 0.072 |
| C1 | 197 | 210936.497 | 754.776 | 2706.571 | 12909.761 | 0.243 | 17.087 | 16.479 | 22.301 |
| C3 | 197 | 218554.571 | 695.863 | 3164.941 | 19093.530 | 0.479 | 23.569 | 22.734 | 31.950 |
| C4 | 197 | 213362.680 | 844.793 | 2047.739 | 21403.639 | 0.244 | 25.321 | 23.695 | 38.222 |
| C5 | 197 | 213626.536 | 828.269 | 2023.331 | 21478.575 | 16948.308 | 23.817 | 22.331 | 33.634 |
