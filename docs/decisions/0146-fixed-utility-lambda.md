# 0146 A fixed λ for stage-1 utility (off by default)

The learning study's Part 0 (`eval/learn/PRECHECK.md`) showed that utility
never changed a block in the sleep study. λ leaves 0 only when a learned
model is promoted through the gate set (BASE.md "Promotion by
evaluation"), and a database with no approved gate questions can never
promote one. To measure whether utility helps at all, this record adds a
fixed λ that applies the stage-1 utility directly, without a learned model
and without the gate.

## Keys

| Key | Default | Meaning |
| --- | --- | --- |
| `ranking.utility_lambda` | 0 (off) | When > 0, read step 7 uses this λ and the stage-1 utility as its term: final = (1 − λ)·relevance + λ·u. Here u is the posterior mean α/(α+β) of a record with at least `ranking.min_exposures` exposures, and 0.5 (neutral) for every other record. Range [0, `ranking.lambda_max`]. |

With `utility_lambda` > 0 the promoted model's term is not used. The
learned path and this one are alternatives, and the fixed one wins. The
runtime switch `ranking.enabled` still turns both off. Utility, as before,
never enters the relevance cut.

Off by default, `omitempty`; the default configuration hash is
unchanged. λ and `min_exposures` for the study are chosen on the
development set (decision 0147).
