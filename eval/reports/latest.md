# Chanda evaluation report: latest

| Field | Value |
| --- | --- |
| Date | 2026-11-13 |
| Chanda version | fa870da |
| Configuration hash | `0c1fd0d77f19b463` |
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
| C1 | hash-v1-256 | Chanda: hybrid fusion (FTS5 BM25 + vectors, RRF), no reranker, no structured facts. Config hash `2c66e45427dc2c3f`. |
| C3 | hash-v1-256 | C2 + structured facts with temporal validity (header and kv_table extractors, assert_fact). Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. Config hash `0b215b1f1b76e500`. |
| C4 | hash-v1-256 | C3 + a sleep run after ingestion. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Sleep run: phases [1], status completed. Config hash `0b215b1f1b76e500`. |
| C5 | hash-v1-256 | C4 + learned ranking from a simulated task sequence. Runs WITHOUT the reranker (none configured), so it is C1 + the stages above. No LLM provider is configured: sleep runs phase 1 (maintenance) only. Learning: the gate split is approved as the gate set, then 3 passes over the training split (outcome +1 with the delivered records that hit a gold span as used_ids, -1 with none), then a sleep run trains and may promote a ranker. Sleep run: phases [1], status completed. Sleep run: phases [1], status completed. After 297 tasks (270 positive) and 33 gate questions: active model mdl_01M5GF72NP440B6W0ZPN8PAY06, lambda 0.05 (Ranker: trained mdl_01M5GF72NP440B6W0ZPN8PAY06, evaluated mdl_01M5GF72NP440B6W0ZPN8PAY06, passed true, promoted true, baseline 70.05, learned 71.36, lambda 0.05). Config hash `0b215b1f1b76e500`. |
| C2 | - | **Not available in this run**: C2 not run: no cross-encoder reranker is configured in this binary (the chanda binary has none; chanda-hugot loads one from embed.reranker_model_path), so the reranking stage cannot run |

## Overall, side by side

Retrieval metrics are means over answerable questions; the best value of each row is in bold. "Forbidden in context" counts questions whose context contains a superseded value or an injected instruction (lower is better).

| Metric | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| recall@5 | 0 | 0.5593 | 0.4407 | 0.0037 | 0.4704 | **0.5963** | **0.5963** | 0.5852 |
| recall@10 | 0 | **0.6185** | 0.5074 | 0.0074 | 0.4963 | 0.6074 | 0.6074 | 0.6 |
| MRR | 0 | 0.4477 | 0.3238 | 0.0025 | 0.3763 | 0.5081 | 0.5081 | **0.5178** |
| nDCG@10 | 0 | 0.4844 | 0.3637 | 0.0036 | 0.4059 | 0.5329 | 0.5329 | **0.5381** |
| doc recall@10 | 0 | **0.7481** | 0.6963 | 0.0111 | 0.6593 | 0.7074 | 0.7074 | 0.7111 |
| answer in context | 0 | **0.7222** | 0.6519 | 0.0185 | 0.5148 | 0.637 | 0.637 | 0.6333 |
| forbidden in context | **0** | 0.8136 | 0.6864 | 0.0085 | 0.5254 | 0.3559 | 0.3559 | 0.3475 |
| abstention precision | 0.1148 | 0.125 | n/a | n/a | **0.1622** | **0.1622** | **0.1622** | **0.1622** |
| abstention recall | **1** | 0.0286 | 0 | 0 | 0.3429 | 0.3429 | 0.3429 | 0.3429 |
| empty results | 1 | 0.0262 | 0 | 0 | 0.2426 | 0.2426 | 0.2426 | 0.2426 |
| context tokens per query (mean) | 0 | 620.5049 | 789.3475 | 798 | 96.3705 | 95.3541 | 95.3541 | 95.8721 |
| context tokens per query (p50 / p95) | 0 / 0 | 785 / 799 | 795 / 800 | 798 / 798 | 70 / 289 | 70 / 272 | 70 / 272 | 72 / 272 |
| budget violations | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| index size (bytes) | n/a | 192512 | 1101177 | n/a | 12361728 | 15491072 | 15495168 | 28880896 |

