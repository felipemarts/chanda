# Pre-check of judge verdicts: D failures and C-right/D-wrong cases (LongMemEval battery of 2026-11-14)

Prepared for the owner's human check. This is read-only: no LLM calls were made and no tracked files were changed.

**Sources**
- `eval/review.csv`: 60 rows. Its `answer_*` and `verdict_*` columns for C and D match `raw/C.jsonl` and `raw/D.jsonl` exactly (checked by script).
- `eval/reports/2026-11-14/raw/{B,C,D}.jsonl`.
- Evidence turns (`has_answer: true`) come from `longmemeval_s_cleaned.json`, read through the 13-question extract `scratchpad/dataset13.json`. All 13 cases are in that extract.
- Rubric: `eval/lme/py/prompts.py`. Its `judge_prompt` picks the template from the question type: DEFAULT, TEMPORAL (off-by-one *day* tolerance), KNOWLEDGE_UPDATE, PREFERENCE, or ABSTENTION for `_abs` ids.

**Case sets**
- Set 1, D wrong: 13 questions.
- Set 2, C right and D wrong: 6 questions: 2c63a862, 2ce6a0f2, gpt4_f420262d, gpt4_fe651585, e3038f8c, gpt4_1e4a8aec.
- Set 2 is entirely contained in set 1, so there are **13 unique cases**.
- There is no question where C is wrong and D is right.

The order below is: first the 6 cases in both sets, then the 7 cases in set 1 only. "In D ctx" and "in C ctx" record whether the decisive text was literally present in that condition's `context` field.

---

## Cases in set 1 and set 2 (C right, D wrong)

### 1. 2c63a862 (temporal-reasoning) [sets 1+2]
- **Q:** How many days did it take for me to find a house I loved after starting to work with Rachel? Question date 2022/03/02.
- **Gold:** "14 days. 15 days (including the last day) is also acceptable."
- **Evidence:**
  - answer_d39b7977_1 (2022/03/02), t8 user: "Since I started working with her on 2/15".
  - answer_d39b7977_2 (2022/03/02), t0 user: "I recently saw a house that I really love on 3/1".
