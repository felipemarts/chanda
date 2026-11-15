# Chanda evaluation report: latest

| Field | Value |
| --- | --- |
| Date | 2026-11-15 |
| Chanda version | eval-lme-freeze-17-g5579bf1 |
| Configuration hash | `3cc17014d76d162e` |
| Embedding model_id | `hash-v1-256` |
| Reader model | n/a (retrieval-only; no LLM reader or judge) |
| Hardware | darwin/arm64, 8 CPUs, Apple M1 |
| Go | go1.26.2 |
| Dataset | synthetic-pt, hash `025a822399d1b1e6`: 197 items (101 documents in 133 versions, 64 sessions), 305 evaluated questions (+149 training and 33 gate questions for C5, never evaluated) |
| Context budget | 800 tokens per query, same for every system (estimator: ceil(characters / 4)) |
| Report schema | chanda-eval-report/v1 |

Questions by tag: abstention 35, dedup 23, injection 7, paraphrase 66, plain 86, session 30, temporal 53, update 64.

## Systems

| System | Embedding | Description |
| --- | --- | --- |
| B0 | n/a | No memory: always returns an empty context. |
| B1 | n/a | BM25 (SQLite FTS5, unicode61 remove_diacritics 2) over raw chunks of every item and version. |
| B2 | hash-v1-256 | Plain RAG: raw chunks of every item and version, exact cosine top-k. |
| B3 | n/a | Full context: all 1006 raw chunks (17627 tokens) when they fit the budget, else the most recent chunks that fit; truncated in 305 of 305 queries. |
| C1 | hash-v1-256 | Chanda: hybrid fusion (FTS5 BM25 + vectors, RRF), no reranker, no structured facts. Config hash `6df740d09c43d18a`. |
| C3 | hash-v1-256 | C2 + structured facts with temporal validity (header and kv_table extractors, assert_fact). Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. Config hash `e8bd9be926a0573d`. |
| C4 | hash-v1-256 | C3 + a sleep run after ingestion. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Sleep run: phases [1], status completed. Config hash `e8bd9be926a0573d`. |
| C5 | hash-v1-256 | C4 + learned ranking from a simulated task sequence. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Learning: the gate split is approved as the gate set, then 3 passes over the training split (outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none), then a sleep run trains and may promote a ranker. Sleep run: phases [1], status completed. Sleep run: phases [1], status completed. After 447 tasks (270 positive) and 33 gate questions: active model mdl_01M5GFG8GWYGPSSN6JWCCKYH8A, lambda 0.05 (Ranker: trained mdl_01M5GFG8GWYGPSSN6JWCCKYH8A, evaluated mdl_01M5GFG8GWYGPSSN6JWCCKYH8A, passed true, promoted true, baseline 62.67, learned 63.70, lambda 0.05). Config hash `e8bd9be926a0573d`. |
| C2 | - | **Not available in this run**: C2 not run: no cross-encoder reranker is configured in this binary (the chanda binary has none; chanda-hugot loads one from embed.reranker_model_path), so the reranking stage cannot run |

## Overall, side by side

Retrieval metrics are means over answerable questions; the best value of each row is in bold. "Forbidden in context" counts questions whose context contains a superseded value or an injected instruction (lower is better).