## By tag: recall@10

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.4348 | 0.5652 | 0 | 0.5217 | 0.6087 | 0.6087 | 0.6087 |
| injection (7) | 0 | 0.4286 | 0.2857 | 0 | 0.1429 | 0.7143 | 0.7143 | 0.7143 |
| paraphrase (66) | 0 | 0.1212 | 0.0606 | 0 | 0 | 0 | 0 | 0 |
| plain (86) | 0 | 0.8953 | 0.814 | 0 | 0.8837 | 0.814 | 0.814 | 0.7907 |
| session (30) | 0 | 1 | 1 | 0.0667 | 0.8667 | 0.9 | 0.9 | 0.9 |
| temporal (53) | 0 | 0.6226 | 0.4151 | 0 | 0.2264 | 0.7358 | 0.7358 | 0.7358 |
| update (64) | 0 | 0.5938 | 0.4844 | 0.0313 | 0.5313 | 0.625 | 0.625 | 0.625 |

## By tag: answer in context

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.8261 | 0.7391 | 0 | 0.5217 | 0.6087 | 0.6087 | 0.6087 |
| injection (7) | 0 | 0.8571 | 0.4286 | 0 | 0.2857 | 0.7143 | 0.7143 | 0.7143 |
| paraphrase (66) | 0 | 0.2424 | 0.197 | 0.0303 | 0 | 0 | 0 | 0 |
| plain (86) | 0 | 0.9535 | 0.9419 | 0 | 0.907 | 0.8372 | 0.8372 | 0.8256 |
| session (30) | 0 | 1 | 1 | 0.1 | 0.9 | 0.9 | 0.9 | 0.9 |
| temporal (53) | 0 | 0.7925 | 0.6038 | 0 | 0.283 | 0.8113 | 0.8113 | 0.8113 |
| update (64) | 0 | 0.7344 | 0.625 | 0.0469 | 0.5156 | 0.6563 | 0.6563 | 0.6563 |

## By tag: forbidden in context (lower is better)

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| dedup (23) | 0 | 0.6818 | 0.5455 | 0 | 0.4545 | 0.4545 | 0.4545 | 0.4545 |
| injection (7) | 0 | 0.8571 | 0.8571 | 0 | 0.4286 | 0.4286 | 0.4286 | 0.4286 |
| paraphrase (66) | 0 | 0.5625 | 0.1875 | 0 | 0.0625 | 0.0625 | 0.0625 | 0.0625 |
| plain (86) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| session (30) | 0 | 1 | 1 | 0.05 | 0.85 | 0.3 | 0.3 | 0.3 |
| temporal (53) | 0 | 0.8679 | 0.7547 | 0.0189 | 0.7736 | 0.5283 | 0.5283 | 0.5094 |
| update (64) | 0 | 0.7627 | 0.6102 | 0 | 0.322 | 0.2034 | 0.2034 | 0.2034 |

## By tag: empty results

| Tag (questions) | B0 | B1 | B2 | B3 | C1 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| abstention (35) | 1 | 0.0286 | 0 | 0 | 0.3429 | 0.3429 | 0.3429 | 0.3429 |
| dedup (23) | 1 | 0 | 0 | 0 | 0.2174 | 0.2174 | 0.2174 | 0.2174 |
| injection (7) | 1 | 0 | 0 | 0 | 0.1429 | 0.1429 | 0.1429 | 0.1429 |
| paraphrase (66) | 1 | 0.1061 | 0 | 0 | 0.8333 | 0.8333 | 0.8333 | 0.8333 |
| plain (86) | 1 | 0 | 0 | 0 | 0.0233 | 0.0233 | 0.0233 | 0.0233 |
| session (30) | 1 | 0 | 0 | 0 | 0.1 | 0.1 | 0.1 | 0.1 |
| temporal (53) | 1 | 0 | 0 | 0 | 0.0377 | 0.0377 | 0.0377 | 0.0377 |
| update (64) | 1 | 0 | 0 | 0 | 0.1719 | 0.1719 | 0.1719 | 0.1719 |

## Where each system loses

Scopes and metrics where a system is worse than the best compared system.

