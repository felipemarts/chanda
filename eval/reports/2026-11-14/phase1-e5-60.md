# LongMemEval phase 1: retrieval without LLM

- bm25_unit: session
- budget_tokens: 2000
- chanda_search_as_of: filters.as_of = question day (YYYY-MM-DD, end of day; events dated after it are dropped, decision 0135)
- chanda_search_limit: 100
- conditions: [bm25 rag chanda]
- dataset: xiaowu0162/longmemeval-cleaned@98d7416c24c778c2fee6e6f3006e7a073259d48f/longmemeval_s_cleaned.json
- date: 2026-11-14T08:00:08Z
- embed_dim: 384
- embed_model: e5-small@hugot0.8.6+simd
- embedder: e5
- go: go1.27.0-X:simd
- goos_goarch: darwin/arm64
- harness_cache_hits: 24905
- harness_cache_misses: 13919
- questions: 60
- rag_chunk_chars: 1000
- rag_top_k: 50
- selection: sample ../sample.json
- token_estimator: ceil(runes/4)
- wall_seconds: 1776.020174917
- workers: 3

Recall@k = share of the evidence sessions (answer_session_ids) among the first k distinct sessions ranked; MRR = 1/rank of the first evidence session. Abstention questions are excluded from every metric (official README: they have no evidence location). Official = LongMemEval session-level recall_any/recall_all/ndcg_any (src/retrieval/eval_utils.py) over the official targets; questions without a target are skipped, as run_retrieval.py does.

## Summary (all non-abstention questions)

| condition | n | recall@5 | recall@10 | MRR | official n | recall_any@5 | recall_all@5 | ndcg_any@5 | recall_any@10 | recall_all@10 | ndcg_any@10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 54 | 93.7 | 96.2 | 0.910 | 47 | 100.0 | 83.0 | 0.871 | 100.0 | 91.5 | 0.882 |
| rag | 54 | 98.6 | 99.1 | 0.939 | 47 | 100.0 | 95.7 | 0.926 | 100.0 | 97.9 | 0.928 |
| chanda | 54 | 96.3 | 98.0 | 0.941 | 47 | 100.0 | 89.4 | 0.936 | 100.0 | 93.6 | 0.946 |

## By question type (recall@5 / recall@10 / MRR)

| type | n | bm25 | rag | chanda |
| --- | --- | --- | --- | --- |
| all | 54 | 93.7 / 96.2 / 0.910 | 98.6 / 99.1 / 0.939 | 96.3 / 98.0 / 0.941 |
| knowledge-update | 8 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 |
| multi-session | 13 | 82.1 / 88.5 / 0.865 | 94.2 / 96.2 / 0.923 | 90.4 / 94.2 / 0.923 |
| single-session-assistant | 7 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 |
| single-session-preference | 4 | 100.0 / 100.0 / 0.750 | 100.0 / 100.0 / 0.613 | 100.0 / 100.0 / 0.708 |
| single-session-user | 7 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 0.929 |
| temporal-reasoning | 15 | 92.9 / 96.4 / 0.861 | 100.0 / 100.0 / 0.950 | 95.1 / 97.8 / 0.967 |

Abstention questions run but not scored: 6.

## Cost and latency

| condition | retrieval p50 ms | retrieval p95 ms | ingest/index p50 ms | ingest/index p95 ms | sessions ranked (mean) | DB bytes p50 | DB bytes max | budget recall | context tokens (mean) | errors |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 0.3 | 0.6 | 10 | 32 | 32.9 | - | - | - | - | 0 |
| rag | 4.5 | 184.6 | 1503 | 47560 | 47.3 | - | - | 98.1 | 1981 | 0 |
| chanda | 33.8 | 54.3 | 3827 | 196582 | 26.5 | 3629056 | 3817472 | - | - | 0 |

Retrieval latency: BM25 = the FTS5 query; RAG = query embedding + exact scan + sort + budget fill (chunk embeddings are ingest); Chanda = the search tool call round trip over MCP stdio. Chanda ingest = every log_session call plus flush (embedding included). DB bytes = Chanda's .db + -wal + -shm on disk after the run.
