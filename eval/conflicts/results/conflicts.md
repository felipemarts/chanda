# Conflict resolution in sleep: test results

107 of 107 test questions.

## Decision rule (PROTOCOL.md)

1. stale delivered falls from C0 to C1, exact paired sign test p < 0.05: C0 30/31, C1 29/31, p = 1.0000: **fails**
2. hidden evidence in at most 2 of the 107 questions: 0 questions: **holds**
3. C1's accuracy is not lower than C0's with p < 0.05: C0 89/107, C1 88/107, p = 1.0000: **holds**

The claim "conflict resolution reduces outdated information in the context without hiding valid information" was not established. sleep.conflicts stays off by default, and no other variant is proposed or built.

## Primary measures

- Outdated information delivered (31 stale-measure questions): C0 30, C1 29; discordant: only C0 1, only C1 0; exact sign test p = 1.0000.
- Hidden evidence: 0 questions (0 turns).


## Secondary measures

- Accuracy (n = 107): C0 89 (83.2%, Wilson 75.0%-89.1%), C1 88 (82.2%, Wilson 73.9%-88.3%); only C0 1, only C1 0, sign test p = 1.0000.
- Knowledge-update (outdated, current) pairs: 42 qualifying; same event 0; candidate 16; judged 16; applied 1 (reversed 0); correct 1 of 16 scored.
- Applied supersessions by class: {"correct": 1, "wrong": 1}.
- Candidates per history: mean 9.056, max 29; judged: mean 9.056, max 29.

| question type | histories | candidates | judged | verdicts | applied | accuracy C0 | accuracy C1 |
|---|---|---|---|---|---|---|---|
| knowledge-update | 47 | 444 | 444 | {"coexist": 138, "unrelated": 283, "uncertain": 21, "supersedes": 2} | 2 | 42/47 | 42/47 |
| multi-session | 16 | 173 | 173 | {"coexist": 58, "unrelated": 110, "uncertain": 5} | 0 | 9/16 | 9/16 |
| single-session-assistant | 8 | 47 | 47 | {"coexist": 7, "uncertain": 2, "unrelated": 38} | 0 | 8/8 | 8/8 |
| single-session-preference | 5 | 35 | 35 | {"coexist": 9, "unrelated": 25, "uncertain": 1} | 0 | 3/5 | 3/5 |
| single-session-user | 9 | 50 | 50 | {"coexist": 13, "unrelated": 37} | 0 | 9/9 | 9/9 |
| temporal-reasoning | 22 | 220 | 220 | {"unrelated": 148, "coexist": 71, "uncertain": 1} | 0 | 18/22 | 17/22 |

Sleep per history (mean / median / max): calls 9.056 / 8 / 29; input tokens 20815.813 / 17543 / 65747; output tokens 194.579 / 171 / 633; seconds 10.58 / 9.141 / 32.941; US$ (paid-equivalent) 0.003 / 0.0023170379999999996 / 0.007752953999999999, total 0.2754613199999999.
Spent (cost file): US$ 0.694127094000001 of the US$ 2 cap.