- **Gold check:** 2/15 to 3/1/2022 is 14 days, or 15 counting both ends. **Gold OK.**
- **D (no):** "It took **19 days** from starting to work with Rachel (February 10) to finding the house". D used the mortgage pre-approval date; the 2/15 turn was not in its context.
- **C (yes):** "It took 14 days."
- **B:** yes.
- **My view on D: agree.** 19 is not 14 or 15. The off-by-one allowance (and the gold's explicit 15) do not cover it.
- **My view on C: agree.** 14 matches the gold exactly.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 2. 2ce6a0f2 (multi-session) [sets 1+2]
- **Q:** How many different art-related events did I attend in the past month? Question date 2023/03/08.
- **Gold:** 4.
- **Evidence (all sessions dated 2023/03/08):**
  - _2 t0: "volunteered at the Children's Museum for their 'Art Afternoon' event on February 17th".
  - _1 t2: "the 'Women in Art' exhibition which I attended on February 10th".
  - _4 t0: "attended a lecture at the Art Gallery on 'The Evolution of Street Art' on March 3rd".
  - _3 t0: "guided tour at the History Museum on February 24th, and it really sparked my interest in ancient history and art".
- **Gold check:** all four dates fall inside 2/8 to 3/8. Counting the History Museum tour as "art-related" is a little generous, but it is clearly what the dataset intends. **Gold OK.**
- **D (no):** "The available chat history does not contain enough information to determine how many art-related events you attended". None of the four facts was in D's context.
- **C (yes):** "Answer: 4 different art-related events." C lists all four.
- **B:** yes.
- **My view on D: agree.** D gives no number.
- **My view on C: agree.** C gives the exact count with the correct items.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 3. gpt4_f420262d (temporal-reasoning) [sets 1+2]
- **Q:** What was the airline that I flied with on Valentine's day? Question date 2023/03/02.
- **Gold:** American Airlines.
- **Evidence:**
  - answer_d8a1af6c_5 (2023/02/14), t0 user: "I'm still recovering from my American Airlines flight from LAX to JFK, which was delayed by 2 hours".
  - Also _4 (2023/02/20), t0: "considering flying with American Airlines ... terrible experience with it on my flight from New York to Los Angeles today". That is a different date.
- **Gold check:** the gold is an inference. "Still recovering" on 2/14 does not literally say the flight was on 2/14. It is the dataset's intended reading, and I see no competing airline for that date. **Gold acceptable** (mild inference).
- **D (no):** "The chat history does not contain information about an airline you flew with on Valentine's Day — it only shows you were planning a trip on that date." The user's AA turn was not in D's context; only an assistant echo was.
- **C (yes):** "Answer: American Airlines." C's reasoning hedges ("flew ... on that date (or was recovering from that flight)"), but its final answer is unambiguous.
- **B:** yes.
- **My view on D: agree.** D gives no airline; this is not an abstention question.
- **My view on C: agree.** C names the gold airline.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 4. gpt4_fe651585 (temporal-reasoning) [sets 1+2] — FLAG: haystack self-contradictory
- **Q:** Who became a parent first, Rachel or Alex? Question date 2023/03/17.
- **Gold:** Alex.
- **Evidence:**
  - answer_65600ff6_2 (2023/03/17), t0 user: "my cousin Alex just adopted a baby girl from China in January".
  - answer_65600ff6_1 (2023/03/17), t8 user: "my sister-in-law, Rachel, is doing great with the twins, Jackson and Julia".
  - The deciding turn is **not** flagged has_answer. It is _1 t0 user: "my sister-in-law's twins, Jackson and Julia, who were born on February 12th".
- **Gold check: unsure.** The same session contradicts itself:
  - t0 says "born on February 12th" (no year), and t1 (assistant) calls them "the new additions to your family". This points to Feb 2023, which makes **Alex** first.
  - t2 user says "Jackson and Julia's first birthday party coming up", and t9 (assistant) says "now they're already celebrating their first birthday!". This points to a 2022 birth, which makes **Rachel** first.
  - The gold follows the first reading, which is the more plausible intent. But the evidence does not settle it.
- **In context:** D's context lacked "February 12" and had "first birthday"; C's context had both.
- **D (no):** "Rachel became a parent first (about a year before Alex)."
- **C (yes):** "Alex became a parent first (adopted a baby girl in January 2023), before Rachel (whose twins were born on February 12, 2023)."
- **B:** no.
- **My view on D: agree** (judged against the gold). D's answer is the opposite of the gold. Note that, given the context D had, its reading was reasonable.
- **My view on C: agree.** C matches the gold and cites the decisive date.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 5. e3038f8c (multi-session) [sets 1+2]
- **Q:** How many rare items do I have in total? Question date 2023/05/30.
- **Gold:** 99.
- **Evidence:**
  - _2 (2023/05/22), t4: "I have 12 rare figurines".
  - _4 (2023/05/22), t0: "a small but valuable collection of 5 books".
  - _1 (2023/05/22), t0: "my 57 rare records".
  - _3 (2023/05/23), t4: "I actually have 25 rare coins".
- **Gold check:** 12 + 5 + 57 + 25 = 99. **Gold OK.**
- **D (no):** "The chat history does not provide enough information to determine how many rare items you have in total."
- **C (yes):** "Total: 57 + 12 + 25 + 5 = 99 rare items".
- **B:** no.
- **My view on D: agree.** D gives no number.
- **My view on C: agree.** C gives the exact total with the correct components.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 6. gpt4_1e4a8aec (temporal-reasoning) [sets 1+2]
- **Q:** What gardening-related activity did I do two weeks ago? Question date 2023/05/05.
- **Gold:** planting 12 new tomato saplings.
- **Evidence:** answer_16bd5ea6_2 (2023/04/21), t0 user: "I just planted 12 new tomato saplings today".
- **Gold check:** 4/21 to 5/5 is exactly 14 days. **Gold OK.**
- **D (no):** "Two weeks ago, you were planning how often to water your tomato plants during a dry spell, using a gardening app". The planting turn was not in D's context.
- **C (yes):** "Two weeks ago (on April 21, 2023), you planted 12 new tomato saplings in your garden."
- **B:** no.
- **My view on D: agree.** D names the wrong activity: watering, not planting.
- **My view on C: agree.** C gives the exact gold activity.
- Human: [ ] agree with judge  [ ] disagree  — note:

---

## Cases in set 1 only (D wrong, C also wrong)

### 7. gpt4_15e38248 (multi-session) [set 1]
- **Q:** How many pieces of furniture did I buy, assemble, sell, or fix in the past few months? Question date 2023/05/30.
- **Gold:** 4.
- **Evidence:**
  - _3 (2023/05/21), t0: "ordered one [mattress] from Casper".
  - _1 (2023/05/26), t0: "new coffee table from West Elm about three weeks ago".
  - _4 (2023/05/26), t2: "fixing the wobbly leg on my kitchen table last weekend".
  - _2 (2023/05/29), t0: "assembled that IKEA bookshelf ... about two months ago".
- **Gold check:** coffee table + kitchen table + bookshelf + mattress = 4. Counting a mattress as "furniture" is arguable, but it is clearly the dataset's intent. **Gold acceptable** (minor note).
- **D (no):** "a total of **3 pieces of furniture** bought, assembled, or fixed". The mattress is missing.
- **C (no):** "Answer: 3 pieces of furniture". The mattress is also missing.
- **B:** no.
- **My view on D: agree.** 3 is not 4.
- **My view on C: agree.** 3 is not 4.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 8. 09ba9854_abs (multi-session, abstention) [set 1]
- **Q:** How much will I save by taking the bus from the airport to my hotel instead of a taxi? Question date 2023/05/30.
- **Gold (explanation):** "The information provided is not enough. You did not mention how much will the bus take."
- **Evidence:** answer_96c743d0_abs_1 (2023/05/20), t6 user: "taking a taxi from the airport to my hotel would cost around $60". The user never states a bus fare; only the assistant's generic estimate "$10-$20" appears.
- **Gold check:** consistent with the haystack. **Gold OK.** As the earlier error analysis notes, whether an assistant's estimate counts as "information" is debatable. The rubric's explanation is explicit, though.
- **D (no):** "you would save approximately **$40–$50**".
- **C (no):** "you would save roughly ¥12,000–27,000, or approximately $110–$240".
- **B:** no.
- **My view on D: agree.** The ABSTENTION rubric requires saying the information is unavailable. D gives a number.
- **My view on C: agree.** C also gives a number and does not flag the missing information.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 9. 91b15a6e (multi-session) [set 1]
- **Q:** What is the minimum amount I could get if I sold the vintage diamond necklace and the antique vanity? Question date 2023/05/30.
- **Gold:** $5,150.
- **Evidence:**
  - _1 (2023/05/23), t6: "my vintage diamond necklace, which is worth $5,000".
  - _2 (2023/05/25), t8: "I bought it for $150 ... I'm confident it's worth at least that amount now".
- **Gold check:** $5,000 + $150 = $5,150, using the user's own floor. **Gold OK.**
- **D (no):** "The minimum amount you could get is **$5,200**". D used the assistant's $200 estimate.
- **C (no):** "The minimum amount the user could get is **$5,200**". Same reasoning as D.
- **B:** yes.
- **My view on D: agree.** $5,200 is not $5,150, and the DEFAULT rubric has no numeric tolerance.
- **My view on C: agree.** Same reason.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 10. 88432d0a (multi-session) [set 1]
- **Q:** How many times did I bake something in the past two weeks? Question date 2023/05/30.
- **Gold:** 4.
- **Evidence:**
  - _3 (Sun 2023/05/21), t0: sourdough bread recipe "on Tuesday", i.e. 5/16.
  - _4 (Wed 2023/05/24), t0: "whole wheat baguette last Saturday", i.e. 5/20.
  - _2 (Sun 2023/05/28), t2: "last Thursday to bake a batch of cookies", i.e. 5/25.
  - _1 (Tue 2023/05/30), t0: "baked a chocolate cake ... last weekend", i.e. 5/27–28.
- **Gold check:** all four fall within 5/16 to 5/30; 5/16 is right on the boundary. **Gold OK.**
- **D (no):** "The user baked something **once** in the past two weeks".
- **C (no):** "Answer: 7 times." C double-counts repeated mentions of the same bakes.
- **B:** no.
- **My view on D: agree.** 1 is not 4.
- **My view on C: agree.** 7 is not 4.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 11. gpt4_2f56ae70 (temporal-reasoning) [set 1] — FLAG: dataset timestamps
- **Q:** Which streaming service did I start using most recently? Question date 2023/05/26 00:18.
- **Gold:** Disney+.
- **Evidence:**
  - _3 (2023/05/26 01:08), t0: "a specific documentary I saw on Disney+ during my free trial last month". Also t10: "Since I started a free trial of Disney+ last month".
  - _2 (2023/05/26 23:40), t0: "I've also been using Apple TV+ for a few months now".
  - _1 (2023/05/26 08:25), t0: "using Netflix, Hulu, and Amazon Prime for the past 6 months".
- **Gold check:** Disney+ (last month) is the most recent of the services the user has *used*. **Gold OK.**
- **Dataset quirk:** all 45 haystack sessions are timestamped after the question time, on the same day.
- **Competing statement:** _2 t8 (not has_answer) says "I think I'll go with the Amazon Prime Video with HBO add-on option". That is a stated intention, not use.
- **D (no):** "Amazon Prime (Prime Video)". The Disney+ turns were not in D's context.
- **C (no):** "The most recently started streaming service is the **HBO add-on to Amazon Prime Video**".
- **B:** no.
- **My view on D: agree.** Wrong service; Prime has been used for 6 months.
- **My view on C: agree.** C's answer rests on an intention ("I think I'll go with"), not on having started to use HBO. It does not match the gold.
- Human: [ ] agree with judge  [ ] disagree  — note:

### 12. gpt4_731e37d7 (multi-session) [set 1] — FLAG: date conflict in haystack
- **Q:** How much total money did I spend on attending workshops in the last four months? Question date 2023/02/26.
- **Gold:** $720.
- **Evidence (all sessions dated 2023/02/26):**
  - _4 t0: "writing workshop ... in November ... I paid $200".
  - _2 t0: "I just attended a digital marketing workshop at the city convention center on March 15-16 ... I paid $500".
  - _3 t10: "half-day mindfulness workshop ... on December 12 ... I paid $20".
  - _3 t0: photography workshop on February 22, "a free event".
- **Gold check:** $200 + $500 + $20 + $0 = $720, so the gold is arithmetically consistent with the has_answer turns.
- **The conflict:** the $500 workshop is dated "March 15-16", after the 2023-02-26 session and question date, yet the user says "I just attended". March 2022 would fall outside the four-month window.
- **Assessment:** the gold follows "just attended". It is defensible, but a reader that respects the date can reasonably exclude the $500. **Gold questionable (unsure).**
- **In context:** D's context had only the $20 item. C's context had all four items.
- **D (no):** "the total known amount spent on workshops is **$20**".
- **C (no):** "You spent **$220** total on workshops in the last four months". C excluded the "March 15–16" workshop as a future event.
- **B:** no.
- **My view on D: agree.** $20 is wrong under any reading.
- **My view on C: agree** (judged against the gold). $220 is not $720. If the owner decides the "March 15-16" date makes the $500 ineligible, C would be correct (unsure).
- Human: [ ] agree with judge  [ ] disagree  — note:

### 13. 370a8ff4 (temporal-reasoning) [set 1] — FLAG: GOLD LOOKS WRONG
- **Q:** How many weeks had passed since I recovered from the flu when I went on my 10th jog outdoors? Question date 2023/10/15.
- **Gold:** 15.
- **Evidence:**
  - answer_61d1be50_1 (2023/01/19), t0: "I finally recovered from the flu today".
  - answer_61d1be50_2 (2023/04/10), t0: "I went on my 10th jog outdoors today".
  - No other flu or 10th-jog mentions exist in the haystack (searched).
- **Gold check: gold looks wrong.** 2023-01-19 to 2023-04-10 is 81 days, which is 11 weeks and 4 days (about 11.6 weeks). Fifteen weeks after 1/19 would be about 5/4, and there is no evidence for that date.
- **D (no):** "About 11–12 weeks (approximately 11 weeks and 4 days) had passed".
- **C (no):** "About 11–12 weeks had passed".
- **B:** no.
- **My view on D: agree with the judge given the literal gold.** Neither 11 nor 12 is 15, and the off-by-one tolerance does not reach it. But the gold itself appears wrong. Against a corrected gold (11 or 12 weeks), D's answer, which shows 81 days and 11 weeks 4 days, would be correct.
- **My view on C: agree with the judge given the literal gold.** Same as D, and C would also be correct against a corrected gold.
- Human: [ ] agree with judge  [ ] disagree  — note:

---

## Summary table

| id | sets | D verdict | my view (D) | C verdict | my view (C) | B | flags |
|---|---|---|---|---|---|---|---|
| 2c63a862 | 1+2 | no | agree | yes | agree | yes | — |
| 2ce6a0f2 | 1+2 | no | agree | yes | agree | yes | — |
| gpt4_f420262d | 1+2 | no | agree | yes | agree | yes | gold is a mild inference |
| gpt4_fe651585 | 1+2 | no | agree (vs gold) | yes | agree | no | haystack self-contradictory; gold unsure |
| e3038f8c | 1+2 | no | agree | yes | agree | no | — |
| gpt4_1e4a8aec | 1+2 | no | agree | yes | agree | no | — |
| gpt4_15e38248 | 1 | no | agree | no | agree | no | mattress counted as furniture (minor) |
| 09ba9854_abs | 1 | no | agree | no | agree | no | abstention; neither abstained |
| 91b15a6e | 1 | no | agree | no | agree | yes | — |
| 88432d0a | 1 | no | agree | no | agree | no | — |
| gpt4_2f56ae70 | 1 | no | agree | no | agree | no | haystack timestamped after question time |
| gpt4_731e37d7 | 1 | no | agree | no | agree (vs gold) | no | "March 15-16" vs Feb 26 date conflict; gold questionable |
| 370a8ff4 | 1 | no | agree (vs gold) | no | agree (vs gold) | no | **gold looks wrong** (81 days, about 11.6 weeks, not 15) |

## Counts
- **Judge disagreements found:** D 0 of 13; C 0 of 13. These 13 cases cover 6 of C's "yes" verdicts and 7 of its "no" verdicts. With the literal gold, every verdict checked is consistent with the official rubric.
- **Golds that look wrong:** 1 (370a8ff4).
- **Golds that are questionable or unsure:** 2:
  - gpt4_fe651585: the haystack contradicts itself on the twins' birth year.
  - gpt4_731e37d7: the $500 workshop's date conflicts with the session date.
- **Minor gold notes:** gpt4_f420262d (inference) and gpt4_15e38248 (mattress as furniture).

## Effect on the D-vs-C gap (6 C-right/D-wrong vs 0 D-right/C-wrong)
- **If my pre-check is confirmed as-is:** no verdict flips, so the gap stays **6 vs 0**. The scores stay D 47/60 and C 53/60.
- **If 370a8ff4's gold is corrected to 11 or 12 weeks:** both D and C flip to correct, since they gave the same answer. The scores become D 48 and C 54, and the gap stays 6 vs 0.
- **If gpt4_fe651585 is dropped as unreliable:** the gap becomes **5 vs 0**. The scores become D 47/59 and C 52/59.
- **If the owner rules that the "March 15-16" $500 workshop is ineligible in gpt4_731e37d7 (gold $220):** C flips to correct while D stays wrong ($20). The gap becomes **7 vs 0**.
- **Conclusion:** none of the plausible corrections changes the direction of the gap. It ranges from 5 to 7 vs 0.
