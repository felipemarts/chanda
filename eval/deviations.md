# Deviations and defects found during the evaluation battery

Each entry: what was found, by which check, what changed, and which
conditions or results had to be re-run. Entries are added in the order found.

## 1. Forgetting an entity left free text that names it (phase 5, scenario F3)

- **Found by:** phase 5 scenario F3, before any LongMemEval run and before any
  LLM call.
- **What it was:** `forget {kind: entity}` removed the entity's facts and their
  origin events, but not the free-text events that name the entity. Decision
  0050 says the entity scope resolves to the events that mention the entity,
  but nothing outside tests ever wrote `mentions` edges, so that set was always
  empty. The statement survived in `events`, FTS, the FTS5 vocabulary and
  `embeddings`, and its bytes stayed in the database file. Forget's own
  verification reported `passed: true` because it only probes the records it
  removed.
- **What changed:** see the fix commit referenced below.
- **Re-run:** phase 5 in full. No LongMemEval condition had run yet.
