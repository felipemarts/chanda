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
- **What changed:** commits b7a4f9e..8b1e8d0 (decision 0135): a turn's `ts`
  becomes its event's `valid_from` (an unparsable `ts` is rejected); facts
  without a date take their origin events' `valid_from`; with an explicit
  `as_of`, `context` and `search` drop events dated after it.
- **Re-run:** the smoke run; no full phase 1 or LLM condition had run.

## 5. The context block shows no date for raw excerpts

- **Found by:** the same smoke run.
- **What it was:** `internal/read/render.go` prints a date only for facts. A
  reader cannot place an excerpt in time, while plain RAG shows session dates.
  BASE.md does not require the date in the block; this is fixed as a general
  defect of the pre-context (an agent cannot use "what held on that date"
  without dates), not tuned on LongMemEval questions, and it is disclosed here
  because it was found through the battery.
- **What changed:** 9450ca5 (decision 0135): excerpts show `date:` (their
  `valid_from`) or `recorded:`, pages `updated:`; about 6 tokens per item.
- **Re-run:** as above.

## 6. Context ceiling raised to the battery budget

- BASE.md sets the `context` ceiling at 1,500 tokens; the owner's rule 3 fixes
  2,000 tokens for every condition. The harness sets
  `read.context_max_tokens: 2000` in the configuration of conditions D and E.
  No code change.

## 7. The harness passes the question's day, not its minute, as `as_of`

- **Found by:** the smoke run after fix 4 (hash, 20 questions, no LLM):
  Chanda's recall@10 fell from 95.6 to 83.8, all of it on temporal-reasoning
  questions. LongMemEval_S times 75 evidence sessions (41 of the 470 scored
  questions) later than their question on the same day; with a minute-exact
  `as_of` those sessions "did not exist yet". No evidence session falls on a
  later day.
- **What changed:** `eval/lme` passes the question's calendar day
  (`YYYY-MM-DD`), read by Chanda as the end of that day. No Chanda code
  changed. This choice was made after seeing the smoke drop; it is the
  reading that keeps the benchmark's own evidence available while still
  using the question date as the reference, as the owner asked.
- **Re-run:** the smoke run.

## 8. The e5 embedder leaked memory under a long load (phase 1, e5)

- **Found by:** LongMemEval phase 1 with e5 (no LLM). The `lme embedserve`
  process, which embeds through Chanda's `embedders/hugot`, was killed
  (SIGKILL) after about 40 minutes, on every attempt; `footprint` showed
  11 GB on an 8 GB machine. Not visible in the synthetic evaluation, which
  makes far fewer calls.
- **What it was:** GoMLX's Go backend keeps the buffers of every call in pools
  that never shrink, about 0.45 MB per call (decision 0136).
- **What changed:** Chanda reloads the hugot session every 1,000 model calls
  (decision 0136). Vectors are bit-identical across reloads (tested), so the
  embeddings already cached by the harness and every e5 result already
  reported stay valid.
- **Re-run:** nothing to re-run for correctness. Phase 1 with e5 was
  restarted on the fixed build; the 60-question e5 run that finished before
  the fix used the same vectors.
- **Defect in the first fix (3591184), fixed in the next commit:** the
  reload created the new session with the context of the call that
  triggered it, and hugot derives the session's context from it; once that
  call ended, every later call failed with "context canceled" (about 1,000
  calls after a start). It stopped phase 1 attempt 1 on the fixed build and
  the smoke run's condition C on its 10th question. The reload now uses
  `context.WithoutCancel`; `TestRecycleSurvivesTheCallersContext` fails on
  3591184 and passes on the fix. No result was produced on the broken
  build: both runs failed and are re-run.

## 9. Condition E called `sleep_run` inside Chanda's write-burst window (harness)

- **Found by:** the smoke run: `sleep_run` answered `conflict: sleep: write
  burst in progress; try again later` on the first question.
- **What it was:** a harness defect, not a Chanda one. Chanda refuses a
  sleep run while a write is less than `sleep.burst_seconds` (10) old
  (BASE.md: "A run never starts during a write burst"); the harness called
  `sleep_run` right after `flush`.
- **What changed:** `eval/lme` waits 11 s after ingest before `sleep_run`
  and retries (at most 3 attempts) while Chanda still reports a burst.
  No Chanda code changed. The wait is not counted in `sleep_ms`.
- **Re-run:** condition E of the smoke run (no E result existed).


## Known limitations found during setup (not fixed)

- Raw excerpts in the context block do not say who spoke (user or assistant).
  This likely hurts single-session-assistant questions. Not changed: it is a
  presentation choice, and changing it now would be tuning on the battery.
