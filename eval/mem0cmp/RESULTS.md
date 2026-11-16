# New selection (D2) against Mem0 on the battery's 60 questions

This is the only possible comparison with Mem0 after the selection fix:
Mem0 (condition C) was run only on the 60 questions of the 2026-11-14
battery (`eval/sample.json`). D2 is the selection of decision 0141 (config
hash `fb55961138acd36b`, the same as the selection study's held-out D2
run), with e5-small embeddings, a 2,000-token context, and the official
LongMemEval reader and judge (deepseek-flash, temperature 0), exactly as in
the battery. The other rows are the battery's results
(`eval/reports/2026-11-14/raw/`).

| Condition | Accuracy [Wilson 95%] |
| --- | --- |
| A, no memory | 11.7% [5.8, 22.2] (7/60) |
| B, plain RAG | 81.7% [70.1, 89.4] (49/60) |
| **C, Mem0** | **88.3%** [77.8, 94.2] (53/60) |
| D, old selection | 78.3% [66.4, 86.9] (47/60) |
| E, old sleep | 76.7% [64.6, 85.6] (46/60) |
| **D2, new selection** | **80.0%** [68.2, 88.2] (48/60) |

Paired comparisons (exact two-sided sign test):

| Pair | Only D2 right | Only the other right | p |
| --- | --- | --- | --- |
| D2 vs Mem0 | 1 | 6 | 0.125 |
| D2 vs plain RAG | 2 | 3 | 1.0 |
| D2 vs old selection | 1 | 0 | 1.0 |

By type (D2 / Mem0 correct): temporal 9 / 13 of 15, multi-session 8 / 9
of 13. All other types are equal: knowledge-update 8 / 8, single-session
user 7 / 7, single-session assistant 7 / 7, preference 4 / 4,
abstention 6 / 6.

What this shows: on these 60 questions Mem0 remains ahead of Chanda by
8.3 points. The difference is not established (p = 0.125) but points one
way, and it lies almost entirely in temporal questions. These 60 questions
were used to pick the battery's deviations 6 and 7, and 60 is too few to
separate the conditions. The held-out evidence for the new selection
(`eval/selection/RESULTS.md`) does not include Mem0.

Cost: US$0.031 (120 calls; `cost.json`, `llm_calls.jsonl`).
