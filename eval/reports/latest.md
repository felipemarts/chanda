# Chanda evaluation report: latest

| Field | Value |
| --- | --- |
| Date | 2026-11-14 |
| Chanda version | 0cfb60f |
| Configuration hash | `410bb9f2d923a85c` |
| Embedding model_id | `hash-v1-256` |
| Reader model | n/a (retrieval-only; no LLM reader or judge) |
| Hardware | darwin/arm64, 8 CPUs, Apple M1 |
| Go | go1.26.2 |
| Dataset | synthetic-pt, hash `36db5d525454ae8f`: 197 items (101 documents in 133 versions, 64 sessions), 305 evaluated questions (+99 training and 33 gate questions for C5, never evaluated) |
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
| C1 | hash-v1-256 | Chanda: hybrid fusion (FTS5 BM25 + vectors, RRF), no reranker, no structured facts. Config hash `f4981ab9946154b5`. |
| C3 | hash-v1-256 | C2 + structured facts with temporal validity (header and kv_table extractors, assert_fact). Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. Config hash `5831d2d2595da8ea`. |
| C4 | hash-v1-256 | C3 + a sleep run after ingestion. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Sleep run: phases [1], status completed. Config hash `5831d2d2595da8ea`. |
| C5 | hash-v1-256 | C4 + learned ranking from a simulated task sequence. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Learning: the gate split is approved as the gate set, then 3 passes over the training split (outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none), then a sleep run trains and may promote a ranker. Sleep run: phases [1], status completed. Sleep run: phases [1], status completed. After 297 tasks (270 positive) and 33 gate questions, no ranker model was promoted (lambda 0; Ranker: trained mdl_01M5GF72NRSBMANBN256HRRDP4, evaluated mdl_01M5GF72NRSBMANBN256HRRDP4, passed false, promoted false, baseline 63.71, learned 64.63, lambda 0.00 (gate_failed)): ranking equals C4. Config hash `5831d2d2595da8ea`. |
| C2 | - | **Not available in this run**: C2 not run: no cross-encoder reranker is configured in this binary (the chanda binary has none; chanda-hugot loads one from embed.reranker_model_path), so the reranking stage cannot run |

## Overall, side by side

Retrieval metrics are means over answerable questions; the best value of each row is in bold. "Forbidden in context" counts questions whose context contains a superseded value or an injected instruction (lower is better).

| Metric | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| recall@5 | 0 | 0.5593 | 0.4407 | 0.0037 | 0.4852 | **0.5963** | **0.5963** | **0.5963** |
| recall@10 | 0 | 0.6185 | 0.5074 | 0.0074 | 0.5074 | **0.6259** | **0.6259** | **0.6259** |
| MRR | 0 | 0.4477 | 0.3238 | 0.0025 | 0.3815 | **0.5105** | **0.5105** | **0.5105** |
| nDCG@10 | 0 | 0.4844 | 0.3637 | 0.0036 | 0.4126 | **0.5389** | **0.5389** | **0.5389** |
| doc recall@10 | 0 | 0.7481 | 0.6963 | 0.0111 | 0.7 | **0.7519** | **0.7519** | **0.7519** |
| answer in context | 0 | **0.7222** | 0.6519 | 0.0185 | 0.5333 | 0.6593 | 0.6593 | 0.6593 |
| forbidden in context | **0** | 0.8136 | 0.6864 | 0.0085 | 0.5678 | 0.0508 | 0.0508 | 0.0508 |
| abstention precision | 0.1148 | **0.125** | n/a | n/a | 0.0714 | 0.0357 | 0.0357 | 0.0357 |
| abstention recall | **1** | 0.0286 | 0 | 0 | 0.0571 | 0.0286 | 0.0286 | 0.0286 |
| empty results | 1 | 0.0262 | 0 | 0 | 0.0918 | 0.0918 | 0.0918 | 0.0918 |
| context tokens per query (mean) | 0 | 620.5049 | 789.3475 | 798 | 179.0459 | 162.3115 | 162.3115 | 162.3115 |
| context tokens per query (p50 / p95) | 0 / 0 | 785 / 799 | 795 / 800 | 798 / 798 | 209 / 318 | 192 / 287 | 192 / 287 | 192 / 287 |
| budget violations | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| index size (bytes) | n/a | 192512 | 1101177 | n/a | 12783616 | 15998976 | 16003072 | 29249536 |

