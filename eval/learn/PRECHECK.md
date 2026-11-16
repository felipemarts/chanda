# Part 0: checks on the development set (no LLM)

Development set: the sleep study's LoCoMo conversations conv-30, conv-47
and conv-49 (`eval/sleep/`), whose use and test halves are already spent.
Script: `precheck.py`; numbers: `precheck.json`. The only computation was
one `lme study ask` of each test half on a copy of the base database (e5
embeddings, no LLM call).

## 1. Did utility have any effect in the sleep study?

**No.** In S0 (and in every other condition) λ was **0** for the whole
test half, so the final score was the relevance score and utility changed
no ranking.

| Conversation | λ in effect | `ranking.min_exposures` | Utility rows | Rows with ≥ 5 exposures | Max exposures | Learned model | Promoted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| conv-30 | 0 | 5 | 312 | 104 | 15 | logreg, 1,223 examples | no |
| conv-47 | 0 | 5 | 574 | 183 | 30 | logreg, 2,390 examples | no |
| conv-49 | 0 | 5 | 444 | 186 | 36 | logreg, 2,268 examples | no |

Why λ stayed at 0. The suspected cause was too few exposures, and that is
not it: a third or more of the utility rows passed the minimum. Utility
enters the final score only through the term of a **promoted** learned
model (`internal/read/rank.go`, `finalScore`). λ leaves 0 only when
`rank.Promote` finds the learned model better than the baseline on the
**gate set** (`internal/rank/promote.go`). The study databases have no
approved gate questions, so both scores were 0. The promotion gate failed
(`gate_failed`), and λ stayed at 0 whatever the exposures. The
evaluation score stored with each model was also below its baseline score
(`ranker_models`; conv-30: 0.563 against 0.602).

Block check. The test half was asked again on a copy of each base
database that never ran phase 1, and its blocks were compared with S0's:

| Conversation | Test questions | Same items, same order, same text |
| --- | --- | --- |
| conv-30 | 41 | 41 |
| conv-47 | 76 | 76 |
| conv-49 | 80 | 80 |

All 197 blocks are identical. Phase 1 had no effect on what S0 delivered.

## 2. Overlap ceiling

The question: for what share of test-half questions was at least one
evidence turn marked used (`outcomes.used_ids`, the mechanical rule of the
sleep protocol) by some use-half question?

| Conversation | Test questions | Evidence used by a use question | …in a positive outcome | Evidence delivered to a use question |
| --- | --- | --- | --- | --- |
| conv-30 | 41 | 9 | 9 | 38 |
| conv-47 | 76 | 24 | 22 | 68 |
| conv-49 | 80 | 47 | 44 | 77 |
| **total** | **197** | **80 (40.6%)** | **75 (38.1%)** | 183 (92.9%) |

The ceiling is **40.6%**, above the 10% stop line, so the work goes on.
Keys are created only from positive outcomes, so the ceiling for learned
keys is 38.1%.

A tighter ceiling, the one that bounds a gain in accuracy:

- 58 test questions were missing an evidence turn in S0's block. In 25
  of them, a missing turn had been used in a positive outcome.
- 59 test questions were wrong in S0. In **18** of them (9.1% of the test
  half), a missing evidence turn had been used in a positive outcome.
  Bringing evidence into the block can correct at most those 18. The
  other wrong answers either had all their evidence in the block already
  or are missing turns that no use question touched.

So on this data, a learned-key gain above about 9 points (18 of 197) would
be too good for the measured ceiling and would trigger the leak check
first.

## Consequences for Part 1

- **Utility.** To let it act at all, U1 needs a λ that does not depend on
  the gate set. Part 1 adds `ranking.utility_lambda`: a fixed λ whose term
  is the stage-1 utility score, used for records with at least
  `ranking.min_exposures` exposures. It is off by default, and its values
  are chosen on this development set.
- **Keys.** The work goes on (ceiling 38.1%, at least 10%).
