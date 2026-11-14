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
- **What changed:** commit 30b6a93 (decision 0130): the entity scope matches
  the entity's canonical name and aliases (and merged entities) as whole-word
  phrases over every event of the namespace; a new `entity_mentions` probe
  re-runs the rule over what remains. The synthetic deletion suite now runs
  entity requests last (11ff703).
- **Re-run:** phase 5 in full (F3 passes). No LongMemEval condition had run yet.

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

## 4. `log_session` dropped the turn timestamp (found by the LongMemEval harness)

- **Found by:** the phase 1 smoke run (20 questions, hash, no LLM), before any
  LongMemEval result was used for anything.
- **What it was:** `internal/app/write.go` built `write.Turn` without `TS`, so
  every event got `valid_from` NULL and `recorded_at` = ingest time. BASE.md
  says `valid_from` uses the date given in the call first. Chanda therefore
  knew no session dates, and temporal and knowledge-update questions could not
  test what they are meant to test.
- **What changed:** see the fix commit (pending).
- **Re-run:** the smoke run; no full phase 1 or LLM condition had run.

## 5. The context block shows no date for raw excerpts

- **Found by:** the same smoke run.
- **What it was:** `internal/read/render.go` prints a date only for facts. A
  reader cannot place an excerpt in time, while plain RAG shows session dates.
  BASE.md does not require the date in the block; this is fixed as a general
  defect of the pre-context (an agent cannot use "what held on that date"
  without dates), not tuned on LongMemEval questions, and it is disclosed here
  because it was found through the battery.
- **What changed:** see the fix commit (pending).
- **Re-run:** as above.

## 6. Context ceiling raised to the battery budget

- BASE.md sets the `context` ceiling at 1,500 tokens; the owner's rule 3 fixes
  2,000 tokens for every condition. The harness sets
  `read.context_max_tokens: 2000` in the configuration of conditions D and E.
  No code change.

## Known limitations found during setup (not fixed)

- Raw excerpts in the context block do not say who spoke (user or assistant).
  This likely hurts single-session-assistant questions. Not changed: it is a
  presentation choice, and changing it now would be tuning on the battery.