## By tag: recall@10

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.4348 | 0.5652 | 0 | 0.5652 | 0.6522 | 0.6522 | 0.6522 |
| injection (7) | 0 | 0.4286 | 0.2857 | 0 | 0.2857 | 0.8571 | 0.8571 | 0.8571 |
| paraphrase (66) | 0 | 0.1212 | 0.0606 | 0 | 0.0152 | 0.0152 | 0.0152 | 0.0152 |
| plain (86) | 0 | 0.8953 | 0.814 | 0 | 0.8953 | 0.8256 | 0.8256 | 0.8256 |
| session (30) | 0 | 1 | 1 | 0.0667 | 0.9 | 0.9333 | 0.9333 | 0.9333 |
| temporal (53) | 0 | 0.6226 | 0.4151 | 0 | 0.2264 | 0.7736 | 0.7736 | 0.7736 |
| update (64) | 0 | 0.5938 | 0.4844 | 0.0313 | 0.5469 | 0.6406 | 0.6406 | 0.6406 |

## By tag: answer in context

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.8261 | 0.7391 | 0 | 0.5652 | 0.6522 | 0.6522 | 0.6522 |
| injection (7) | 0 | 0.8571 | 0.4286 | 0 | 0.4286 | 0.8571 | 0.8571 | 0.8571 |
| paraphrase (66) | 0 | 0.2424 | 0.197 | 0.0303 | 0.0152 | 0.0152 | 0.0152 | 0.0152 |
| plain (86) | 0 | 0.9535 | 0.9419 | 0 | 0.9302 | 0.8605 | 0.8605 | 0.8605 |
| session (30) | 0 | 1 | 1 | 0.1 | 0.9333 | 0.9333 | 0.9333 | 0.9333 |
| temporal (53) | 0 | 0.7925 | 0.6038 | 0 | 0.3019 | 0.8491 | 0.8491 | 0.8491 |
| update (64) | 0 | 0.7344 | 0.625 | 0.0469 | 0.5313 | 0.6719 | 0.6719 | 0.6719 |

## By tag: forbidden in context (lower is better)

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.6818 | 0.5455 | 0 | 0.4545 | 0 | 0 | 0 |
| injection (7) | 0 | 0.8571 | 0.8571 | 0 | 0.7143 | 0.7143 | 0.7143 | 0.7143 |
| paraphrase (66) | 0 | 0.5625 | 0.1875 | 0 | 0.0625 | 0 | 0 | 0 |
| plain (86) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| session (30) | 0 | 1 | 1 | 0.05 | 1 | 0 | 0 | 0 |
| temporal (53) | 0 | 0.8679 | 0.7547 | 0.0189 | 0.7925 | 0 | 0 | 0 |
| update (64) | 0 | 0.7627 | 0.6102 | 0 | 0.3559 | 0.0339 | 0.0339 | 0.0339 |

## By tag: empty results

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | 1 | 0.0286 | 0 | 0 | 0.0571 | 0.0286 | 0.0286 | 0.0286 |
| dedup (23) | 1 | 0 | 0 | 0 | 0 | 0.0435 | 0.0435 | 0.0435 |
| injection (7) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| paraphrase (66) | 1 | 0.1061 | 0 | 0 | 0.3939 | 0.4091 | 0.4091 | 0.4091 |
| plain (86) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| session (30) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| temporal (53) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| update (64) | 1 | 0 | 0 | 0 | 0.0156 | 0.0313 | 0.0313 | 0.0313 |

## Where each system loses

Scopes and metrics where a system is worse than the best compared system.

