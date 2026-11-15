# Context-block selection study: results

Protocol: `PROTOCOL.md` (pre-registered in 6940e4e, before any run).
Settings chosen on the 140 tuning questions only (decision 0141, commit
d7b7e8e, tag `selection-freeze`), then one run on the 300 held-out
questions (`eval/split-holdout.json`) with that build. Embeddings
e5-small; reader and judge deepseek-flash with the official LongMemEval
prompts; budget 2,000 tokens for every condition.

## Conditions

- **B**: plain RAG (1,000-rune chunks, e5, exact top-k), unchanged since
  the battery.
- **D**: Chanda with the old selection: no item cap, head truncation. Same
  build as D2, with the old values set through overrides
  (`read.context_item_max_tokens=0`, `read.context_truncate=head`,
  `read.context_item_cap_after=0`).
- **D2**: Chanda with the decision 0141 defaults: items capped at 150 tokens
  by best_window, the two best-ranked items exempt.

## Held-out result (n = 300, run once)

| | Accuracy [Wilson 95%] | multi-session (80) | temporal (80) | knowledge-update (47) | single-session-user (42) | single-session-assistant (33) | preference (18) | abstention (16, included) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B plain RAG | 77.7% [72.6, 82.0] | 55 | 58 | 35 | 38 | 33 | 14 | 14 |
| D old selection | 78.0% [73.0, 82.3] | 48 | 63 | 38 | 39 | 33 | 13 | 15 |
| **D2 new selection** | **82.7%** [78.0, 86.5] | **58** | 62 | **41** | **41** | 32 | 14 | 14 |

Paired outcomes (questions only one of the two answered correctly; exact
two-sided sign test):

| Pair | first only | second only | p | Rule of the protocol (p < 0.05) |
| --- | --- | --- | --- | --- |
| D2 vs D | 24 | 10 | **0.024** | **met: the new selection is more accurate than the old one** |
| D2 vs B | 35 | 20 | 0.058 | **not met**: the difference favours D2 but is not established |
| D vs B | 27 | 26 | 1.0 | no difference |

Turn-level evidence recall in the block (no LLM; 289 questions with
evidence turns):

| | Mean turn recall | Full recall | Items per block | Tokens per item |
| --- | --- | --- | --- | --- |
| B | 78.4 | 68.5% | 15.5 | 122 |
| D | 79.0 | 68.2% | 7.5 | 257 |
| D2 | 83.7 | 75.1% | 14.7 | 130 |

Full recall, paired: D2 against B is 40 against 21 (p = 0.020); D against B
is 32 against 33.

Cost: US$0.72 for the whole study, all off-peak: US$0.27 on the tuning
set and US$0.45 on the held-out set. There were 2,924 calls; 4 more were
answered from the call store.

## What it shows

- The fix works where the battery said it would. Capping long items puts
  twice as many turns in the block. Multi-session goes from 48 to 58 of 80
  and knowledge-update from 38 to 41 of 47. The gain over the old selection
  is established on questions never used to choose it.
- Against plain RAG, Chanda now scores 5 points higher (82.7% against
  77.7%). The paired test (35 against 20, p = 0.058) misses the
  pre-registered bar, so "Chanda is more accurate than plain RAG" is **not**
  established. What the data supports is "at least as accurate, with a
  difference in its favour that 300 questions do not settle".
- Old Chanda and plain RAG are tied on this set (78.0% against 77.7%). The
  battery's 60-question gap (78.3% against 81.7%) was within noise, as it
  reported.
- Single-session-assistant lost one question (33 to 32). This is the risk
  the tuning grid showed: truncation can cut a fact out of a long assistant
  reply.

## Caveats

- One run per condition; automatic judge; English conversational data;
  one reader model.
- The held-out set is easier for nobody in particular, but it is harder
  than the tuning set: plain RAG got 88.6% on tuning and 77.7% here.
  Compare conditions within a set, not across sets.
- Mem0 was not run on these questions (cost). The battery's Mem0 result,
  88.3% on 60 questions, is not comparable to these numbers.
- D runs on the same build as D2, which includes the lexical near-duplicate
  rule (decision 0138) and the stored role. It reproduces the battery's
  selection, not the battery's exact binary.

## Files

- `holdout/llm-<cond>/<B|D>.jsonl`: one line per question, with the
  context, answer, verdict and tokens. `report.txt` holds accuracy per type.
- `holdout/evidence.txt`, `.json`: turn-level evidence recall.
- `tune/`: both tuning grids and the reader and judge runs on the tuning set.
- `cost.json`, `llm_calls.jsonl`: every call of the study.
