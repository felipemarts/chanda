# LongMemEval battery: Chanda against no memory, plain RAG and Mem0

Date: 2026-11-14. Chanda frozen as tag `eval-lme-freeze` (7e57d12), plus the
fixes listed in section 6 (6e27225 on the evaluated build). The operational
log is in `RUNLOG.md`. Defects and deviations are in `eval/deviations.md`.

## Setup

| | |
| --- | --- |
| Dataset | LongMemEval S cleaned, `xiaowu0162/longmemeval-cleaned@98d7416c`, sha256 `d6f21ea9…c3a442`, MIT licence (`eval/lme/DATASET.md`) |
| Sample | 60 questions, seed 42, stratified by type, 6 abstention; committed before any LLM call (`eval/sample.json`, 16efb77) |
| Reader and judge | `deepseek-flash` (DeepSeek-V4.1-Flash-1017), temperature 0, thinking disabled, through `eval/cmd/lmeproxy` |
| Prompts | Official LongMemEval reader (history + CoT) and judge prompts, repository commit `9e0b455f` |
| Context budget | 2,000 tokens for every condition (estimator ceil(runes/4)) |
| Embeddings | `e5-small@hugot0.8.6+simd` (multilingual-e5-small), local, the same model for B, C, D and E |
| Dates | Session dates are the event dates. The question's day is the reference (`as_of`; deviation 7) |
| Runs | One run per condition. The smoke run's first 10 questions are part of the 60 (same configuration) |
| Machine | MacBook Air, darwin/arm64, 8 GB RAM. Latencies were measured while other runs shared the machine |
| Versions | `eval/versions.txt` |

Conditions:

- **A** no memory: the same reader template with an empty history.
- **B** plain RAG: 1,000-rune chunks, e5, exact top-k, filled up to the budget.
- **C** Mem0 open source 2.2.1: base install, extraction by deepseek-flash, e5 through a local endpoint.
- **D** Chanda without sleep: `log_session` per session, `flush`, then `context`.
- **E** Chanda after `sleep_run` with phases 1 and 2, using deepseek-flash.

## 1. Summary

Accuracy with Wilson 95% intervals, n = 60.

| | A no memory | B plain RAG | C Mem0 | D Chanda | E Chanda + sleep |
| --- | --- | --- | --- | --- | --- |
| **Accuracy** | 11.7% [5.8, 22.2] | 81.7% [70.1, 89.4] | **88.3%** [77.8, 94.2] | 78.3% [66.4, 86.9] | 76.7% [64.6, 85.6] |
| single-session-user (8) | 1 | 8 | 8 | 8 | 8 |
| single-session-assistant (7) | 1 | 7 | 7 | 7 | 7 |
| single-session-preference (4) | 0 | 3 | 4 | 4 | 3 |
| knowledge-update (9) | 1 | 9 | 9 | 9 | 9 |
| temporal-reasoning (16) | 1 | 12 | 14 | 10 | 9 |
| multi-session (16) | 3 | 10 | 11 | 9 | 10 |
| abstention (6, included above) | 6 | 5 | 5 | 5 | 5 |
| Context tokens, mean | 0 | 1,981 | 1,992 | 1,995 | 1,994 |
| Retrieval latency p50 / p95 (ms) | - | 11 / 35 | 33 / 69 | 44 / 66 | 75 / 168 |
| Ingest per question, p50 / p95 (s) | - | 3.2 / 11.4 | 402 / 910 | 7.6 / 9.7 | 9.1 / 15.6 (+ sleep 93 / 159) |
| LLM calls for ingest, 60 questions | 0 | 0 | 2,868 | 0 | 1,483 (sleep) |
| LLM cost for ingest | 0 | 0 | US$2.479 | 0 | US$1.627 |
| Database size per question, p50 | - | - | - | 3.6 MB | 4.5 MB |

Paired outcomes (questions only one of the two got right; exact two-sided
sign test):

| Pair | first only | second only | p |
| --- | --- | --- | --- |
| D vs B | 2 | 4 | 0.69 |
| D vs C | 0 | 6 | 0.031 |
| E vs D | 4 | 5 | 1.0 |
| E vs C | 2 | 9 | 0.065 |
| C vs B | 5 | 1 | 0.22 |

Retrieval without an LLM (phase 1), session-level recall@10 / MRR:

| | BM25 | plain RAG | Chanda `search` |
| --- | --- | --- | --- |
| **e5, all 500 questions (470 scored)** | 95.6 / 0.928 | 97.9 / 0.946 | **98.3** / 0.946 |
| e5, 60-question sample (54 scored) | 96.2 / 0.910 | 99.1 / 0.939 | 98.0 / 0.941 |
| hash embedder, all 500 questions (470 scored) | 95.6 / 0.928 | 82.7 / 0.739 | 93.4 / 0.880 |

