# LongMemEval phase 1: retrieval without LLM

- bm25_unit: session
- budget_tokens: 2000
- chanda_search_as_of: filters.as_of = question day (YYYY-MM-DD, end of day; events dated after it are dropped, decision 0135)
- chanda_search_limit: 100
- conditions: [bm25 rag chanda]
- dataset: xiaowu0162/longmemeval-cleaned@98d7416c24c778c2fee6e6f3006e7a073259d48f/longmemeval_s_cleaned.json
- date: 2026-11-15T11:52:04Z
- embed_dim: 384
- embed_model: e5-small@hugot0.8.6+simd
- embedder: e5
- go: go1.27.0-X:simd
- goos_goarch: darwin/arm64
- harness_cache_hits: 227776
- harness_cache_misses: 96343
- questions: 500
- rag_chunk_chars: 1000
- rag_top_k: 50
- selection: all
- token_estimator: ceil(runes/4)
- wall_seconds: 19391.743782125
- workers: 3

Recall@k = share of the evidence sessions (answer_session_ids) among the first k distinct sessions ranked; MRR = 1/rank of the first evidence session. Abstention questions are excluded from every metric (official README: they have no evidence location). Official = LongMemEval session-level recall_any/recall_all/ndcg_any (src/retrieval/eval_utils.py) over the official targets; questions without a target are skipped, as run_retrieval.py does.

## Summary (all non-abstention questions)

| condition | n | recall@5 | recall@10 | MRR | official n | recall_any@5 | recall_all@5 | ndcg_any@5 | recall_any@10 | recall_all@10 | ndcg_any@10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 470 | 91.5 | 95.6 | 0.928 | 419 | 96.2 | 84.0 | 0.875 | 98.3 | 91.6 | 0.892 |
| rag | 470 | 95.8 | 97.9 | 0.946 | 419 | 98.1 | 92.6 | 0.919 | 98.8 | 96.7 | 0.927 |
| chanda | 470 | 95.9 | 98.3 | 0.946 | 419 | 98.6 | 91.6 | 0.929 | 99.3 | 97.4 | 0.939 |

## By question type (recall@5 / recall@10 / MRR)

| type | n | bm25 | rag | chanda |
| --- | --- | --- | --- | --- |
| all | 470 | 91.5 / 95.6 / 0.928 | 95.8 / 97.9 / 0.946 | 95.9 / 98.3 / 0.946 |
| knowledge-update | 72 | 98.6 / 99.3 / 0.990 | 100.0 / 100.0 / 0.986 | 99.3 / 100.0 / 0.986 |
| multi-session | 121 | 84.4 / 90.6 / 0.927 | 95.7 / 97.2 / 0.982 | 93.0 / 98.1 / 0.961 |
| single-session-assistant | 56 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 | 100.0 / 100.0 / 1.000 |
| single-session-preference | 30 | 86.7 / 96.7 / 0.654 | 93.3 / 96.7 / 0.726 | 100.0 / 100.0 / 0.718 |
| single-session-user | 64 | 100.0 / 100.0 / 0.977 | 100.0 / 100.0 / 0.964 | 100.0 / 100.0 / 0.982 |
| temporal-reasoning | 127 | 87.3 / 93.8 / 0.902 | 90.3 / 95.6 / 0.908 | 92.0 / 95.7 / 0.920 |

Abstention questions run but not scored: 30.

## Cost and latency

| condition | retrieval p50 ms | retrieval p95 ms | ingest/index p50 ms | ingest/index p95 ms | sessions ranked (mean) | DB bytes p50 | DB bytes max | budget recall | context tokens (mean) | errors |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| bm25 | 0.5 | 1.3 | 26 | 59 | 32.5 | - | - | - | - | 0 |
| rag | 12.9 | 1026.8 | 2196 | 69675 | 47.7 | - | - | 95.7 | 1981 | 0 |
| chanda | 64.1 | 133.5 | 12152 | 328382 | 27.3 | 3645440 | 4042752 | - | - | 0 |

Retrieval latency: BM25 = the FTS5 query; RAG = query embedding + exact scan + sort + budget fill (chunk embeddings are ingest); Chanda = the search tool call round trip over MCP stdio. Chanda ingest = every log_session call plus flush (embedding included). DB bytes = Chanda's .db + -wal + -shm on disk after the run.
