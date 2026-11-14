# 0086 Evaluation: C1..C5 over the real core, the hardened dataset and the own suites

Extends 0085. `chanda eval` (internal/cli, internal/eval, internal/eval/coresys)
compares B0..B3 with C1..C5 and runs the eight own suites of BASE.md.

## C configurations

- Each C system is an `app.App` built with `app.NewWith` and the options of
  decision 0094, over its own database in a temporary directory and a fake
  clock set to each corpus item's date (never backwards). The embedder is the
  one the harness gives B2 (`Options.Embedder`), so B2 and every C system use
  the same model; the report states its `model_id`.
- Writes go through the public API with real source kinds: documents through
  `ingest` (kind `document`, or `tool`/`web` for external items) by a human
  operator actor; sessions through `log_session` (tool turns external) and
  the statements asserted in a session through `assert_fact` by an agent
  actor. Without structured facts (C1, C2: `DisableFacts`) those statements
  are written with `remember`.
- Retrieval is the `context` tool at the harness budget with the question's
  `as_of`. Each delivered record is traced back to its corpus item through
  the event ids the writes returned, else the document uri, else the source
  uri. The harness recounts tokens on the item texts; the block's header and
  labels are not counted (they would only lower C's content).
- C3 adds the `header` and `kv_table` extractors and extends
  `facts.single_predicates` with the Portuguese keys the corpus uses for the
  same attributes as the English defaults (cargo, ramal, preco, ...); the
  list is `coresys.PortugueseSinglePredicates` and is part of C3's config
  hash, printed per system in the report.
- C2 needs a reranker. The chanda binary has none, so C2 is reported as *not
  run* with the reason and C3..C5 say in their description that they run
  without it (C3 is then C1 + facts). chanda-hugot passes the cross-encoder
  of `embed.reranker_model_path` (`cli.NewReranker`), which enables C2.
- C4 runs `sleep_run` once after ingestion, past the write-burst window;
  without `llm.endpoint` only phase 1 runs, and the description says so.

## Learning (C5) and the splits

The dataset has three splits: evaluated questions (no split), `train` and
`gate`. Only C5 sees the last two (`eval.Trainer`), after ingestion and
before any evaluated question:

1. The gate split is added as approved gate questions by the human operator
   (`GateAdd`: question, expected answers, `as_of`; no record ids, which the
   dataset cannot know). Questions proposed by sleep are left unreviewed.
2. The training split is replayed 3 times in dataset order as tasks: the
   agent calls `context` with the question; the delivered records that are
   evidence for a gold unit (same corpus item, span contained) are reported
   as `used_ids` with outcome +1, and when none is the outcome is -1 with no
   used ids. Tasks are spaced on the fake clock to stay under
   `ranking.outcome_rate_per_minute`.
3. A sleep run trains a ranker and promotes it only if the gate allows; the
   report states the ranker line of the sleep report.

Evaluated questions never reach training or the gate. The learning suite
compares C5 with C4 on them: `gain_c5_over_c4` = nDCG@10 difference,
`gate_regression` = C4 minus C5 in gate points (`eval.GatePoints`: answer in
context for answerable questions, empty result for unanswerable ones).

## Dataset (synthetic-pt v2)

Generated from a seed by `eval/cmd/gensynth`; a test fails when the committed
files differ from the generator. It adds, over v1: paraphrased questions
worded with little overlap with their evidence (responsibilities, product
descriptions, branch specialties, rule paraphrases); distractor minutes,
project reports, newsletters, chit-chat sessions and a competitor's page with
the same product names; a raw history of about 22k tokens (B3 is truncated in
every query); temporal chains of three or more values of single predicates
across document versions and session statements, with `as_of` questions in
each period; near-duplicate notices and an FAQ that goes stale; injected
instructions in web pages, tool results and tool turns; 35 unanswerable
questions, many near misses (existing names with absent attributes,
first-name or surname matches).

## Own suites

- temporal (per system): among evaluated questions tagged temporal or update
  (not injection) that list superseded values, the share whose context holds
  one (`stale_answer_rate`); `current_answer_rate` is published next to it.
- abstention (per system): precision and recall of empty results (0085).
- deletion: on a C3 core with the whole corpus, forget requests of every
  scope (record, document, source, entity, date range, search). A request
  passes when forget's own verification passes and independent probes find
  nothing: `get` of removed ids answers not_found, `search` (with history)
  and `context` return nothing with the item's distinctive terms (terms found
  in a single corpus item, matched as whole terms). For an entity scope the
  term probes look at facts only: the scope removes the facts and the events
  they come from or that mention the entity, not every paragraph under the
  same heading.
- injection: zones of every tool/web event and of facts derived from them;
  `context` for injection questions and for the opening words of every
  instruction-like event must deliver no instruction-like item and external
  items only as labelled quotations; `assert_fact` through an external
  channel claiming `user_statement` must stay external and leave the
  internal fact active.
- document update: active facts all of whose origins are retired chunks,
  over the facts derived from retired chunks.
- deduplication: through `remember` on an empty core, for 40 corpus
  paragraphs with a number: an update (first number changed) must not be
  answered duplicate or near_duplicate; two real duplicates (spacing and
  case; a trailing reminder word) must be.
- determinism: C1 and C3 run twice on the whole corpus (ingest, flush, one
  `context` per evaluated question); a pair is identical when the
  checkpointed database files are byte-identical.
- learning: as above; `not_available` when C4 or C5 did not run.

Suite diagnostics (misses, with ids and terms, never content) go to stderr.