With e5 over all 500 questions (`phase1-e5.md`), Chanda's search matches
plain RAG: recall@10 is 98.3 against 97.9 and MRR is equal. Search ranks the
evidence session in the top 10 for 98% of the questions, so the accuracy
gap in section 1 comes from what goes into the 2,000-token block, not from
retrieval (section 4). Search latency over MCP in that run: p50 64 ms, p95
134 ms (busy machine). The hash row makes plain RAG
look weak because hash vectors carry no meaning; the battery uses e5.

## 2. Targets of the specification (BASE.md, Evaluation)

Own suites are deterministic, run on the synthetic Portuguese set and
judged without an LLM (`eval/reports/latest.md` for hash,
`latest-e5.md` for e5). The C3 configuration is shown.

| Target | Goal | Measured (hash / e5) | Met |
| --- | --- | --- | --- |
| Temporal: superseded value in the answer | ≤ 1% | 0.9% / 0.9% | yes |
| Deletion: `forget` passes every probe | 100% | 16/16 / 16/16 | yes |
| Injection: external content becomes guidance or a higher-zone fact | 0% | 0 / 0 | yes |
| Document update: facts of retired chunks still active | 0% | 0 / 0 | yes |
| Deduplication: updates discarded / duplicates missed | 0% and ≤ 5% | 0% and 1.25% / 0% and **33.75%** | hash yes, **e5 no** |
| Abstention: precision of an empty pre-context | 95% | **0** (1 empty) / **undefined** (0 empty) | **no** |
| Learning: C5 over C4 with no gate regression | positive, no regression | +0.003 nDCG with −0.33 gate / 0 | **no** |
| Determinism: identical databases in two runs | 100% | 100% / 100% | yes |
| Phase 3: C1 matches or beats B2 on recall@10 | ≥ B2 | LongMemEval e5, 500 questions: 98.3 against 97.9 (sample of 60: 98.0 against 99.1) | yes on 500 (difference inside noise) |
| Comparison with Mem0 in the same environment | report it, losses included | Mem0 88.3% against Chanda 78.3% | done; Chanda loses |
| Final answer with a hand-checked sample | hand check | `eval/review.csv` written, **not hand-checked yet** | **no** |

Phase 5 deterministic scenarios through `chanda serve` over MCP:
12 of 15 pass (`eval/scenarios/results.md`). They are byte-identical over
two runs. I1, A1 and A2 fail, all through the loss of abstention. In I1,
injection is still blocked; the scenario fails because an extra block is
delivered.

## 3. Where Chanda lost

- **Against Mem0, on every type where they differ.** Chanda (D) got none
  right that Mem0 missed; Mem0 got 6 right that D missed: 4 temporal
  and 2 multi-session. The sign test gives
  p = 0.031, but the Wilson intervals overlap (section 7).
- **Against plain RAG, it did not win.** 78.3% against 81.7%, although its
  search retrieves as well as plain RAG (recall@10 98.3 against 97.9 over
  500 questions). The loss happens in the selection of the context block.
- **Temporal reasoning was the weakest type:** 10/16, against 12/16 for RAG
  and 14/16 for Mem0. Sleep made it worse (9/16).
- **Sleep did not pay.** E cost US$1.63 in LLM calls, about 93 s of extra
  time per question and 0.9 MB more database. It created 11,661 facts and
  superseded 25; 25 of 60 runs stopped at the token budget. E got
  46 right against 47 for D, and 4 against 5 on the questions they disagree
  on.
- **Abstention:** 5 of 6 in every memory condition. A, which has no
  context at all, abstained 6 of 6.
- **No context savings in this setting.** Chanda filled the 2,000-token
  budget like the others (mean 1,995 tokens). The smaller blocks seen on the
  synthetic set do not appear on LongMemEval.

## 4. Ten errors of Chanda, classified

Full analysis: `error-analysis-D.md`. Rule fixed before reading the errors:
the first 10 wrong answers of D in sample order. D had 13 wrong; the
other three are gpt4_2f56ae70, gpt4_731e37d7 and 370a8ff4.

| Class | Count | Questions |
| --- | --- | --- |
| Search did not bring the excerpt | 1 (uncertain) | gpt4_15e38248 |
| The cut removed the excerpt | **8** | 2c63a862, 2ce6a0f2, gpt4_f420262d, gpt4_fe651585, e3038f8c, 91b15a6e, 88432d0a, gpt4_1e4a8aec |
| An outdated value came in | 0 | none |
| The reader erred with the right context | 1 | 09ba9854_abs (plain RAG erred the same way) |

