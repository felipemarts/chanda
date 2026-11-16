# Usage-guided sleep: results

Protocol: `PROTOCOL.md` (pre-registered in dfb68b0, before any LoCoMo
run). Implementation: decisions 0142 (guided sleep) and 0143 (derived share
of the block). Settings frozen after the bench: decision 0144 (tag
`sleep-freeze`), unchanged from the task's proposal. Diagnosis that
motivated it: `DIAGNOSIS.md`. Bench (memorisation, diagnostic): `BENCH.md`.

All runs use deepseek-flash at temperature 0, e5-small embeddings and a
2,000-token context. Every Chanda condition uses the D2 selection
(decision 0141) with the derived share at 0.30, the two best raw excerpts
protected.

## Summary

Test half, 3 conversations pooled (conv-30, conv-47, conv-49; 197
questions, each asked once per condition):

| Condition | Accuracy [Wilson 95%] | vs S0: only this / only S0 | p (sign test) | Full evidence in block | Sleep cost US$ | Sleep share of block |
| --- | --- | --- | --- | --- | --- | --- |
| B plain RAG | 66.0% [59.1, 72.2] | 21 / 29 | 0.32 | 143 / 197 | – | 0% |
| **S0** D2, phase 1 only | **70.1%** [63.3, 76.0] | – | – | 139 / 197 | 0 | 0% |
| S1 guided, extractive | 66.0% [59.1, 72.2] | 10 / 18 | 0.19 | 128 / 197 | 0 (no LLM) | 29.2% |
| S2 guided, LLM | 66.5% [59.6, 72.7] | 13 / 20 | 0.30 | 127 / 197 | 0.095 | 29.7% |
| S3 uniform (old), S2's tokens | 68.0% [61.2, 74.1] | 17 / 21 | 0.63 | 130 / 197 | 0.090 | 27.4% |

By category (test half):

| Condition | multi-hop (35) | temporal (47) | open-domain (14) | single-hop (101) |
| --- | --- | --- | --- | --- |
| B | 18 | 25 | 5 | 82 |
| S0 | 16 | 27 | 7 | 88 |
| S1 | **11** | 27 | 8 | 84 |
| S2 | **12** | 26 | 9 | 84 |
| S3 | 16 | **22** | 8 | 88 |

Per conversation, test half correct:

| Conversation | n | S0 | S1 | S2 | S3 | B |
| --- | --- | --- | --- | --- | --- | --- |
| conv-30 | 41 | 31 | 29 | 28 | 25 | 27 |
| conv-47 | 76 | 56 | 51 | 52 | 58 | 57 |
| conv-49 | 80 | 51 | 50 | 51 | 51 | 46 |

The pooled numbers are the protocol's measure. Each conversation alone is
too small to read.

Ceiling (secondary; the use half asked again after sleep, which measures
memorisation): before sleep 67.9%, S0 68.4%, S1 66.8%, S2 67.4%, S3 68.9%
(190 questions).

## What the decision rule allows

"Usage-guided sleep improves later answers" **cannot be stated**, for
either mode:

- S1 is lower than S0 (66.0% against 70.1%; 10 against 18; p = 0.19);
- S2 is lower than S0 (66.5% against 70.1%; 13 against 20; p = 0.30).

Neither difference is established in the other direction either: the data
are compatible with no effect and with a loss of a few points. The two
tests were not corrected for multiplicity (both fail regardless).

Nor is anything established about the old sleep (S3 against S0, p = 0.63)
or plain RAG against Chanda (B against S0, p = 0.32) on this data.

## Where the sleep made things worse

- **Evidence was displaced.** Full evidence in the block fell from 139
  (S0) to 128 (S1) and 127 (S2). Of the 18 questions S1 lost against S0, 10
  lost an evidence turn from the block. The ficha takes up to 30% of the
  budget (600 tokens), and the protection covers only the two best raw
  excerpts, so the third and later evidence turns are the ones pushed
  out. Of S2's 20 losses, 10 are of this kind.
- **Multi-hop fell most** (16 to 11 for S1, 12 for S2, of 35). Multi-hop
  needs several turns from different sessions, which is exactly the raw
  budget the ficha takes.
- **In the other losses, the evidence stayed and the answer still changed**
  (8 of S1's, 10 of S2's): the reader took a ficha line over the raw turn,
  or answered "Not mentioned" with the evidence in the block (conv-30-q20).
- **The old sleep scored lower on temporal questions** (27 to 22 of 47).
  This was not analysed further.

The wins are real but fewer: S1 gained 10 questions, 2 of them by a ficha
that cites an evidence turn the raw block lacked; S2 gained 13, again 2 of
them that way.

## Five good fichas

Each is a ficha line in a test-half block where S0 was wrong and the sleep
condition right.

