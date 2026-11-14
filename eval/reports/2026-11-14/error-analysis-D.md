# Error analysis: condition D (Chanda without sleep, 2,000-token block), LongMemEval S, 60-question sample

## Sources and method

- D: `eval/reports/2026-11-14/raw/D.jsonl`. **47/60 correct, 13 wrong.** B: `raw/B.jsonl`.
- Sample order: `eval/sample.json` `questions[]`. D.jsonl is **not** in sample order, so the failures were re-sorted by sample index.
- Dataset: the 13 failing questions were stream-extracted from `eval/lme/data/longmemeval_s_cleaned.json` (incremental `raw_decode`, peak RSS about 0.5 GB) to scratch `dataset13.json`.
- Search ranks: `eval/lme/out/phase1-e5-60/phase1.questions.jsonl`, `conditions.chanda.ranked_top20` (session ranking from up to 100 search items). **Only the top 20 sessions are stored.** A session outside the top 20 is reported as ">20 of N ranked", and its exact rank is unknown.
- "In context": I checked whether the `has_answer` turn text (its first or last 120 characters) appears verbatim in D's `context` field. I also checked by hand for paraphrases of the fact in other turns. Turns that hold the fact but are not flagged `has_answer` were found by searching the haystack.
- Caveat: `context_meta.evidence_recall_in_context` is **session-level**. It counts a gold session as present when any excerpt of that session is in the block, even if the evidence turn is not. Several questions below show recall 0.75–1.0 at session level and 0 at turn level.

### Failures in sample order (index in sample.json)

| # | idx | question_id | type | B correct? |
|---|---|---|---|---|
| 1 | 2 | 2c63a862 | temporal-reasoning | yes |
| 2 | 3 | 2ce6a0f2 | multi-session | yes |
| 3 | 18 | gpt4_15e38248 | multi-session | no |
| 4 | 23 | 09ba9854_abs | multi-session (abstention) | no |
| 5 | 24 | gpt4_f420262d | temporal-reasoning | yes |
| 6 | 26 | gpt4_fe651585 | temporal-reasoning | no |
| 7 | 35 | e3038f8c | multi-session | no |
| 8 | 36 | 91b15a6e | multi-session | yes |
| 9 | 46 | 88432d0a | multi-session | no |
| 10 | 47 | gpt4_1e4a8aec | temporal-reasoning | no |

Not analysed (11th–13th, by id only): **gpt4_2f56ae70, gpt4_731e37d7, 370a8ff4**.

---

## 1. 2c63a862 (temporal-reasoning)

- **Q:** How many days did it take for me to find a house I loved after starting to work with Rachel? (question date 2022/03/02)
- **Gold:** 14 days (15 accepted).
- **D:** 19 days. It took Feb 10 (the mortgage pre-approval date) as the start date.
- **Class: 2, cut removed it.**
- **Evidence:**
  - Start date: answer_d39b7977_1, turn 8, user: *"Since I started working with her on 2/15, ..."*. **Not in context.** Session at search rank 2, retrieved, but only other turns of it made the block (a "Rachel, she's been helping me" turn and a truncated assistant turn about Tustin/Lake Forest).
  - End date: answer_d39b7977_2, turn 0: *"I recently saw a house that I really love on 3/1"*. Session at search rank 1. That exact turn is not in context, but a sibling turn is: *"...pre-approved ... on February 10th ... The house I saw on March 1st really checks all the boxes"*. So the end date was present.
  - The block had the end date and a distractor date (Feb 10), but not the start date. The reader filled the gap with the distractor.
- **B:** correct (14). B's block had the "2/15" turn.

## 2. 2ce6a0f2 (multi-session, counting)