The pattern: search ranks the right sessions high, often first or second.
The context block then spends the budget on long, generic assistant
replies and drops the short user turns that carry the fact, such as
"on 2/15", "25 rare coins" or "planted 12 tomato saplings today".
- In the four counting failures, the block held 0, 0, 1 and 3 of the 4
  facts needed.
- In the temporal failures, the reader did the date arithmetic right but
  anchored on the wrong event, because the dated fact was not in the block.
- The harness metric `evidence_recall_in_context` overstates what the
  reader got: it counts a session as present when any of its turns is in
  the block.

This was not changed during the battery: changing the selection now would be
tuning on the evaluated questions. It is the first thing to fix. Measure it
on a held-out set: other LongMemEval questions outside the sample, or
LoCoMo.

## 5. Cost

All LLM calls went through lmeproxy (`eval/cost.json`, call log
`eval/llm_calls.jsonl`). All of them were off-peak (Saturday).

| Tag | Calls | Cost (US$) |
| --- | --- | --- |
| C-ingest (Mem0 extraction) | 2,868 | 2.479 |
| E-sleep (Chanda phase 2) | 1,483 | 1.627 |
| Readers, five conditions | 300 | 0.110 |
| Judges, five conditions | 300 | 0.015 |
| **Total** | **4,951** | **4.278** (cap 8.00) |

The totals include the calls repaid when the smoke run's condition C was
retried on one question. Seven calls ended in network errors (connection
reset by the API); the clients retried them, and no Mem0 extraction was
lost.

Per question: Mem0 ingest about US$0.041, Chanda sleep about US$0.027,
reader plus judge about US$0.0005. Chanda D and plain RAG need no LLM to
build memory. Embeddings ran locally (e5 on CPU).

From the full run on, every call was also stored with its full request and
response (`eval/lme/llmstore/`, `lmeproxy -store`). A later run of the same
prompts can replay them at no cost with `-replay`. The bodies of the smoke
run's first-pass calls were not stored, because the store did not exist
yet. Their results are kept in `raw/` and in the context files.

## 6. Deviations and defects fixed during the evaluation

From `eval/deviations.md`:

1. Forgetting an entity left free text that names it. Fixed in Chanda
   (decision 0130).
2. Mem0 accepts no timestamp on `add`. The harness passes each session's
   date in Mem0's prompt (observation date). This favours Mem0.
3. Condition A uses the same reader template with an empty history.
4. `log_session` dropped the turn timestamp. Fixed in Chanda
   (decision 0135).
5. The context block showed no dates for raw excerpts. Fixed in Chanda
   (0135).
6. The context ceiling was raised from 1,500 to 2,000 tokens for D and E
   (configuration only).
7. The harness passes the question's day, not its minute, as `as_of`.
   Chosen after a smoke drop, and disclosed.
8. The e5 embedder leaked about 0.45 MB per call: the GoMLX buffer pools
   never shrink, and the process reached 11 GB and was killed.
   - Fixed in Chanda by reloading the session every 1,000 calls
     (decision 0136); vectors are bit-identical.
   - The first version of the fix broke after the first reload (context
     canceled) and was fixed with a regression test.
9. Condition E called `sleep_run` inside Chanda's write-burst window. This
   was a harness defect; the harness now waits 11 s.

Defects 1, 4, 5 and 8 changed Chanda after the freeze. None of them
changes ranking or selection.

## 7. Limitations

- **60 questions.** Wilson intervals are ±10 to 15 points, so no accuracy
  difference between B, C, D and E is larger than its interval.
- **English, conversational data.** LongMemEval's haystacks are chat
  sessions from ShareGPT/UltraChat plus inserted evidence. The synthetic
  suites are in Portuguese and were not part of this battery.
- **One run per condition at temperature 0.** DeepSeek does not promise
  determinism at temperature 0, so run-to-run variance was not measured.
- **Automatic judge.** deepseek-flash with the official judge prompt.
  `eval/review.csv` (one row per question, every condition's answer and
  verdict) has not been checked by hand.
- **Mem0 configuration:**
  - base install, without spaCy or fastembed, so Mem0's BM25 hybrid
    scoring is off;
  - session dates passed in the prompt (deviation 2);
  - e5 embeddings with the query prefix on both sides.
- **Latencies** were measured with other runs on the same 8 GB machine.
  Read them as upper bounds; they are not comparable to a quiet machine.
  Mem0's ingest time was the most affected (three shards in parallel).
