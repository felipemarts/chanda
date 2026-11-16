# Learning from use without adding text to the block: results

- **Protocol:** `PROTOCOL.md`, pre-registered in 0fa457e before any run on
  the test conversations.
- **Implementation:** decisions 0145 (learned query keys) and 0146 (fixed
  utility λ).
- **Settings:** frozen on the development set only, in decision 0147 (tag
  `learn-freeze`): `ranking.utility_lambda` 0.2, `ranking.min_exposures` 5,
  `read.learned_keys_weight` 0.5, `read.learned_keys_top_k` 10.
- **Part 0:** `PRECHECK.md`.
- **Full tables:** `results/learn.md` and `results/learn.json`.
- **Hand review:** `review.md`.

Run conditions are the same as the sleep study's: deepseek-flash at
temperature 0, e5-small, the 2,000-token block, the D2 selection, and the
reader and judge prompts verbatim.

## Summary

The test half pools 7 conversations: conv-26, 41, 42, 43, 44, 48 and 50,
584 questions in all. Each question was asked once per condition.

| Condition | Accuracy [Wilson 95%] | multi-hop (109) | temporal (115) | open-domain (37) | single-hop (323) | Full evidence in block (581 scored) |
| --- | --- | --- | --- | --- | --- | --- |
| B, plain RAG | 64.7% [60.8, 68.5] | 55 | 53 | 13 | 257 | 396 (68.2%) |
| **U0**, Chanda D2, no learning | **67.3%** [63.4, 71.0] | 56 | 58 | 10 | 269 | 401 (69.0%) |
| U1, U0 + utility | 66.3% [62.3, 70.0] | 56 | 51 | 11 | 269 | 405 (69.7%) |
| **K**, U1 + learned keys | **67.6%** [63.7, 71.3] | 59 | 56 | 14 | 266 | 408 (70.2%) |

| Pair | Questions only the first got right / only the second | p, exact two-sided sign test | Rule (higher accuracy and p < 0.05) |
| --- | --- | --- | --- |
| **Claim A**: U0 vs B | 72 / 57 | 0.218 | **not established** |
| **Claim B**: K vs U0 | 31 / 29 | 0.897 | **not established** |
| U1 vs U0 (utility alone; descriptive) | 20 / 26 | 0.461 | – |
| K vs U1 (keys alone; descriptive) | 25 / 17 | 0.280 | – |

No correction for multiple tests was applied: there are two claims and
two secondary comparisons, and both claims fail without one anyway.

By conversation, correct answers on the test half:

| | conv-26 | conv-41 | conv-42 | conv-43 | conv-44 | conv-48 | conv-50 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| n | 77 | 77 | 101 | 90 | 62 | 97 | 80 |
| B | 51 | 50 | 52 | 61 | 42 | 67 | 55 |
| U0 | 56 | 56 | 62 | 56 | 42 | 62 | 59 |
| U1 | 49 | 54 | 66 | 57 | 46 | 57 | 58 |
| K | 52 | 56 | 68 | 56 | 44 | 61 | 58 |

## What the decision rules allow

- **Claim A**, "Chanda is more accurate than plain RAG", **cannot be
  stated**. U0 is 2.6 points higher (67.3% against 64.7%; 72 against 57;
  p = 0.218). The direction matches the selection study's held-out result
  (82.7% against 77.7% on LongMemEval, p = 0.058), and neither is
  established.
- **Claim B**, "learning from use improves later answers", **cannot be
  stated**. K is 0.3 points higher than U0: 31 questions only K got right,
  29 only U0 got right, p = 0.897. That is no detectable effect.

Claim B is not established, so under the closing rule no other learning
variant is proposed or built. Learned keys (`read.learned_keys`,
`write.learned_keys`), the fixed utility λ (`ranking.utility_lambda`)
and the guided digest (`sleep.mode: guided`) all stay **off by default**.
Sleep stays maintenance only: integrity, utility and deduplication.

## How the keys behaved

- **Keys created.** 548 keys from 361 positive use outcomes across the 7
  conversations, 48 to 103 per conversation.
- **What they brought.** In 496 of the 584 test questions (84.9%), a key
  brought into K's block a record U0 did not deliver: 1,166 records in
  all. Only **15** of them were evidence turns, in 14 questions.