- **B0** (16): all recall@10 0 vs 0.6259 (C3, C4, C5); all answer_in_context 0 vs 0.7222 (B1); dedup recall@10 0 vs 0.6522 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 0.8571 (C3, C4, C5); injection answer_in_context 0 vs 0.8571 (B1, C3, C4, C5); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); plain recall@10 0 vs 0.8953 (B1, C1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0 vs 1 (B1, B2); session answer_in_context 0 vs 1 (B1, B2); temporal recall@10 0 vs 0.7736 (C3, C4, C5); temporal answer_in_context 0 vs 0.8491 (C3, C4, C5); update recall@10 0 vs 0.6406 (C3, C4, C5); update answer_in_context 0 vs 0.7344 (B1).
- **B1** (15): all recall@10 0.6185 vs 0.6259 (C3, C4, C5); all forbidden_rate 0.8136 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup recall@10 0.4348 vs 0.6522 (C3, C4, C5); dedup forbidden_rate 0.6818 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.4286 vs 0.8571 (C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase forbidden_rate 0.5625 vs 0 (B0, B3, C3, C4, C5); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.6226 vs 0.7736 (C3, C4, C5); temporal answer_in_context 0.7925 vs 0.8491 (C3, C4, C5); temporal forbidden_rate 0.8679 vs 0 (B0, C3, C4, C5); update recall@10 0.5938 vs 0.6406 (C3, C4, C5); update forbidden_rate 0.7627 vs 0 (B0, B3).
- **B2** (23): all recall@10 0.5074 vs 0.6259 (C3, C4, C5); all answer_in_context 0.6519 vs 0.7222 (B1); all forbidden_rate 0.6864 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.5652 vs 0.6522 (C3, C4, C5); dedup answer_in_context 0.7391 vs 0.8261 (B1); dedup forbidden_rate 0.5455 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.2857 vs 0.8571 (C3, C4, C5); injection answer_in_context 0.4286 vs 0.8571 (B1, C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase recall@10 0.0606 vs 0.1212 (B1); paraphrase answer_in_context 0.197 vs 0.2424 (B1); paraphrase forbidden_rate 0.1875 vs 0 (B0, B3, C3, C4, C5); plain recall@10 0.814 vs 0.8953 (B1, C1); plain answer_in_context 0.9419 vs 0.9535 (B1); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.4151 vs 0.7736 (C3, C4, C5); temporal answer_in_context 0.6038 vs 0.8491 (C3, C4, C5); temporal forbidden_rate 0.7547 vs 0 (B0, C3, C4, C5); update recall@10 0.4844 vs 0.6406 (C3, C4, C5); update answer_in_context 0.625 vs 0.7344 (B1); update forbidden_rate 0.6102 vs 0 (B0, B3).
- **B3** (21): all recall@10 0.0074 vs 0.6259 (C3, C4, C5); all answer_in_context 0.0185 vs 0.7222 (B1); all forbidden_rate 0.0085 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0 vs 0.6522 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 0.8571 (C3, C4, C5); injection answer_in_context 0 vs 0.8571 (B1, C3, C4, C5); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0.0303 vs 0.2424 (B1); plain recall@10 0 vs 0.8953 (B1, C1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0.0667 vs 1 (B1, B2); session answer_in_context 0.1 vs 1 (B1, B2); session forbidden_rate 0.05 vs 0 (B0, C3, C4, C5); temporal recall@10 0 vs 0.7736 (C3, C4, C5); temporal answer_in_context 0 vs 0.8491 (C3, C4, C5); temporal forbidden_rate 0.0189 vs 0 (B0, C3, C4, C5); update recall@10 0.0313 vs 0.6406 (C3, C4, C5); update answer_in_context 0.0469 vs 0.7344 (B1).
- **C1** (24): all recall@10 0.5074 vs 0.6259 (C3, C4, C5); all answer_in_context 0.5333 vs 0.7222 (B1); all forbidden_rate 0.5678 vs 0 (B0); all abstention_recall 0.0571 vs 1 (B0); abstention abstention_recall 0.0571 vs 1 (B0); dedup recall@10 0.5652 vs 0.6522 (C3, C4, C5); dedup answer_in_context 0.5652 vs 0.8261 (B1); dedup forbidden_rate 0.4545 vs 0 (B0, B3, C3, C4, C5); injection recall@10 0.2857 vs 0.8571 (C3, C4, C5); injection answer_in_context 0.4286 vs 0.8571 (B1, C3, C4, C5); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.0152 vs 0.1212 (B1); paraphrase answer_in_context 0.0152 vs 0.2424 (B1); paraphrase forbidden_rate 0.0625 vs 0 (B0, B3, C3, C4, C5); plain answer_in_context 0.9302 vs 0.9535 (B1); session recall@10 0.9 vs 1 (B1, B2); session answer_in_context 0.9333 vs 1 (B1, B2); session forbidden_rate 1 vs 0 (B0, C3, C4, C5); temporal recall@10 0.2264 vs 0.7736 (C3, C4, C5); temporal answer_in_context 0.3019 vs 0.8491 (C3, C4, C5); temporal forbidden_rate 0.7925 vs 0 (B0, C3, C4, C5); update recall@10 0.5469 vs 0.6406 (C3, C4, C5); update answer_in_context 0.5313 vs 0.7344 (B1); update forbidden_rate 0.3559 vs 0 (B0, B3).
- **C3** (14): all answer_in_context 0.6593 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup answer_in_context 0.6522 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.0152 vs 0.1212 (B1); paraphrase answer_in_context 0.0152 vs 0.2424 (B1); plain recall@10 0.8256 vs 0.8953 (B1, C1); plain answer_in_context 0.8605 vs 0.9535 (B1); session recall@10 0.9333 vs 1 (B1, B2); session answer_in_context 0.9333 vs 1 (B1, B2); update answer_in_context 0.6719 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C4** (14): all answer_in_context 0.6593 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup answer_in_context 0.6522 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.0152 vs 0.1212 (B1); paraphrase answer_in_context 0.0152 vs 0.2424 (B1); plain recall@10 0.8256 vs 0.8953 (B1, C1); plain answer_in_context 0.8605 vs 0.9535 (B1); session recall@10 0.9333 vs 1 (B1, B2); session answer_in_context 0.9333 vs 1 (B1, B2); update answer_in_context 0.6719 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).
- **C5** (14): all answer_in_context 0.6593 vs 0.7222 (B1); all forbidden_rate 0.0508 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup answer_in_context 0.6522 vs 0.8261 (B1); injection forbidden_rate 0.7143 vs 0 (B0, B3); paraphrase recall@10 0.0152 vs 0.1212 (B1); paraphrase answer_in_context 0.0152 vs 0.2424 (B1); plain recall@10 0.8256 vs 0.8953 (B1, C1); plain answer_in_context 0.8605 vs 0.9535 (B1); session recall@10 0.9333 vs 1 (B1, B2); session answer_in_context 0.9333 vs 1 (B1, B2); update answer_in_context 0.6719 vs 0.7344 (B1); update forbidden_rate 0.0339 vs 0 (B0, B3).

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
| temporal | C1 | Up to 1% | current_answer_rate 0.4505, questions 111, stale_answer_rate 0.5586, stale_answers 62 | fail: stale_answer_rate = 0.5586, want <= 0.01 |
| abstention | C1 | 95% precision | empty 28, precision 0.0714, recall 0.0571, unanswerable 35 | fail: precision = 0.0714, want >= 0.95 |
| temporal | C3 | Up to 1% | current_answer_rate 0.7387, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C3 | 95% precision | empty 28, precision 0.0357, recall 0.0286, unanswerable 35 | fail: precision = 0.0357, want >= 0.95 |
| temporal | C4 | Up to 1% | current_answer_rate 0.7387, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C4 | 95% precision | empty 28, precision 0.0357, recall 0.0286, unanswerable 35 | fail: precision = 0.0357, want >= 0.95 |
| temporal | C5 | Up to 1% | current_answer_rate 0.7387, questions 111, stale_answer_rate 0.009, stale_answers 1 | pass |
| abstention | C5 | 95% precision | empty 28, precision 0.0357, recall 0.0286, unanswerable 35 | fail: precision = 0.0357, want >= 0.95 |
| deletion | - | 100% | core_probe_failures 0, forget_pass_rate 1, independent_probe_failures 0, passed 14, requests 14, requests_date_range 1, requests_document 3, requests_entity 2, requests_record 1, requests_search 5, requests_source 2 | pass |
| injection | - | 0% | attempts 28, context_guidance 0, context_queries 9, external_events 14, external_events_not_external 0, facts_from_external 2, facts_from_external_not_external 0, forged_assertions 3, forged_assertions_accepted 0, injection_success_rate 0, successes 0 | pass |
| document_update | - | 0% | facts_from_retired_chunks 136, facts_stale_source 21, stale_fact_active_rate 0, stale_facts_active 0, update_questions 58, update_questions_with_stale_fact_in_context 0 | pass |
| deduplication | - | 0% and up to 5% | base_paragraphs 40, duplicates 80, duplicates_missed 3, duplicates_missed_rate 0.0375, updates 40, updates_discarded 0, updates_discarded_rate 0 | pass |
| learning | - | Positive, with no regression at the gate | gain_c5_over_c4 0, gate_c4 58.6885, gate_c5 58.6885, gate_regression 0, ndcg10_c4 0.5389, ndcg10_c5 0.5389, train_questions 99 | fail: gain_c5_over_c4 = 0, want > 0 |
| determinism | - | 100% | identical 2, identical_rate 1, pairs 2 | pass |

## Latency (not deterministic)

Measured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.

| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Train | Retrieve mean | Retrieve p50 | Retrieve p95 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 197 | 0.056 | 0.000 | 0.001 | 0.000 | 0.000 | 0.000 | 0.000 | 0.001 |
| B1 | 197 | 32.861 | 0.115 | 0.485 | 0.000 | 0.000 | 0.955 | 0.803 | 1.741 |
| B2 | 197 | 86.163 | 0.327 | 1.322 | 0.000 | 0.000 | 1.013 | 0.963 | 1.155 |
| B3 | 197 | 5.625 | 0.021 | 0.084 | 0.000 | 0.000 | 0.128 | 0.121 | 0.156 |
| C1 | 197 | 4984.752 | 19.599 | 49.970 | 222.132 | 0.031 | 6.498 | 6.022 | 11.028 |
| C3 | 197 | 5579.368 | 21.449 | 54.811 | 540.974 | 0.045 | 13.686 | 13.141 | 23.497 |
| C4 | 197 | 5537.312 | 21.502 | 56.576 | 414.324 | 0.021 | 12.220 | 11.823 | 19.764 |
| C5 | 197 | 5497.915 | 21.768 | 62.310 | 412.123 | 9680.605 | 12.962 | 12.848 | 20.544 |
