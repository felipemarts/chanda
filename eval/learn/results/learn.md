## Accuracy on the test half (7 conversations pooled)

| Condition | Total | multi-hop (1) | temporal (2) | open-domain (3) | single-hop (4) |
| --- | --- | --- | --- | --- | --- |
| B | 64.7% [60.8, 68.5] (378/584) | 50.5% [41.2, 59.7] (55/109) | 46.1% [37.3, 55.2] (53/115) | 35.1% [21.8, 51.2] (13/37) | 79.6% [74.8, 83.6] (257/323) |
| U0 | 67.3% [63.4, 71.0] (393/584) | 51.4% [42.1, 60.6] (56/109) | 50.4% [41.4, 59.4] (58/115) | 27.0% [15.4, 43.0] (10/37) | 83.3% [78.8, 87.0] (269/323) |
| U1 | 66.3% [62.3, 70.0] (387/584) | 51.4% [42.1, 60.6] (56/109) | 44.3% [35.6, 53.5] (51/115) | 29.7% [17.5, 45.8] (11/37) | 83.3% [78.8, 87.0] (269/323) |
| K | 67.6% [63.7, 71.3] (395/584) | 54.1% [44.8, 63.2] (59/109) | 48.7% [39.8, 57.7] (56/115) | 37.8% [24.1, 53.9] (14/37) | 82.4% [77.8, 86.1] (266/323) |

Accuracy with Wilson 95% interval (correct/n).

### By conversation

| Condition | conv-26 | conv-41 | conv-42 | conv-43 | conv-44 | conv-48 | conv-50 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| B | 66.2% [55.1, 75.8] (51/77) | 64.9% [53.8, 74.7] (50/77) | 51.5% [41.9, 61.0] (52/101) | 67.8% [57.6, 76.5] (61/90) | 67.7% [55.4, 78.0] (42/62) | 69.1% [59.3, 77.4] (67/97) | 68.8% [57.9, 77.8] (55/80) |
| U0 | 72.7% [61.9, 81.4] (56/77) | 72.7% [61.9, 81.4] (56/77) | 61.4% [51.6, 70.3] (62/101) | 62.2% [51.9, 71.5] (56/90) | 67.7% [55.4, 78.0] (42/62) | 63.9% [54.0, 72.8] (62/97) | 73.8% [63.2, 82.1] (59/80) |
| U1 | 63.6% [52.5, 73.5] (49/77) | 70.1% [59.2, 79.2] (54/77) | 65.3% [55.7, 73.9] (66/101) | 63.3% [53.0, 72.6] (57/90) | 74.2% [62.1, 83.4] (46/62) | 58.8% [48.8, 68.0] (57/97) | 72.5% [61.9, 81.1] (58/80) |
| K | 67.5% [56.5, 76.9] (52/77) | 72.7% [61.9, 81.4] (56/77) | 67.3% [57.7, 75.7] (68/101) | 62.2% [51.9, 71.5] (56/90) | 71.0% [58.7, 80.8] (44/62) | 62.9% [53.0, 71.8] (61/97) | 72.5% [61.9, 81.1] (58/80) |

## Decision rules (exact two-sided sign test on the paired outcomes)

| Claim | Pair | n | first only | second only | p | Decision (higher accuracy and p < 0.05) |
| --- | --- | --- | --- | --- | --- | --- |
| A: Chanda is more accurate than plain RAG | U0 vs B | 584 | 72 | 57 | 0.218 | not established |
| B: learning from use improves later answers | K vs U0 | 584 | 31 | 29 | 0.897 | not established |

Claim A, "Chanda is more accurate than plain RAG": not established (U0 67.3% vs B 64.7%, 72 vs 57 discordant, p = 0.218).
Claim B, "learning from use improves later answers": not established (K 67.6% vs U0 67.3%, 31 vs 29 discordant, p = 0.897).

Claim B is not established. No other learning variant is proposed or built; keys and digestion stay off by default, and sleep remains maintenance only: integrity, utility and deduplication.

### Secondary comparisons (descriptive only)

| Pair | What | n | first only | second only | p |
| --- | --- | --- | --- | --- | --- |
| U1 vs U0 | utility alone | 584 | 20 | 26 | 0.461 |
| K vs U1 | keys alone | 584 | 25 | 17 | 0.280 |

No correction for multiple tests is applied (two claims and two secondary comparisons).

## Full evidence in the block (test half, no LLM)

| Condition | Scored | Full evidence | Turns reached | Mean turn recall |
| --- | --- | --- | --- | --- |
| B | 581 | 396 (68.2%) | 585/875 | 74.5% |
| U0 | 581 | 401 (69.0%) | 573/875 | 75.5% |
| U1 | 581 | 405 (69.7%) | 584/875 | 76.4% |
| K | 581 | 408 (70.2%) | 591/875 | 77.1% |

## Learned keys

| Conversation | Outcomes (positive) | Keys created | Key table | Test questions | With a key-brought record | Records | Evidence records |
| --- | --- | --- | --- | --- | --- | --- | --- |
| conv-26 | 75 (51) | 67 | learned_keys | 77 | 65 | 134 | 1 |
| conv-41 | 75 (48) | 82 | learned_keys | 77 | 55 | 92 | 0 |
| conv-42 | 98 (59) | 103 | learned_keys | 101 | 89 | 198 | 4 |
| conv-43 | 88 (54) | 83 | learned_keys | 90 | 80 | 209 | 0 |
| conv-44 | 61 (40) | 74 | learned_keys | 62 | 59 | 147 | 7 |
| conv-48 | 94 (55) | 48 | learned_keys | 97 | 80 | 222 | 3 |
| conv-50 | 78 (54) | 91 | learned_keys | 80 | 68 | 164 | 0 |

Pooled: in 496 of 584 test questions (84.9%) a key brought into K's block a record U0 did not deliver (`features.key_rank` > 0); 1166 such records, 15 of them evidence turns, in 14 questions. Questions with any key candidate: 584. Keys created: 548.

## Overlap ceiling (as PRECHECK.md, on these conversations)

| Conversation | Test | Evidence used by a use question | …in a positive outcome | Evidence delivered to a use question | Missing evidence in U0 | …used positively | Wrong in U0 | Wrong and missing evidence used positively |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| conv-26 | 77 | 28 | 26 | 71 | 17 | 4 | 20 | 2 |
| conv-41 | 77 | 25 | 23 | 74 | 22 | 2 | 21 | 2 |
| conv-42 | 101 | 48 | 41 | 100 | 31 | 11 | 39 | 9 |
| conv-43 | 90 | 29 | 26 | 87 | 29 | 9 | 34 | 7 |
| conv-44 | 62 | 27 | 23 | 52 | 25 | 11 | 20 | 7 |
| conv-48 | 97 | 18 | 14 | 88 | 33 | 6 | 35 | 4 |
| conv-50 | 80 | 34 | 28 | 73 | 23 | 7 | 20 | 3 |
| **total** | 584 | 209 | 181 | 545 | 180 | 50 | 189 | 34 |

Ceiling for keys (evidence in a positive outcome): 31.0%. Tight ceiling on a gain in accuracy: 34 questions (5.8%).

## Read latency (`retrieval_ms`, test half)

| Condition | n | Mean | p50 | p95 |
| --- | --- | --- | --- | --- |
| B (plain RAG, in the harness) | 584 | 2.9 | 2.8 | 4.3 |
| U0 | 584 | 30.7 | 23.0 | 69.7 |
| U1 | 584 | 10.6 | 9.1 | 15.2 |
| K | 584 | 12.8 | 11.0 | 19.2 |

U0 and U1 read without keys, K with them.

## Database size (MB)

| Conversation | base | U0 | U1 | K | K, compacted | K without keys (reset), compacted |
| --- | --- | --- | --- | --- | --- | --- |
| conv-26 | 5.19 | 8.13 | 8.34 | 8.81 | 8.77 | 8.55 |
| conv-41 | 6.31 | 9.26 | 9.46 | 9.96 | 9.90 | 9.62 |
| conv-42 | 7.07 | 10.96 | 11.26 | 11.87 | 11.80 | 11.46 |
| conv-43 | 6.78 | 10.21 | 10.47 | 11.03 | 10.97 | 10.69 |
| conv-44 | 5.73 | 8.15 | 8.31 | 8.70 | 8.64 | 8.39 |
| conv-48 | 6.92 | 10.66 | 10.94 | 11.54 | 11.48 | 11.31 |
| conv-50 | 10.11 | 9.08 | 9.29 | 9.81 | 9.74 | 9.44 |

File sizes include the WAL and SHM files. Every copy holds the keys the use half created (inert unless `read.learned_keys` is on); the compacted pair measures the keys' own size.

## LLM calls and cost

Proxy log `eval/learn/llm_calls.jsonl`: 5818 calls; calls with a tag other than this study's reader/judge tags: 0.
Learning steps (sleep phase 1, no LLM configured): LLM calls reported per run {"U1/conv-26": 0, "U1/conv-41": 0, "U1/conv-42": 0, "U1/conv-43": 0, "U1/conv-44": 0, "U1/conv-48": 0, "U1/conv-50": 0, "K/conv-26": 0, "K/conv-41": 0, "K/conv-42": 0, "K/conv-43": 0, "K/conv-44": 0, "K/conv-48": 0, "K/conv-50": 0}. **The learning steps made zero LLM calls.**
Deviation: 1480 calls were made in peak hours {"base-use-reader": 248, "base-use-judge": 248, "U0-test-reader": 154, "U0-test-judge": 154, "U1-test-reader": 154, "U1-test-judge": 70, "K-test-reader": 154, "K-test-judge": 55, "B-test-reader": 154, "B-test-judge": 89}.

| Tag | Calls | Replayed | Cost US$ |
| --- | --- | --- | --- |
| B-test-judge | 326 | 258 | 0.0093 |
| B-test-reader | 577 | 7 | 0.2038 |
| K-test-judge | 188 | 396 | 0.0056 |
| K-test-reader | 584 | 0 | 0.3032 |
| U0-test-judge | 578 | 6 | 0.0163 |
| U0-test-reader | 584 | 0 | 0.3054 |
| U1-test-judge | 237 | 347 | 0.0071 |
| U1-test-reader | 584 | 0 | 0.3043 |
| base-use-judge | 569 | 3 | 0.0183 |
| base-use-reader | 574 | 0 | 0.3444 |

Spent US$1.5179 of the US$4 cap; replayed calls 1017.

## Leak checks (before each test half)

All checks passed: yes (28 checks).

Gold answers found inside a use-half question's text (listed, not a stop):

