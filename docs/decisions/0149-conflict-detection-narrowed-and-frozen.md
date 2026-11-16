# 0149 Conflict detection narrowed to user turns, and the study settings frozen

This revises decision 0148. Part 0 of the conflict study
(`eval/conflicts/PRECHECK.md`) found that 0148's detection rule is not
selective. It produced about 1,000 candidate pairs per 500-turn history at
the default floor and reached the outdated→current pair in only 10 of 18
development questions. An LLM verdict on every one of those pairs would
cost about 20 times the study's cap. The user chose the option of a
stricter detection, tuned on the development set only.

## Change to detection

Two new keys, both `omitempty` and off by default:

| Key | Default | Meaning |
| --- | --- | --- |
| `sleep.conflicts.user_only` | `false` | A pair forms only between user turns, or events that are not turns (documents, statements). An assistant, system or tool turn never pairs. |
| `sleep.conflicts.require_entity` | `false` | The two records must share a signature entity, not only a content term. |

Both are conditions 6 and 7 of `conflicts.Detect`, applied at write time
and in `scan`. Tests: `TestDetectUserOnlyAndRequireEntity`.

## Detection reach on the development set (no LLM, `conflicts scan`)

| Rule | Pairs per history (mean / max) | Outdated→current pair reached |
| --- | --- | --- |
| 0148, min_cosine 0.80 | 1,087 / 1,155 | 10 of 18 |
| user_only + require_entity, 0.80 | 91 / 158 | 7 of 18 |
| user_only + require_entity, 0.88 | 9.1 / 22 | 7 of 18 |
| **user_only, 0.90** | **10.3 / 33** | **8 of 18** |
| user_only, 0.92 | 2.7 / 17 | 5 of 18 |

Every outdated→current pair the rule reaches has a cosine of 0.90 or more.

## Verdict settings (development set, LLM; `eval/conflicts/tune/grid.md`)

Here C0 is the D2 block on the base and C1 the block after maintenance
sleep. The stale measure covers 15 questions. Hidden evidence counts
questions whose needed evidence turn was dropped as superseded. Applied
supersessions are classed against the knowledge-update ground truth.

| context_turns | min_confidence | Outdated delivered C0 → C1 (p) | Hidden evidence | Applied: correct / unverified / wrong |
| --- | --- | --- | --- | --- |
| 1 | 0.70 | 14 → 7 (0.016) | 4 | 6 / 3 / 8 |
| 1 | 0.85 | 14 → 8 (0.031) | 2 | 5 / 2 / 6 |
| 1 | 0.90 | 14 → 8 (0.031) | 2 | 5 / 1 / 6 |
| **1** | **0.95** | **14 → 12 (0.50)** | **0** | **2 / 0 / 1** |
| 2 | 0.85 | 14 → 9 (0.062) | 2 | 5 / 4 / 10 |
| 2 | 0.90 | 14 → 9 (0.062) | 2 | 5 / 3 / 10 |
| 2 | 0.95 | 14 → 13 (1.0) | 0 | 0 / 0 / 1 |

(`context_turns: 0` means the default of 1, so a grid point at 0 repeated
the point at 1; it was dropped.)

**Selection rule.** The protocol allows hidden evidence in at most 2 of the
107 test questions. Scaled to the 52 development questions, that is at most
1. Among the points that meet this, take the largest stale reduction. Only
the points at 0.95 meet it, and context 1 reduces more than context 2. This
also follows the study's principle that a wrong supersession is worse than
none. The cost is a small stale reduction on the development set
(14 → 12, not significant). The test decides.

## Frozen (`eval/conflicts/frozen.set`, tag `conflicts-freeze`)

```
sleep.conflicts.user_only=true
sleep.conflicts.min_cosine=0.90
sleep.conflicts.context_turns=1
sleep.conflicts.min_confidence=0.95
sleep.conflicts.max_pairs=40   # a cost guard; the development maximum was 33 pairs
```

`max_per_record` keeps its default of 3. The development spend was US$0.30
of the study's US$2 cap.

## Outcome (`eval/conflicts/RESULTS.md`)

On the 107 test questions, hidden evidence was 0 and accuracy did not
drop (89 → 88, p = 1.0). Outdated information delivered fell only from 30
to 29 of 31 (p = 1.0), so the pre-registered claim was not established.
`sleep.conflicts` stays off by default, and no other variant is built.