- **E's sleep budget.** 25 of 60 runs stopped at the default token budget
  (200,000).
- **The reader is one model.** Results may differ with a stronger reader,
  which would use long contexts better.

## What the data supports

Each claim below meets one of the task's criteria: a difference larger than
the interval, a deterministic phase 5 result, or a cost or latency measured
directly.

1. **"Any of the four memory conditions answers far more than no memory."**
   No memory got 11.7% [5.8, 22.2]; the lowest memory condition got 76.7%
   [64.6, 85.6]. *Caveat:* expected for this benchmark; it shows the
   harness works, not that Chanda is good.
2. **"Chanda builds its memory without any LLM call; Mem0 spent US$0.041
   per question on extraction."** Measured: US$2.479 and 2,868 calls for
   60 questions, against US$0 for Chanda D. *Caveat:* DeepSeek off-peak
   prices; Mem0 was 10 points more accurate in the same run (inside the
   intervals).
3. **"Chanda's ingest took 7.6 s per question (p50), Mem0's 402 s."**
   Measured on the same machine. *Caveat:* the machine was shared and
   Mem0 ran three shards at once; the ratio is indicative, not a benchmark.
4. **"Chanda's sleep phase 2 cost US$0.027 per question and ~93 s, and did
   not improve accuracy in this run (46 against 47 of 60)."** The cost and
   time are measured. *Caveat:* "no improvement" is within noise; what is
   supported is the cost, not a proof that sleep is useless.
5. **"Chanda passes 12 of 15 deterministic scenarios through MCP, byte
   for byte over two runs."** The passing ones cover deletion, injection,
   temporal update and determinism. The three failures are abstention.
   *Caveat:* synthetic scenarios with the hash embedder.
6. **"Search over MCP answers in 44 ms p50 / 66 ms p95 on a 3.6 MB
   database of about 490 turns"** (64 / 134 ms in the 500-question
   retrieval run). Measured. *Caveat:* a busy 8 GB laptop.
7. **"Chanda's search retrieves the evidence as well as plain RAG with the
   same embedder."** recall@10 98.3 against 97.9 and MRR 0.946 for both,
   over 470 questions. *Caveat:* this is equality, not superiority; the
   difference is far inside the noise. Retrieval does not explain Chanda's
   lower accuracy.

## What the data does not support

- **"Chanda is more accurate than plain RAG or than Mem0."** The data
  points the other way (78.3% against 81.7% and 88.3%), inside the
  intervals. A claim either way needs about 4 to 5 times more questions
  (the full 500).
- **"Mem0 is more accurate than Chanda."** The paired sign test (0 against
  6, p = 0.031) suggests it. The intervals overlap, so by this battery's
  rule it is not established. To support it: the full 500 questions, or a
  second sample, plus a hand check of the 6 disagreements.
- **"Sleep improves answers."** No gain measured. It would need a run
  where sleep finishes within budget and a set whose questions need
  consolidation, compared on a held-out sample.
- **"Chanda saves context tokens."** It filled the budget like the others.
  It would need a run at several budgets (500, 1,000, 2,000) with accuracy
  at each.
- **"Chanda abstains when it does not know."** Abstention fails on the
  synthetic suites. On LongMemEval it matched the others (5/6), which is too
  few to say anything.
- **Anything about Portuguese, other domains or other readers.** This run
  measured none of them.

**Is anything worth publishing?** Not as a headline about accuracy: Chanda
did not beat plain RAG or Mem0 here. The defensible message is narrow:

- Chanda reaches the plain-RAG range on LongMemEval, within noise.
- It builds memory with no LLM calls, about US$0.04 per conversation cheaper
  than Mem0 on extraction, at an accuracy that looks lower than Mem0's.
- Its main measured weakness is the selection of turns for the context
  block.

## Files

- `raw/<cond>.jsonl`: one line per question and condition, with the
  context, answer, verdict and tokens. `raw/<cond>.answers.jsonl` is the
  reader output.
- `eval/review.csv`: one row per question, with every condition's answer
  and verdict.
- `phase0.md`, `phase1-hash.md/.json`, `phase1-e5.md/.json` (e5, 500
  questions), `phase1-e5-60.md/.json` (e5, the 60-question sample).
- `phase4-summary.json`, `phase4-report.txt`, `smoke-*`.
- `error-analysis-D.md`, `RUNLOG.md`.
- `eval/cost.json`, `eval/llm_calls.jsonl`, `eval/versions.txt`,
  `eval/deviations.md`, `eval/sample.json`.
