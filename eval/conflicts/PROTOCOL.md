# Conflict resolution in sleep: protocol (pre-registered)

This file is written and committed before any run on the test set. The
study asks whether rule detection at write time plus an LLM verdict in sleep
(decision 0148) delivers less outdated information in the context without
hiding valid information. The goal is not benchmark accuracy. A wrong
supersession is worse than none.

## Data and sets

LongMemEval S (`eval/lme/data/longmemeval_s_cleaned.json`).
`sets.py` defines the sets (output `sets.json`, committed in d5d0fe0):

- **Development**: every knowledge-update question of `eval/split-tune.json`
  (22), plus 30 other-type questions of that split drawn with
  `random.Random(46)`. All tuning happens here.
- **Test**: every knowledge-update question of `eval/split-holdout.json`
  (47), plus 60 other-type questions of that split drawn with
  `random.Random(46)`, for **107** in all. These questions were already
  answered in the selection study (`eval/selection/`). No setting of this
  study is chosen on them. They are run once, after the freeze (decision
  0149, tag `conflicts-freeze`).

### Outdated and current turns

LongMemEval does not label which evidence turn is outdated. Its README
says only that `has_answer` marks "the turns that contain the required
evidence". The rule here (`kuturns.py`) infers it from the session dates:

- A knowledge-update question **qualifies** when it is not an abstention
  question and has exactly two `has_answer` turns, in sessions with
  different timestamps. In the test set, 42 of the 47 qualify. The others
  have 1 or 3 evidence turns, or are abstention questions.
- The turn of the earlier session is the **outdated** one, and the later
  one is the **current** one.
- A question that asks for the old value (its text matches `previous`,
  `before`, `initially`, `originally`, `used to`, `at first` or `first`)
  **needs the outdated turn**. If it also says `now`, `currently` or
  `current`, it needs both turns. Every other qualifying question needs the
  current turn.

- When the two sessions fall on the same calendar day, the order is not
  trusted. Both turns are needed, and the question is left out of the
  outdated-delivery measure. This applies to 1 development and 2 test
  questions.

The rule was checked against the 9 qualifying knowledge-update questions,
in both splits, where the gold answer resembles the earlier turn more:

- 8 ask for the previous value;
- the ninth is 618f13b2, whose two sessions fall on the same day and whose
  earlier turn holds the gold value. That case is why the same-day rule
  exists.

`kuturns.stale_measure` and `kuturns.needed` implement these rules.

## Conditions

Both conditions run on the same build, with the frozen settings, a
2,000-token context and the D2 selection. Learned keys, the utility λ and
the guided digest are all off.

- **C0**: the base database after ingestion, with no sleep.
  `sleep.conflicts.enabled` is on during ingestion, so the conflict queue
  is filled. The queue changes no read.
- **C1**: a copy of the base that ran sleep in `maintenance` mode with
  conflicts on: phase 1, plus conflict verdicts in phase 2.

## Procedure, per test question

1. Ingest the haystack into the base database.
2. Copy the base to C1, run C1's sleep, and record its result: calls,
   tokens, cost, time, pairs judged, verdicts and supersessions.
3. Run the leak check on both databases. Neither may contain the question
   text or the gold answer outside the ingested turns, and neither may
   hold a retrieval.
4. Ask the question once on C0 and once on C1 (`context`, `as_of` = the
   question date). Answer with the official LongMemEval reader and judge,
   unchanged (deepseek-flash, temperature 0).

The sleep never sees the question or the gold answer.

## Smoke run and cost

The first 10 test questions in `sets.json` order run first, as a smoke
run. It measures pairs judged and sleep cost per history, and projects the
total for 107 questions. The study's cap is US$2, in its own cost file
(`eval/conflicts/cost.json`), through lmeproxy with the call store on. If
the projection exceeds the cap, the run stops and the options go to the
user. The smoke questions are part of the test. They are not rerun or
replaced.

## Measures

The primary measures use no LLM. Turns are matched by the selection
study's rule: a turn reaches the block when 60% of it, as a prefix with
whitespace removed, is in the block text.

- **Outdated information delivered**: in the qualifying knowledge-update
  questions that need only the current turn and whose sessions fall on
  different days (`stale_measure`), the number of blocks
  containing the outdated turn, in C0 and in C1.
- **Hidden evidence**: over all 107 questions, the needed evidence turns
  that are in C0's block but missing from C1's because C1 dropped them as
  superseded (drop reason `superseded` in C1's retrieval log). Needed
  turns are the ones defined above for the qualifying knowledge-update
  questions, and any `has_answer` turn for the others.

Secondary measures:

- accuracy in C0 and C1, with Wilson intervals and an exact paired sign
  test;
- candidate pairs, judged pairs and verdicts by type;
- sleep calls, tokens, cost and time per history;
- the Portuguese suite (Part 2).

## Decision rule

The claim "conflict resolution reduces outdated information in the context
without hiding valid information" may be made only if all three conditions
hold:

1. In the knowledge-update questions of the stale measure, the outdated
   information delivered falls from C0 to C1, with an exact paired sign
   test p < 0.05.
2. Hidden evidence occurs in at most 2 of the 107 questions.
3. C1's accuracy is not lower than C0's with p < 0.05. That is, C1 being
   lower with p < 0.05 fails this condition.

Otherwise the report says the claim was not established, and shows the
numbers. If it is not established, `sleep.conflicts` stays off by default
and no other variant is proposed or built.

## Stops

- The smoke projection exceeds the cap.
- The leak check finds a question or a gold answer in either database.
- The Portuguese suite applies more than 2 false supersessions among its
  20 controls. That suite runs before the test, on the frozen settings.
