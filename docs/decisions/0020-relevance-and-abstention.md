# 0020 Relevance measure and abstention evidence

BASE.md fixes the stages (RRF k = 60, optional rerank of the top 30,
final = (1 − λ)·relevance + λ·utility, cut by a relevance threshold in
`context`) but not what "relevance" is. RRF alone cannot abstain: it is
rank-based, so the best item of any query, however unrelated, gets the top
score. Principle 8 needs absolute evidence.

## Relevance (orders, and is cut by `read.relevance_threshold`)

- `rrf = Σ 1/(k + rank)`. The keyword side contributes one list per record
  type (BM25 scales differ between `events_fts`, `facts_fts` and `pages_fts`,
  so ranks are only compared within a type); the vector side is one list.
- `relevance = rrf / (L/(k+1))`, where L is the number of list kinds that ran
  (1 keyword-only, 2 hybrid). Query-independent, in [0, 1], 1 only for an item
  ranked first everywhere.
- With the reranker on: `(relevance + σ(rerank))/2` for the top N,
  `relevance/2` for the rest.

## Evidence (the abstention gate, `context` only)

An item passes the cut only if `relevance ≥ read.relevance_threshold` **and**
it has evidence:

- lexical: `coverage ≥ read.min_term_coverage` (default 0.5), where coverage =
  distinct query terms present in the record ÷ min(query terms,
  `read.coverage_term_cap` = 4). Terms are the query tokens after folding
  (lowercase, no diacritics) and removing a small pt/en stopword list. The cap
  stops long natural-language requests from needing every word to match;
- or semantic: `cosine ≥ read.min_cosine` (default 0.5). This floor depends on
  the embedding model (e5-style models give 0.7+ to unrelated pairs) and must
  be calibrated per `model_id` on the abstention suite.

`evidence = max(coverage, cosine)` is logged with every candidate. The final
score is `(1 − λ)·relevance + λ·u`, where u is the record's utility or the
neutral 0.5 when it has none (internal/rank contract). Utility
never enters the cut (BASE.md "The cut stays relevance-based"). If nothing
passes, the result is empty with `no_relevant_memory`; if items pass but none
fits the token budget, the result is empty with `budget_too_small`.

`read.relevance_threshold` stays 0 by default: the evidence gate does the
abstaining, and the threshold remains available to tune once the abstention
suite exists.

## Token estimate

Budgets use `ceil(runes/4)`, model-independent and deterministic. The block
header, footer and each item block are estimated separately; their sum bounds
the estimate of the whole text, so `tokens_used ≤ budget` always holds. An
item larger than what remains is truncated (marked `truncated`) only if at
least `read.context_min_item_tokens` remain; otherwise it is skipped.