| Metric | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| recall@5 | 0 | 0.5593 | 0.4407 | 0.0037 | 0.5074 | **0.6185** | **0.6185** | 0.6148 |
| recall@10 | 0 | 0.6185 | 0.5074 | 0.0074 | 0.5556 | **0.6778** | **0.6778** | 0.6704 |
| MRR | 0 | 0.4477 | 0.3238 | 0.0025 | 0.4036 | 0.522 | 0.522 | **0.5289** |
| nDCG@10 | 0 | 0.4844 | 0.3637 | 0.0036 | 0.439 | 0.5591 | 0.5591 | **0.5622** |
| doc recall@10 | 0 | 0.7481 | 0.6963 | 0.0111 | 0.7519 | 0.8259 | 0.8259 | **0.8296** |
| answer in context | 0 | **0.7222** | 0.6519 | 0.0185 | 0.5926 | 0.7074 | 0.7074 | 0.7037 |
| forbidden in context | **0** | 0.8136 | 0.6864 | 0.0085 | 0.2458 | 0.0508 | 0.0508 | 0.0508 |
| abstention precision | 0.1148 | **0.125** | n/a | n/a | 0 | 0 | 0 | 0 |
| abstention recall | **1** | 0.0286 | 0 | 0 | 0 | 0 | 0 | 0 |
| empty results | 1 | 0.0262 | 0 | 0 | 0.0098 | 0.0033 | 0.0033 | 0.0033 |
| context tokens per query (mean) | 0 | 620.5049 | 789.3475 | 798 | 248.6 | 218.8197 | 218.8197 | 219.4197 |
| context tokens per query (p50 / p95) | 0 / 0 | 785 / 799 | 795 / 800 | 798 / 798 | 256 / 297 | 225 / 284 | 225 / 284 | 225 / 284 |
| budget violations | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| index size (bytes) | n/a | 192512 | 1101177 | n/a | 12570624 | 15831040 | 15835136 | 34136064 |

## By tag: recall@10

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.4348 | 0.5652 | 0 | 0.6522 | 0.7391 | 0.7391 | 0.7391 |
| injection (7) | 0 | 0.4286 | 0.2857 | 0 | 0.4286 | 1 | 1 | 1 |
| paraphrase (66) | 0 | 0.1212 | 0.0606 | 0 | 0.1364 | 0.1061 | 0.1061 | 0.1061 |
| plain (86) | 0 | 0.8953 | 0.814 | 0 | 0.907 | 0.8372 | 0.8372 | 0.814 |
| session (30) | 0 | 1 | 1 | 0.0667 | 1 | 1 | 1 | 1 |
| temporal (53) | 0 | 0.6226 | 0.4151 | 0 | 0.2642 | 0.8491 | 0.8491 | 0.8491 |
| update (64) | 0 | 0.5938 | 0.4844 | 0.0313 | 0.6094 | 0.7031 | 0.7031 | 0.7031 |

## By tag: answer in context

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.8261 | 0.7391 | 0 | 0.6522 | 0.7391 | 0.7391 | 0.7391 |
| injection (7) | 0 | 0.8571 | 0.4286 | 0 | 0.7143 | 1 | 1 | 1 |
| paraphrase (66) | 0 | 0.2424 | 0.197 | 0.0303 | 0.1667 | 0.1364 | 0.1364 | 0.1364 |
| plain (86) | 0 | 0.9535 | 0.9419 | 0 | 0.9419 | 0.8605 | 0.8605 | 0.8488 |
| session (30) | 0 | 1 | 1 | 0.1 | 1 | 1 | 1 | 1 |
| temporal (53) | 0 | 0.7925 | 0.6038 | 0 | 0.3396 | 0.9057 | 0.9057 | 0.9057 |
| update (64) | 0 | 0.7344 | 0.625 | 0.0469 | 0.6094 | 0.7188 | 0.7188 | 0.7188 |

## By tag: forbidden in context (lower is better)

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.6818 | 0.5455 | 0 | 0.3636 | 0 | 0 | 0 |
| injection (7) | 0 | 0.8571 | 0.8571 | 0 | 0.7143 | 0.7143 | 0.7143 | 0.7143 |
| paraphrase (66) | 0 | 0.5625 | 0.1875 | 0 | 0.125 | 0 | 0 | 0 |
| plain (86) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| session (30) | 0 | 1 | 1 | 0.05 | 0.65 | 0 | 0 | 0 |
| temporal (53) | 0 | 0.8679 | 0.7547 | 0.0189 | 0.0566 | 0 | 0 | 0 |
| update (64) | 0 | 0.7627 | 0.6102 | 0 | 0.3729 | 0.0339 | 0.0339 | 0.0339 |