| Check | Table.column | Row | Gold | Use question |
| --- | --- | --- | --- | --- |
| U0/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FP | Running | What does Melanie say running has been great for? |
| U0/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FQ | Running | What does Melanie say running has been great for? |
| U0/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FR | Running | What does Melanie say running has been great for? |
| U0/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FS | Running | What does Melanie say running has been great for? |
| U0/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FT | Running | What does Melanie say running has been great for? |
| U0/conv-26 | retrievals.query_text | ret_01M7H6VYX907JBYQ8JBVKCX0XJ | Running | What does Melanie say running has been great for? |
| U0/conv-26 | retrievals.query_text | ret_01M7H6VYZMDX62JYZDQ45Y7BEQ | painting | What inspired Caroline's painting for the art show? |
| U0/conv-26 | retrievals.query_text | ret_01M7H6VZ846WD5FXRZ0FQDCJPT | painting | What kind of painting did Caroline share with Melanie on October 13, 2023? |
| U0/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | learned_keys.text | key_01M7H7VSAYEB4PDYPCBZRB39RT | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U0/conv-41 | learned_keys.text | key_01M7H7VSAZDX62JYZDQ45Y7BEQ | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U0/conv-41 | learned_keys.text | key_01M7H7VSBG9Z8F8CV4NQHCJAXX | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U0/conv-41 | learned_keys.text | key_01M7H7VSBH6WD5FXRZ0FQDCJPT | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U0/conv-41 | retrievals.query_text | ret_01M7H7502G4996KBXZQ01MEE90 | beach | When did Maria go to the beach? |
| U0/conv-41 | retrievals.query_text | ret_01M7H750RVWCGERD39GSHD50K6 | community work | When did Maria take up community work with her church friends? |
| U0/conv-41 | retrievals.query_text | ret_01M7H750SPXXHZNY7QEGYCD1TP | Coco | When did Maria get Coco? |
| U0/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U0/conv-41 | retrievals.query_text | ret_01M7H7518Z7B2FA1NQARA2R37X | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U0/conv-41 | retrievals.query_text | ret_01M7H751FT5EG6YKXN0GBCNBZR | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U0/conv-42 | learned_keys.text | key_01M7H8490Z0ZSV5TRF308E3M4X | Joanna | Is it likely that Nate has friends besides Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H84911BN5NCX3MM7TV6ZF2 | Joanna | Is it likely that Nate has friends besides Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H84918P2Z7MTYC9MYNM1SC | Joanna | What are Joanna's hobbies? |
| U0/conv-42 | learned_keys.text | key_01M7H8491HSAK2ZZA969PYWJXM | Joanna | Which outdoor spot did Joanna visit in May? |
| U0/conv-42 | learned_keys.text | key_01M7H8491JDPPH8Q68GVZHRCNB | Joanna | Which outdoor spot did Joanna visit in May? |
| U0/conv-42 | learned_keys.text | key_01M7H8491QSG9SY1T55TPPNJDP | May 2022 | Who was the new addition to Nate's family in May 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H8492N41PJHZDBE34V5MY3 | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| U0/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNP | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNQ | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNR | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNS | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNT | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHH | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U0/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHJ | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U0/conv-42 | learned_keys.text | key_01M7H8493Y07JBYQ8JBVKCX0XK | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H84947MVCTMADBBPD8S6FP | Turtles. | Why does Nate like turtles as pets? |
| U0/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07H | Joanna | What was Joanna's audition for? |
| U0/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07J | Joanna | What was Joanna's audition for? |
| U0/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07K | Joanna | What was Joanna's audition for? |
| U0/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPT | Joanna | What superhero is Joanna a fan of? |
| U0/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPV | Joanna | What superhero is Joanna a fan of? |
| U0/conv-42 | learned_keys.text | key_01M7H864RAWH55FFS77PM2F5BS | Joanna | What superhero is Joanna a fan of? |
| U0/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHB | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHC | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DX | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DY | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DZ | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | learned_keys.text | key_01M7H864SEKAHT0VWJ0J0TMN84 | Joanna | What specific themes are explored in Joanna's new book? |
| U0/conv-42 | learned_keys.text | key_01M7H864SPK2TV7EBM1Z12TET5 | Joanna | What kind of impact does Joanna hope to have with her writing? |
| U0/conv-42 | learned_keys.text | key_01M7H864SSV46TJR4DBME45J56 | Joanna | What motivates Joanna to keep writing even on tough days? |
| U0/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZV | Joanna | What motivates Joanna to keep writing even on tough days? |
| U0/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZW | Joanna | What motivates Joanna to keep writing even on tough days? |
| U0/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGNC | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGND | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS1 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS2 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS3 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS4 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS5 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCA | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCB | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GJ | Joanna | What did Joanna share with her writers group in August 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GK | Joanna | What did Joanna share with her writers group in August 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GM | Joanna | What did Joanna share with her writers group in August 2022? |
| U0/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JE | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U0/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JF | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U0/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U0/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A1 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U0/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A2 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U0/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A3 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U0/conv-42 | learned_keys.text | key_01M7H864TJ26NK86CJA2MCFQS4 | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMH | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMJ | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMK | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMM | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | learned_keys.text | key_01M7H864TSTSVY01RDKNKCRAG9 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| U0/conv-42 | learned_keys.text | key_01M7H864V52AJ3JR012PH63NC5 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| U0/conv-42 | learned_keys.text | key_01M7H864V7H5YM3RZFSKTYDQKN | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| U0/conv-42 | learned_keys.text | key_01M7H864V9JMTT6V6GYGWR5BA0 | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| U0/conv-42 | learned_keys.text | key_01M7H864VBW9PG3EF120AD2PCF | Joanna | What encouragement does Nate give to Joanna after her setback? |
| U0/conv-42 | learned_keys.text | key_01M7H864VDD8FND55GFK6R0XKN | Joanna | Where does Joanna get her ideas for the characters from? |
| U0/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFE | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U0/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFF | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U0/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4M | Turtles. | What type of diet do Nate's turtles have? |
| U0/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4N | Turtles. | What type of diet do Nate's turtles have? |
| U0/conv-42 | learned_keys.text | key_01M7H864VR3SD9KQMD3VNHDZ9S | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| U0/conv-42 | learned_keys.text | key_01M7H864VWBBC86E4BZY9BVW11 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JFQ0ZSV5TRF308E3M4W | Joanna | Is it likely that Nate has friends besides Joanna? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JGVBN5NCX3MM7TV6ZF2 | Joanna | What kind of interests do Joanna and Nate share? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JJ5XZTTG156R1Z8DZ19 | Joanna | What are Joanna's hobbies? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JJSP2Z7MTYC9MYNM1SB | Joanna | What is Joanna allergic to? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JKCHSV4C48NDPKRDBSC | Joanna | What underlying condition might Joanna have based on her allergies? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JM2R5ZTFXYZFH1J05P4 | Joanna | What nickname does Nate use for Joanna? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JMQX0Y4X0ZM4ZEJYHTN | purple | When did Nate get purple hair? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JNAWEGC07PRYFAY5Z43 | Joanna | Which outdoor spot did Joanna visit in May? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JP0SAK2ZZA969PYWJXK | Joanna | How many times has Joanna found new hiking trails? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JQ66JFKQTP1E1HQW9D6 | May 2022 | Who was the new addition to Nate's family in May 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | screenplay | When did Joanna start writing her third screenplay? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | Joanna | When did Joanna start writing her third screenplay? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JRGSG9SY1T55TPPNJDN | A gaming party | When is Nate hosting a gaming party? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JS1ST2F7MHVD15EGY1H | Joanna | When did Joanna hike with her buddies? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JTA0ERZ4SK9BWXVWSGW | Joanna | What places has Joanna submitted her work to? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JTZS63XNN9QRKBKH68B | Joanna | When is Joanna going to make Nate's ice cream for her family? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JVN5K6QQY3CPD53DDFV | Joanna | What kind of writings does Joanna do? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JXP6ZW9NBP4B3RPAKCG | Joanna | Where did Joanna travel to in July 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JYA7ETGDJBGB6HZQN26 | Joanna | When did someone write Joanna a touching letter? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82JYY2XR4NYWHRVWZ5DMT | Joanna | What book recommendations has Joanna given to Nate? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | her book | When did Joanna share her book with her writers group? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | Joanna | When did Joanna share her book with her writers group? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K0SP0Z0KNRDBBS7Y8A5 | Joanna | When did Joanna make a desert with almond milk? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K1EP7B5T51EHGVD1KAS | a cooking show | When did Nate attend a cooking show? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | her book | How long did it take for Joanna to finish writing her book? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | Joanna | How long did it take for Joanna to finish writing her book? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K3EXXHZNY7QEGYCD1TP | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K4266P93Z5P3QZYYXZ1 | Joanna | When did Nate get Tilly for Joanna? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K5841PJHZDBE34V5MY2 | Joanna | When did Joanna plan to go over to Nate's and share recipes? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K6R21XVNJDAEX6WCTRW | Joanna | How many letters has Joanna recieved? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K7ED79SY0CX9WD7DC1P | Valorant | When did Nate win a big Valorant tourney? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K85KPGW15JTGAQYHTS8 | Joanna | How many hikes has Joanna been on? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | Turtles. | What activities does Nate do with his turtles? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | his turtles | What activities does Nate do with his turtles? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82K9FQX65AZ8J79N0HX09 | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KAB7QAXRCVSNCJQHZTT | Joanna | What recommendations has Nate received from Joanna? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KAY9NDEJXHECWBGT1RP | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KBK0V6D1PE6PHTZBJNP | Joanna | How many screenplays has Joanna written? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KCR4RRZ1GBAH4DV1WMM | Joanna | What kind of job is Joanna beginning to preform the duties of because of her movie scripts? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | screenplay | What is Joanna's third screenplay about? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | Joanna | What is Joanna's third screenplay about? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KHQCY5GRGPK7YWX9QSX | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | Turtles. | For how long has Nate had his turtles? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | his turtles | For how long has Nate had his turtles? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KKEQW30V4394CA5WXNF | Joanna | What did Joanna recently watch and recommend to Nate on February 7, 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KMN5EG6YKXN0GBCNBZR | Turtles. | Why does Nate like turtles as pets? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KN7EB4PDYPCBZRB39RS | Turtles. | How does Nate describe the process of taking care of turtles? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KNTDX62JYZDQ45Y7BEQ | Joanna | What was Joanna's audition for? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KSPZ8R8RP3X4HZFF07G | Joanna | What superhero is Joanna a fan of? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KVJ24DTP1QHSEWBGXGQ | Joanna | What recipe Nate offer to share with Joanna? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KWS9Z8F8CV4NQHCJAXW | Joanna | Where did Joanna go for a road trip for research? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KXE6WD5FXRZ0FQDCJPT | Joanna | What specific themes are explored in Joanna's new book? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KY2WH55FFS77PM2F5BS | Joanna | What inspired Joanna's new script in July 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82KYN3PWFMMMDFH25DG2B | Joanna | What did Nate do while Joanna was on her road trip? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M07PC2W9Q4FCQC0J2SH | Joanna | What kind of impact does Joanna hope to have with her writing? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M12S89FS616R3FFQ2TW | Joanna | What motivates Joanna to keep writing even on tough days? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M1TKQYS07SMQ316BS4N | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M2J2BEWZC2NJSJEVKHA | Joanna | What did Joanna share with her writers group in August 2022? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M4JFES9ZQC9V9PKC3XG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M5WFYCV55KN50DYRDAR | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M6QC8JH0JZFHKN2A39D | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82M7FK2TV7EBM1Z12TET4 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MA7GRNX1YEHT98W7XS1 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MAV9K9XWBDZRZXEKYC6 | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MBGEBZVG1XQDFBG79GH | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MCAADK0J7NVP9FX10E9 | Joanna | What encouragement does Nate give to Joanna after her setback? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MD08B1HN99H0VP8H5XX | Joanna | Where does Joanna get her ideas for the characters from? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MDK1MC18J61WPT8J7JD | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MEBDW03EDH2VVHDP7FD | Turtles. | What type of diet do Nate's turtles have? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MEXSBTCDB1WYNA273A0 | Joanna | What did Joanna find in old notebooks last week that prompted her to reflect on her progress as a writer? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MFM26NK86CJA2MCFQS3 | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| U0/conv-42 | retrievals.query_text | ret_01M7H82MGXTSVY01RDKNKCRAG8 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTV | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTW | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCW89NDEJXHECWBGT1RQ | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Y | surfing | How long has John been surfing? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Z | surfing | How long has John been surfing? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCWSJ5E1ZCAPY1628QZ2 | surfing | How does John feel while surfing? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX307JBYQ8JBVKCX0XK | basketball | What is John's number one goal in his basketball career? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX6EB4PDYPCBZRB39RT | August 2023 | What new activity has Tim started learning in August 2023? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XH | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XJ | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XK | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN83 | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN84 | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN85 | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN86 | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN87 | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8HXFYCV55KN50DYRDAR | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8J0C8JH0JZFHKN2A39E | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8JG9K9XWBDZRZXEKYC7 | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8K2TSVY01RDKNKCRAG9 | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| U0/conv-43 | learned_keys.text | key_01M7H8J8K4JQBQ8887RK2P69E9 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYQSP2Z7MTYC9MYNM1SB | Seattle | When was John in Seattle for a game? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYRBHSV4C48NDPKRDBSC | surfing | What year did John start surfing? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYT7WEGC07PRYFAY5Z43 | "That" | What is a prominent charity organization that John might want to work with and why? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYTWSAK2ZZA969PYWJXK | Chicago | When did John meet with his teammates after returning from Chicago? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYW16JFKQTP1E1HQW9D6 | August 15, 2023 | Where was John between August 11 and August 15 2023? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYWK3E6A30YP4VZX604Z | basketball | What schools did John play basketball in and how many years was he with his team during high school? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYXVST2F7MHVD15EGY1H | September 2023 | Which US states might Tim be in during September 2023 based on his plans of visiting Universal Studios? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYYE6KF4JZN6CSW3148W | basketball | What could John do after his basketball career? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CYYZ0ERZ4SK9BWXVWSGW | basketball | Who is Tim and John's favorite basketball player? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ1X7ETGDJBGB6HZQN26 | LeBron James | What does John like about Lebron James? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ316A76G13RWC6RRNC4 | November, 2023. | Where was Tim in the week before 16 November 2023? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ3KM74VFS80VSX49P99 | Greenhouse | When did John get married at a greenhouse? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ4SP7B5T51EHGVD1KAS | yoga | What kind of yoga for building core strength might John benefit from? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ7466P93Z5P3QZYYXZ1 | basketball | When did John organize a basketball camp for kids? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ854EASMNNC86WRSW03 | "That" | What is a Star Wars book that Tim might enjoy? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ8N21XVNJDAEX6WCTRW | Ireland | When will Tim leave for Ireland? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ96D79SY0CX9WD7DC1P | Ireland | Which Star Wars-related locations would Tim enjoy during his visit to Ireland? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZ9SKPGW15JTGAQYHTS8 | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZAC2N40BX17PK9F64XH | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZBM7QAXRCVSNCJQHZTT | Harry Potter | What kind of picture did Tim share as part of their Harry Potter book collection? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZDW4RRZ1GBAH4DV1WMM | surfing | How long has John been surfing? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZED1J8W0GJVBGJSXXHH | surfing | How does John feel while surfing? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZFHTQKZVD4Z6WH17M8Q | Fantasy | Which two fantasy novels does Tim particularly enjoy writing about? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZH8J5E1ZCAPY1628QZ1 | basketball | What is John's number one goal in his basketball career? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZJDCY5GRGPK7YWX9QSX | August 2023 | What new activity has Tim started learning in August 2023? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZQDZPS8FMJT6QCT94RV | basketball | What does John want to do after his basketball career? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZSPZ8R8RP3X4HZFF07G | basketball | Which basketball team does Tim support? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZY56WD5FXRZ0FQDCJPT | "That" | What genre is the novel that Tim is writing? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8CZYRWH55FFS77PM2F5BS | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8D02Z7TYZ0PR3T7V140DX | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8D05ZC8JH0JZFHKN2A39D | German | What language does Tim know besides German? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8D06JK2TV7EBM1Z12TET4 | "That" | What book did Tim get in Italy that inspired him to cook? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8D089XE593KYSWVEYQGNB | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| U0/conv-43 | retrievals.query_text | ret_01M7H8D08ZGRNX1YEHT98W7XS1 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| U0/conv-44 | learned_keys.text | key_01M7H8RZ2NHSV4C48NDPKRDBSC | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| U0/conv-44 | learned_keys.text | key_01M7H8RZ35SG9SY1T55TPPNJDP | August | Where did Andrew go during the first weekend of August 2023? |
| U0/conv-44 | learned_keys.text | key_01M7H8RZ3D5K6QQY3CPD53DDFW | work | How does Andrew feel about his current work? |
| U0/conv-44 | learned_keys.text | key_01M7H8RZ4821XVNJDAEX6WCTRX | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| U0/conv-44 | learned_keys.text | key_01M7H8RZ5807JBYQ8JBVKCX0XJ | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QXGTP2Z7MTYC9MYNM1SB | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QXK6WEGC07PRYFAY5Z43 | Go hiking | When is Andrew going to go hiking with Audrey? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QXNF3E6A30YP4VZX604Z | August | Where did Andrew go during the first weekend of August 2023? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QXQV0ERZ4SK9BWXVWSGW | work | How does Andrew feel about his current work? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QY0066P93Z5P3QZYYXZ1 | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement | Why did Audrey think positive reinforcement training is important for pets? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement training | Why did Audrey think positive reinforcement training is important for pets? |
| U0/conv-44 | retrievals.query_text | ret_01M7H8QY7T1J8W0GJVBGJSXXHH | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| U0/conv-48 | learned_keys.text | key_01M7H9188W21XVNJDAEX6WCTRX | play the console | Did Jolene teach herself how to play the console? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZGYP4996KBXZQ01MEE90 | in 2010 | Which country were Jolene and her mother visiting in 2010? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZH1ZR5ZTFXYZFH1J05P4 | Seraphim | When did Jolene buy her pet Seraphim? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHEC7ETGDJBGB6HZQN26 | meditation | How long has Jolene been doing yoga and meditation? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHFW6A76G13RWC6RRNC4 | Seraphim | When did Jolene buy a new aquarium for Seraphim? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHGQM74VFS80VSX49P99 | Susie | When did Jolene adopt her snake Susie? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHK87ZKTPX3RYZ61D3Q6 | last week of August 2023 | Which country was Jolene located in during the last week of August 2023? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHQG4EASMNNC86WRSW03 | play the console | Did Jolene teach herself how to play the console? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHSWKPGW15JTGAQYHTS8 | Surfing | Has Jolene tried surfing? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZHTK2N40BX17PK9F64XH | Surfing | When did the Deboran and Jolene agree to go surfing? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZJEXMVCTMADBBPD8S6FN | Seraphim | For how long has Jolene had Seraphim as a pet? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZJFP6N88KZKJQH3J89S2 | running group | What made being part of the running group easy for Deborah to stay motivated? |
| U0/conv-48 | retrievals.query_text | ret_01M7H8ZJJCDEXBDEXP9XNN12XH | Susie | How did Jolene come to have her pet, Susie? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP4 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP5 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMM | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMN | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB4QQW30V4394CA5WXNG | Proud | What car brand does Calvin own that he is proud of? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BER | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BES | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB5C0QA51R4Y4T02QW73 | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGQ | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGR | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGS | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGT | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9BB603PWFMMMDFH25DG2C | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6TAKC4CQS3709XTTBZW | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6TFXE593KYSWVEYQGNC | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6VG26NK86CJA2MCFQS4 | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U0/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YFQX0Y4X0ZM4ZEJYHTN | Tokyo | Which places or events has Calvin visited in Tokyo? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YND3E6A30YP4VZX604Z | two weeks | Where was Dave in the last two weeks of August 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | Boston | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | November 2023 | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YSA0ERZ4SK9BWXVWSGW | October 3, 2023 | Which city was Calvin at on October 3, 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YT4S63XNN9QRKBKH68B | Boston | When did Calvin met with local artists in Boston? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YWM6ZW9NBP4B3RPAKCG | Ferrari | When did Calvin buy his second Ferrari? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YXH7ETGDJBGB6HZQN26 | Tokyo | When did Calvin plan on travelling to Tokyo the second time? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99YZ96A76G13RWC6RRNC4 | Boston | When did Calvin visit some of the sights in Boston with a former high school friend? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99Z0YP0Z0KNRDBBS7Y8A5 | auto engineering | Which events in Dave's life inspired him to take up auto engineering? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99Z2N7ZKTPX3RYZ61D3Q6 | Ford Mustang | How long did Dave's work on the Ford Mustang take? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99Z3GWCGERD39GSHD50K6 | San Francisco | How long was the car modification workshop in San Francisco? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99Z5Y41PJHZDBE34V5MY2 | Boston | When did Dave take a photo of a Boston clock tower? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99Z7Z21XVNJDAEX6WCTRW | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZH14RRZ1GBAH4DV1WMM | Proud | What car brand does Calvin own that he is proud of? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZKYTQKZVD4Z6WH17M8Q | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZR69W5SCB7BK6GA03PW | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZS8CY5GRGPK7YWX9QSX | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZV8P2P9VWMAAVGHMCR8 | Boston | What plans do Calvin and Dave have for when Calvin visits Boston? |
| U0/conv-50 | retrievals.query_text | ret_01M7H99ZYH5EG6YKXN0GBCNBZR | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A01M3YV41MT63704PTZJ | Working on cars | What does working on cars represent for Dave? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A07BDEXBDEXP9XNN12XH | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A08924DTP1QHSEWBGXGQ | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A0FJS89FS616R3FFQ2TW | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U0/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FP | Running | What does Melanie say running has been great for? |
| U1/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FQ | Running | What does Melanie say running has been great for? |
| U1/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FR | Running | What does Melanie say running has been great for? |
| U1/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FS | Running | What does Melanie say running has been great for? |
| U1/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FT | Running | What does Melanie say running has been great for? |
| U1/conv-26 | retrievals.query_text | ret_01M7H6VYX907JBYQ8JBVKCX0XJ | Running | What does Melanie say running has been great for? |
| U1/conv-26 | retrievals.query_text | ret_01M7H6VYZMDX62JYZDQ45Y7BEQ | painting | What inspired Caroline's painting for the art show? |
| U1/conv-26 | retrievals.query_text | ret_01M7H6VZ846WD5FXRZ0FQDCJPT | painting | What kind of painting did Caroline share with Melanie on October 13, 2023? |
| U1/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | learned_keys.text | key_01M7H7VSAYEB4PDYPCBZRB39RT | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U1/conv-41 | learned_keys.text | key_01M7H7VSAZDX62JYZDQ45Y7BEQ | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U1/conv-41 | learned_keys.text | key_01M7H7VSBG9Z8F8CV4NQHCJAXX | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U1/conv-41 | learned_keys.text | key_01M7H7VSBH6WD5FXRZ0FQDCJPT | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U1/conv-41 | retrievals.query_text | ret_01M7H7502G4996KBXZQ01MEE90 | beach | When did Maria go to the beach? |
| U1/conv-41 | retrievals.query_text | ret_01M7H750RVWCGERD39GSHD50K6 | community work | When did Maria take up community work with her church friends? |
| U1/conv-41 | retrievals.query_text | ret_01M7H750SPXXHZNY7QEGYCD1TP | Coco | When did Maria get Coco? |
| U1/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| U1/conv-41 | retrievals.query_text | ret_01M7H7518Z7B2FA1NQARA2R37X | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| U1/conv-41 | retrievals.query_text | ret_01M7H751FT5EG6YKXN0GBCNBZR | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| U1/conv-42 | learned_keys.text | key_01M7H8490Z0ZSV5TRF308E3M4X | Joanna | Is it likely that Nate has friends besides Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H84911BN5NCX3MM7TV6ZF2 | Joanna | Is it likely that Nate has friends besides Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H84918P2Z7MTYC9MYNM1SC | Joanna | What are Joanna's hobbies? |
| U1/conv-42 | learned_keys.text | key_01M7H8491HSAK2ZZA969PYWJXM | Joanna | Which outdoor spot did Joanna visit in May? |
| U1/conv-42 | learned_keys.text | key_01M7H8491JDPPH8Q68GVZHRCNB | Joanna | Which outdoor spot did Joanna visit in May? |
| U1/conv-42 | learned_keys.text | key_01M7H8491QSG9SY1T55TPPNJDP | May 2022 | Who was the new addition to Nate's family in May 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H8492N41PJHZDBE34V5MY3 | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| U1/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNP | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNQ | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNR | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNS | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNT | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHH | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U1/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHJ | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U1/conv-42 | learned_keys.text | key_01M7H8493Y07JBYQ8JBVKCX0XK | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H84947MVCTMADBBPD8S6FP | Turtles. | Why does Nate like turtles as pets? |
| U1/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07H | Joanna | What was Joanna's audition for? |
| U1/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07J | Joanna | What was Joanna's audition for? |
| U1/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07K | Joanna | What was Joanna's audition for? |
| U1/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPT | Joanna | What superhero is Joanna a fan of? |
| U1/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPV | Joanna | What superhero is Joanna a fan of? |
| U1/conv-42 | learned_keys.text | key_01M7H864RAWH55FFS77PM2F5BS | Joanna | What superhero is Joanna a fan of? |
| U1/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHB | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHC | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DX | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DY | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DZ | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | learned_keys.text | key_01M7H864SEKAHT0VWJ0J0TMN84 | Joanna | What specific themes are explored in Joanna's new book? |
| U1/conv-42 | learned_keys.text | key_01M7H864SPK2TV7EBM1Z12TET5 | Joanna | What kind of impact does Joanna hope to have with her writing? |
| U1/conv-42 | learned_keys.text | key_01M7H864SSV46TJR4DBME45J56 | Joanna | What motivates Joanna to keep writing even on tough days? |
| U1/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZV | Joanna | What motivates Joanna to keep writing even on tough days? |
| U1/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZW | Joanna | What motivates Joanna to keep writing even on tough days? |
| U1/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGNC | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGND | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS1 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS2 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS3 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS4 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS5 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCA | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCB | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GJ | Joanna | What did Joanna share with her writers group in August 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GK | Joanna | What did Joanna share with her writers group in August 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GM | Joanna | What did Joanna share with her writers group in August 2022? |
| U1/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JE | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U1/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JF | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U1/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U1/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A1 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U1/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A2 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U1/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A3 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U1/conv-42 | learned_keys.text | key_01M7H864TJ26NK86CJA2MCFQS4 | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMH | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMJ | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMK | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMM | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | learned_keys.text | key_01M7H864TSTSVY01RDKNKCRAG9 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| U1/conv-42 | learned_keys.text | key_01M7H864V52AJ3JR012PH63NC5 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| U1/conv-42 | learned_keys.text | key_01M7H864V7H5YM3RZFSKTYDQKN | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| U1/conv-42 | learned_keys.text | key_01M7H864V9JMTT6V6GYGWR5BA0 | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| U1/conv-42 | learned_keys.text | key_01M7H864VBW9PG3EF120AD2PCF | Joanna | What encouragement does Nate give to Joanna after her setback? |
| U1/conv-42 | learned_keys.text | key_01M7H864VDD8FND55GFK6R0XKN | Joanna | Where does Joanna get her ideas for the characters from? |
| U1/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFE | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U1/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFF | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U1/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4M | Turtles. | What type of diet do Nate's turtles have? |
| U1/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4N | Turtles. | What type of diet do Nate's turtles have? |
| U1/conv-42 | learned_keys.text | key_01M7H864VR3SD9KQMD3VNHDZ9S | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| U1/conv-42 | learned_keys.text | key_01M7H864VWBBC86E4BZY9BVW11 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JFQ0ZSV5TRF308E3M4W | Joanna | Is it likely that Nate has friends besides Joanna? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JGVBN5NCX3MM7TV6ZF2 | Joanna | What kind of interests do Joanna and Nate share? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JJ5XZTTG156R1Z8DZ19 | Joanna | What are Joanna's hobbies? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JJSP2Z7MTYC9MYNM1SB | Joanna | What is Joanna allergic to? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JKCHSV4C48NDPKRDBSC | Joanna | What underlying condition might Joanna have based on her allergies? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JM2R5ZTFXYZFH1J05P4 | Joanna | What nickname does Nate use for Joanna? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JMQX0Y4X0ZM4ZEJYHTN | purple | When did Nate get purple hair? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JNAWEGC07PRYFAY5Z43 | Joanna | Which outdoor spot did Joanna visit in May? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JP0SAK2ZZA969PYWJXK | Joanna | How many times has Joanna found new hiking trails? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JQ66JFKQTP1E1HQW9D6 | May 2022 | Who was the new addition to Nate's family in May 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | screenplay | When did Joanna start writing her third screenplay? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | Joanna | When did Joanna start writing her third screenplay? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JRGSG9SY1T55TPPNJDN | A gaming party | When is Nate hosting a gaming party? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JS1ST2F7MHVD15EGY1H | Joanna | When did Joanna hike with her buddies? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JTA0ERZ4SK9BWXVWSGW | Joanna | What places has Joanna submitted her work to? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JTZS63XNN9QRKBKH68B | Joanna | When is Joanna going to make Nate's ice cream for her family? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JVN5K6QQY3CPD53DDFV | Joanna | What kind of writings does Joanna do? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JXP6ZW9NBP4B3RPAKCG | Joanna | Where did Joanna travel to in July 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JYA7ETGDJBGB6HZQN26 | Joanna | When did someone write Joanna a touching letter? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82JYY2XR4NYWHRVWZ5DMT | Joanna | What book recommendations has Joanna given to Nate? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | her book | When did Joanna share her book with her writers group? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | Joanna | When did Joanna share her book with her writers group? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K0SP0Z0KNRDBBS7Y8A5 | Joanna | When did Joanna make a desert with almond milk? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K1EP7B5T51EHGVD1KAS | a cooking show | When did Nate attend a cooking show? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | her book | How long did it take for Joanna to finish writing her book? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | Joanna | How long did it take for Joanna to finish writing her book? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K3EXXHZNY7QEGYCD1TP | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K4266P93Z5P3QZYYXZ1 | Joanna | When did Nate get Tilly for Joanna? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K5841PJHZDBE34V5MY2 | Joanna | When did Joanna plan to go over to Nate's and share recipes? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K6R21XVNJDAEX6WCTRW | Joanna | How many letters has Joanna recieved? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K7ED79SY0CX9WD7DC1P | Valorant | When did Nate win a big Valorant tourney? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K85KPGW15JTGAQYHTS8 | Joanna | How many hikes has Joanna been on? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | Turtles. | What activities does Nate do with his turtles? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | his turtles | What activities does Nate do with his turtles? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82K9FQX65AZ8J79N0HX09 | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KAB7QAXRCVSNCJQHZTT | Joanna | What recommendations has Nate received from Joanna? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KAY9NDEJXHECWBGT1RP | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KBK0V6D1PE6PHTZBJNP | Joanna | How many screenplays has Joanna written? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KCR4RRZ1GBAH4DV1WMM | Joanna | What kind of job is Joanna beginning to preform the duties of because of her movie scripts? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | screenplay | What is Joanna's third screenplay about? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | Joanna | What is Joanna's third screenplay about? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KHQCY5GRGPK7YWX9QSX | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | Turtles. | For how long has Nate had his turtles? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | his turtles | For how long has Nate had his turtles? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KKEQW30V4394CA5WXNF | Joanna | What did Joanna recently watch and recommend to Nate on February 7, 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KMN5EG6YKXN0GBCNBZR | Turtles. | Why does Nate like turtles as pets? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KN7EB4PDYPCBZRB39RS | Turtles. | How does Nate describe the process of taking care of turtles? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KNTDX62JYZDQ45Y7BEQ | Joanna | What was Joanna's audition for? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KSPZ8R8RP3X4HZFF07G | Joanna | What superhero is Joanna a fan of? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KVJ24DTP1QHSEWBGXGQ | Joanna | What recipe Nate offer to share with Joanna? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KWS9Z8F8CV4NQHCJAXW | Joanna | Where did Joanna go for a road trip for research? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KXE6WD5FXRZ0FQDCJPT | Joanna | What specific themes are explored in Joanna's new book? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KY2WH55FFS77PM2F5BS | Joanna | What inspired Joanna's new script in July 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82KYN3PWFMMMDFH25DG2B | Joanna | What did Nate do while Joanna was on her road trip? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M07PC2W9Q4FCQC0J2SH | Joanna | What kind of impact does Joanna hope to have with her writing? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M12S89FS616R3FFQ2TW | Joanna | What motivates Joanna to keep writing even on tough days? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M1TKQYS07SMQ316BS4N | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M2J2BEWZC2NJSJEVKHA | Joanna | What did Joanna share with her writers group in August 2022? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | her book | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M4JFES9ZQC9V9PKC3XG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M5WFYCV55KN50DYRDAR | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M6QC8JH0JZFHKN2A39D | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82M7FK2TV7EBM1Z12TET4 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MA7GRNX1YEHT98W7XS1 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MAV9K9XWBDZRZXEKYC6 | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MBGEBZVG1XQDFBG79GH | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MCAADK0J7NVP9FX10E9 | Joanna | What encouragement does Nate give to Joanna after her setback? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MD08B1HN99H0VP8H5XX | Joanna | Where does Joanna get her ideas for the characters from? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MDK1MC18J61WPT8J7JD | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MEBDW03EDH2VVHDP7FD | Turtles. | What type of diet do Nate's turtles have? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MEXSBTCDB1WYNA273A0 | Joanna | What did Joanna find in old notebooks last week that prompted her to reflect on her progress as a writer? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MFM26NK86CJA2MCFQS3 | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| U1/conv-42 | retrievals.query_text | ret_01M7H82MGXTSVY01RDKNKCRAG8 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTV | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTW | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCW89NDEJXHECWBGT1RQ | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Y | surfing | How long has John been surfing? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Z | surfing | How long has John been surfing? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCWSJ5E1ZCAPY1628QZ2 | surfing | How does John feel while surfing? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX307JBYQ8JBVKCX0XK | basketball | What is John's number one goal in his basketball career? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX6EB4PDYPCBZRB39RT | August 2023 | What new activity has Tim started learning in August 2023? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XH | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XJ | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XK | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN83 | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN84 | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN85 | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN86 | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN87 | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8HXFYCV55KN50DYRDAR | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8J0C8JH0JZFHKN2A39E | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8JG9K9XWBDZRZXEKYC7 | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8K2TSVY01RDKNKCRAG9 | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| U1/conv-43 | learned_keys.text | key_01M7H8J8K4JQBQ8887RK2P69E9 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | "That" | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | basketball | What are John's goals for his career that are not related to his basketball skills? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYQSP2Z7MTYC9MYNM1SB | Seattle | When was John in Seattle for a game? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYRBHSV4C48NDPKRDBSC | surfing | What year did John start surfing? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYT7WEGC07PRYFAY5Z43 | "That" | What is a prominent charity organization that John might want to work with and why? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYTWSAK2ZZA969PYWJXK | Chicago | When did John meet with his teammates after returning from Chicago? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYW16JFKQTP1E1HQW9D6 | August 15, 2023 | Where was John between August 11 and August 15 2023? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYWK3E6A30YP4VZX604Z | basketball | What schools did John play basketball in and how many years was he with his team during high school? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYXVST2F7MHVD15EGY1H | September 2023 | Which US states might Tim be in during September 2023 based on his plans of visiting Universal Studios? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYYE6KF4JZN6CSW3148W | basketball | What could John do after his basketball career? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CYYZ0ERZ4SK9BWXVWSGW | basketball | Who is Tim and John's favorite basketball player? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ1X7ETGDJBGB6HZQN26 | LeBron James | What does John like about Lebron James? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ316A76G13RWC6RRNC4 | November, 2023. | Where was Tim in the week before 16 November 2023? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ3KM74VFS80VSX49P99 | Greenhouse | When did John get married at a greenhouse? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ4SP7B5T51EHGVD1KAS | yoga | What kind of yoga for building core strength might John benefit from? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ7466P93Z5P3QZYYXZ1 | basketball | When did John organize a basketball camp for kids? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ854EASMNNC86WRSW03 | "That" | What is a Star Wars book that Tim might enjoy? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ8N21XVNJDAEX6WCTRW | Ireland | When will Tim leave for Ireland? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ96D79SY0CX9WD7DC1P | Ireland | Which Star Wars-related locations would Tim enjoy during his visit to Ireland? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZ9SKPGW15JTGAQYHTS8 | May, 2023 | Which team did John sign with on 21 May, 2023? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZAC2N40BX17PK9F64XH | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZBM7QAXRCVSNCJQHZTT | Harry Potter | What kind of picture did Tim share as part of their Harry Potter book collection? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZDW4RRZ1GBAH4DV1WMM | surfing | How long has John been surfing? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZED1J8W0GJVBGJSXXHH | surfing | How does John feel while surfing? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZFHTQKZVD4Z6WH17M8Q | Fantasy | Which two fantasy novels does Tim particularly enjoy writing about? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZH8J5E1ZCAPY1628QZ1 | basketball | What is John's number one goal in his basketball career? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZJDCY5GRGPK7YWX9QSX | August 2023 | What new activity has Tim started learning in August 2023? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZQDZPS8FMJT6QCT94RV | basketball | What does John want to do after his basketball career? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZSPZ8R8RP3X4HZFF07G | basketball | Which basketball team does Tim support? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZY56WD5FXRZ0FQDCJPT | "That" | What genre is the novel that Tim is writing? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8CZYRWH55FFS77PM2F5BS | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8D02Z7TYZ0PR3T7V140DX | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8D05ZC8JH0JZFHKN2A39D | German | What language does Tim know besides German? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8D06JK2TV7EBM1Z12TET4 | "That" | What book did Tim get in Italy that inspired him to cook? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8D089XE593KYSWVEYQGNB | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| U1/conv-43 | retrievals.query_text | ret_01M7H8D08ZGRNX1YEHT98W7XS1 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| U1/conv-44 | learned_keys.text | key_01M7H8RZ2NHSV4C48NDPKRDBSC | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| U1/conv-44 | learned_keys.text | key_01M7H8RZ35SG9SY1T55TPPNJDP | August | Where did Andrew go during the first weekend of August 2023? |
| U1/conv-44 | learned_keys.text | key_01M7H8RZ3D5K6QQY3CPD53DDFW | work | How does Andrew feel about his current work? |
| U1/conv-44 | learned_keys.text | key_01M7H8RZ4821XVNJDAEX6WCTRX | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| U1/conv-44 | learned_keys.text | key_01M7H8RZ5807JBYQ8JBVKCX0XJ | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QXGTP2Z7MTYC9MYNM1SB | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QXK6WEGC07PRYFAY5Z43 | Go hiking | When is Andrew going to go hiking with Audrey? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QXNF3E6A30YP4VZX604Z | August | Where did Andrew go during the first weekend of August 2023? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QXQV0ERZ4SK9BWXVWSGW | work | How does Andrew feel about his current work? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QY0066P93Z5P3QZYYXZ1 | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement | Why did Audrey think positive reinforcement training is important for pets? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement training | Why did Audrey think positive reinforcement training is important for pets? |
| U1/conv-44 | retrievals.query_text | ret_01M7H8QY7T1J8W0GJVBGJSXXHH | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| U1/conv-48 | learned_keys.text | key_01M7H9188W21XVNJDAEX6WCTRX | play the console | Did Jolene teach herself how to play the console? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZGYP4996KBXZQ01MEE90 | in 2010 | Which country were Jolene and her mother visiting in 2010? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZH1ZR5ZTFXYZFH1J05P4 | Seraphim | When did Jolene buy her pet Seraphim? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHEC7ETGDJBGB6HZQN26 | meditation | How long has Jolene been doing yoga and meditation? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHFW6A76G13RWC6RRNC4 | Seraphim | When did Jolene buy a new aquarium for Seraphim? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHGQM74VFS80VSX49P99 | Susie | When did Jolene adopt her snake Susie? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHK87ZKTPX3RYZ61D3Q6 | last week of August 2023 | Which country was Jolene located in during the last week of August 2023? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHQG4EASMNNC86WRSW03 | play the console | Did Jolene teach herself how to play the console? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHSWKPGW15JTGAQYHTS8 | Surfing | Has Jolene tried surfing? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZHTK2N40BX17PK9F64XH | Surfing | When did the Deboran and Jolene agree to go surfing? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZJEXMVCTMADBBPD8S6FN | Seraphim | For how long has Jolene had Seraphim as a pet? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZJFP6N88KZKJQH3J89S2 | running group | What made being part of the running group easy for Deborah to stay motivated? |
| U1/conv-48 | retrievals.query_text | ret_01M7H8ZJJCDEXBDEXP9XNN12XH | Susie | How did Jolene come to have her pet, Susie? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP4 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP5 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMM | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMN | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB4QQW30V4394CA5WXNG | Proud | What car brand does Calvin own that he is proud of? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BER | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BES | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB5C0QA51R4Y4T02QW73 | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGQ | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGR | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGS | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGT | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9BB603PWFMMMDFH25DG2C | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6TAKC4CQS3709XTTBZW | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6TFXE593KYSWVEYQGNC | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6VG26NK86CJA2MCFQS4 | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YFQX0Y4X0ZM4ZEJYHTN | Tokyo | Which places or events has Calvin visited in Tokyo? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YND3E6A30YP4VZX604Z | two weeks | Where was Dave in the last two weeks of August 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | Boston | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | November 2023 | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YSA0ERZ4SK9BWXVWSGW | October 3, 2023 | Which city was Calvin at on October 3, 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YT4S63XNN9QRKBKH68B | Boston | When did Calvin met with local artists in Boston? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YWM6ZW9NBP4B3RPAKCG | Ferrari | When did Calvin buy his second Ferrari? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YXH7ETGDJBGB6HZQN26 | Tokyo | When did Calvin plan on travelling to Tokyo the second time? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99YZ96A76G13RWC6RRNC4 | Boston | When did Calvin visit some of the sights in Boston with a former high school friend? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99Z0YP0Z0KNRDBBS7Y8A5 | auto engineering | Which events in Dave's life inspired him to take up auto engineering? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99Z2N7ZKTPX3RYZ61D3Q6 | Ford Mustang | How long did Dave's work on the Ford Mustang take? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99Z3GWCGERD39GSHD50K6 | San Francisco | How long was the car modification workshop in San Francisco? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99Z5Y41PJHZDBE34V5MY2 | Boston | When did Dave take a photo of a Boston clock tower? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99Z7Z21XVNJDAEX6WCTRW | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZH14RRZ1GBAH4DV1WMM | Proud | What car brand does Calvin own that he is proud of? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZKYTQKZVD4Z6WH17M8Q | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZR69W5SCB7BK6GA03PW | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZS8CY5GRGPK7YWX9QSX | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZV8P2P9VWMAAVGHMCR8 | Boston | What plans do Calvin and Dave have for when Calvin visits Boston? |
| U1/conv-50 | retrievals.query_text | ret_01M7H99ZYH5EG6YKXN0GBCNBZR | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A01M3YV41MT63704PTZJ | Working on cars | What does working on cars represent for Dave? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A07BDEXBDEXP9XNN12XH | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A08924DTP1QHSEWBGXGQ | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A0FJS89FS616R3FFQ2TW | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| U1/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FP | Running | What does Melanie say running has been great for? |
| K/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FQ | Running | What does Melanie say running has been great for? |
| K/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FR | Running | What does Melanie say running has been great for? |
| K/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FS | Running | What does Melanie say running has been great for? |
| K/conv-26 | learned_keys.text | key_01M7H6X9B1MVCTMADBBPD8S6FT | Running | What does Melanie say running has been great for? |
| K/conv-26 | retrievals.query_text | ret_01M7H6VYX907JBYQ8JBVKCX0XJ | Running | What does Melanie say running has been great for? |
| K/conv-26 | retrievals.query_text | ret_01M7H6VYZMDX62JYZDQ45Y7BEQ | painting | What inspired Caroline's painting for the art show? |
| K/conv-26 | retrievals.query_text | ret_01M7H6VZ846WD5FXRZ0FQDCJPT | painting | What kind of painting did Caroline share with Melanie on October 13, 2023? |
| K/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XJ | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | learned_keys.text | key_01M7H7VS9Y2N40BX17PK9F64XK | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | learned_keys.text | key_01M7H7VSAYEB4PDYPCBZRB39RT | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| K/conv-41 | learned_keys.text | key_01M7H7VSAZDX62JYZDQ45Y7BEQ | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| K/conv-41 | learned_keys.text | key_01M7H7VSBG9Z8F8CV4NQHCJAXX | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| K/conv-41 | learned_keys.text | key_01M7H7VSBH6WD5FXRZ0FQDCJPT | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| K/conv-41 | retrievals.query_text | ret_01M7H7502G4996KBXZQ01MEE90 | beach | When did Maria go to the beach? |
| K/conv-41 | retrievals.query_text | ret_01M7H750RVWCGERD39GSHD50K6 | community work | When did Maria take up community work with her church friends? |
| K/conv-41 | retrievals.query_text | ret_01M7H750SPXXHZNY7QEGYCD1TP | Coco | When did Maria get Coco? |
| K/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Shadow | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | retrievals.query_text | ret_01M7H750V541PJHZDBE34V5MY2 | Coco | How many weeks passed between Maria adopting Coco and Shadow? |
| K/conv-41 | retrievals.query_text | ret_01M7H7518Z7B2FA1NQARA2R37X | fundraiser | What does Maria need to spread the word about for the fundraiser for the volunteer shelter? |
| K/conv-41 | retrievals.query_text | ret_01M7H751FT5EG6YKXN0GBCNBZR | assistant manager | What was one of the biggest challenges John faced in his journey to becoming assistant manager? |
| K/conv-42 | learned_keys.text | key_01M7H8490Z0ZSV5TRF308E3M4X | Joanna | Is it likely that Nate has friends besides Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H84911BN5NCX3MM7TV6ZF2 | Joanna | Is it likely that Nate has friends besides Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H84918P2Z7MTYC9MYNM1SC | Joanna | What are Joanna's hobbies? |
| K/conv-42 | learned_keys.text | key_01M7H8491HSAK2ZZA969PYWJXM | Joanna | Which outdoor spot did Joanna visit in May? |
| K/conv-42 | learned_keys.text | key_01M7H8491JDPPH8Q68GVZHRCNB | Joanna | Which outdoor spot did Joanna visit in May? |
| K/conv-42 | learned_keys.text | key_01M7H8491QSG9SY1T55TPPNJDP | May 2022 | Who was the new addition to Nate's family in May 2022? |
| K/conv-42 | learned_keys.text | key_01M7H8492N41PJHZDBE34V5MY3 | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| K/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNP | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNQ | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNR | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNS | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | learned_keys.text | key_01M7H849340V6D1PE6PHTZBJNT | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHH | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| K/conv-42 | learned_keys.text | key_01M7H849391J8W0GJVBGJSXXHJ | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| K/conv-42 | learned_keys.text | key_01M7H8493Y07JBYQ8JBVKCX0XK | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| K/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| K/conv-42 | learned_keys.text | key_01M7H84945ZPS8FMJT6QCT94RW | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| K/conv-42 | learned_keys.text | key_01M7H84947MVCTMADBBPD8S6FP | Turtles. | Why does Nate like turtles as pets? |
| K/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07H | Joanna | What was Joanna's audition for? |
| K/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07J | Joanna | What was Joanna's audition for? |
| K/conv-42 | learned_keys.text | key_01M7H8494CZ8R8RP3X4HZFF07K | Joanna | What was Joanna's audition for? |
| K/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPT | Joanna | What superhero is Joanna a fan of? |
| K/conv-42 | learned_keys.text | key_01M7H864R96WD5FXRZ0FQDCJPV | Joanna | What superhero is Joanna a fan of? |
| K/conv-42 | learned_keys.text | key_01M7H864RAWH55FFS77PM2F5BS | Joanna | What superhero is Joanna a fan of? |
| K/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHB | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H864RW2BEWZC2NJSJEVKHC | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DX | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DY | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H864RX7TYZ0PR3T7V140DZ | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | learned_keys.text | key_01M7H864SEKAHT0VWJ0J0TMN84 | Joanna | What specific themes are explored in Joanna's new book? |
| K/conv-42 | learned_keys.text | key_01M7H864SPK2TV7EBM1Z12TET5 | Joanna | What kind of impact does Joanna hope to have with her writing? |
| K/conv-42 | learned_keys.text | key_01M7H864SSV46TJR4DBME45J56 | Joanna | What motivates Joanna to keep writing even on tough days? |
| K/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZV | Joanna | What motivates Joanna to keep writing even on tough days? |
| K/conv-42 | learned_keys.text | key_01M7H864STKC4CQS3709XTTBZW | Joanna | What motivates Joanna to keep writing even on tough days? |
| K/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGNC | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SXXE593KYSWVEYQGND | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS1 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS2 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS3 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS4 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS5 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SYGRNX1YEHT98W7XS9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC6 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC7 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC8 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYC9 | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCA | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864SZ9K9XWBDZRZXEKYCB | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GJ | Joanna | What did Joanna share with her writers group in August 2022? |
| K/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GK | Joanna | What did Joanna share with her writers group in August 2022? |
| K/conv-42 | learned_keys.text | key_01M7H864T2EBZVG1XQDFBG79GM | Joanna | What did Joanna share with her writers group in August 2022? |
| K/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | her book | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EA | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | her book | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | learned_keys.text | key_01M7H864T5ADK0J7NVP9FX10EB | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JE | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| K/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JF | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| K/conv-42 | learned_keys.text | key_01M7H864TA1MC18J61WPT8J7JG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| K/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A1 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| K/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A2 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| K/conv-42 | learned_keys.text | key_01M7H864TFSBTCDB1WYNA273A3 | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| K/conv-42 | learned_keys.text | key_01M7H864TJ26NK86CJA2MCFQS4 | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMH | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMJ | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMK | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | learned_keys.text | key_01M7H864TKBTXW58G6P5HVCGMM | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | learned_keys.text | key_01M7H864TSTSVY01RDKNKCRAG9 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| K/conv-42 | learned_keys.text | key_01M7H864V52AJ3JR012PH63NC5 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| K/conv-42 | learned_keys.text | key_01M7H864V7H5YM3RZFSKTYDQKN | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| K/conv-42 | learned_keys.text | key_01M7H864V9JMTT6V6GYGWR5BA0 | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| K/conv-42 | learned_keys.text | key_01M7H864VBW9PG3EF120AD2PCF | Joanna | What encouragement does Nate give to Joanna after her setback? |
| K/conv-42 | learned_keys.text | key_01M7H864VDD8FND55GFK6R0XKN | Joanna | Where does Joanna get her ideas for the characters from? |
| K/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFE | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| K/conv-42 | learned_keys.text | key_01M7H864VEXW943XMFF9WBDEFF | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| K/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4M | Turtles. | What type of diet do Nate's turtles have? |
| K/conv-42 | learned_keys.text | key_01M7H864VMDE2GXAF99MEMHB4N | Turtles. | What type of diet do Nate's turtles have? |
| K/conv-42 | learned_keys.text | key_01M7H864VR3SD9KQMD3VNHDZ9S | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| K/conv-42 | learned_keys.text | key_01M7H864VWBBC86E4BZY9BVW11 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JFQ0ZSV5TRF308E3M4W | Joanna | Is it likely that Nate has friends besides Joanna? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JGVBN5NCX3MM7TV6ZF2 | Joanna | What kind of interests do Joanna and Nate share? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JJ5XZTTG156R1Z8DZ19 | Joanna | What are Joanna's hobbies? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JJSP2Z7MTYC9MYNM1SB | Joanna | What is Joanna allergic to? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JKCHSV4C48NDPKRDBSC | Joanna | What underlying condition might Joanna have based on her allergies? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JM2R5ZTFXYZFH1J05P4 | Joanna | What nickname does Nate use for Joanna? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JMQX0Y4X0ZM4ZEJYHTN | purple | When did Nate get purple hair? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JNAWEGC07PRYFAY5Z43 | Joanna | Which outdoor spot did Joanna visit in May? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JP0SAK2ZZA969PYWJXK | Joanna | How many times has Joanna found new hiking trails? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JQ66JFKQTP1E1HQW9D6 | May 2022 | Who was the new addition to Nate's family in May 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | screenplay | When did Joanna start writing her third screenplay? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JQV3E6A30YP4VZX604Z | Joanna | When did Joanna start writing her third screenplay? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JRGSG9SY1T55TPPNJDN | A gaming party | When is Nate hosting a gaming party? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JS1ST2F7MHVD15EGY1H | Joanna | When did Joanna hike with her buddies? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JTA0ERZ4SK9BWXVWSGW | Joanna | What places has Joanna submitted her work to? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JTZS63XNN9QRKBKH68B | Joanna | When is Joanna going to make Nate's ice cream for her family? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JVN5K6QQY3CPD53DDFV | Joanna | What kind of writings does Joanna do? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JXP6ZW9NBP4B3RPAKCG | Joanna | Where did Joanna travel to in July 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JYA7ETGDJBGB6HZQN26 | Joanna | When did someone write Joanna a touching letter? |
| K/conv-42 | retrievals.query_text | ret_01M7H82JYY2XR4NYWHRVWZ5DMT | Joanna | What book recommendations has Joanna given to Nate? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | her book | When did Joanna share her book with her writers group? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K05M74VFS80VSX49P99 | Joanna | When did Joanna share her book with her writers group? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K0SP0Z0KNRDBBS7Y8A5 | Joanna | When did Joanna make a desert with almond milk? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K1EP7B5T51EHGVD1KAS | a cooking show | When did Nate attend a cooking show? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | her book | How long did it take for Joanna to finish writing her book? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K287ZKTPX3RYZ61D3Q6 | Joanna | How long did it take for Joanna to finish writing her book? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K3EXXHZNY7QEGYCD1TP | Joanna | What is something Nate gave to Joanna that brings her a lot of joy? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K4266P93Z5P3QZYYXZ1 | Joanna | When did Nate get Tilly for Joanna? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K5841PJHZDBE34V5MY2 | Joanna | When did Joanna plan to go over to Nate's and share recipes? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K6R21XVNJDAEX6WCTRW | Joanna | How many letters has Joanna recieved? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K7ED79SY0CX9WD7DC1P | Valorant | When did Nate win a big Valorant tourney? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K85KPGW15JTGAQYHTS8 | Joanna | How many hikes has Joanna been on? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | Turtles. | What activities does Nate do with his turtles? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K8T2N40BX17PK9F64XH | his turtles | What activities does Nate do with his turtles? |
| K/conv-42 | retrievals.query_text | ret_01M7H82K9FQX65AZ8J79N0HX09 | Joanna | What do both Joanna and Nate appreciate the beauty of? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KAB7QAXRCVSNCJQHZTT | Joanna | What recommendations has Nate received from Joanna? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KAY9NDEJXHECWBGT1RP | vegan ice cream | How has Nate tried to disburse his vegan ice-cream recipes? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KBK0V6D1PE6PHTZBJNP | Joanna | How many screenplays has Joanna written? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KCR4RRZ1GBAH4DV1WMM | Joanna | What kind of job is Joanna beginning to preform the duties of because of her movie scripts? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | screenplay | What is Joanna's third screenplay about? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KG07B2FA1NQARA2R37X | Joanna | What is Joanna's third screenplay about? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KHQCY5GRGPK7YWX9QSX | Joanna | What did Joanna just finish last Friday on 23 January, 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | Turtles. | For how long has Nate had his turtles? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KJA4N425SA5W2Q2YPJD | his turtles | For how long has Nate had his turtles? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KKEQW30V4394CA5WXNF | Joanna | What did Joanna recently watch and recommend to Nate on February 7, 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | screenplay | What inspired Joanna's new screenplay on 25 February, 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KM307JBYQ8JBVKCX0XJ | Joanna | What inspired Joanna's new screenplay on 25 February, 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KMN5EG6YKXN0GBCNBZR | Turtles. | Why does Nate like turtles as pets? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KN7EB4PDYPCBZRB39RS | Turtles. | How does Nate describe the process of taking care of turtles? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KNTDX62JYZDQ45Y7BEQ | Joanna | What was Joanna's audition for? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KSPZ8R8RP3X4HZFF07G | Joanna | What superhero is Joanna a fan of? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KVJ24DTP1QHSEWBGXGQ | Joanna | What recipe Nate offer to share with Joanna? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KWS9Z8F8CV4NQHCJAXW | Joanna | Where did Joanna go for a road trip for research? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KXE6WD5FXRZ0FQDCJPT | Joanna | What specific themes are explored in Joanna's new book? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KY2WH55FFS77PM2F5BS | Joanna | What inspired Joanna's new script in July 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82KYN3PWFMMMDFH25DG2B | Joanna | What did Nate do while Joanna was on her road trip? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M07PC2W9Q4FCQC0J2SH | Joanna | What kind of impact does Joanna hope to have with her writing? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M12S89FS616R3FFQ2TW | Joanna | What motivates Joanna to keep writing even on tough days? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M1TKQYS07SMQ316BS4N | Joanna | What type of ice cream does Joanna mention that Nate makes and is delicious? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M2J2BEWZC2NJSJEVKHA | Joanna | What did Joanna share with her writers group in August 2022? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | her book | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M377TYZ0PR3T7V140DX | Joanna | How did Joanna celebrate after sharing her book with her writers group? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M4JFES9ZQC9V9PKC3XG | Joanna | Why is Joanna experimenting with dairy-free options in her dessert recipes? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M5WFYCV55KN50DYRDAR | Joanna | What dessert did Joanna share a photo of that has an almond flour crust, chocolate ganache, and fresh raspberries? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M6QC8JH0JZFHKN2A39D | Joanna | What kind of cake did Joanna share a photo of that she likes making for birthdays and special days? |
| K/conv-42 | retrievals.query_text | ret_01M7H82M7FK2TV7EBM1Z12TET4 | Joanna | What did Joanna make for one of the ladies at her writing club? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MA7GRNX1YEHT98W7XS1 | Joanna | What does Joanna recommend to make a living room comfy like hers? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MAV9K9XWBDZRZXEKYC6 | Joanna | Why did Joanna name the stuffed animal dog Tilly? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MBGEBZVG1XQDFBG79GH | Joanna | How does Nate feel about Joanna's ability to bounce back from setbacks? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MCAADK0J7NVP9FX10E9 | Joanna | What encouragement does Nate give to Joanna after her setback? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MD08B1HN99H0VP8H5XX | Joanna | Where does Joanna get her ideas for the characters from? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MDK1MC18J61WPT8J7JD | Joanna | How did Joanna feel on October 25, 2022 about seeing her characters come alive on the big screen? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MEBDW03EDH2VVHDP7FD | Turtles. | What type of diet do Nate's turtles have? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MEXSBTCDB1WYNA273A0 | Joanna | What did Joanna find in old notebooks last week that prompted her to reflect on her progress as a writer? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MFM26NK86CJA2MCFQS3 | Joanna | What did Joanna receive from her brother that brought back childhood memories? |
| K/conv-42 | retrievals.query_text | ret_01M7H82MGXTSVY01RDKNKCRAG8 | Joanna | What advice does Joanna give to Nate about making YouTube videos? |
| K/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | "That" | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | learned_keys.text | key_01M7H8GCT50ZSV5TRF308E3M4X | basketball | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | "That" | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | learned_keys.text | key_01M7H8GCT6BN5NCX3MM7TV6ZF2 | basketball | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTV | May, 2023 | Which team did John sign with on 21 May, 2023? |
| K/conv-43 | learned_keys.text | key_01M7H8GCW67QAXRCVSNCJQHZTW | May, 2023 | Which team did John sign with on 21 May, 2023? |
| K/conv-43 | learned_keys.text | key_01M7H8GCW89NDEJXHECWBGT1RQ | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| K/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Y | surfing | How long has John been surfing? |
| K/conv-43 | learned_keys.text | key_01M7H8GCWQ7B2FA1NQARA2R37Z | surfing | How long has John been surfing? |
| K/conv-43 | learned_keys.text | key_01M7H8GCWSJ5E1ZCAPY1628QZ2 | surfing | How does John feel while surfing? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX307JBYQ8JBVKCX0XK | basketball | What is John's number one goal in his basketball career? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX6EB4PDYPCBZRB39RT | August 2023 | What new activity has Tim started learning in August 2023? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BER | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BES | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8GCX8DX62JYZDQ45Y7BET | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XH | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XJ | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HVFES9ZQC9V9PKC3XK | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN83 | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN84 | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN85 | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN86 | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HWKAHT0VWJ0J0TMN87 | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8HXFYCV55KN50DYRDAR | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | learned_keys.text | key_01M7H8J8J0C8JH0JZFHKN2A39E | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| K/conv-43 | learned_keys.text | key_01M7H8J8JG9K9XWBDZRZXEKYC7 | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| K/conv-43 | learned_keys.text | key_01M7H8J8K2TSVY01RDKNKCRAG9 | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| K/conv-43 | learned_keys.text | key_01M7H8J8K4JQBQ8887RK2P69E9 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | "That" | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYN20ZSV5TRF308E3M4W | basketball | What are John's goals for his career that are not related to his basketball skills? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYQSP2Z7MTYC9MYNM1SB | Seattle | When was John in Seattle for a game? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYRBHSV4C48NDPKRDBSC | surfing | What year did John start surfing? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYT7WEGC07PRYFAY5Z43 | "That" | What is a prominent charity organization that John might want to work with and why? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYTWSAK2ZZA969PYWJXK | Chicago | When did John meet with his teammates after returning from Chicago? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYW16JFKQTP1E1HQW9D6 | August 15, 2023 | Where was John between August 11 and August 15 2023? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYWK3E6A30YP4VZX604Z | basketball | What schools did John play basketball in and how many years was he with his team during high school? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYXVST2F7MHVD15EGY1H | September 2023 | Which US states might Tim be in during September 2023 based on his plans of visiting Universal Studios? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYYE6KF4JZN6CSW3148W | basketball | What could John do after his basketball career? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CYYZ0ERZ4SK9BWXVWSGW | basketball | Who is Tim and John's favorite basketball player? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ1X7ETGDJBGB6HZQN26 | LeBron James | What does John like about Lebron James? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ316A76G13RWC6RRNC4 | November, 2023. | Where was Tim in the week before 16 November 2023? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ3KM74VFS80VSX49P99 | Greenhouse | When did John get married at a greenhouse? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ4SP7B5T51EHGVD1KAS | yoga | What kind of yoga for building core strength might John benefit from? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ7466P93Z5P3QZYYXZ1 | basketball | When did John organize a basketball camp for kids? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ854EASMNNC86WRSW03 | "That" | What is a Star Wars book that Tim might enjoy? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ8N21XVNJDAEX6WCTRW | Ireland | When will Tim leave for Ireland? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ96D79SY0CX9WD7DC1P | Ireland | Which Star Wars-related locations would Tim enjoy during his visit to Ireland? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZ9SKPGW15JTGAQYHTS8 | May, 2023 | Which team did John sign with on 21 May, 2023? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZAC2N40BX17PK9F64XH | Harry Potter | What aspects of the Harry Potter universe will be discussed in John's fan project collaborations? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZBM7QAXRCVSNCJQHZTT | Harry Potter | What kind of picture did Tim share as part of their Harry Potter book collection? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZDW4RRZ1GBAH4DV1WMM | surfing | How long has John been surfing? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZED1J8W0GJVBGJSXXHH | surfing | How does John feel while surfing? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZFHTQKZVD4Z6WH17M8Q | Fantasy | Which two fantasy novels does Tim particularly enjoy writing about? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZH8J5E1ZCAPY1628QZ1 | basketball | What is John's number one goal in his basketball career? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZJDCY5GRGPK7YWX9QSX | August 2023 | What new activity has Tim started learning in August 2023? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | Harry Potter | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZJY4N425SA5W2Q2YPJD | "Harry Potter and the Philosopher's Stone" | What special memory does "Harry Potter and the Philosopher's Stone" bring to Tim? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZQDZPS8FMJT6QCT94RV | basketball | What does John want to do after his basketball career? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZSPZ8R8RP3X4HZFF07G | basketball | Which basketball team does Tim support? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZY56WD5FXRZ0FQDCJPT | "That" | What genre is the novel that Tim is writing? |
| K/conv-43 | retrievals.query_text | ret_01M7H8CZYRWH55FFS77PM2F5BS | J.K. Rowling | What J.K. Rowling quote does Tim resonate with? |
| K/conv-43 | retrievals.query_text | ret_01M7H8D02Z7TYZ0PR3T7V140DX | December, 2023 | What book did Tim just finish reading on 8th December, 2023? |
| K/conv-43 | retrievals.query_text | ret_01M7H8D05ZC8JH0JZFHKN2A39D | German | What language does Tim know besides German? |
| K/conv-43 | retrievals.query_text | ret_01M7H8D06JK2TV7EBM1Z12TET4 | "That" | What book did Tim get in Italy that inspired him to cook? |
| K/conv-43 | retrievals.query_text | ret_01M7H8D089XE593KYSWVEYQGNB | Aragorn | What is the painting of Aragorn a reminder for John to be in everything he does? |
| K/conv-43 | retrievals.query_text | ret_01M7H8D08ZGRNX1YEHT98W7XS1 | Ireland | Which city in Ireland will Tim be staying in during his semester abroad? |
| K/conv-44 | learned_keys.text | key_01M7H8RZ2NHSV4C48NDPKRDBSC | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| K/conv-44 | learned_keys.text | key_01M7H8RZ35SG9SY1T55TPPNJDP | August | Where did Andrew go during the first weekend of August 2023? |
| K/conv-44 | learned_keys.text | key_01M7H8RZ3D5K6QQY3CPD53DDFW | work | How does Andrew feel about his current work? |
| K/conv-44 | learned_keys.text | key_01M7H8RZ4821XVNJDAEX6WCTRX | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| K/conv-44 | learned_keys.text | key_01M7H8RZ5807JBYQ8JBVKCX0XJ | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QXGTP2Z7MTYC9MYNM1SB | three | How many years passed between Audrey adopting Pixie and her other three dogs? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QXK6WEGC07PRYFAY5Z43 | Go hiking | When is Andrew going to go hiking with Audrey? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QXNF3E6A30YP4VZX604Z | August | Where did Andrew go during the first weekend of August 2023? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QXQV0ERZ4SK9BWXVWSGW | work | How does Andrew feel about his current work? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QY0066P93Z5P3QZYYXZ1 | Family | What did Andrew express missing about exploring nature trails with his family's dog? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement | Why did Audrey think positive reinforcement training is important for pets? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QY3H2N40BX17PK9F64XH | Positive reinforcement training | Why did Audrey think positive reinforcement training is important for pets? |
| K/conv-44 | retrievals.query_text | ret_01M7H8QY7T1J8W0GJVBGJSXXHH | Grooming | What advice did Audrey give to Andrew regarding grooming Toby? |
| K/conv-48 | learned_keys.text | key_01M7H9188W21XVNJDAEX6WCTRX | play the console | Did Jolene teach herself how to play the console? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZGYP4996KBXZQ01MEE90 | in 2010 | Which country were Jolene and her mother visiting in 2010? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZH1ZR5ZTFXYZFH1J05P4 | Seraphim | When did Jolene buy her pet Seraphim? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHEC7ETGDJBGB6HZQN26 | meditation | How long has Jolene been doing yoga and meditation? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHFW6A76G13RWC6RRNC4 | Seraphim | When did Jolene buy a new aquarium for Seraphim? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHGQM74VFS80VSX49P99 | Susie | When did Jolene adopt her snake Susie? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHK87ZKTPX3RYZ61D3Q6 | last week of August 2023 | Which country was Jolene located in during the last week of August 2023? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHQG4EASMNNC86WRSW03 | play the console | Did Jolene teach herself how to play the console? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHSWKPGW15JTGAQYHTS8 | Surfing | Has Jolene tried surfing? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZHTK2N40BX17PK9F64XH | Surfing | When did the Deboran and Jolene agree to go surfing? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZJEXMVCTMADBBPD8S6FN | Seraphim | For how long has Jolene had Seraphim as a pet? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZJFP6N88KZKJQH3J89S2 | running group | What made being part of the running group easy for Deborah to stay motivated? |
| K/conv-48 | retrievals.query_text | ret_01M7H8ZJJCDEXBDEXP9XNN12XH | Susie | How did Jolene come to have her pet, Susie? |
| K/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP4 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB3RK00MYF4VJRXFBCP5 | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMM | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB3S4RRZ1GBAH4DV1WMN | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB4QQW30V4394CA5WXNG | Proud | What car brand does Calvin own that he is proud of? |
| K/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BER | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| K/conv-50 | learned_keys.text | key_01M7H9BB50DX62JYZDQ45Y7BES | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| K/conv-50 | learned_keys.text | key_01M7H9BB5C0QA51R4Y4T02QW73 | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGQ | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGR | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGS | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB5H24DTP1QHSEWBGXGT | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9BB603PWFMMMDFH25DG2C | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| K/conv-50 | learned_keys.text | key_01M7H9D6TAKC4CQS3709XTTBZW | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| K/conv-50 | learned_keys.text | key_01M7H9D6TFXE593KYSWVEYQGNC | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| K/conv-50 | learned_keys.text | key_01M7H9D6VG26NK86CJA2MCFQS4 | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| K/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9D6VMBTXW58G6P5HVCGMJ | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-50 | learned_keys.text | key_01M7H9D6VNTSVY01RDKNKCRAG8 | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YFQX0Y4X0ZM4ZEJYHTN | Tokyo | Which places or events has Calvin visited in Tokyo? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YND3E6A30YP4VZX604Z | two weeks | Where was Dave in the last two weeks of August 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | Boston | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| K/conv-50 | retrievals.query_text | ret_01M7H99YR96KF4JZN6CSW3148W | November 2023 | Based on the conversation, did Calvin and Dave have a meeting in Boston between August and November 2023? Answer in yes or no. |
| K/conv-50 | retrievals.query_text | ret_01M7H99YSA0ERZ4SK9BWXVWSGW | October 3, 2023 | Which city was Calvin at on October 3, 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YT4S63XNN9QRKBKH68B | Boston | When did Calvin met with local artists in Boston? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YWM6ZW9NBP4B3RPAKCG | Ferrari | When did Calvin buy his second Ferrari? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YXH7ETGDJBGB6HZQN26 | Tokyo | When did Calvin plan on travelling to Tokyo the second time? |
| K/conv-50 | retrievals.query_text | ret_01M7H99YZ96A76G13RWC6RRNC4 | Boston | When did Calvin visit some of the sights in Boston with a former high school friend? |
| K/conv-50 | retrievals.query_text | ret_01M7H99Z0YP0Z0KNRDBBS7Y8A5 | auto engineering | Which events in Dave's life inspired him to take up auto engineering? |
| K/conv-50 | retrievals.query_text | ret_01M7H99Z2N7ZKTPX3RYZ61D3Q6 | Ford Mustang | How long did Dave's work on the Ford Mustang take? |
| K/conv-50 | retrievals.query_text | ret_01M7H99Z3GWCGERD39GSHD50K6 | San Francisco | How long was the car modification workshop in San Francisco? |
| K/conv-50 | retrievals.query_text | ret_01M7H99Z5Y41PJHZDBE34V5MY2 | Boston | When did Dave take a photo of a Boston clock tower? |
| K/conv-50 | retrievals.query_text | ret_01M7H99Z7Z21XVNJDAEX6WCTRW | last week of October 2023 | Where was Calvin located in the last week of October 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZH14RRZ1GBAH4DV1WMM | Proud | What car brand does Calvin own that he is proud of? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZKYTQKZVD4Z6WH17M8Q | Tokyo | How did the audience in Tokyo react when Calvin sang one of his songs? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZR69W5SCB7BK6GA03PW | September 2023 | Where did Dave come back from with insights on car modification on 1st September 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZS8CY5GRGPK7YWX9QSX | September 2023 | What did Calvin book a flight ticket for on 1st September 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZV8P2P9VWMAAVGHMCR8 | Boston | What plans do Calvin and Dave have for when Calvin visits Boston? |
| K/conv-50 | retrievals.query_text | ret_01M7H99ZYH5EG6YKXN0GBCNBZR | His Dad | Which song from the childhood of Calvin brings back memories of a road trip with his dad? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A01M3YV41MT63704PTZJ | Working on cars | What does working on cars represent for Dave? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A07BDEXBDEXP9XNN12XH | Tokyo | What specific location in Tokyo does Calvin mention being excited to explore? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A08924DTP1QHSEWBGXGQ | Tokyo | What dish does Dave recommend Calvin to try in Tokyo? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A0FJS89FS616R3FFQ2TW | Car mods | What kind of impact does Dave's blog on car mods have on people? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | Boston | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |
| K/conv-50 | retrievals.query_text | ret_01M7H9A0GMKQYS07SMQ316BS4N | November 2023 | Who did Calvin invite to see him perform in Boston on 13 November, 2023? |

Use half (U0, base database): 63.4% [59.4, 67.3] (361/569).
Unparsed judge verdicts (scored WRONG): 0.
