# 0147 Learning study: settings frozen on the development set

The settings below were chosen on the development set only (conv-30,
conv-47 and conv-49, the sleep study's spent LoCoMo halves). The measure
was the one without an LLM: full evidence in the block on the dev test
half, then mean turn recall (`eval/learn/tune/grid.md`, `eval/learn/tune.sh`).
None of the 7 test conversations had been run or read when this was
committed (tag `learn-freeze`).

| Key | Frozen | Used by |
| --- | --- | --- |
| `ranking.utility_lambda` | 0.2 | U1, K |
| `ranking.min_exposures` | 5 (the default) | U1, K |
| `read.learned_keys_weight` | 0.5 | K |
| `read.learned_keys_top_k` | 10 | K |

`eval/learn/frozen.set` holds them. `read.learned_keys` is on for K only,
and `write.learned_keys` is on for the base database, so keys exist in
every copy but only K reads them.

## Evidence (development test half, 197 questions)

| Variant | Full evidence | Mean turn recall |
| --- | --- | --- |
| U0 (no learning) | 139 | 77.5% |
| U1, best (λ 0.2, min exposures 5) | 140 | 78.3% |
| U1, λ 0.3, min exposures 1 | 138 | 76.6% |
| K, best (U1 + weight 0.5, top k 10) | 140 | 77.9% |
| K, weight 1, top k 10 | 138 | 77.0% |
| K, weight 2, top k 20 | 115 | 65.9% |
| keys alone (λ 0, weight 1, top k 20) | 137 | 76.3% |

What the grid already says, before the test:

- **Utility** moves full evidence by at most one question out of 197.
- **Keys** add nothing over U1 at the best weight and hurt at higher
  weights. At the frozen setting, keys bring a record U0 did not deliver
  into 163 of 197 blocks: 411 records, only 5 of them evidence. Keys
  match by question similarity, and LoCoMo's questions about the same two
  people resemble each other, so a key points to the records of a
  different question.
- The selection rule picks the best K anyway, because the protocol
  defines K as U1 plus keys. The test decides the claims.
