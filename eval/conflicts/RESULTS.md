# Conflict resolution in sleep: results

Protocol: `PROTOCOL.md` (pre-registered in 2c2dde3, same-day amendment in
62dc279, both before any test run). Design: decision 0148. Detection
narrowed and settings frozen on the development set: decision 0149 (tag
`conflicts-freeze`, `frozen.set`). Diagnosis: `PRECHECK.md`. Development
grid: `tune/grid.md`. Portuguese suite: `suite/results.md`. Full test
report, generated: `results/conflicts.md` and `results/conflicts.json`.

All runs use deepseek-flash at temperature 0, e5-small embeddings, a
2,000-token D2 block, and the official LongMemEval reader and judge. C0 is
the base after phase 1. C1 is a copy of the same base after a maintenance
sleep (phase 1, plus conflict verdicts in phase 2).

## Verdict

**The claim was not established.** Condition 1 fails, and conditions 2 and
3 hold. `sleep.conflicts` stays off by default. No other variant is
proposed or built.

| Condition | Result | |
| --- | --- | --- |
| 1. Outdated information delivered falls, p < 0.05 | C0 30/31, C1 29/31; discordant 1 / 0; p = 1.0 | **fails** |
| 2. Hidden evidence in at most 2 of 107 | 0 questions (0 turns) | holds |
| 3. C1 accuracy not lower at p < 0.05 | C0 89/107 (83.2%, Wilson 75.0–89.1), C1 88/107 (82.2%, 73.9–88.3); discordant 1 / 0; p = 1.0 | holds |

## Part 0: diagnosis (development set)

- The outdated turn reached the D2 block in 14 of the 15 stale-measure
  questions. In 1 of them the current turn did not reach it.
- The near-duplicate guard refused 1.3 writes per history. None of the 18
  outdated→current pairs was among them, so the guard does not resolve
  updates.
- 0148's detection rule produced about 1,087 candidate pairs per history
  and reached the outdated→current pair in 10 of 18. That is not
  selective, which was a stop condition. The user chose a stricter
  detection: user turns only, cosine ≥ 0.90. That gave 10.3 pairs per
  history and reached 8 of 18 (decision 0149).

## Part 2: Portuguese suite (frozen settings, before the test)

- Updates: 16 of 20 detected, 15 applied. u20 was left uncertain at
  confidence 0.85.
- Controls: 11 of 20 detected, 0 of 20 false supersessions. The stop limit
  was 2.
- Cost: US$0.002.

## Part 3: test (107 questions: 47 knowledge-update + 60 others, seed 46)

| | C0 | C1 |
| --- | --- | --- |
| Outdated turn in the block (31 stale-measure questions) | 30 | 29 |
| Hidden evidence (107 questions) | – | 0 |
| Accuracy | 89 / 107 (83.2%) | 88 / 107 (82.2%) |

The only discordant accuracy question is 0bc8ad93, temporal-reasoning (18/22 →
17/22). No supersession was applied in that history, so the block is
unchanged by conflicts. The difference is reader or judge variance on a
different retrieval log, not a dropped record.

**Where it stops.** The pipeline is safe, but it does too little to move
the stale measure:

- Detection reached the outdated→current pair in 14 of the 31
  stale-measure questions (16 of 42 qualifying knowledge-update pairs).
- On the 14 reached pairs the model said: uncertain 7, coexist 5,
  unrelated 1, supersedes 1.
- Across all 107 histories, 969 pairs were judged and 2 supersessions were
  applied: 0.2%.

The 0.95 confidence floor, chosen on development to keep hidden evidence
at zero, did exactly that. It also left almost every real update in
place.

**The two applied supersessions.**

- 1cea1afa, Instagram followers: "now at 600" supersedes the earlier
  count. The pre-registered rule classes it correct.
- c6853660, coffee limit: "I have increased the limit to two cups"
  (s13t6) supersedes "cut back to just one cup" (s2t0). The pre-registered
  rule classes it **wrong**, because the ground-truth current turn is
  s13t0, in the same session. On manual reading it is a correct update. It
  is the one question where C1 dropped the outdated turn from the block.
  The report keeps the rule's class; this note does not change any
  condition.

**Cost and load.**

- Sleep per history: 9.1 LLM calls (median 8, max 29), 20.8k input
  tokens, 10.6 s, US$0.003. That is US$0.28 for the 107 histories.
- Detection: 9.1 candidate pairs per history (max 29). The 40-pair guard
  never bound.
- Study spend in the cost file: **US$0.69 of the US$2 cap**. That covers
  Part 0, the development grid, the suite, the smoke run and the test.
  Calls replayed from the store are free.

## Deviations and incidents

1. **Sharded run.** After the smoke run and 11 more histories ran
   sequentially, the remaining histories ran as 4 disjoint shards
   (`run_test.sh --shard i/N`, commit 8e3e875). They shared one proxy and
   one cost file. The steps and settings are unchanged. History 6aeb4375
   was interrupted mid-run when the sequential run stopped. It was deleted
   and rerun from scratch before any of its results were read.
2. **One transient provider error.** In gpt4_70e84552, the first verdict
   call got HTTP 502 ("unexpected EOF") from DeepSeek. The sleep ended
   `partial`, with 0 of 10 pairs judged. It was the only failed call out
   of 6,248.
   - The history was set aside in `work/aside/gpt4_70e84552.502`, then
     rerun from a fresh base. The rerun judged all 10 pairs (5 coexist,
     5 unrelated) and applied none.
   - The question is temporal-reasoning and outside the stale measure. Its
     accuracy was correct in both conditions, both times.
   - No condition changes. The report before the rerun is kept as
     `work/aside/conflicts.before-rerun.md`.
3. **Leak check.** All 214 databases (107 × C0, C1) passed: no question
   text, no gold answer outside the ingested turns, no retrievals before
   the ask.