- **Q:** How many different art-related events did I attend in the past month? (2023/03/08)
- **Gold:** 4.
- **D:** "no information".
- **Class: 2, cut removed it.**
- **Evidence (4 facts needed; 0 of 4 in context):**
  - answer_901a6763_2, turn 0: *"volunteered at the Children's Museum for their 'Art Afternoon' event on February 17th"*. Rank 1, session retrieved, **turn not in context**.
  - answer_901a6763_1, turn 2: *"the 'Women in Art' exhibition which I attended on February 10th"*. Rank 2, session retrieved, **turn not in context**.
  - answer_901a6763_4, turn 0: *"attended a lecture at the Art Gallery on 'The Evolution of Street Art' on March 3rd"*. Rank 3, session retrieved, **turn not in context**. Only a truncated assistant turn about Shepard Fairey made it.
  - answer_901a6763_3, turn 0: *"guided tour at the History Museum on February 24th"*. Rank 4, **session not in context**.
  - The block is filled with long, generic assistant answers ("how to find local art events") from the same sessions. Session-level recall says 0.75; turn-level recall is 0.
- **B:** correct (4).

## 3. gpt4_15e38248 (multi-session, counting)

- **Q:** How many pieces of furniture did I buy, assemble, sell, or fix in the past few months? (2023/05/30)
- **Gold:** 4.
- **D:** 3 (bookshelf assembled, coffee table bought, kitchen table fixed).
- **Class: 1, search miss** (for the one missing fact; see the uncertainty note).
- **Evidence (4 facts; 3 of 4 in context):**
  - IKEA bookshelf assembled. answer_8858d9dc_2, turn 0. Rank 1, **in context**.
  - Kitchen table leg fixed, plus the new coffee table. answer_8858d9dc_4, turn 2. Rank 3, **in context**.
  - Coffee table bought from West Elm. answer_8858d9dc_1, turn 0. Session not in the top 20 (31 ranked) and not in context, but redundant: the coffee table is already covered by the _4 turn.
  - **Casper mattress ordered.** answer_8858d9dc_3, turn 0: *"last week I finally took the plunge and ordered one from Casper"*. Session **not in the top 20 of 31 ranked**, not in context. This is the missing item.
- **Uncertainty:** the _3 session may sit at rank 21–31, which would still be inside the ≤100 search items. In that case one could argue "cut". But it was not among the top 20 ranked sessions, and the mattress sentence is incidental in a throw-pillow conversation. So "search miss" is the better fit. The reader did nothing wrong with what it had.
- **B:** wrong (2).

## 4. 09ba9854_abs (multi-session, abstention)