1. **conv-49-q122, S1** ("What digestive issue did Sam experience
   lately?", gold: gastritis). The evidence turn (D14:1) was not in S0's
   block; S0 answered "Not mentioned". The ficha carried it literally:
   `2023-10-17 | assistant | evt_… | Sam: … ended up in the ER with a
   severe stomachache. Turns out, it was gastritis …`. Answer: "Gastritis".
2. **conv-47-q25, S1 and S2** ("Are John and James fans of the same
   football team?", gold: no, Liverpool and Manchester City). S0 lacked
   D13:15 and answered "Yes …". Both fichas held John's line ("As a
   Manchester City fan, I can't agree with you"; S2: "John says as a
   Manchester City fan he can't agree …"). Answer: "No".
3. **conv-49-q116, S1** ("What painting did Evan share with Sam in
   October?", gold: a cactus in the desert). The ficha line kept the photo
   caption of the dated turn; S0 answered "a sunset", S1 "A painting of a
   cactus in the desert".
4. **conv-30-q4, S2** ("Why did Jon decide to start his dance studio?",
   gold: he lost his job …). The raw block lacked D1:2; an LLM line from a
   later session said "Jon says losing his job pushed him to start his own
   dance studio business". Answer correct.
5. **conv-30-q49, S1** ("What did Gina want her customers to feel in her
   store?", gold: cozy and comfortable). The extractive window kept "Cozy
   and inviting - perfect for customers …"; S0 answered "a cool oasis".

## Five bad fichas

1. **conv-30-q40, S1** ("What is Jon's favorite style of dance?", gold:
   contemporary). Three fichas titled "jon, thanks, great", "gina, what,
   great", "gina, studio, jon" held greetings ("Jon: Thanks! I'm going for
   my dreams!", "Gina: Wow, it looks great! What dances do you practice?")
   and took the room of the evidence turn ("contemporary is my top pick").
   S0 right, S1 "Not mentioned".
2. **conv-47-q23, S2** ("When will John start his new job?", gold: July
   2022). Fichas of chit-chat ("John agrees with James."; "James says he has
   been checking out different styles of a hobby …") displaced the short
   evidence turn "I'm starting next month". S2 "Not mentioned".
3. **conv-47-q77, S2** ("What advice did James receive from the famous
   players …?"). Two fichas digested the same event ("John played his
   favorite CS:GO game in an intense tournament …") almost word for word;
   the evidence turn with the advice was pushed out. S2 "gaming tips".
4. **conv-47-q111, S2** ("Which RPG game is John playing … on 10 August,
   2022?", gold: The Witcher 3). Two fichas of 284 and 288 tokens
   ("James greets John and asks what's up …"; "James tells John he will
   keep him updated on all the new games and says bye for now") filled the
   share; the evidence turn was lost. S2 "Not mentioned".
5. **conv-30-q20, S1** ("When did Gina get accepted for the design
   internship?"). The evidence turn stayed in the block, but four fichas
   repeated the same greetings across groups ("Gina: Wow! What did you
   get?" in two of them), and one held the interview line ("I had an
   interview for a design internship yesterday"). S0 "2023-05-27", S1
   "Not mentioned".

The pattern of the bad ones: in conversation data, the group terms are
names and small talk ("jon, thanks, great"), so the extractive window
picks greetings, and the LLM summarises greetings faithfully. Groups that
overlap below the merge threshold produce near-duplicate fichas. The ficha
is shortened to the share room by best window, so a block often holds two
or three partial fichas rather than one whole one.

## Cost

| | US$ |
| --- | --- |
| Bench (9 questions: reader, judge, LLM digest) | 0.022 |
| LoCoMo reader and judge (use, test, ceiling; all conditions) | 1.443 |
| LoCoMo sleep: S2 LLM digest | 0.095 |
| LoCoMo sleep: S3 uniform | 0.090 |
| Unverified: the proxy's estimate for 3 calls that ended in an upstream error | 0.003 |
| **Total** (proxy, `cost.json`) | **1.653** of the US$2 cap |

3,479 calls, 977 of them answered from the call store at no cost. The run
was made at the user's request without the off-peak restriction, partly
in peak hours (prices from `eval/cmd/lmeproxy/prices.deepseek.json`).

Per sleep mode and conversation:

| Mode | Conversation | LLM calls | Tokens in/out | US$ | Time s | Groups cand. / merged / digested / left | Lines written | Lines discarded |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| S1 | conv-30 | 0 | – | 0 | 10.2 | 40 / 34 / 33 / 0 | 329 | 7 repeated |
| S1 | conv-47 | 0 | – | 0 | 0.5 | 74 / 73 / 49 / 24 | 492 | 0 |
| S1 | conv-49 | 0 | – | 0 | 15.6 | 76 / 70 / 49 / 21 | 496 | 0 |
| S2 | conv-30 | 33 | 40,339 / 20,585 | 0.017 | 117 | 40 / 34 / 33 / 0 | 291 | 60 unsupported_name |
| S2 | conv-47 | 49 | 56,170 / 30,031 | 0.025 | 165 | 74 / 73 / 49 / 24 | 433 | 81 unsupported_name |
| S2 | conv-49 | 49 | 59,997 / 32,632 | 0.053 | 191 | 76 / 70 / 49 / 21 | 482 | 44 unsupported_name |
| S3 | conv-30 | 23 | 43,479 / 8,653 | 0.022 | 61 | – | – | – |
| S3 | conv-47 | 31 | 67,904 / 16,298 | 0.038 | 98 | – | – | – |
| S3 | conv-49 | 28 | 58,875 / 12,044 | 0.030 | 78 | – | – | – |

The verifier discarded 185 of 1,391 LLM lines (13%), all for a proper
name not found in the cited turns. Its text was not kept, so which names
cannot be shown; see Limitations.

Gain per dollar: S2 −3.6 points for US$0.095 (−37 points per dollar). S1
−4.1 points at no LLM cost.

## Deviations

1. **S0 runs phase 1.** Pre-registered in the protocol: every sleep
   recomputes utility from the use half's outcomes, so S0 runs phase 1
   only and S1/S2 against S0 measures the digest alone.
2. **Runs in peak hours.** The user asked to run at once; the off-peak
   flag of the proxy was turned off (`OFFPEAK_ONLY=0` in `lib.sh`). The
   US$2 cap stayed on.
3. **The leak check's literal-origin rule became a review.** The run
   stopped on conv-30 S2: gold strings in fichas not found literally in
   their origins. They were third-person rewrites (`leak-review.md`). The
   driver now logs these and they were all reviewed by hand after the run;
   tampered outcomes, test questions in retrievals and judge words remain
   hard stops. No test half was asked before its condition's check had
   run.
4. **conv-47 rerun after an outcome rate limit.** Chanda allows 60
   outcomes per minute; conv-47's use half has 74. The first attempt
   recorded 60, the driver took the partial file as complete, and S0 and
   S1 ran their sleep and test half on 60 outcomes. Those runs were set
   aside (`work/locomo/conv-47/discarded-60-outcomes/`), the 14 missing
   outcomes were recorded and S0, S1 and S2 were run again. The set-aside
   runs scored S0 56/76 and S1 51/76, the same as the kept ones. The
   harness now waits on the rate limit and checks the outcome count.
5. **The guided sleep stopped at its event budget** on conv-47 and conv-49
   (500 events; 24 and 21 groups left undigested). This is the configured
   default budget, not tuned; both runs are "partial".
6. **Part 0 premise.** The task said 25 battery runs stopped at the token
   ceiling: 23 hit the event ceiling and 2 a provider error
   (`DIAGNOSIS.md` item 3).
7. **"Items from sleep" = facts and pages** in the composition rule
   (decision 0143): in this data all of them come from sleep.

## Limitations

- Three conversations, 197 test questions, one run per condition, one
  reader model, an automatic judge with a prompt written for this study.
  A difference of 4 points is within what this sample can resolve.
- LoCoMo's gold answers have known errors (issues #27, #35, #43, #44);
  they affect all conditions alike.
- The use half is 40 to 76 questions per conversation, so each sleep saw
  one retrieval per question and few repeats; "usage" is thin. A memory in
  long use would see the same topics again and again, which this test does
  not reproduce.
- Only the frozen settings were tested. The bench did not justify tuning,
  so no other share, top k, window or group rule was tried; a smaller
  share or a larger top k may behave differently and is untested.
- The verifier's discarded lines were counted, not stored.
- The derived-share rule caps the ficha, but derived candidates still take
  candidate slots before the cut (decision 0143, not covered).

## What the data support

- On LoCoMo, with a 2,000-token block, adding usage-guided fichas did not
  improve answers to later questions: S1 66.0% and S2 66.5% against S0
  70.1% on 197 test questions, both lower, neither difference established.
- The fichas cost evidence: full evidence in the block fell from 139 to
  128 (S1) and 127 (S2); multi-hop questions fell the most.
- The guided sleep is cheap: the extractive mode makes no LLM call, and
  the LLM mode cost US$0.095 for three conversations, against US$1.63 for
  the old sleep on the battery's 60 questions.
- The old sleep's facts in the battery displaced evidence turns
  (`DIAGNOSIS.md`); about 95% of the events it read had no evidence for the
  question.
- When a ficha line carries the answer literally and the raw block lacks
  it, the reader uses it (2 such wins per mode, five examples above).
- The block composition rule holds its cap. Sleep items never exceeded 30%
  of the budget. In all 591 blocks of S1, S2 and S3, the two best-ranked
  raw excerpts that passed the cut were delivered.

## What the data do not support

- That usage-guided sleep improves later answers (the pre-registered rule
  is not met for S1 or S2).
- That it makes answers worse: the losses are not established either
  (p = 0.19 and 0.30).
- That the LLM digest is better than the extractive one, or the reverse
  (66.5% against 66.0%).
- That the old uniform sleep helps or hurts on LoCoMo (68.0% against
  70.1%, p = 0.63).
- That Chanda beats plain RAG on LoCoMo (70.1% against 66.0%, p = 0.32).
- Anything about other shares, other top-k values, longer use, or other
  datasets: none was tested.
