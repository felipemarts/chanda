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

## 2. Mem0 does not accept a timestamp on `add` (condition C setup)

- **Found by:** setting up condition C, before any LLM call.
- **What it was:** Mem0 2.2.1 (open source) rejects `add(timestamp=...)`, and
  its extraction prompt writes today's date as the "Observation Date". Left
  alone, every LongMemEval session would be dated on the day of the run, which
  handicaps Mem0 on temporal and knowledge-update questions while Chanda gets
  the session dates (owner's data rule).
- **What changed:** the harness stores the session date as the memory's
  `created_at` and patches one internal Mem0 function so its prompt shows the
  session date as the Observation Date (`--date-mode` default in
  `eval/lme/py/mem0_cond.py`). This favours Mem0 relative to a stock install;
  it is stated in the report. `--date-mode instructions` (public API only) is
  kept for a stock comparison but is not the run's setting.
- **Re-run:** nothing had run.

## 3. Condition A uses the same reader template as the others

- Owner rule 3 (same reader prompt for every condition) takes precedence over
  the official LongMemEval question-only template: condition A uses the
  official retrieval template with an empty history block. The official
  question-only variant (`--a-template official-no-retrieval`) is not run.
