# Part 0: diagnosis of the battery's sleep (condition E), no LLM

Data: the 2026-11-14 battery (`eval/reports/2026-11-14/raw/{D,E}.jsonl`,
60 questions), the LongMemEval S haystacks, and the E-sleep requests and
responses in the call store (`eval/lme/llmstore/`, 50 of the 60 questions:
the smoke run's 10 predate the store). Script: `diagnose.py`; numbers:
`diagnosis.json`. No LLM call was made.

Evidence matching follows the selection study (`eval/lme/lme/evidence.go`):
a has_answer turn reaches the block when at least 60% of it, as a prefix
and with whitespace removed, appears in the block text. The measure counts
raw turns only. A fact that restates an evidence turn does not count, so
the measure undervalues E (see item 1).

## Summary

- **The old sleep displaced evidence.** E answered 5 of the 9 divergent
  questions worse and 4 better. In 3 of the 5 losses, sleep facts took the
  place of an evidence turn that D had in its block. In one of the three
  (08f4fc43), the two evidence turns were D's top two items. The block
  composition rule of Part 1 is therefore mandatory.
- **The facts help when they arrive.** 3 of the 4 wins came from a fact
  extracted from an evidence session that carried the answer. In the 60
  blocks, 151 of the 183 fact items (83%) came from evidence sessions:
  retrieval already directs at read time.
- **The extraction is undirected.** Of 9,777 facts extracted in 48
  questions, 644 (6.6%) came from evidence sessions and 155 (1.6%) from an
  evidence turn. This is about the evidence share of the events read (4.5%).
  About 95% of the events it read, and so roughly that share of its
  spend, came from sessions that hold no evidence for the question.

How this bears on the hypothesis: it confirms that the sleep works without
direction, and the money shows it. It also says that on this data the lost
accuracy came from **block composition**, not from facts about the wrong
things. A usage-guided sleep should therefore be judged on two things:
whether it gets the useful facts for less money, and whether the facts stop
displacing evidence once the composition rule caps them. Direction alone is
not expected to fix accuracy.

## 1. The 9 questions where D and E differ

Here, "sleep tokens" are the fact items of E's block, in rendered tokens
(ceil(runes/4)), against the raw excerpts. "Displaced" lists evidence turns
that reach D's block and not E's.

| Question | Type | D | E | Sleep status | Sleep items / tokens | Share of block | Evidence turns reached D → E | Displaced |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt4_385a5000 | temporal | ✓ | ✗ | completed | 4 / 156 | 8.1% | 2 → 1 | s47t0 (D item 4, 101 tok) |
| gpt4_9a159967 | temporal | ✓ | ✗ | completed | 4 / 187 | 9.7% | 3 → 2 | s6t2 (D item 6, 148 tok) |
| 08f4fc43 | temporal | ✓ | ✗ | completed | 6 / 239 | 12.4% | 2 → 0 | s23t0, s27t0 (D items 1 and 2) |
| 1d4e3b97 | preference | ✓ | ✗ | completed | 0 / 0 | 0% | 1 → 0 | s40t4 (no fact in the block; see below) |
| gpt4_468eb063 | temporal | ✓ | ✗ | partial | 6 / 260 | 13.5% | 1 → 1 | none |
| 2c63a862 | temporal | ✗ | ✓ | partial | 2 / 80 | 4.2% | 0 → 0 | none |
| gpt4_f420262d | temporal | ✗ | ✓ | completed | 2 / 84 | 4.4% | 0 → 0 | none |
| 91b15a6e | multi-session | ✗ | ✓ | completed | 1 / 41 | 2.1% | 1 → 1 | none |
| gpt4_2f56ae70 | temporal | ✗ | ✓ | completed | 6 / 252 | 13.1% | 0 → 0 | none |

The losses:

- **08f4fc43** ("days between the Sunday mass at St. Mary's and the Ash
  Wednesday service"). D's top two items were the two evidence turns, both
  dated. In E, six facts lead the block: one about the sermon topic at St.
  Mary's, four about a trip to Europe, one about the Parthenon. Both
  evidence turns are gone.
- **gpt4_385a5000** and **gpt4_9a159967**: four facts (156 and 187 tokens)
  come first, and one short evidence turn is cut from the end of the
  budget. The facts are from the evidence sessions but do not carry the
  date or airline the question needs.
- **1d4e3b97**: E's block holds no fact, yet its excerpts differ from D's
  and the evidence turn is missing. The sleep's facts probably took
  candidate slots before the cut (the top 50 per signal, the rerank window)
  without reaching the block. This cannot be checked: the E databases were
  deleted after the battery.
- **gpt4_468eb063**: the evidence reaches both blocks. E's answer is wrong
  for another reason.

The wins: 2c63a862 (fact "Rachel working_relationship_start_date: February
15th"), 91b15a6e ("antique vanity planned_sale_price: at least $150") and
gpt4_2f56ae70 ("Disney+ free trial last month") each answered from a
fact. gpt4_f420262d's facts do not hold the answer (Spirit Airlines, where
the answer is American Airlines). Its win is reader variance.

Across all 60 blocks, sleep items took 6.8% of the block tokens on average.

## 2. Full evidence in the block, D against E (60 questions)

57 questions have evidence turns (the 3 others are abstention questions
without has_answer turns).

| | Full evidence | Mean turn recall |
| --- | --- | --- |
| D | 40 of 57 | 76.4 |
| E | 35 of 57 | 69.6 |

Paired: both 35, D only 5, E only 0, neither 17. Exact two-sided sign test:
p = 0.0625. The sleep never brought a missing evidence turn into the block.
It removed one in 5 questions.

## 3. E against D by how the sleep ended

The premise was 35 runs that finished and 25 that stopped at the token
ceiling. The logs show that the 25 partial runs split into 23 that hit the
**event** ceiling (500 events per run; the 200,000-token budget was never
reached) and 2 that stopped on a DeepSeek 502.

| Group | n | D correct | E correct | E only | D only |
| --- | --- | --- | --- | --- | --- |
| completed | 35 | 27 | 26 | 3 | 4 |
| partial, event ceiling | 23 | 18 | 18 | 1 | 1 |
| partial, provider error | 2 | 2 | 2 | 0 | 0 |
| partial, all | 25 | 20 | 20 | 1 | 1 |

There is no difference between the groups. Finishing did not help. The
ceiling did not hurt either: in 20 of the 21 partial runs with recorded
calls, the sleep had read every evidence session before it stopped, because
it reads in an order that has nothing to do with use.

## 4. Where the sleep's facts came from (48 questions)

These counts come from the recorded extraction calls. Calls were assigned to
questions by the sleep run's time window, then by text: more than half of a
call's events must match a turn of that question's haystack. 1,212 calls
were assigned and 11 were not. Two questions have no assigned call, and
the smoke run's 10 have no record. The extracted total (9,777) matches the
sleep reports' "facts from extraction" (9,801) to 0.3%.

| | Count | Share |
| --- | --- | --- |
| Events read by the sleep | 23,271 | |
| from evidence sessions | 1,055 | 4.5% |
| not matched to a turn (text changed at ingest) | 1,447 | 6.2% |
| Facts extracted | 9,777 | |
| with an origin in an evidence session | 644 | 6.6% |
| with an origin in an evidence turn (has_answer) | 155 | 1.6% |
| with an origin in a filler session | 8,644 | 88.4% |
| with no matched origin | 489 | 5.0% |
| Invalid JSON answers | 59 calls | 4.9% |

Evidence sessions read: 97 of 98. Questions whose evidence sessions were
all read: 47 of 48 (20 of the 21 partial runs).

What this means for cost: at the battery's US$1.63 for the E sleep, about
US$1.50 went to facts from filler sessions. Of those, few reached a block,
because retrieval rarely returns filler.

## Consequences for Part 1

1. The block composition rule is mandatory: at most 30% of the budget for
   sleep items, and the two best-ranked raw items are never displaced
   (08f4fc43 is the case it prevents).
2. A sleep item should earn its place by holding what the question needs
   (a date, a count, a value). The extractive ficha keeps the literal,
   dated line, which is what the losses missed.
3. A sleep guided by use should cost a small fraction of the old one, since
   about 95% of the old spend went to filler sessions.
4. Facts can also displace raw items before the cut, as in 1d4e3b97, so
   the composition rule must apply to the selection, not only to the
   rendering.
