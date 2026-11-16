# 0144 Guided sleep: settings frozen after the bench, unchanged

The bench of the sleep study (`eval/sleep/BENCH.md`) ran the guided sleep
in both digest modes on 9 questions from the LongMemEval tuning set: the
first 6 that D2 gets wrong among multi-session and temporal questions,
then the first 3 it gets right. Each question was asked, the sleep ran,
and the same question was asked again. Repeating a question measures
memorisation, not later improvement.

Result: 3/9 correct before the sleep, and 3/9 after it in both modes. The 3
correct answers stayed correct, and none of the 6 wrong ones was fixed. The
verifier discarded 1 of about 100 LLM lines (`unsupported_name`). The
bench cost US$0.022.

## Decision

The settings the task proposed are frozen as they are, with no tuning:
weights 1, 2, 2 and 1; top 10; the derived share of the block 0.30, with
the two best-ranked raw excerpts protected; the line format of decision
0142; and a window of 60 tokens per extractive line.
`eval/sleep/frozen.set` holds them, and the LoCoMo test runs them (tag
`sleep-freeze`).

Why nothing was changed: on 9 questions, each asked again after a sleep
that saw only that question, no setting showed an effect to tune for. In
the 6 wrong questions, the missing evidence was not in the query's top
10, so the group could not contain it, whatever the weights. The ranks
of the missing evidence turns among the event candidates: 27 (a08a253f),
12 (55241a1f), 24 (51c32626), 58, 39 and not retrieved (gpt4_e061b84f),
not retrieved (eac54add), 11 (6cb6f249). Two of them sit just below the
cut. A larger `top_k` is the obvious next thing to test, on fresh data and
not here. Choosing new
values on this bench would mean fitting noise before the test that counts.

Observations kept for the record, not acted on:

- Extractive lines from long assistant replies are windows of generic
  advice. User turns survive whole and carry the facts.
- With the share at 30%, a ficha took the place of an unprotected evidence
  turn in 2 cases (51c32626 in both modes, 6cb6f249 in L). The answers
  did not change, since all of them were already wrong.
- An LLM line can cite many events (8 ids in 55241a1f), and the ids cost
  block tokens.