- **Q:** How much will I save by taking the bus from the airport to my hotel instead of a taxi? (2023/05/30)
- **Gold:** not answerable. The user never said what the bus costs.
- **D:** "$40–$50". This takes the taxi at $60 (user) and the bus at $10–20 (the assistant's generic estimate).
- **Class: 4, reader error with the right context.**
- **Evidence:**
  - answer_96c743d0_abs_1, turn 6: *"I was told that taking a taxi from the airport to my hotel would cost around $60"*. Rank 1, **in context**.
  - The context also holds the assistant's line *"The Airport Limousine Bus ... fare is around $10-$20"*.
  - The second gold session (answer_96c743d0_abs_2, rank 2) is not in context. In this _abs variant it carries no `has_answer` turn.
  - The context contains everything needed to abstain: no user-stated bus fare. The reader computed from the assistant's estimate instead.
- **Note:** this is not a judge disagreement, because the answer does not abstain. But the gold depends on treating the assistant's own fare estimate as "not information the user gave", which is debatable. B made exactly the same error.
- **B:** wrong.

## 5. gpt4_f420262d (temporal-reasoning)

- **Q:** What was the airline that I flied with on Valentine's day? (2023/03/02)
- **Gold:** American Airlines.
- **D:** "no information; on Feb 14 you were only planning a trip".
- **Class: 2, cut removed it** (borderline with 4; see the note).
- **Evidence:**
  - answer_d8a1af6c_5 (dated 2023-02-14), turn 0: *"I'm still recovering from my American Airlines flight from LAX to JFK, which was delayed by 2 hours"*. Session at rank 1, retrieved, **turn not in context**.
  - The block has three other turns of that session. The last, truncated, item is the assistant reply dated 2023-02-14: *"Sorry to hear that your last flight with American Airlines was delayed due to bad weather conditions!"*.
  - Distractor sessions are also present (2023-01-15 and 2022-11-17, both mentioning AA in-flight entertainment).
  - Sessions _4 (rank 6) and _3 (rank 7) are not in context.
- **Why borderline:** the assistant echo, dated Valentine's Day, lets a careful reader infer "AA". But the user's own statement was cut, and the echo says "last flight", not "today". I keep "cut" as primary because the needed user turn is missing. The reader was also over-strict with the indirect evidence it did have.
- **B:** correct. B had the turn 0 text.

## 6. gpt4_fe651585 (temporal-reasoning)

- **Q:** Who became a parent first, Rachel or Alex? (2023/03/17)
- **Gold:** Alex.
- **D:** Rachel. The twins "already celebrating their first birthday" implies a birth around March 2022.
- **Class: 2, cut removed it** (with a dataset-quality note).
- **Evidence:**
  - Both `has_answer` turns are **in context**:
    - answer_65600ff6_2, turn 0: *"my cousin Alex just adopted a baby girl from China in January"*. Rank 2.
    - answer_65600ff6_1, turn 8: *"Rachel, is doing great with the twins, Jackson and Julia"*. Rank 1.
  - These two turns do not settle the order. The deciding fact is in turn 0 of answer_65600ff6_1, which is **not flagged `has_answer`**: *"my sister-in-law's twins, Jackson and Julia, who were born on February 12th"*. That turn is **not in context**; "February 12" does not occur in the block.
  - What is in context is the haystack assistant's own line *"now they're already celebrating their first birthday!"*. This, together with turn 2's "first birthday party coming up", contradicts a Feb-2023 birth. The reader followed it.
- **Why not class 4:** with the block as delivered, "Rachel" is a reasonable reading. The reader did not misuse correct evidence.
- **Dataset note:** the haystack is internally inconsistent: born Feb 12 (presumably 2023) versus a first birthday coming up. B also lacked "February 12" in its block and gave the same wrong answer.
- **B:** wrong (Rachel).

## 7. e3038f8c (multi-session, counting)

- **Q:** How many rare items do I have in total? (2023/05/30)
- **Gold:** 99 (12 figurines + 5 books + 57 records + 25 coins).
- **D:** "no information".
- **Class: 2, cut removed it.**
- **Evidence (4 facts; 0 of 4 in context):**
  - 25 rare coins. answer_b6018747_3, turn 4: *"I actually have 25 rare coins"*. Rank 2, session retrieved, **turn not in context**. A sibling turn about "update my inventory list of rare records" is in.
  - 12 rare figurines. answer_b6018747_2, turn 4. Rank 4, **not in context**.
  - 5 rare books. answer_b6018747_4, turn 0. Rank 5, **not in context**.
  - 57 rare records. answer_b6018747_1, turn 0: *"my 57 rare records"*. Rank 6, **not in context**.
  - Rank 1 is a distractor session (a3d8e134_2, "starting a collection of rare items"). Its long generic assistant answers take most of the 2,000 tokens.
  - All four gold sessions were in the top 6 of the search, so search found them; the cut dropped them.
- **B:** wrong (95: it found 4 counts but replaced the 5 books with a 1-item vase).

## 8. 91b15a6e (multi-session)

- **Q:** What is the minimum amount I could get if I sold the vintage diamond necklace and the antique vanity? (2023/05/30)
- **Gold:** $5,150.
- **D:** $5,200 (necklace $5,000 plus the assistant's lowest resale estimate of $200).
- **Class: 2, cut removed it.**
- **Evidence:**
  - Necklace. answer_5404a208_1, turn 6: *"my vintage diamond necklace, which is worth $5,000"*. Rank 1, **in context**.
  - Vanity. answer_5404a208_2, turn 8: *"I bought it for $150 and put in some work to restore it, so I'm confident it's worth at least that amount now"*. Rank 2, session retrieved, **turn not in context**.
  - The block has the assistant turn of the same session ("The $150 you paid ... you might be able to sell it for $200-$300"). The reader used that turn's $200.
- **B:** correct ($5,150).

## 9. 88432d0a (multi-session, counting)

- **Q:** How many times did I bake something in the past two weeks? (2023/05/30)
- **Gold:** 4.
- **D:** 1 (cookies).
- **Class: 2, cut removed it** (one of the four facts is borderline search).
- **Evidence (4 facts; 1 of 4 in context):**
  - Cookies baked with convection. answer_733e443a_2, turn 2. Rank 3, **in context**. A second mention of the same cookies from session _4 is also in.
  - Whole wheat baguette "last Saturday". answer_733e443a_4, turn 0. Rank 1, session retrieved, **turn not in context**.
  - Sourdough bread "on Tuesday". answer_733e443a_3, turn 0. Rank 7, **not in context**.
  - Chocolate cake "last weekend". answer_733e443a_1, turn 0. Rank 13 of 31, **not in context**.
  - Three of the four sessions were in the search top 7, and the fourth at 13. Meanwhile the block spent tokens on unrelated sessions: jam packaging, meal prep, a productivity chat, and a long blue-cheese-cookie blog post.
- **B:** wrong (3).

## 10. gpt4_1e4a8aec (temporal-reasoning)

- **Q:** What gardening-related activity did I do two weeks ago? (2023/05/05)
- **Gold:** planting 12 new tomato saplings.
- **D:** "planning how often to water your tomato plants (04-21)".
- **Class: 2, cut removed it.**
- **Evidence:**
  - answer_16bd5ea6_2 (2023-04-21), turn 0: *"I just planted 12 new tomato saplings today"*. Session at rank 2, retrieved, **turn not in context**.
  - Another turn of the same session (the watering question, dated 04-21) is in.
  - The reader got the date arithmetic right (04-21 = two weeks before) and picked the only 04-21 gardening text it had.
- **B:** wrong (a gardening workshop).

---

## Counts per class (first 10 D failures)

| Class | Count | Questions |
|---|---|---|
| 1. search miss | 1 | gpt4_15e38248 (uncertain: the missing session may be at rank 21–31) |
| 2. cut removed it | 8 | 2c63a862, 2ce6a0f2, gpt4_f420262d*, gpt4_fe651585†, e3038f8c, 91b15a6e, 88432d0a, gpt4_1e4a8aec |
| 3. stale value | 0 | none |
| 4. reader error with the right context | 1 | 09ba9854_abs (abstention; no judge disagreement) |

\* Borderline with class 4: an indirect assistant echo of the fact was in the block.
† The deciding turn is not flagged `has_answer`, and the haystack itself is inconsistent.

Plain RAG (B) got 4 of these 10 right: 2c63a862, 2ce6a0f2, gpt4_f420262d, 91b15a6e. All four are "cut removed it" cases where B's block contained the evidence turn.

## Patterns (only what was observed)

1. **The search is almost never the bottleneck.** In 8 of 10 questions every needed session was in Chanda's top 7 (often ranks 1–2). The exceptions: 88432d0a had one session at rank 13, and gpt4_15e38248 had one session outside the top 20. The failures come from turn selection inside the 2,000-token block: the block holds the right *sessions* but the wrong *turns*. Long, generic assistant replies from the same session (or from a lexical distractor session at rank 1) take the budget, while the short user turn carrying the fact ("on 2/15", "25 rare coins", "planted 12 tomato saplings today", "at least $150") is left out.
2. **Session-level `evidence_recall_in_context` overstates what the reader got.** It was 0.75–1.0 on several questions whose turn-level recall was 0 (2ce6a0f2, 2c63a862, gpt4_1e4a8aec).
3. **Counting questions fail by undercount.** D counted 1 of 4, 3 of 4, and "no information" twice. Over the four counting questions the block held 0/4, 0/4, 1/4 and 3/4 of the needed facts.
4. **Temporal failures were not date-arithmetic errors.** The reader computed "two weeks ago" and day spans correctly. It anchored on the wrong date or event because the dated fact was missing: Feb 10 pre-approval instead of the 2/15 start, a watering question instead of the planting turn on the same date.
5. **No stale-value failure appeared**, and the only reader-side failure was an abstention question where the reader treated the assistant's generic estimate as the user's data. B made the same mistake.
