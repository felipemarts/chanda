# LongMemEval phase 1: retrieval without LLM

- bm25_unit: session
- budget_tokens: 2000
- chanda_search_as_of: filters.as_of = question day (YYYY-MM-DD, end of day; events dated after it are dropped, decision 0135)
- chanda_search_limit: 100
- conditions: [bm25 rag chanda]
- dataset: xiaowu0162/longmemeval-cleaned@98d7416c24c778c2fee6e6f3006e7a073259d48f/longmemeval_s_cleaned.json
- date: 2026-11-14T06:08:10Z
- embed_dim: 256
- embed_model: hash-v1-256
- embedder: hash
- go: go1.27.0-X:simd
- goos_goarch: darwin/arm64
- harness_cache_hits: 0
- harness_cache_misses: 324119
- questions: 500
- rag_chunk_chars: 1000
- rag_top_k: 50
- selection: all
- token_estimator: ceil(runes/4)
- wall_seconds: 1427.248605083
- workers: 4

Recall@k = share of the evidence sessions (answer_session_ids) among the first k distinct sessions ranked; MRR = 1/rank of the first evidence session. Abstention questions are excluded from every metric (official README: they have no evidence location). Official = LongMemEval session-level recall_any/recall_all/ndcg_any (src/retrieval/eval_utils.py) over the official targets; questions without a target are skipped, as run_retrieval.py does.

## Summary (all non-abstention questions)

| condition | n | recall@5 | recall@10 | MRR | official n | recall_any@5 | recall_all@5 | ndcg_any@5 | recall_any@10 | recall_all@10 | ndcg_any@10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 470 | 91.5 | 95.6 | 0.928 | 419 | 96.2 | 84.0 | 0.875 | 98.3 | 91.6 | 0.892 |
| rag | 470 | 72.2 | 82.7 | 0.739 | 419 | 84.2 | 59.7 | 0.657 | 91.9 | 72.8 | 0.699 |
| chanda | 470 | 88.2 | 93.4 | 0.880 | 419 | 94.0 | 80.0 | 0.827 | 96.7 | 87.4 | 0.848 |

## By question type (recall@5 / recall@10 / MRR)

| type | n | bm25 | rag | chanda |
| --- | --- | --- | --- | --- |
| all | 470 | 91.5 / 95.6 / 0.928 | 72.2 / 82.7 / 0.739 | 88.2 / 93.4 / 0.880 |
| knowledge-update | 72 | 98.6 / 99.3 / 0.990 | 85.4 / 93.1 / 0.873 | 97.9 / 99.3 / 0.970 |
| multi-session | 121 | 84.4 / 90.6 / 0.927 | 65.8 / 80.6 / 0.770 | 82.2 / 89.9 / 0.895 |
| single-session-assistant | 56 | 100.0 / 100.0 / 1.000 | 87.5 / 89.3 / 0.757 | 100.0 / 100.0 / 0.926 |
| single-session-preference | 30 | 86.7 / 96.7 / 0.654 | 36.7 / 56.7 / 0.307 | 66.7 / 80.0 / 0.435 |
| single-session-user | 64 | 100.0 / 100.0 / 0.977 | 81.2 / 92.2 / 0.709 | 100.0 / 100.0 / 0.973 |
| temporal-reasoning | 127 | 87.3 / 93.8 / 0.902 | 67.8 / 77.5 / 0.743 | 82.2 / 90.3 / 0.855 |

Abstention questions run but not scored: 30.

## Cost and latency

| condition | retrieval p50 ms | retrieval p95 ms | ingest/index p50 ms | ingest/index p95 ms | sessions ranked (mean) | DB bytes p50 | DB bytes max | budget recall | context tokens (mean) | errors |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 0.2 | 0.4 | 11 | 14 | 32.5 | - | - | - | - | 0 |
| rag | 1.5 | 2.7 | 509 | 549 | 47.7 | - | - | 81.5 | 1981 | 0 |
| chanda | 25.9 | 35.1 | 10747 | 12000 | 31.4 | 3293184 | 3596288 | - | - | 0 |

Retrieval latency: BM25 = the FTS5 query; RAG = query embedding + exact scan + sort + budget fill (chunk embeddings are ingest); Chanda = the search tool call round trip over MCP stdio. Chanda ingest = every log_session call plus flush (embedding included). DB bytes = Chanda's .db + -wal + -shm on disk after the run.
