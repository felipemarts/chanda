# Sleep transfer test on LoCoMo: protocol (pre-registered)

Written and committed before any LoCoMo run. The question it answers: does
a sleep guided by use improve answers to *later* questions, ones the sleep
never saw? The bench on the LongMemEval tuning set (`BENCH.md`) repeats the
same question after sleep, which measures memorisation, so it is
diagnostic only. This test is the evidence.

## Data

LoCoMo, `snap-research/locomo` commit
`3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376`, file `data/locomo10.json`
(sha256 `79fa87e9…98ff4`, see `eval/locomo/SOURCE.md`). Licence CC BY-NC
4.0: research use, kept out of git.

Selection, by `split_locomo.py` (output `split-locomo.json`, committed with
this file):

- three conversations drawn with `random.Random(44)`: **conv-30, conv-47,
  conv-49**;
- categories 1 to 4. Category 5 (adversarial) is excluded: 444 of its 446
  items have no gold answer. Category names follow `eval/locomo/SOURCE.md`:
  1 multi-hop, 2 temporal, 3 open-domain, 4 single-hop;
- within each conversation and category, the questions are shuffled with
  the same generator and split in half: n//2 to **use**, the rest to
  **test**. A question whose text repeats in its conversation goes to the
  half of its first occurrence (none moved).

| Conversation | Use | Test | Test by category (1/2/3/4) |
| --- | --- | --- | --- |
| conv-30 | 40 | 41 | 6 / 13 / 0 / 22 |
| conv-47 | 74 | 76 | 10 / 17 / 7 / 42 |
| conv-49 | 76 | 80 | 19 / 17 / 7 / 37 |
| total | 190 | 197 | 35 / 47 / 14 / 101 |

Only the turns, speaker names and session dates go into memory. The
dataset's `observation`, `session_summary` and `event_summary` are never
read.

## Ingestion

One Chanda database per conversation. Each LoCoMo session is one
`log_session` call:

- session id `<sample_id>-s<n>`;
- turns in order. `speaker_a` gets role `user` and `speaker_b` gets role
  `assistant`. The content is `"<Speaker>: <text>"`, followed by
  `" [shares a photo: <blip_caption>]"` when the turn has a caption;
- the turn timestamp is the session's `date_time` (parsed as local time,
  stored as UTC).

Embeddings are e5-small through the cached embed server, as in the
battery. Every Chanda condition uses the configuration frozen at the end of
the bench (decision 0144, tag `sleep-freeze`): the selection defaults of
decision 0141, plus `read.context_derived_max_share` and the guided-sleep
keys as frozen there. The context budget is 2,000 tokens.

## Procedure, per conversation

1. Ingest into the base database.
2. Answer the **use** half with Chanda (D2 selection), one `context` call
   per question in split order, each logged as a retrieval. Read, then
   judge. Record one outcome per retrieval:
   - `value` is +1 if judged correct, −1 otherwise;
   - `used_ids` are the delivered items the answer draws on, decided
     mechanically. The answer's tokens are its lower-case alphanumeric
     words of three letters or more, not on a stop list, plus its digit
     runs. An item counts as used when at least half of those tokens
     (rounded up) occur in the item's text. An answer that says "Not
     mentioned", or that has no tokens, uses nothing.

   The sleep sees only these rows: retrievals, deliveries, used marks and
   the outcome sign. It never sees gold answers, judge output or query
   text (decision 0142).
3. Copy the base database once per condition and run that condition's
   sleep on its copy.
4. Answer the **test** half once per condition. Read and judge.
5. Secondary, labelled as a ceiling: answer the **use** half again on
   each condition's database after its sleep.

## Conditions (all with a 2,000-token context)

| | Memory | Sleep on the copy |
| --- | --- | --- |
| B | plain RAG: 1,000-rune chunks per session with its date, e5, exact top-k (the battery's B) | none |
| S0 | Chanda D2 | phase 1 only (`phases: [1]`) |
| S1 | Chanda D2 | guided, extractive digest |
| S2 | Chanda D2 | guided, LLM digest |
| S3 | Chanda D2 | uniform (the old sleep), token budget = the tokens S2 spent on that conversation, event budget unlimited |

S0 runs phase 1 for a reason. Phase 1 recomputes utility from the use
half's outcomes, and every sleep runs it. Running it in S0 too means
S1/S2 against S0 measures the digest alone, not the utility learning that
any sleep brings (a divergence from "D2 without sleep": that condition is
S0 without phase 1 and would mix the two effects). S3 runs last, and only
if the spend so far leaves room for it under the US$2 cap. B is not
affected by the use half.

## Reader and judge

deepseek-flash, temperature 0, through lmeproxy, off-peak only, with its
own cost file (`eval/sleep/cost.json`) and the call store on.

Reader (user message, no system message; max_tokens 200):

```
Below are memories retrieved from a long conversation between {speaker_a} and {speaker_b}. Each memory may carry a date.

{context}

Answer the question using only these memories. When the question is about time, use the dates shown to give the date or period as precisely as the memories allow. Answer with a short phrase. If the memories do not contain the answer, answer "Not mentioned".

Question: {question}
Answer:
```

Judge (user message, max_tokens 5):

```
You are grading an answer to a question about a conversation.

Question: {question}
Gold answer: {gold}
Generated answer: {answer}

The generated answer is CORRECT if it states the same thing as the gold answer, even if it is longer or worded differently. For a date or a time, it is CORRECT if it refers to the same date or period, in any format. It is WRONG if it contradicts the gold answer, leaves out its key content, or says the information is not available.

Reply with one word: CORRECT or WRONG.
```

For B, `{context}` is the plain-RAG rendering. For Chanda, it is the
`context` block as delivered.

## Decision rule

"Usage-guided sleep improves later answers" may be stated for S1 or S2
only if, on the test half (197 questions, 3 conversations pooled), its
accuracy is higher than S0's **and** the exact two-sided sign test on the
paired outcomes against S0 gives p < 0.05. Otherwise the report says the
difference was not established and shows the numbers. Each of S1 and S2 is
tested on its own. No correction for the two tests is applied, and the
report says so.

## Measures

- Accuracy per condition on the test half, total and by category, with
  Wilson 95% intervals; the paired sign test against S0, and B against S0.
- Full evidence in the block (no LLM). The evidence `dia_id`s are mapped to
  turns; the 9 malformed or dangling ids of `SOURCE.md` are parsed where
  the turn exists and skipped otherwise, and questions with no valid
  evidence are not scored. A turn reaches the block by the selection
  study's rule (60% prefix, whitespace removed). A secondary count also
  credits a ficha line that cites the evidence event's id.
- Per sleep mode, per conversation: LLM calls, tokens, cost, time, groups
  processed, lines written, lines discarded by the verifier.
- Gain per dollar: (accuracy of S1 or S2 − accuracy of S0), in points,
  over the sleep's cost.
- Share of the block taken by sleep items: rendered tokens of facts and
  pages over block tokens.
- Ceiling: use-half accuracy after sleep, per condition.

## Stops

- The cost projection before the test run exceeds the US$2 cap (together
  with the bench). Ask before running.
- A result that looks too good. Before reporting, check the sleep's inputs
  for any trace of gold answers, judge text or test questions: the
  database's retrievals, outcomes and fichas.
