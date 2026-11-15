# Context-block selection study: protocol (pre-registered)

Written and committed on 2026-11-15, together with the question split and
before any of the 440 new questions was run or looked at.

## Why

The LongMemEval battery of 2026-11-14 (`eval/reports/2026-11-14/REPORT.md`)
found that Chanda's search ranks the evidence as well as plain RAG
(recall@10 98.3 against 97.9 over 500 questions). Accuracy was nonetheless
lower (78.3% against 81.7% and 88.3% for Mem0, n = 60). In 8 of the 10
errors analysed, the cause was the 2,000-token block: long assistant replies
used up the budget and the short user turns that carried the fact were
dropped. The 60 questions of that battery were used to pick deviations 6 and
7, so they cannot give the final result.

## Question sets (`eval/lme/split.py`, seed 43)

| Set | File | n | Use |
| --- | --- | --- | --- |
| Battery sample | `eval/sample.json` | 60 | not used to choose anything below |
| Tuning | `eval/split-tune.json` | 140 (8 abstention) | may be read, run and analysed as often as needed |
| Held out | `eval/split-holdout.json` | 300 (16 abstention) | run once, after every setting is frozen and committed |

## What may change

Chanda's context-mode selection: the per-item token cap, the truncation of
long items, the per-session item limit, the role shown in the label, and
the cut. Search ranking, the embedder (e5-small), the reader and judge
(deepseek-flash, official prompts), the 2,000-token budget, the token
estimator and plain RAG stay as they were in the battery.

## Measures

- Primary, no LLM: **turn-level evidence recall in the block**. This is the
  share of a question's evidence turns (`has_answer: true`) whose text
  reaches the block. Scored questions are those with at least one such turn.
- Final: answer accuracy with the official reader and judge, with Wilson
  95% intervals and a paired exact sign test against plain RAG and against
  the battery's Chanda (D).

## Procedure

1. Settings are chosen on the tuning set only, from the no-LLM measure.
   Reader and judge may also be run on it (about US$0.0005 per question and
   condition).
2. The chosen settings are frozen in a commit, with a decision record.
3. On the held-out set, plain RAG (B), the battery's Chanda (D, the frozen
   tag of the battery plus its fixes) and the new Chanda (D2) run once.
   Mem0 does not run there (cost).
4. The result is reported whatever it is.

## Decision rule (fixed now)

- **"D2 is more accurate than plain RAG"** may be claimed only if, on the
  held-out set, D2's accuracy exceeds B's and the paired exact sign test
  gives p < 0.05.
- The same rule applies to D2 against D.
- Otherwise the report says the difference is not established and gives
  the numbers.

## Budget

LLM calls go through `lmeproxy` with its own cost file
(`eval/selection/cost.json`), a cap of US$3, off-peak only, and the call
store on (`-store eval/lme/llmstore -replay`).
