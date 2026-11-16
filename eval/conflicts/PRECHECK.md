# Part 0: diagnosis on the development set (no LLM)

The development set is the 22 knowledge-update questions of
`eval/split-tune.json` plus 30 other questions (`sets.json`). Every
haystack was ingested with `sleep.conflicts.enabled=true` and asked once
with D2. Then `conflicts scan -details` was run at four similarity floors.

- Scripts: `precheck.sh`, `precheck.py` and `precheck_filters.py`.
- Tables: `precheck/precheck.md`, `precheck/filters.txt`, `precheck/precheck.json`.

Outdated and current turns follow `kuturns.py` (`PROTOCOL.md`). 18 of the
22 knowledge-update questions qualify, and 15 are in the outdated-delivery
measure.

## 1. Is the outdated turn delivered today?

Yes. It is in 14 of the 15 D2 blocks of the measure (93%), and in 1 of
them the current turn is missing. Over all 18 qualifying questions, the
outdated turn is in 17 blocks. This is far above the 10% stop line, so
there is something to improve.

## 2. What the near-duplicate guard already refuses

| | Knowledge-update histories | Other histories |
| --- | --- | --- |
| Refusals per history (mean / median / max) | 1.3 / 1 / 4 | 2.1 / 1.5 / 11 |
| By reason (totals) | numbers 20, entities 8, negation 1 | numbers 37, entities 16, negation 9 |
| Refusals that link an outdated turn to its current turn | **0 of 18** | – |

The guard refuses very few pairs, and none of them is an update pair. The
update turns are long and say other things too, so their cosine is far
below the near-duplicate threshold.

## 3. Candidates the detection rule of decision 0148 would generate

| min_cosine | Candidates per history (mean / median / max) | Update pair is a candidate |
| --- | --- | --- |
| 0.70 | 1,363 / 1,359 / 1,479 | 10 of 18 |
| 0.75 | 1,344 / 1,336 / 1,461 | 10 of 18 |
| 0.80 (default) | 1,074 / 1,089 / 1,155 | 10 of 18 |
| 0.85 | 363 / 352 / 482 | 10 of 18 |

(The candidate counts are for the knowledge-update histories. The other
histories are within 3%.)

**The rule as specified is not selective.** In a 500-turn history, almost
every similar pair of turns shares a content word, differs in some number
or capitalised word, and has a different date. That makes about 1,000
pairs per history at the default floor. The update pair is reached in 10 of
18 questions at every floor. The other 8 are below 0.70 or fail a condition.

Simple filters, measured the same way (`precheck_filters.py`):

| Filter | at 0.80: candidates / reach | at 0.85: candidates / reach |
| --- | --- | --- |
| none (the rule) | 1,087 / 10 of 18 | 370 / 10 of 18 |
| both turns spoken by the user | 375 / 10 | 160 / 10 |
| the two turns share a capitalised name | 183 / 7 | 86 / 7 |
| both of the above | 58 / 7 | **28 / 7** |

## Consequence

An LLM verdict on about 1,000 pairs per history would cost about 20 times
the study's cap for the 107 test histories. It would also ask the model for
a thousand verdicts where one pair matters, which is the setting in which a
false supersession is most likely. The cost projection exceeds the cap
(`PROTOCOL.md`, Stops), so the study stops here for a decision before any
tuning with the LLM.