| question | type | candidates | judged | verdicts | applied | stale C0 / C1 | hidden | sleep calls | sleep s | sleep US$ |
|---|---|---|---|---|---|---|---|---|---|---|
| 031748ae | knowledge-update | 8 | 8 | {"coexist": 1, "unrelated": 7} | 0 | yes / yes | 0 | 8 | 9.971 | 0.00236 |
| 031748ae_abs | knowledge-update | 10 | 10 | {"coexist": 6, "unrelated": 4} | 0 | - | 0 | 10 | 10.899 | 0.00248 |
| 06db6396 | knowledge-update | 3 | 3 | {"coexist": 1, "unrelated": 2} | 0 | yes / yes | 0 | 3 | 3.928 | 0.00088 |
| 07741c44 | knowledge-update | 8 | 8 | {"coexist": 5, "unrelated": 3} | 0 | - | 0 | 8 | 9.823 | 0.00225 |
| 07741c45 | knowledge-update | 12 | 12 | {"coexist": 7, "unrelated": 5} | 0 | yes / yes | 0 | 12 | 15.042 | 0.00320 |
| 08e075c7 | knowledge-update | 8 | 8 | {"coexist": 2, "unrelated": 6} | 0 | yes / yes | 0 | 8 | 8.907 | 0.00248 |
| 0977f2af | knowledge-update | 9 | 9 | {"coexist": 1, "uncertain": 1, "unrelated": 7} | 0 | - | 0 | 9 | 11.235 | 0.00271 |
| 0ddfec37 | knowledge-update | 16 | 16 | {"coexist": 4, "uncertain": 1, "unrelated": 11} | 0 | - | 0 | 16 | 18.627 | 0.00482 |
| 10e09553 | knowledge-update | 4 | 4 | {"uncertain": 1, "unrelated": 3} | 0 | - | 0 | 4 | 4.375 | 0.00112 |
| 184da446 | knowledge-update | 19 | 19 | {"coexist": 6, "uncertain": 2, "unrelated": 11} | 0 | yes / yes | 0 | 19 | 23.199 | 0.00549 |
| 18bc8abd | knowledge-update | 7 | 7 | {"uncertain": 1, "unrelated": 6} | 0 | yes / yes | 0 | 7 | 8.834 | 0.00237 |
| 1cea1afa | knowledge-update | 28 | 28 | {"coexist": 13, "supersedes": 1, "uncertain": 1, "unrelated": 13} | 1 | no / no | 0 | 28 | 32.941 | 0.00775 |
| 2133c1b5 | knowledge-update | 5 | 5 | {"coexist": 1, "unrelated": 4} | 0 | yes / yes | 0 | 5 | 5.535 | 0.00162 |
| 2133c1b5_abs | knowledge-update | 10 | 10 | {"coexist": 6, "unrelated": 4} | 0 | - | 0 | 10 | 11.484 | 0.00272 |
| 3ba21379 | knowledge-update | 11 | 11 | {"coexist": 1, "unrelated": 10} | 0 | yes / yes | 0 | 11 | 12.241 | 0.00349 |
| 42ec0761 | knowledge-update | 8 | 8 | {"coexist": 2, "unrelated": 6} | 0 | yes / yes | 0 | 8 | 9.338 | 0.00234 |
| 45dc21b6 | knowledge-update | 6 | 6 | {"coexist": 3, "unrelated": 3} | 0 | yes / yes | 0 | 6 | 7.569 | 0.00175 |
| 59524333 | knowledge-update | 5 | 5 | {"coexist": 1, "uncertain": 1, "unrelated": 3} | 0 | yes / yes | 0 | 5 | 5.715 | 0.00153 |
| 5a4f22c0 | knowledge-update | 15 | 15 | {"coexist": 6, "unrelated": 9} | 0 | - | 0 | 15 | 16.803 | 0.00433 |
| 5c40ec5b | knowledge-update | 5 | 5 | {"coexist": 2, "unrelated": 3} | 0 | yes / yes | 0 | 5 | 5.382 | 0.00155 |
| 618f13b2 | knowledge-update | 7 | 7 | {"coexist": 2, "uncertain": 1, "unrelated": 4} | 0 | - | 0 | 7 | 8.575 | 0.00192 |
| 69fee5aa | knowledge-update | 9 | 9 | {"coexist": 2, "uncertain": 1, "unrelated": 6} | 0 | yes / yes | 0 | 9 | 10.625 | 0.00295 |
| 6a1eabeb | knowledge-update | 14 | 14 | {"uncertain": 2, "unrelated": 12} | 0 | yes / yes | 0 | 14 | 16.363 | 0.00408 |
| 6aeb4375 | knowledge-update | 16 | 16 | {"coexist": 8, "unrelated": 8} | 0 | yes / yes | 0 | 16 | 21.08 | 0.00501 |
| 71315a70 | knowledge-update | 10 | 10 | {"coexist": 5, "uncertain": 2, "unrelated": 3} | 0 | yes / yes | 0 | 10 | 12.878 | 0.00271 |
| 7e974930 | knowledge-update | 3 | 3 | {"coexist": 2, "unrelated": 1} | 0 | yes / yes | 0 | 3 | 3.391 | 0.00078 |
| 830ce83f | knowledge-update | 8 | 8 | {"coexist": 3, "unrelated": 5} | 0 | - | 0 | 8 | 9.883 | 0.00257 |
| 852ce960 | knowledge-update | 7 | 7 | {"unrelated": 7} | 0 | yes / yes | 0 | 7 | 9.333 | 0.00255 |
| 89941a93 | knowledge-update | 4 | 4 | {"coexist": 2, "unrelated": 2} | 0 | yes / yes | 0 | 4 | 4.961 | 0.00110 |
| 89941a94 | knowledge-update | 4 | 4 | {"coexist": 1, "unrelated": 3} | 0 | - | 0 | 4 | 4.807 | 0.00081 |
| 9bbe84a2 | knowledge-update | 7 | 7 | {"coexist": 1, "uncertain": 1, "unrelated": 5} | 0 | - | 0 | 7 | 7.854 | 0.00242 |
| a1eacc2a | knowledge-update | 9 | 9 | {"coexist": 2, "unrelated": 7} | 0 | yes / yes | 0 | 9 | 10.804 | 0.00260 |
| b01defab | knowledge-update | 20 | 20 | {"coexist": 7, "uncertain": 1, "unrelated": 12} | 0 | yes / yes | 0 | 20 | 23.597 | 0.00540 |
| ba61f0b9 | knowledge-update | 9 | 9 | {"coexist": 3, "unrelated": 6} | 0 | yes / yes | 0 | 9 | 10.012 | 0.00239 |
| c6853660 | knowledge-update | 12 | 12 | {"coexist": 5, "supersedes": 1, "uncertain": 3, "unrelated": 3} | 1 | yes / no | 0 | 12 | 13.738 | 0.00347 |
| c7dc5443 | knowledge-update | 6 | 6 | {"coexist": 3, "unrelated": 3} | 0 | yes / yes | 0 | 6 | 7.509 | 0.00181 |
| ce6d2d27 | knowledge-update | 5 | 5 | {"coexist": 3, "unrelated": 2} | 0 | yes / yes | 0 | 5 | 6.6 | 0.00136 |
| cf22b7bf | knowledge-update | 9 | 9 | {"coexist": 1, "unrelated": 8} | 0 | yes / yes | 0 | 9 | 10.129 | 0.00230 |
| dad224aa | knowledge-update | 18 | 18 | {"coexist": 3, "unrelated": 15} | 0 | yes / yes | 0 | 18 | 19.975 | 0.00439 |
| dfde3500 | knowledge-update | 8 | 8 | {"unrelated": 8} | 0 | - | 0 | 8 | 9.542 | 0.00261 |
| e493bb7c | knowledge-update | 3 | 3 | {"uncertain": 2, "unrelated": 1} | 0 | yes / yes | 0 | 3 | 3.243 | 0.00076 |
| e61a7584 | knowledge-update | 5 | 5 | {"unrelated": 5} | 0 | yes / yes | 0 | 5 | 6.819 | 0.00160 |
| e66b632c | knowledge-update | 7 | 7 | {"coexist": 4, "unrelated": 3} | 0 | - | 0 | 7 | 8.296 | 0.00172 |
| eace081b | knowledge-update | 7 | 7 | {"coexist": 3, "unrelated": 4} | 0 | - | 0 | 7 | 7.335 | 0.00190 |
| f685340e | knowledge-update | 25 | 25 | {"coexist": 7, "unrelated": 18} | 0 | - | 0 | 25 | 31.089 | 0.00751 |
| f685340e_abs | knowledge-update | 4 | 4 | {"coexist": 1, "unrelated": 3} | 0 | - | 0 | 4 | 4.957 | 0.00067 |
| f9e8c073 | knowledge-update | 11 | 11 | {"coexist": 2, "unrelated": 9} | 0 | yes / yes | 0 | 11 | 12.485 | 0.00353 |
| 09ba9854 | multi-session | 7 | 7 | {"coexist": 2, "unrelated": 5} | 0 | - | 0 | 7 | 8.523 | 0.00185 |
| 09d032c9 | single-session-preference | 9 | 9 | {"coexist": 3, "unrelated": 6} | 0 | - | 0 | 9 | 12.536 | 0.00287 |
| 0a995998 | multi-session | 5 | 5 | {"coexist": 3, "unrelated": 2} | 0 | - | 0 | 5 | 6.247 | 0.00140 |
| 0bc8ad93 | temporal-reasoning | 4 | 4 | {"unrelated": 4} | 0 | - | 0 | 4 | 4.761 | 0.00133 |
| 15745da0_abs | single-session-user | 3 | 3 | {"coexist": 1, "unrelated": 2} | 0 | - | 0 | 3 | 4.026 | 0.00084 |
| 16c90bf4 | single-session-assistant | 9 | 9 | {"coexist": 3, "uncertain": 1, "unrelated": 5} | 0 | - | 0 | 9 | 10.77 | 0.00235 |
| 1a8a66a6 | multi-session | 11 | 11 | {"coexist": 5, "unrelated": 6} | 0 | - | 0 | 11 | 13.045 | 0.00288 |
| 1de5cff2 | single-session-assistant | 4 | 4 | {"unrelated": 4} | 0 | - | 0 | 4 | 4.814 | 0.00103 |
| 1f2b8d4f | multi-session | 12 | 12 | {"coexist": 7, "unrelated": 5} | 0 | - | 0 | 12 | 14.802 | 0.00332 |
| 28dc39ac | multi-session | 24 | 24 | {"coexist": 4, "uncertain": 2, "unrelated": 18} | 0 | - | 0 | 24 | 26.905 | 0.00692 |
| 2bf43736 | single-session-assistant | 6 | 6 | {"unrelated": 6} | 0 | - | 0 | 6 | 7.443 | 0.00210 |
| 2e6d26dc | multi-session | 5 | 5 | {"coexist": 1, "unrelated": 4} | 0 | - | 0 | 5 | 6.124 | 0.00133 |
| 41275add | single-session-assistant | 13 | 13 | {"coexist": 2, "uncertain": 1, "unrelated": 10} | 0 | - | 0 | 13 | 15.237 | 0.00389 |
| 46a3abf7 | multi-session | 7 | 7 | {"coexist": 3, "uncertain": 1, "unrelated": 3} | 0 | - | 0 | 7 | 8.009 | 0.00224 |
| 5d3d2817 | single-session-user | 4 | 4 | {"coexist": 1, "unrelated": 3} | 0 | - | 0 | 4 | 4.623 | 0.00116 |
| 60bf93ed_abs | multi-session | 5 | 5 | {"coexist": 1, "uncertain": 1, "unrelated": 3} | 0 | - | 0 | 5 | 5.749 | 0.00118 |
| 6ade9755 | single-session-user | 9 | 9 | {"coexist": 1, "unrelated": 8} | 0 | - | 0 | 9 | 9.998 | 0.00218 |
| 6b7dfb22 | single-session-preference | 9 | 9 | {"coexist": 2, "unrelated": 7} | 0 | - | 0 | 9 | 10.369 | 0.00302 |
| 6e984302 | temporal-reasoning | 8 | 8 | {"coexist": 4, "unrelated": 4} | 0 | - | 0 | 8 | 8.468 | 0.00252 |
| 71017276 | temporal-reasoning | 8 | 8 | {"coexist": 2, "unrelated": 6} | 0 | - | 0 | 8 | 9.141 | 0.00198 |
| 7161e7e2 | single-session-assistant | 3 | 3 | {"unrelated": 3} | 0 | - | 0 | 3 | 3.651 | 0.00061 |
| 8077ef71 | temporal-reasoning | 9 | 9 | {"unrelated": 9} | 0 | - | 0 | 9 | 10.345 | 0.00241 |
| 80ec1f4f | multi-session | 14 | 14 | {"coexist": 3, "unrelated": 11} | 0 | - | 0 | 14 | 14.724 | 0.00328 |
| 87f22b4a | multi-session | 2 | 2 | {"coexist": 2} | 0 | - | 0 | 2 | 2.642 | 0.00062 |
| 8c18457d | temporal-reasoning | 29 | 29 | {"coexist": 14, "unrelated": 15} | 0 | - | 0 | 29 | 30.787 | 0.00665 |
| 982b5123 | temporal-reasoning | 7 | 7 | {"coexist": 3, "unrelated": 4} | 0 | - | 0 | 7 | 7.688 | 0.00202 |
| 993da5e2 | temporal-reasoning | 6 | 6 | {"coexist": 2, "unrelated": 4} | 0 | - | 0 | 6 | 6.577 | 0.00171 |
| 9a707b81 | temporal-reasoning | 7 | 7 | {"coexist": 2, "unrelated": 5} | 0 | - | 0 | 7 | 7.493 | 0.00173 |
| 9d25d4e0 | multi-session | 26 | 26 | {"coexist": 10, "unrelated": 16} | 0 | - | 0 | 26 | 29.634 | 0.00768 |
| a96c20ee | multi-session | 6 | 6 | {"coexist": 1, "unrelated": 5} | 0 | - | 0 | 6 | 6.55 | 0.00168 |
| af082822 | temporal-reasoning | 10 | 10 | {"coexist": 2, "unrelated": 8} | 0 | - | 0 | 10 | 10.396 | 0.00242 |
| b320f3f8 | single-session-user | 3 | 3 | {"unrelated": 3} | 0 | - | 0 | 3 | 3.344 | 0.00079 |
| b46e15ee | temporal-reasoning | 18 | 18 | {"coexist": 7, "unrelated": 11} | 0 | - | 0 | 18 | 20.1 | 0.00558 |
| b6025781 | single-session-preference | 7 | 7 | {"coexist": 2, "unrelated": 5} | 0 | - | 0 | 7 | 7.729 | 0.00189 |
| b759caee | single-session-assistant | 2 | 2 | {"unrelated": 2} | 0 | - | 0 | 2 | 2.887 | 0.00064 |
| c8c3f81d | single-session-user | 13 | 13 | {"coexist": 1, "unrelated": 12} | 0 | - | 0 | 13 | 14.74 | 0.00427 |
| c8f1aeed | single-session-assistant | 2 | 2 | {"coexist": 1, "unrelated": 1} | 0 | - | 0 | 2 | 2.352 | 0.00044 |
| caf9ead2 | single-session-user | 4 | 4 | {"coexist": 1, "unrelated": 3} | 0 | - | 0 | 4 | 4.665 | 0.00139 |
| d24813b1 | single-session-preference | 8 | 8 | {"coexist": 1, "uncertain": 1, "unrelated": 6} | 0 | - | 0 | 8 | 9.05 | 0.00203 |
| d6233ab6 | single-session-preference | 2 | 2 | {"coexist": 1, "unrelated": 1} | 0 | - | 0 | 2 | 2.569 | 0.00049 |
| dc439ea3 | single-session-assistant | 8 | 8 | {"coexist": 1, "unrelated": 7} | 0 | - | 0 | 8 | 9.108 | 0.00232 |
| ef9cf60a | multi-session | 4 | 4 | {"unrelated": 4} | 0 | - | 0 | 4 | 4.026 | 0.00107 |
| f4f1d8a4 | single-session-user | 1 | 1 | {"coexist": 1} | 0 | - | 0 | 1 | 1.422 | 0.00019 |
| f8c5f88b | single-session-user | 9 | 9 | {"coexist": 4, "unrelated": 5} | 0 | - | 0 | 9 | 9.725 | 0.00279 |
| faba32e5 | single-session-user | 4 | 4 | {"coexist": 3, "unrelated": 1} | 0 | - | 0 | 4 | 4.18 | 0.00089 |
| gpt4_1a1dc16d | temporal-reasoning | 6 | 6 | {"coexist": 2, "unrelated": 4} | 0 | - | 0 | 6 | 6.979 | 0.00170 |
| gpt4_2655b836 | temporal-reasoning | 11 | 11 | {"coexist": 5, "unrelated": 6} | 0 | - | 0 | 11 | 13.297 | 0.00281 |
| gpt4_2f8be40d | multi-session | 14 | 14 | {"coexist": 4, "unrelated": 10} | 0 | - | 0 | 14 | 15.53 | 0.00362 |
| gpt4_31ff4165 | multi-session | 11 | 11 | {"coexist": 7, "uncertain": 1, "unrelated": 3} | 0 | - | 0 | 11 | 12.676 | 0.00327 |
| gpt4_4929293a | temporal-reasoning | 3 | 3 | {"coexist": 2, "unrelated": 1} | 0 | - | 0 | 3 | 3.767 | 0.00106 |
| gpt4_5438fa52 | temporal-reasoning | 6 | 6 | {"coexist": 5, "unrelated": 1} | 0 | - | 0 | 6 | 6.884 | 0.00105 |
| gpt4_59149c78 | temporal-reasoning | 7 | 7 | {"unrelated": 7} | 0 | - | 0 | 7 | 9.528 | 0.00212 |
| gpt4_70e84552 | temporal-reasoning | 10 | 10 | {"coexist": 5, "unrelated": 5} | 0 | - | 0 | 10 | 12.286 | 0.00305 |
| gpt4_7a0daae1 | temporal-reasoning | 15 | 15 | {"unrelated": 15} | 0 | - | 0 | 15 | 17.392 | 0.00411 |
| gpt4_85da3956 | temporal-reasoning | 2 | 2 | {"unrelated": 2} | 0 | - | 0 | 2 | 2.4 | 0.00068 |
| gpt4_8e165409 | temporal-reasoning | 9 | 9 | {"coexist": 2, "unrelated": 7} | 0 | - | 0 | 9 | 10.074 | 0.00246 |
| gpt4_ab202e7f | multi-session | 20 | 20 | {"coexist": 5, "unrelated": 15} | 0 | - | 0 | 20 | 23.176 | 0.00669 |
| gpt4_e414231e | temporal-reasoning | 15 | 15 | {"coexist": 3, "unrelated": 12} | 0 | - | 0 | 15 | 18.295 | 0.00438 |
| gpt4_f420262c | temporal-reasoning | 25 | 25 | {"coexist": 11, "unrelated": 14} | 0 | - | 0 | 25 | 28.614 | 0.00679 |
| gpt4_fa19884d | temporal-reasoning | 5 | 5 | {"uncertain": 1, "unrelated": 4} | 0 | - | 0 | 5 | 5.465 | 0.00152 |

## Five correct supersessions

- 1cea1afa (knowledge-update): verdict supersedes, confidence 0.95
  - old: [2023/05/27 (Sat) 07:39, user, s2t0] I've been trying to increase my social media presence, especially on Instagram. Can you help me come up with some ideas for more engaging promotional content, like the kind that will resonate with my audience and get more likes and comments...
  - new: [2023/05/28 (Sun) 22:57, user, s47t0] I'm trying to increase my social media presence. Can you help me come up with some ideas for more engaging promotional content on Instagram? By the way, I just checked and I'm now at 600 followers, which is a nice milestone!

## Five wrong supersessions

- c6853660 (knowledge-update): verdict supersedes, confidence 0.95; not the (outdated, current) update
  - old: [2023/05/25 (Thu) 12:35, user, s2t0] I'm trying to make my morning routine more efficient. Can you give me some tips on how to optimize my coffee brewing method? By the way, I've switched to a darker roast and cut back to just one cup in the morning, which has been really help...
  - new: [2023/05/26 (Fri) 19:25, user, s13t6] I'm excited to try out the French press method with my darker roast. Speaking of my morning coffee, I have increased the limit to two cups, and I'm thinking of experimenting with different sweeteners, like stevia or honey. Do you have any r...