- **Overlap ceiling,** measured as in `PRECHECK.md`:
  - 181 test questions (31.0%) had an evidence turn used in a positive
    outcome of the use half;
  - 34 questions (5.8%) were wrong in U0 and missing an evidence turn
    that a positive outcome had used. Those 34 are the most that keys
    could have fixed by bringing evidence.

  K's net gain over U0 was 2 questions, well under that ceiling, so the
  result is not "too good".
- **Why the flips happened.** Of the 60 questions where K and U0 differ,
  46 had the same evidence turns in both blocks (25 won by K, 21 by U0).
  In those, the answer changed because other items around the evidence
  changed, not because evidence arrived. Evidence was gained in 8 (6 won
  by K, 2 lost) and lost in 5 (all lost).
- **What a key matches on.** A key matches on question similarity. In
  this data the questions about the same two people look alike, so most
  keys pull in records from a neighbouring question. In conv-43, "Which
  book was John reading during his recovery?" pulled in Tim's line about
  *A Dance with Dragons* through the key "What book did Tim just finish
  reading …?".

## Where learning made things worse

- **Utility alone (U1) lost temporal questions,** 58 to 51 of 115. It lost
  7 questions in conv-26 and 5 in conv-48, and gained 4 in conv-42 and
  conv-44 each. Overall it was 6 questions below U0 (20 against 26,
  p = 0.46). Its full evidence rose slightly (401 to 405), so the losses
  are in how the reader used the block, not in what reached it.
- **Keys lost 29 questions to U0.** In 26 of them a key had brought in a
  record U0 did not have, and in 6 an evidence turn U0 delivered was
  pushed out of the block. The keys were not removed one by one, so how
  many of the 26 losses each key-brought record caused is not known.
- **Single-hop** fell slightly with keys (269 to 266 of 323).

## Five cases where a key helped

Each is a test question U0 got wrong and K got right.

1. **conv-48-q34**, "Which year did Jolene and her partner start
   dating?" (gold: 2020). The key "How long have Jolene and her partner
   been together?" (a use question) brought in, at key rank 1, the
   evidence turn "We're not married yet but we've been together for three
   years". U0: "Not mentioned". K: "2020".
2. **conv-44-q20**, "Which meat does Audrey prefer eating more than
   others?" (gold: chicken). The key "What is Audrey's favorite recipe
   that she shares with Andrew on 3 July, 2023?" brought in "My favorite
   recipe is Chicken Pot Pie". U0: "Not mentioned". K: "Chicken (Chicken
   Pot Pie)".
3. **conv-44-q15**, "What is a shared frustration regarding dog
   ownership for Audrey and Andrew?" (gold: not being able to find
   pet-friendly spots). A key brought in the evidence turn "I get how
   frustrating it can be not to find pet-friendly spots". The key was
   "How did Audrey describe she dog he met at the pet store?", a
   neighbouring question that happened to be right. U0: "Not mentioned".
4. **conv-41-q30**, "What shelters does Maria volunteer at?" (gold: the
   homeless shelter, the dog shelter). Evidence went from 2/3 turns in
   U0 to 3/3 in K. A key, through a question about Maria's yoga, also
   brought in her line about volunteering at the homeless shelter. U0:
   "homeless shelter". K: "Homeless shelter and local dog shelter".
5. **conv-48-q102**, "What project was Jolene working on as of 1
   February, 2023?" (gold: a robotics project). The evidence turn reached
   K's block with no key on it (`key_rank` 0). It came from the
   reordering that K's utility and key boosts cause. U0: "Not mentioned".
   K: "A robotics project for her engineering professor."

## Five cases where a key hurt

Each is a test question U0 got right and K got wrong, with an evidence
turn lost from the block.

1. **conv-43-q50**, "Which book was John reading during his recovery from
   an ankle injury?" (gold: *The Alchemist*). The key "What book did Tim
   just finish reading on 8th December, 2023?" brought in Tim's *A Dance
   with Dragons* turn, and John's *The Alchemist* turn left the block. K
   answered "*Dune*".
2. **conv-43-q88**, "How did John get introduced to basketball?" (gold:
   his dad signed him up for a local league). Six key-brought records
   about John's career goals and seminars displaced the evidence turn. K
   answered "Not mentioned".
3. **conv-43-q118**, "What did John's team win at the end of the season?"
   (gold: a trophy). Keys about his ankle injury, his new team and a
   celebration brought in neighbouring turns. "We even won a trophy!"
   was pushed out, and K answered "Not mentioned".