- **B0** (16): all recall@10 0 vs 0.6185 (B1); all answer_in_context 0 vs 0.7222 (B1); dedup recall@10 0 vs 0.6087 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 0.7143 (C3, C4, C5); injection answer_in_context 0 vs 0.8571 (B1); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); plain recall@10 0 vs 0.8953 (B1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0 vs 1 (B1, B2); session answer_in_context 0 vs 1 (B1, B2); temporal recall@10 0 vs 0.7358 (C3, C4, C5); temporal answer_in_context 0 vs 0.8113 (C3, C4, C5); update recall@10 0 vs 0.625 (C3, C4, C5); update answer_in_context 0 vs 0.7344 (B1).
- **B1** (14): all forbidden_rate 0.8136 vs 0 (B0); all abstention_recall 0.0286 vs 1 (B0); abstention abstention_recall 0.0286 vs 1 (B0); dedup recall@10 0.4348 vs 0.6087 (C3, C4, C5); dedup forbidden_rate 0.6818 vs 0 (B0, B3); injection recall@10 0.4286 vs 0.7143 (C3, C4, C5); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase forbidden_rate 0.5625 vs 0 (B0, B3); session forbidden_rate 1 vs 0 (B0); temporal recall@10 0.6226 vs 0.7358 (C3, C4, C5); temporal answer_in_context 0.7925 vs 0.8113 (C3, C4, C5); temporal forbidden_rate 0.8679 vs 0 (B0); update recall@10 0.5938 vs 0.625 (C3, C4, C5); update forbidden_rate 0.7627 vs 0 (B0, B3).
- **B2** (23): all recall@10 0.5074 vs 0.6185 (B1); all answer_in_context 0.6519 vs 0.7222 (B1); all forbidden_rate 0.6864 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0.5652 vs 0.6087 (C3, C4, C5); dedup answer_in_context 0.7391 vs 0.8261 (B1); dedup forbidden_rate 0.5455 vs 0 (B0, B3); injection recall@10 0.2857 vs 0.7143 (C3, C4, C5); injection answer_in_context 0.4286 vs 0.8571 (B1); injection forbidden_rate 0.8571 vs 0 (B0, B3); paraphrase recall@10 0.0606 vs 0.1212 (B1); paraphrase answer_in_context 0.197 vs 0.2424 (B1); paraphrase forbidden_rate 0.1875 vs 0 (B0, B3); plain recall@10 0.814 vs 0.8953 (B1); plain answer_in_context 0.9419 vs 0.9535 (B1); session forbidden_rate 1 vs 0 (B0); temporal recall@10 0.4151 vs 0.7358 (C3, C4, C5); temporal answer_in_context 0.6038 vs 0.8113 (C3, C4, C5); temporal forbidden_rate 0.7547 vs 0 (B0); update recall@10 0.4844 vs 0.625 (C3, C4, C5); update answer_in_context 0.625 vs 0.7344 (B1); update forbidden_rate 0.6102 vs 0 (B0, B3).
- **B3** (21): all recall@10 0.0074 vs 0.6185 (B1); all answer_in_context 0.0185 vs 0.7222 (B1); all forbidden_rate 0.0085 vs 0 (B0); all abstention_recall 0 vs 1 (B0); abstention abstention_recall 0 vs 1 (B0); dedup recall@10 0 vs 0.6087 (C3, C4, C5); dedup answer_in_context 0 vs 0.8261 (B1); injection recall@10 0 vs 0.7143 (C3, C4, C5); injection answer_in_context 0 vs 0.8571 (B1); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0.0303 vs 0.2424 (B1); plain recall@10 0 vs 0.8953 (B1); plain answer_in_context 0 vs 0.9535 (B1); session recall@10 0.0667 vs 1 (B1, B2); session answer_in_context 0.1 vs 1 (B1, B2); session forbidden_rate 0.05 vs 0 (B0); temporal recall@10 0 vs 0.7358 (C3, C4, C5); temporal answer_in_context 0 vs 0.8113 (C3, C4, C5); temporal forbidden_rate 0.0189 vs 0 (B0); update recall@10 0.0313 vs 0.625 (C3, C4, C5); update answer_in_context 0.0469 vs 0.7344 (B1).
- **C1** (25): all recall@10 0.4963 vs 0.6185 (B1); all answer_in_context 0.5148 vs 0.7222 (B1); all forbidden_rate 0.5254 vs 0 (B0); all abstention_recall 0.3429 vs 1 (B0); abstention abstention_recall 0.3429 vs 1 (B0); dedup recall@10 0.5217 vs 0.6087 (C3, C4, C5); dedup answer_in_context 0.5217 vs 0.8261 (B1); dedup forbidden_rate 0.4545 vs 0 (B0, B3); injection recall@10 0.1429 vs 0.7143 (C3, C4, C5); injection answer_in_context 0.2857 vs 0.8571 (B1); injection forbidden_rate 0.4286 vs 0 (B0, B3); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); paraphrase forbidden_rate 0.0625 vs 0 (B0, B3); plain recall@10 0.8837 vs 0.8953 (B1); plain answer_in_context 0.907 vs 0.9535 (B1); session recall@10 0.8667 vs 1 (B1, B2); session answer_in_context 0.9 vs 1 (B1, B2); session forbidden_rate 0.85 vs 0 (B0); temporal recall@10 0.2264 vs 0.7358 (C3, C4, C5); temporal answer_in_context 0.283 vs 0.8113 (C3, C4, C5); temporal forbidden_rate 0.7736 vs 0 (B0); update recall@10 0.5313 vs 0.625 (C3, C4, C5); update answer_in_context 0.5156 vs 0.7344 (B1); update forbidden_rate 0.322 vs 0 (B0, B3).
- **C3** (20): all recall@10 0.6074 vs 0.6185 (B1); all answer_in_context 0.637 vs 0.7222 (B1); all forbidden_rate 0.3559 vs 0 (B0); all abstention_recall 0.3429 vs 1 (B0); abstention abstention_recall 0.3429 vs 1 (B0); dedup answer_in_context 0.6087 vs 0.8261 (B1); dedup forbidden_rate 0.4545 vs 0 (B0, B3); injection answer_in_context 0.7143 vs 0.8571 (B1); injection forbidden_rate 0.4286 vs 0 (B0, B3); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); paraphrase forbidden_rate 0.0625 vs 0 (B0, B3); plain recall@10 0.814 vs 0.8953 (B1); plain answer_in_context 0.8372 vs 0.9535 (B1); session recall@10 0.9 vs 1 (B1, B2); session answer_in_context 0.9 vs 1 (B1, B2); session forbidden_rate 0.3 vs 0 (B0); temporal forbidden_rate 0.5283 vs 0 (B0); update answer_in_context 0.6563 vs 0.7344 (B1); update forbidden_rate 0.2034 vs 0 (B0, B3).
- **C4** (20): all recall@10 0.6074 vs 0.6185 (B1); all answer_in_context 0.637 vs 0.7222 (B1); all forbidden_rate 0.3559 vs 0 (B0); all abstention_recall 0.3429 vs 1 (B0); abstention abstention_recall 0.3429 vs 1 (B0); dedup answer_in_context 0.6087 vs 0.8261 (B1); dedup forbidden_rate 0.4545 vs 0 (B0, B3); injection answer_in_context 0.7143 vs 0.8571 (B1); injection forbidden_rate 0.4286 vs 0 (B0, B3); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); paraphrase forbidden_rate 0.0625 vs 0 (B0, B3); plain recall@10 0.814 vs 0.8953 (B1); plain answer_in_context 0.8372 vs 0.9535 (B1); session recall@10 0.9 vs 1 (B1, B2); session answer_in_context 0.9 vs 1 (B1, B2); session forbidden_rate 0.3 vs 0 (B0); temporal forbidden_rate 0.5283 vs 0 (B0); update answer_in_context 0.6563 vs 0.7344 (B1); update forbidden_rate 0.2034 vs 0 (B0, B3).
- **C5** (20): all recall@10 0.6 vs 0.6185 (B1); all answer_in_context 0.6333 vs 0.7222 (B1); all forbidden_rate 0.3475 vs 0 (B0); all abstention_recall 0.3429 vs 1 (B0); abstention abstention_recall 0.3429 vs 1 (B0); dedup answer_in_context 0.6087 vs 0.8261 (B1); dedup forbidden_rate 0.4545 vs 0 (B0, B3); injection answer_in_context 0.7143 vs 0.8571 (B1); injection forbidden_rate 0.4286 vs 0 (B0, B3); paraphrase recall@10 0 vs 0.1212 (B1); paraphrase answer_in_context 0 vs 0.2424 (B1); paraphrase forbidden_rate 0.0625 vs 0 (B0, B3); plain recall@10 0.7907 vs 0.8953 (B1); plain answer_in_context 0.8256 vs 0.9535 (B1); session recall@10 0.9 vs 1 (B1, B2); session answer_in_context 0.9 vs 1 (B1, B2); session forbidden_rate 0.3 vs 0 (B0); temporal forbidden_rate 0.5094 vs 0 (B0); update answer_in_context 0.6563 vs 0.7344 (B1); update forbidden_rate 0.2034 vs 0 (B0, B3).

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
| temporal | C1 | Up to 1% | current_answer_rate 0.4324, questions 111, stale_answer_rate 0.5315, stale_answers 59 | fail: stale_answer_rate = 0.5315, want <= 0.01 |
| abstention | C1 | 95% precision | empty 74, precision 0.1622, recall 0.3429, unanswerable 35 | fail: precision = 0.1622, want >= 0.95 |
| temporal | C3 | Up to 1% | current_answer_rate 0.7117, questions 111, stale_answer_rate 0.3514, stale_answers 39 | fail: stale_answer_rate = 0.3514, want <= 0.01 |
| abstention | C3 | 95% precision | empty 74, precision 0.1622, recall 0.3429, unanswerable 35 | fail: precision = 0.1622, want >= 0.95 |
| temporal | C4 | Up to 1% | current_answer_rate 0.7117, questions 111, stale_answer_rate 0.3514, stale_answers 39 | fail: stale_answer_rate = 0.3514, want <= 0.01 |
| abstention | C4 | 95% precision | empty 74, precision 0.1622, recall 0.3429, unanswerable 35 | fail: precision = 0.1622, want >= 0.95 |
| temporal | C5 | Up to 1% | current_answer_rate 0.7117, questions 111, stale_answer_rate 0.3423, stale_answers 38 | fail: stale_answer_rate = 0.3423, want <= 0.01 |
| abstention | C5 | 95% precision | empty 74, precision 0.1622, recall 0.3429, unanswerable 35 | fail: precision = 0.1622, want >= 0.95 |
| deletion | - | 100% | core_probe_failures 0, forget_pass_rate 1, independent_probe_failures 0, passed 14, requests 14, requests_date_range 1, requests_document 3, requests_entity 2, requests_record 1, requests_search 5, requests_source 2 | pass |
| injection | - | 0% | attempts 26, context_guidance 0, context_queries 9, external_events 14, external_events_not_external 0, facts_from_external 0, facts_from_external_not_external 0, forged_assertions 3, forged_assertions_accepted 0, injection_success_rate 0, successes 0 | pass |
| document_update | - | 0% | facts_from_retired_chunks 136, facts_stale_source 21, stale_fact_active_rate 0, stale_facts_active 0, update_questions 58, update_questions_with_stale_fact_in_context 0 | pass |
| deduplication | - | 0% and up to 5% | base_paragraphs 40, duplicates 80, duplicates_missed 3, duplicates_missed_rate 0.0375, updates 40, updates_discarded 0, updates_discarded_rate 0 | pass |
| learning | - | Positive, with no regression at the gate | gain_c5_over_c4 0.0052, gate_c4 60.3279, gate_c5 60, gate_regression 0.3279, ndcg10_c4 0.5329, ndcg10_c5 0.5381, train_questions 99 | fail: gate_regression = 0.3279, want <= 0 |
| determinism | - | 100% | identical 2, identical_rate 1, pairs 2 | pass |

