# 0092 Sleep wiring, the gate evaluator and who writes the gate set

`internal/app` builds the `sleep.Sleeper` (unless `Options.DisableSleep`):

| `sleep.Deps` | Wired to |
|---|---|
| `LLM` | `Options.LLM`, else `llm.FromConfig(cfg.LLM, os.Getenv)` (nil without an endpoint: phase 1 only) |
| `Facts` | `facts.Assert` with the candidate's origin events, then the embedding queue and the per-write integrity check, exactly as `AssertFact` (nil with `DisableFacts`) |
| `Integrity` | `prov.Check` over the queryer it is given (the gate transaction or the store); violations counted |
| `Embeddings` | `Indexer.RunOnce` until the namespace has nothing pending |
| `Utility` | `rank.RecomputeUtility` |
| `Evaluator` | the approved gate set (below); also implements `sleep.GateSizer` |
| `EntityVectors` | canonical names embedded in memory with the configured embedder (nil with `DisableVectors`) |
| `Learning` (new) | train and promote the ranker (below; nil with `DisableLearning`) |

`App.Start` runs the scheduler (`Sleeper.Start`) next to the embedding
indexer when `sleep.scheduler` is true (default) and sleep is not disabled.
`fact_assertion` events are never sent for extraction or lessons: sleep's
queries exclude them (their fact is asserted by the call itself).

`sleep_run` returns the run report (ids, counts, cost, never content) and
the counts of what phase 2 kept: `created` (every extraction action but
`confirmed`, plus quarantined facts activated), `superseded`, `merged`,
`rejected`. A reverted run reports zeros (nothing was kept). `audit_id` is
the run's `sleep_end` entry; run notices are envelope notices.

## The gate evaluator

`Evaluator.Score` must be read-only and use only the gate transaction
(decision 0061). `read.Reader.Score` is the read path without side effects:
no retrieval row, no audit entry, no id drawn, no shadow score, every query
on the queryer it is given. Through a transaction it is **keyword only with
λ = 0**: the vector index, the utility table and the learned ranker read the
store on their own and would deadlock on the single connection. (Vectors of
records staged by the run do not exist yet anyway.) The reranker, which
needs no database, still runs when enabled.

The score is `internal/eval.GateScoreBudget`'s formula, copied into
`internal/app` (eval is the harness, not a server dependency) and checked
against it on a fixture: per approved question, context mode at
`read.context_default_tokens`, the harness budget enforcement, then nDCG@10
over `record_ids` and answer-in-context over `answers` (mean of the present
components; neither = abstention, 1 if empty); mean × 100. An item's text is
what the agent sees (page title + body). With no approved question the score
is 0 before and after, so only the integrity check guards the run; the run
then carries the notice "gate: no approved gate questions".

## Who writes the gate set (BASE.md open question)

**Sleep proposes, only a person approves.** Phase 1 proposes up to
`sleep.gate_proposals` (default 10) questions per run, status `proposed`,
from active facts of the internal and user zones (never external), in zone,
confidence and id order: question `What is <subject> <predicate>?`, expected
`{"record_ids":[fact id],"answers":[value],"as_of":<fact valid_from>}`. The
date keeps the question true after a later, legitimate supersede (at that
date the old fact held), so the gate does not block temporal updates. A
question whose text already exists in the namespace, in any status, is not
proposed again. Audited as `gate_propose` (details: fact id and run).

Review is an `App` method, not an MCP tool: approving the gate set is a
human act and the CLI is the human channel. `App.GateList(ctx, ns, status)`,
`App.GateReview(ctx, GateReviewInput{ID, Status: approved|retired})` and
`App.GateAdd(ctx, GateAddInput{Question, Expected})` (a person's own
question, approved at once) refuse a non-human actor with
`permission_denied`; audited as `gate_review`. Forget already deletes gate
questions whose `expected` cites a removed id, so proposals always carry the
fact id.

## Learning in phase 1

After the utility recompute, `Learning.Learn` trains a new model
(`rank.Train`, actor `sleep`) when there are at least
`ranking.min_train_examples` examples and more than the newest model was
trained on (otherwise λ could never ramp: a new model always restarts at one
step), then calls `rank.Promote` with an evaluator that scores the approved
gate set **outside** any transaction with the full read path: baseline at
λ = 0, candidate at the λ it would get with the newest model's prediction as
the term. Without approved questions both score 0 and no model passes. The
outcome (model ids, scores, λ, notices) is in the run report and outputs.