4. **conv-48-q6**, "What symbolic gifts do Deborah and Jolene have from
   their mothers?" (gold: pendants). Keys about Deborah's mother's
   cooking and flowers brought in their turns, and the pendant turn left. K
   answered "Not mentioned".
5. **conv-48-q104**, "What activity did Jolene and her partner plan to do
   together instead of resuming yoga?" (gold: play the console). The key
   "What is the favorite game Jolene plays with her partner?" brought in
   game-related turns, and the evidence turn was lost. K answered
   "Camping trip …".

## Cost and resources

| | |
| --- | --- |
| Spend | **US$1.518** of the US$4 cap: US$1.512 priced, plus US$0.006 estimated for 8 calls that ended in an upstream error |
| Calls | 4,801 forwarded and 1,017 answered from the call store. Tags: reader and judge of `base-use`, `U0-test`, `U1-test`, `K-test` and `B-test` only |
| LLM calls made by learning | **none**. Each of the 14 learning steps (sleep phase 1 for U1 and K, 7 conversations) reports 0 LLM calls, and no other tag appears in the proxy log |
| Read latency, mean / p95 (ms) | U0 30.7 / 69.7, U1 10.6 / 15.2, K 12.8 / 19.2 |
| Database size | Keys add 0.17 to 0.34 MB per conversation, about 2 to 3% of a 9 to 12 MB database (K compacted against K with its keys reset, compacted) |

On latency: U0 was always the first condition asked after the copies were
made, so its higher mean most likely reflects a cold cache. This was not
measured separately. Between U1 and K, which ran under the same
conditions, keys add about 2 ms per read (+21%).

## Deviations

1. **Peak hours.** At the user's request the run was not restricted to
   off-peak. 1,480 calls fell in peak hours, as listed in
   `results/learn.md`. The drivers now allow peak hours by default
   (`OFFPEAK_ONLY=1` restores the old behaviour).
2. **U1 and K use a fixed λ.** In the sleep study, λ could never leave 0
   without a gate set (`PRECHECK.md`). Decision 0146 adds a λ that
   applies the stage-1 utility directly, and its value was chosen on the
   development set.
3. **Keys exist in every copy.** The base database had `write.learned_keys`
   on, so U0 and U1 also hold the keys. They read with
   `read.learned_keys` off, so the keys are inert there.
4. **Gold strings inside use questions.** The leak check lists gold
   strings found inside use-half question texts, in keys and retrievals
   (for example "Running" in "What does Melanie say running has been great
   for?"). They were listed, not treated as stops, as the protocol
   provides. All 28 leak checks passed otherwise: no test question, no
   judge output, and every key and every retrieval query is a use-half
   question.
5. **Development tuning used the sleep study's base databases,** whose
   keys were rebuilt from their recorded outcomes with
   `learned_keys rebuild`. On the test conversations, keys were created
   at outcome time as specified.

## Limitations

- 584 test questions over 7 conversations, one run per condition, one
  reader model and an automatic judge. A difference of 2 to 3 points is
  within the noise at this size.
- The use half gives one question per topic, so each memory is reached by
  a handful of use questions. Long real use would repeat topics more
  often. This test measures transfer from one use pass, not from months
  of use.
- The keys were tested only at the frozen setting. Higher weights were
  worse on the development set, and lower weights get closer to U1.
- LoCoMo's gold answers have known errors, and they affect all conditions
  alike.

## What the data support

- On these 7 LoCoMo conversations, learning from one pass of use did not
  measurably improve later answers. K and U0 scored 67.6% and 67.3%, with
  31 against 29 discordant questions.
- The learned keys bring many records into the block, 1,166 records in
  85% of test questions, but almost none are evidence (15). Their effect
  on accuracy nets out to zero.
- Learning made no LLM calls. Keys cost about 2 to 3% of database size
  and about 2 ms per read.
- Utility, given a fixed λ, changes blocks slightly (full evidence 401 to
  405) but not accuracy in a detectable way.
- The maximum gain any learning by use could have produced here was
  small: 34 of 584 questions (5.8%) were wrong and missing evidence that
  use had touched.
- Chanda without learning scored 2.6 points above plain RAG, without
  significance.

## What the data do not support

- That Chanda is more accurate than plain RAG (claim A, p = 0.218).
- That learning from use improves later answers (claim B, p = 0.897).
- That utility ranking helps or hurts (U1 against U0, p = 0.46).
- That keys alone help or hurt (K against U1, p = 0.28).
- Anything about longer use, other key weights, other datasets or other
  reader models, none of which was tested.