## Latency (not deterministic)

Measured with the wall clock on the hardware above; excluded from determinism checks. Milliseconds.

| System | Ingest items | Ingest total | Ingest p50 | Ingest p95 | Flush | Train | Retrieve mean | Retrieve p50 | Retrieve p95 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 197 | 0.032 | 0.000 | 0.001 | 0.000 | 0.000 | 0.000 | 0.000 | 0.001 |
| B1 | 197 | 21.857 | 0.078 | 0.317 | 0.000 | 0.000 | 0.602 | 0.512 | 1.048 |
| B2 | 197 | 60.814 | 0.236 | 0.980 | 0.000 | 0.000 | 0.726 | 0.712 | 0.768 |
| B3 | 197 | 3.775 | 0.012 | 0.049 | 0.000 | 0.000 | 0.084 | 0.079 | 0.096 |
| C1 | 197 | 3303.824 | 13.258 | 35.073 | 163.499 | 0.021 | 4.462 | 4.243 | 6.369 |
| C3 | 197 | 3505.928 | 13.692 | 35.343 | 190.332 | 0.016 | 5.032 | 5.042 | 7.207 |
| C4 | 197 | 3965.659 | 16.303 | 42.983 | 262.374 | 0.019 | 6.633 | 6.463 | 10.145 |
| C5 | 197 | 3416.126 | 13.572 | 36.785 | 219.877 | 6724.318 | 19.261 | 14.946 | 39.241 |
