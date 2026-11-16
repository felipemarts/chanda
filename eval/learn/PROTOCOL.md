# Learning from use without adding text to the block: protocol (pre-registered)

This file is written and committed before any run on the test
conversations. The study asks whether use can improve search and ranking
(utility, decision 0146; learned query keys, decision 0145) so that later
answers get better, with no new content in the block. It is the last
learning test before publication. Development checks: `PRECHECK.md`.

## Data

LoCoMo, `snap-research/locomo` commit
`3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376`, `data/locomo10.json` (sha256
`79fa87e9…98ff4`; CC BY-NC 4.0, kept out of git).

- **Development**: conv-30, conv-47 and conv-49. These are the sleep
  study's conversations, and their halves are spent. They may be read,
  run and analysed freely, and all tuning happens on them.
- **Test**: the other 7 conversations, conv-26, conv-41, conv-42, conv-43,
  conv-44, conv-48 and conv-50. Categories 1 to 4. Nobody looks at them
  before the freeze (decision 0147, tag `learn-freeze`).

The split is made by `split_locomo.py` (output `split-locomo.json`,
committed with this file). It follows the sleep study's rule with seed 45
and no draw of conversations. Within each conversation and category, the
questions are shuffled and n//2 go to **use**, the rest to **test**. A
question whose text repeats in its conversation goes to the half of its
first occurrence. Nine were moved this way, all in conv-48.

| Conversation | Use | Test | Test by category (1/2/3/4) |
| --- | --- | --- | --- |
| conv-26 | 75 | 77 | 16 / 19 / 7 / 35 |
| conv-41 | 75 | 77 | 16 / 14 / 4 / 43 |
| conv-42 | 98 | 101 | 19 / 20 / 6 / 56 |
| conv-43 | 88 | 90 | 16 / 13 / 7 / 54 |
| conv-44 | 61 | 62 | 15 / 12 / 4 / 31 |
| conv-48 | 94 | 97 | 11 / 21 / 5 / 60 |
| conv-50 | 78 | 80 | 16 / 16 / 4 / 44 |
| **total** | **569** | **584** | 109 / 115 / 37 / 323 |

## What stays as in the sleep study

Ingestion (one `log_session` per session, roles, `"<Speaker>: <text>"`,
photo captions, session dates), e5-small embeddings, the 2,000-token
context, deepseek-flash at temperature 0, and the reader and judge prompts
of `eval/sleep/PROTOCOL.md`, unchanged and verbatim. Plain RAG (B) is the
same too. Calls go through lmeproxy with the call store on and this
study's own cost file (`eval/learn/cost.json`), under a cap of US$4,
off-peak preferred. A run in peak hours is recorded as a deviation.

## Procedure, per conversation

1. Ingest into the base database.
2. Answer the **use** half with U0, one `context` call per question, in
   split order. Read, judge, and record one outcome per retrieval:
   - `value` is +1 if judged correct, −1 otherwise;
   - `used_ids` follow the sleep protocol's mechanical rule.

   The base database has `write.learned_keys` on, so positive outcomes
   create keys. They are inert until a condition reads them.
3. Copy the base database once per condition and apply that condition's
   learning step.
4. Run the leak check (below), then answer the **test** half once.

## Conditions (all with a 2,000-token context and the D2 selection)

| | Memory | Learning step on the copy |
| --- | --- | --- |
| B | plain RAG (the sleep study's B) | none |
| U0 | Chanda D2, no learning: `ranking.utility_lambda` 0, `read.learned_keys` off | none (no phase 1) |
| U1 | U0 plus utility ranking: `ranking.utility_lambda` and `ranking.min_exposures` as frozen | sleep phase 1 only (recomputes utility, drains the embedding queue) |
| K | U1 plus learned keys: `read.learned_keys` on, weight and top k as frozen | sleep phase 1 only, as U1 |

The learning steps call no LLM, and the report confirms this from the
proxy log.

## Decision rules

Each claim needs higher accuracy on the test half (584 questions, the 7
conversations pooled) **and** an exact two-sided sign test on the paired
outcomes with p < 0.05:

- **Claim A**, "Chanda is more accurate than plain RAG": U0 against B.
- **Claim B**, "learning from use improves later answers": K against U0.

Secondary comparisons, descriptive only: U1 against U0 (utility alone) and
K against U1 (keys alone). No correction for multiple tests is applied,
and the report says so.

If claim B is not established, no other learning variant is proposed or
built. Keys and digestion stay off by default, the report says so, and
sleep remains maintenance only: integrity, utility and deduplication.

## Leak check (before each condition's test half)

A run stops if any of these holds:

- a test question's text is in the database: retrieval queries, key
  text, or any other text column outside the ingested turns;
- a key's text is not a use-half question, or a retrieval's query is not
  a use-half question;
- an outcome does not have a value of ±1 with used ids among the
  retrieval's delivered ids;
- judge output is in the database;
- a gold answer is in the keys, retrievals or outcomes, unless it occurs
  inside the text of a use-half question itself. Such cases are listed in
  the report.

Keys holding use-half questions are expected. A gain larger than the
measured overlap ceiling allows (the share of test questions with an
evidence turn used in a positive outcome of the use half, computed on the
7 conversations as in `PRECHECK.md`) is treated as too good and checked
for leaks before it is reported.

## Measures

- Accuracy per condition: total, by category and by conversation, with
  Wilson 95% intervals and the paired tests above.
- Full evidence in the block, with no LLM, by the selection study's
  rule.
- Keys:
  - keys created per conversation;
  - the share of test questions where a key brought into the block a
    record that U0 did not deliver (`features.key_rank` > 0 in K's
    retrieval log);
  - how many of those records were evidence.
- The overlap ceiling on the 7 conversations.
- Read latency (`retrieval_ms`) and database size, with and without
  keys.
- Cost.
- Hand review: `review.md`. It lists every question where K and U0
  differ, plus 20 judge-approved answers drawn with
  `random.Random(45)`: 10 from U0 and 10 from B.
