# 0141 Context selection defaults: cap items at 150 tokens, except the best two

The defaults of the decision 0137 knobs, chosen by the selection study
(`eval/selection/PROTOCOL.md`) on its tuning set only (`eval/split-tune.json`,
140 questions; e5-small, budget 2,000 tokens). The held-out set was not
run before this commit.

## Decision

| Key | Before | Now |
| --- | --- | --- |
| `read.context_item_max_tokens` | 0 (no cap) | **150** |
| `read.context_truncate` | head | **best_window** |
| `read.context_item_cap_after` | 0 | **2** |
| `read.context_max_per_session`, `context_max_per_document`, `context_show_role` | off | off (unchanged) |

## Evidence (tuning set)

Turn-level evidence recall in the block (`lme evidence`, n = 133 scored;
files in `eval/selection/tune/`):

| Variant | all | multi-session | temporal | preference | single-session-assistant |
| --- | --- | --- | --- | --- | --- |
| plain RAG (B) | 88.5 | 85.3 | 81.1 | 68.8 | 100 |
| before (no cap) | 83.8 | 63.6 | 86.0 | 62.5 | 100 |
| cap 150 on every item, head | 80.1 | 75.0 | 91.9 | 81.2 | 25.0 |
| cap 150 on every item, best_window | 79.3 | 75.0 | 90.5 | 81.2 | 25.0 |
| cap 100, best_window | 56.2 | 46.9 | 61.3 | 87.5 | 12.5 |
| cap 150, best_window, first 1 exempt | 86.4 | 73.1 | 90.5 | 81.2 | 87.5 |
| **cap 150, best_window, first 2 exempt** | **86.6** | 71.0 | 90.5 | 81.2 | 87.5 |
| cap 150, best_window, first 3 exempt | 86.4 | 69.0 | 91.9 | 81.2 | 87.5 |
| cap 200, best_window, first 2 exempt | 85.6 | 69.0 | 91.4 | 68.8 | 87.5 |
| first 2 exempt, plus role label | 86.1 | 70.3 | 89.6 | 81.2 | 87.5 |
| first 2 exempt, at most 3 per session (cap on all) | lower (grid 1) | | | | |

The turn metric undercounts truncated evidence: it requires 60% of the
turn as a contiguous prefix. So reader and judge (deepseek-flash, official
prompts) were run on the four main variants, 140 questions each, at a cost
of US$0.27:

| Variant | accuracy | multi-session | temporal | single-session-assistant |
| --- | --- | --- | --- | --- |
| plain RAG | 88.6% [82.2, 92.8] | 29/37 | 31/37 | 16/16 |
| before | 84.3% [77.4, 89.4] | 23/37 | 33/37 | 16/16 |
| **chosen** | **87.1%** [80.6, 91.7] | 25/37 | 34/37 | 16/16 |
| chosen + role label | 87.1% [80.6, 91.7] | 25/37 | 33/37 | 16/16 |

Paired comparisons:
- chosen against before: 5 questions only the chosen variant answers, 1
  only the old one answers (p = 0.22);
- chosen against plain RAG: 8 against 10 (p = 0.82).

Single-session-assistant accuracy stays at 16/16, so the drop the turn
metric shows there is mostly the metric's prefix rule, not lost facts. The
role label made no difference and costs label tokens, so it stays off. The
role is still stored (0137).

## Not decided here

Multi-session remains the gap to plain RAG (25 against 29 of 37). Whether the
chosen defaults beat plain RAG or the old selection is decided only by the
held-out run (`eval/split-holdout.json`, 300 questions), under the rule fixed
in the protocol.

## Consequences

- Context blocks hold about twice as many items (about 15 instead of 8), at
  about 125 tokens each instead of about 240.
- The default configuration hash changes (4dd590544d868b15).
- Search mode is unchanged.