## By tag: empty results

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | 1 | 0.0286 | 0 | 0 | 0 | 0 | 0 | 0 |
| dedup (23) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| injection (7) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| paraphrase (66) | 1 | 0.1061 | 0 | 0 | 0.0455 | 0.0152 | 0.0152 | 0.0152 |
| plain (86) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| session (30) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| temporal (53) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| update (64) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

## Where each system loses

Scopes and metrics where a system is worse than the best compared system.

- **B0** (16): all recall@10 0 vs 0.6778 (C3, C4); all answer_in_context 0 vs 0.7222 (B1); dedup recall@10 0 vs 0.7391 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 1 (C3, C4, C5); injection answer_in_context 0 vs 1 (C3, C4, C5); paraphrase recall@10 0 vs 0.1364 (C1); paraphrase answer_in_context 0 vs 0.2424 (B1); plain recall@10 0 vs 0.907 (C1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0 vs 1 (B1, B2, C1, C3, C4, C5); session answer_in_context 0 vs 1 (B1, B2, C1, C3, C4, C5); temporal recall@10 0 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0 vs 0.9057 (C3, C4, C5); update recall@10 0 vs 0.7031 (C3, C4, C5); update answer_in_context 0 vs 0.7344 (B1).
- **B1** (18): all recall@10 0.6185 vs 0.6778 (C3, C4); all forbidden_rate 0.8136 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup recall@10 0.4348 vs 0.7391 (C3, C4, C5); dedup forbidden_rate 0.6818 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.4286 vs 1 (C3, C4, C5); injection answer_in_context 0.8571 vs 1 (C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase recall@10 0.1212 vs 0.1364 (C1); paraphrase forbidden_rate 0.5625 vs 0 (B0, B3, C3, C4, C5); plain recall@10 0.8953 vs 0.907 (C1); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.6226 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.7925 vs 0.9057 (C3, C4, C5); temporal forbidden_rate 0.8679 vs 0 (B0, C3, C4, C5); update recall@10 0.5938 vs 0.7031 (C3, C4, C5); update forbidden_rate 0.7627 vs 0 (B0, B3).
- **B2** (23): all recall@10 0.5074 vs 0.6778 (C3, C4); all answer_in_context 0.6519 vs 0.7222 (B1); all forbidden_rate 0.6864 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.5652 vs 0.7391 (C3, C4, C5); dedup answer_in_context 0.7391 vs 0.8261 (B1); dedup forbidden_rate 0.5455 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.2857 vs 1 (C3, C4, C5); injection answer_in_context 0.4286 vs 1 (C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase recall@10 0.0606 vs 0.1364 (C1); paraphrase answer_in_context 0.197 vs 0.2424 (B1); paraphrase forbidden_rate 0.1875 vs 0 (B0, B3, C3, C4, C5); plain recall@10 0.814 vs 0.907 (C1); plain answer_in_context 0.9419 vs 0.9535 (B1); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.4151 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.6038 vs 0.9057 (C3, C4, C5); temporal forbidden_rate 0.7547 vs 0 (B0, C3, C4, C5); update recall@10 0.4844 vs 0.7031 (C3, C4, C5); update answer_in_context 0.625 vs 0.7344 (B1); update forbidden_rate 0.6102 vs 0 (B0, B3).
- **B3** (21): all recall@10 0.0074 vs 0.6778 (C3, C4); all answer_in_context 0.0185 vs 0.7222 (B1); all forbidden_rate 0.0085 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0 vs 0.7391 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 1 (C3, C4, C5); injection answer_in_context 0 vs 1 (C3, C4, C5); paraphrase recall@10 0 vs 0.1364 (C1); paraphrase answer_in_context 0.0303 vs 0.2424 (B1); plain recall@10 0 vs 0.907 (C1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0.0667 vs 1 (B1, B2, C1, C3, C4, C5); session answer_in_context 0.1 vs 1 (B1, B2, C1, C3, C4, C5); session forbidden_rate 0.05 vs 0 (B0, C3, C4, C5); temporal recall@10 0 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0 vs 0.9057 (C3, C4, C5); temporal forbidden_rate 0.0189 vs 0 (B0, C3, C4, C5); update recall@10 0.0313 vs 0.7031 (C3, C4, C5); update answer_in_context 0.0469 vs 0.7344 (B1).
- **C1** (21): all recall@10 0.5556 vs 0.6778 (C3, C4); all answer_in_context 0.5926 vs 0.7222 (B1); all forbidden_rate 0.2458 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.6522 vs 0.7391 (C3, C4, C5); dedup answer_in_context 0.6522 vs 0.8261 (B1); dedup forbidden_rate 0.3636 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.4286 vs 1 (C3, C4, C5); injection answer_in_context 0.7143 vs 1 (C3, C4, C5); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase answer_in_context 0.1667 vs 0.2424 (B1); paraphrase forbidden_rate 0.125 vs 0 (B0, B3, C3, C4, C5); plain answer_in_context 0.9419 vs 0.9535 (B1); session forbidden_rate 0.65 vs 0 (B0, C3, C4, C5); temporal recall@10 0.2642 vs 0.8491 (C3, C4, C5); temporal answer_in_context 0.3396 vs 0.9057 (C3, C4, C5); temporal forbidden_rate 0.0566 vs 0 (B0, C3, C4, C5); update recall@10 0.6094 vs 0.7031 (C3, C4, C5); update answer_in_context 0.6094 vs 0.7344 (B1); update forbidden_rate 0.3729 vs 0 (B0, B3).
- **C3** (12): all answer_in_context 0.7074 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.7391 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.1061 vs 0.1364 (C1); paraphrase answer_in_context 0.1364 vs 0.2424 (B1); plain recall@10 0.8372 vs 0.907 (C1); plain answer_in_context 0.8605 vs 0.9535 (B1); update answer_in_context 0.7188 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C4** (12): all answer_in_context 0.7074 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.7391 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.1061 vs 0.1364 (C1); paraphrase answer_in_context 0.1364 vs 0.2424 (B1); plain recall@10 0.8372 vs 0.907 (C1); plain answer_in_context 0.8605 vs 0.9535 (B1); update answer_in_context 0.7188 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C5** (13): all recall@10 0.6704 vs 0.6778 (C3, C4); all answer_in_context 0.7037 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup answer_in_context 0.7391 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.1061 vs 0.1364 (C1); paraphrase answer_in_context 0.1364 vs 0.2424 (B1); plain recall@10 0.814 vs 0.907 (C1); plain answer_in_context 0.8488 vs 0.9535 (B1); update answer_in_context 0.7188 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).

## Own suites

Targets from BASE.md; the measured value is published even when the target is not met.

| Suite | System | Target | Measured | Result |
| --- | --- | --- | --- | --- |
| temporal | B0 | Up to 1% | current_answer_rate 0, questions 111, stale_answer_rate 0, stale_answers 0 | pass |
| abstention | B0 | 95% precision | empty 305, precision 0.1148, recall 1, unanswerable 35 | fail: precision = 0.1148, want >= 0.95 |
| temporal | B1 | Up to 1% | current_answer_rate 0.7838, questions 111, stale_answer_rate 0.8108, stale_answers 90 | fail: stale_answer_rate = 0.8108, want <= 0.01 |
| abstention | B1 | 95% precision | empty 8, precision 0.125, recall 0.0286, unanswerable 35 | fail: precision = 0.125, want >= 0.95 |
| temporal | B2 | Up to 1% | current_answer_rate 0.6486, questions 111, stale_answer_rate 0.6757, stale_answers 75 | fail: stale_answer_rate = 0.6757, want <= 0.01 |
| abstention | B2 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | B3 | Up to 1% | current_answer_rate 0.027, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | B3 | 95% precision | empty 0, recall 0, unanswerable 35 | fail: precision undefined |
| temporal | C1 | Up to 1% | current_answer_rate 0.5045, questions 111, stale_answer_rate 0.2162, stale_answers 24 | fail: stale_answer_rate = 0.2162, want <= 0.01 |
| abstention | C1 | 95% precision | empty 3, precision 0, recall 0, unanswerable 35 | fail: precision = 0, want >= 0.95 |
| temporal | C3 | Up to 1% | current_answer_rate 0.7928, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C3 | 95% precision | empty 1, precision 0, recall 0, unanswerable 35 | fail: precision = 0, want >= 0.95 |
| temporal | C4 | Up to 1% | current_answer_rate 0.7928, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C4 | 95% precision | empty 1, precision 0, recall 0, unanswerable 35 | fail: precision = 0, want >= 0.95 |
| temporal | C5 | Up to 1% | current_answer_rate 0.7928, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C5 | 95% precision | empty 1, precision 0, recall 0, unanswerable 35 | fail: precision = 0, want >= 0.95 |
| deletion | - | 100% | core_probe_failures 0, forget_pass_rate 1, independent_probe_failures 0, passed 16, requests 16, requests_date_range 1, requests_document 3, requests_entity 2, requests_record 1, requests_search 7, requests_source 2 | pass |
| injection | - | 0% | attempts 28, context_guidance 0, context_queries 9, external_events 14, external_events_not_external 0, facts_from_external 2, facts_from_external_not_external 0, forged_assertions 3, forged_assertions_accepted 0, injection_success_rate 0, successes 0 | pass |
| document_update | - | 0% | facts_from_retired_chunks 136, facts_stale_source 21, stale_fact_active_rate 0, stale_facts_active 0, update_questions 58, update_questions_with_stale_fact_in_context 0 | pass |
| deduplication | - | 0% and up to 5% | base_paragraphs 40, duplicates 80, duplicates_missed 0, duplicates_missed_rate 0, updates 40, updates_discarded 0, updates_discarded_rate 0 | pass |
| learning | - | Positive, with no regression at the gate | gain_c5_over_c4 0.003, gate_c4 62.623, gate_c5 62.2951, gate_regression 0.3279, ndcg10_c4 0.5591, ndcg10_c5 0.5622, train_questions 149 | fail: gate_regression = 0.3279, want <= 0 |
| determinism | - | 100% | identical 2, identical_rate 1, pairs 2 | pass |

## Latency (not deterministic)

Measured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.

| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Train | Retrieve mean | Retrieve p50 | Retrieve p95 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 197 | 0.063 | 0.000 | 0.001 | 0.000 | 0.000 | 0.000 | 0.000 | 0.001 |
| B1 | 197 | 40.263 | 0.144 | 0.532 | 0.000 | 0.000 | 1.256 | 1.081 | 2.420 |
| B2 | 197 | 126.264 | 0.403 | 1.732 | 0.000 | 0.000 | 1.287 | 1.186 | 1.970 |
| B3 | 197 | 8.142 | 0.023 | 0.120 | 0.000 | 0.000 | 0.172 | 0.152 | 0.232 |
| C1 | 197 | 8916.669 | 34.694 | 91.000 | 302.331 | 0.035 | 13.945 | 11.333 | 28.726 |
| C3 | 197 | 8256.947 | 29.090 | 102.454 | 565.898 | 0.093 | 29.753 | 23.826 | 65.327 |
| C4 | 197 | 8479.044 | 32.073 | 117.189 | 492.369 | 0.032 | 31.859 | 27.601 | 58.765 |
| C5 | 197 | 12096.149 | 45.657 | 152.527 | 540.559 | 29611.056 | 40.022 | 34.963 | 80.485 |
