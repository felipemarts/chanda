# Learning study: development tuning grid (no LLM)

Dev conversations conv-30, conv-47, conv-49, test half of `eval/sleep/split-locomo.json`, asked on a copy of the sleep study's base database per variant (`eval/learn/tune.sh`). Full evidence: scored questions whose every valid evidence turn reached the block; recall: mean share of evidence turns reached.

| Variant | Overrides | Full evidence (pooled) | Mean turn recall | conv-30 full / recall | conv-47 full / recall | conv-49 full / recall | Key brought a non-U0 record (share) | Records, of them evidence |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| U0 | ranking.utility_lambda=0 | 139/197 | 77.5% | 31/41 / 81.5% | 55/76 / 79.4% | 53/80 / 73.6% | - | - |
| U1-l0.1-m1 | ranking.utility_lambda=0.1, ranking.min_exposures=1 | 139/197 | 77.8% | 31/41 / 81.5% | 55/76 / 79.4% | 53/80 / 74.4% | - | - |
| U1-l0.1-m2 | ranking.utility_lambda=0.1, ranking.min_exposures=2 | 139/197 | 77.8% | 31/41 / 81.5% | 55/76 / 79.4% | 53/80 / 74.4% | - | - |
| U1-l0.1-m5 | ranking.utility_lambda=0.1, ranking.min_exposures=5 | 140/197 | 78.2% | 31/41 / 81.5% | 55/76 / 79.4% | 54/80 / 75.4% | - | - |
| U1-l0.2-m1 | ranking.utility_lambda=0.2, ranking.min_exposures=1 | 139/197 | 77.9% | 31/41 / 81.5% | 54/76 / 78.1% | 54/80 / 75.9% | - | - |
| U1-l0.2-m2 | ranking.utility_lambda=0.2, ranking.min_exposures=2 | 140/197 | 78.2% | 31/41 / 81.5% | 55/76 / 78.7% | 54/80 / 75.9% | - | - |
| U1-l0.2-m5 **(best U1)** | ranking.utility_lambda=0.2, ranking.min_exposures=5 | 140/197 | 78.3% | 31/41 / 81.5% | 55/76 / 79.4% | 54/80 / 75.6% | - | - |
| U1-l0.3-m1 | ranking.utility_lambda=0.3, ranking.min_exposures=1 | 138/197 | 76.6% | 31/41 / 80.9% | 56/76 / 79.4% | 51/80 / 71.8% | - | - |
| U1-l0.3-m2 | ranking.utility_lambda=0.3, ranking.min_exposures=2 | 138/197 | 77.1% | 31/41 / 80.9% | 56/76 / 80.7% | 51/80 / 71.8% | - | - |
| U1-l0.3-m5 | ranking.utility_lambda=0.3, ranking.min_exposures=5 | 140/197 | 77.7% | 31/41 / 80.9% | 56/76 / 79.4% | 53/80 / 74.5% | - | - |
| K-alone | ranking.utility_lambda=0, read.learned_keys=true | 137/197 | 76.3% | 31/41 / 80.3% | 55/76 / 78.9% | 51/80 / 71.8% | 197/197 (100.0%) | 1878, 18 |
| K-w0.5-k10 **(best K)** | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=0.5, read.learned_keys_top_k=10 | 140/197 | 77.9% | 32/41 / 82.1% | 55/76 / 79.4% | 53/80 / 74.3% | 163/197 (82.7%) | 411, 5 |
| K-w0.5-k20 | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=0.5, read.learned_keys_top_k=20 | 139/197 | 77.4% | 31/41 / 79.7% | 55/76 / 79.4% | 53/80 / 74.3% | 181/197 (91.9%) | 608, 8 |
| K-w1-k10 | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=1, read.learned_keys_top_k=10 | 138/197 | 77.0% | 30/41 / 77.2% | 56/76 / 80.3% | 52/80 / 73.8% | 197/197 (100.0%) | 1927, 16 |
| K-w1-k20 | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=1, read.learned_keys_top_k=20 | 139/197 | 77.6% | 31/41 / 79.7% | 56/76 / 80.3% | 52/80 / 73.9% | 197/197 (100.0%) | 2178, 19 |
| K-w2-k10 | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=2, read.learned_keys_top_k=10 | 135/197 | 74.8% | 30/41 / 76.4% | 54/76 / 77.0% | 51/80 / 72.0% | 197/197 (100.0%) | 2532, 17 |
| K-w2-k20 | ranking.utility_lambda=0.2, ranking.min_exposures=5, read.learned_keys=true, read.learned_keys_weight=2, read.learned_keys_top_k=20 | 115/197 | 65.9% | 27/41 / 69.1% | 49/76 / 71.7% | 39/80 / 58.7% | 197/197 (100.0%) | 3848, 22 |

Selection rule (a suggestion; the freeze is decided by hand): most full-evidence questions, then higher mean turn recall, then closer to Chanda's defaults. K is U1's best plus keys (PROTOCOL.md: K = U1 plus learned keys); the keys-alone variant (λ 0) is reported, not selectable.

Suggested `eval/learn/frozen.set` (every key explicit; `run_learn.sh` refuses a missing one):

```
ranking.utility_lambda=0.2
ranking.min_exposures=5
read.learned_keys_weight=0.5
read.learned_keys_top_k=10
```
