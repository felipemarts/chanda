## Accuracy on the test half (3 conversations pooled)

| Condition | Total | multi-hop (1) | temporal (2) | open-domain (3) | single-hop (4) | Conversations |
| --- | --- | --- | --- | --- | --- | --- |
| B | 66.0% [59.1, 72.2] (130/197) | 51.4% [35.6, 67.0] (18/35) | 53.2% [39.2, 66.7] (25/47) | 35.7% [16.3, 61.2] (5/14) | 81.2% [72.5, 87.6] (82/101) | conv-30, conv-47, conv-49 |
| S0 | 70.1% [63.3, 76.0] (138/197) | 45.7% [30.5, 61.8] (16/35) | 57.4% [43.3, 70.5] (27/47) | 50.0% [26.8, 73.2] (7/14) | 87.1% [79.2, 92.3] (88/101) | conv-30, conv-47, conv-49 |
| S1 | 66.0% [59.1, 72.2] (130/197) | 31.4% [18.6, 48.0] (11/35) | 57.4% [43.3, 70.5] (27/47) | 57.1% [32.6, 78.6] (8/14) | 83.2% [74.7, 89.2] (84/101) | conv-30, conv-47, conv-49 |
| S2 | 66.5% [59.6, 72.7] (131/197) | 34.3% [20.8, 50.8] (12/35) | 55.3% [41.2, 68.6] (26/47) | 64.3% [38.8, 83.7] (9/14) | 83.2% [74.7, 89.2] (84/101) | conv-30, conv-47, conv-49 |
| S3 | 68.0% [61.2, 74.1] (134/197) | 45.7% [30.5, 61.8] (16/35) | 46.8% [33.3, 60.8] (22/47) | 57.1% [32.6, 78.6] (8/14) | 87.1% [79.2, 92.3] (88/101) | conv-30, conv-47, conv-49 |

Accuracy with Wilson 95% interval (correct/n).

## Paired against S0 (exact two-sided sign test)

| Pair | n | first only | S0 only | p | Decision rule (higher than S0 and p < 0.05) |
| --- | --- | --- | --- | --- | --- |
| S1 vs S0 | 197 | 10 | 18 | 0.185 | not established |
| S2 vs S0 | 197 | 13 | 20 | 0.296 | not established |
| S3 vs S0 | 197 | 17 | 21 | 0.627 | — |
| B vs S0 | 197 | 21 | 29 | 0.322 | — |

S1 and S2 are each tested on their own; no correction for the two tests is applied.

## Full evidence in the block (test half, no LLM)

| Condition | Scored | Full evidence | Full, crediting citing fichas | Turns reached | Turns reached or cited |
| --- | --- | --- | --- | --- | --- |
| B | 197 | 143 | 143 | 228/344 | 228/344 |
| S0 | 197 | 139 | 139 | 219/344 | 219/344 |
| S1 | 197 | 128 | 132 | 197/344 | 204/344 |
| S2 | 197 | 127 | 132 | 195/344 | 201/344 |
| S3 | 197 | 130 | 130 | 201/344 | 201/344 |

## Sleep per mode and conversation

| Condition | Conversation | Status | LLM calls | Tokens in/out | Cost US$ | Time s | Groups (cand./merged/digested/skipped/left) | Lines written | Discarded (by reason) | Fichas |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| S0 | conv-30 | completed | 0 | 0/0 | 0.0000 (priced) | 0.1 | -/-/-/-/- | - | - | 0 |
| S0 | conv-47 | completed | 0 | 0/0 | 0.0000 (priced) | 0.2 | -/-/-/-/- | - | - | 0 |
| S0 | conv-49 | completed | 0 | 0/0 | 0.0000 (priced) | 0.2 | -/-/-/-/- | - | - | 0 |
| S1 | conv-30 | completed | 0 | 0/0 | 0.0000 (priced) | 10.2 | 40/34/33/1/0 | 329 | 7 {"repeated": 7} | 33 |
| S1 | conv-47 | partial | 0 | 0/0 | 0.0000 (priced) | 0.5 | 74/73/49/0/24 | 492 | 0 | 49 |
| S1 | conv-49 | partial | 0 | 0/0 | 0.0000 (priced) | 15.6 | 76/70/49/0/21 | 496 | 0 | 49 |
| S2 | conv-30 | completed | 33 | 40339/20585 | 0.0171 | 117.2 | 40/34/33/1/0 | 291 | 60 {"unsupported_name": 60} | 33 |
| S2 | conv-47 | partial | 49 | 56170/30031 | 0.0246 | 164.8 | 74/73/49/0/24 | 433 | 81 {"unsupported_name": 81} | 49 |
| S2 | conv-49 | partial | 49 | 59997/32632 | 0.0533 | 191.1 | 76/70/49/0/21 | 482 | 44 {"unsupported_name": 44} | 49 |
| S3 | conv-30 | completed | 23 | 43479/8653 | 0.0218 | 61.1 | -/-/-/-/- | - | - | 0 |
| S3 | conv-47 | partial | 31 | 67904/16298 | 0.0376 | 97.7 | -/-/-/-/- | - | - | 0 |
| S3 | conv-49 | completed | 28 | 58875/12044 | 0.0301 | 77.7 | -/-/-/-/- | - | - | 0 |

## Gain per dollar

| Condition | Gain over S0 (points) | Sleep cost US$ | Points per US$ |
| --- | --- | --- | --- |
| S1 | -4.1 | 0.0000 | n/a (no LLM cost) |
| S2 | -3.6 | 0.0949 | -37.4 |

## Share of the block taken by sleep items (facts and pages, test half)

| Condition | Blocks | With sleep items | Mean share | Max share | Pooled share |
| --- | --- | --- | --- | --- | --- |
| B | 197 | 0 | 0.0% | 0.0% | 0.0% |
| S0 | 197 | 0 | 0.0% | 0.0% | 0.0% |
| S1 | 197 | 197 | 29.2% | 47.5% | 29.2% |
| S2 | 197 | 197 | 29.7% | 48.9% | 29.7% |
| S3 | 197 | 197 | 27.4% | 30.8% | 27.5% |

## Ceiling: use half asked again after sleep (memorisation, not transfer)

| Condition | Accuracy |
| --- | --- |
| before sleep (base) | 67.9% [61.0, 74.1] (129/190) |
| S0 | 68.4% [61.5, 74.6] (130/190) |
| S1 | 66.8% [59.9, 73.1] (127/190) |
| S2 | 67.4% [60.4, 73.6] (128/190) |
| S3 | 68.9% [62.0, 75.1] (131/190) |

Unparsed judge verdicts (scored WRONG): 0.
Proxy spend: US$1.6528 of the US$2 cap; replayed calls 977.
