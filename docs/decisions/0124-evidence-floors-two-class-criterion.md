# 0124 Evidence floors: a two-class criterion on the extended training split

Decision 0121 chose each evidence floor as the 5th percentile of what the
gold records of the training questions reach on that signal. Its training
split held only plainly worded, answerable questions, so the floors encoded
"plain wording only": with e5-small the cosine floor (0.8608) cut the
evidence of paraphrased questions, and 46 of 66 evaluated paraphrase
questions came back empty, while near-miss unanswerable questions still
passed on coverage. Decision 0123 adds paraphrased and unanswerable
questions to the training split; this record changes how the floors are
chosen from it.

## Criterion (stated before any evaluated result was looked at)

`chanda eval -calibrate C3` (and `chanda-hugot eval -config … -calibrate
C3`) ingests the corpus into C3, runs the context query of every question
of `ds.Training()` (never an evaluated or gate question) and reads every
candidate the cut judged, with its coverage and cosine. For a floor pair
(`min_term_coverage` c, `min_cosine` s), a candidate passes when its
coverage ≥ c **or** its cosine ≥ s (decision 0020, unchanged). Two errors
count, one point each, on the natural mix of the training split (which
mirrors the evaluated mix by construction, 0123):

- **lost**: an answerable question whose gold record is among the judged
  candidates, and none of its gold records passes;
- **filled**: an unanswerable question with at least one candidate passing.

The floors minimise lost + filled over every pair: c at each coverage value
observed (or off, 1.01), s at the midpoint between consecutive observed
cosines (or off). Ties go to fewer filled questions (principle 8), then to
fewer non-gold candidates of answerable questions passing (the same
decisions on the training questions with less filler in the context:
principle 8 again), then to the lower c and s. Floors are published with
four decimals (coverage rounded down, cosine rounded unless that would cross
an observed value).

One revision, made on training output only (before any evaluated run with
the new floors): the first version broke the remaining ties by the centre
of the widest run of optimal cosine floors, then the lower c. On the hash
training split every pair that keeps all judged gold also fills all 17
unanswerable questions, so all of them tie, and that rule picked c = 0:
every candidate passes, the cut does nothing. Minimising the non-gold
candidates that pass among the tied pairs keeps the same training decisions
with the strictest floors that make them.

Why these choices: equal unit costs on the natural mix make a lost answer
and a filled abstention equally bad, which is what "weighs both errors"
means without inventing a cost ratio; the alternative of equal weight per
*class* (balanced accuracy) would trade about eight answerable questions
for one unanswerable one, the opposite of the BASE.md target (95%
abstention *precision*). Answerable questions whose gold never reaches the
candidates are left out of "lost": no floor can keep them.

## The rule

The cut's rule was checked, not only its numbers: `read.hasEvidence` is
`coverage ≥ min_term_coverage || (hasCos && cosine ≥ min_cosine)`, so
semantic-only evidence (cosine above its floor, coverage 0) already passes
on its own; nothing structurally requires lexical coverage. The cosine is
known only for vector hits (`hasCos`), which is where semantic-only
evidence comes from. What prevented it from passing was the floor, chosen
from plainly worded questions. The rule is therefore unchanged.

The calibration also prints, per population of the training split (plain,
paraphrase, unanswerable), the 10th, 50th and 90th percentiles of the best
coverage and cosine, so the overlap the floors have to cut through is
visible.

## Calibration (training split, 149 questions)

Best evidence, p10 / p50 / p90: of the gold records for answerable
questions whose gold was judged, of any candidate for unanswerable ones.

| | hash-v1-256 | e5-small@hugot0.8.6+simd |
| --- | --- | --- |
| plain (gold judged) | 99 of 99 | 98 of 99 |
| paraphrase (gold judged) | **5 of 33** | 14 of 33 |
| unanswerable | 17 | 17 |
| plain gold coverage / cosine | 0.5 / 1 / 1 — 0.37 / 0.57 / 0.75 | 0.5 / 1 / 1 — 0.868 / 0.899 / 0.939 |
| paraphrase gold coverage / cosine | 0 / 0.25 / 0.25 — 0.236 / 0.236 / 0.261 | 0 / 0 / 0.25 — 0.817 / 0.838 / 0.862 |
| unanswerable best coverage / cosine | 0.25 / 0.5 / 0.67 — 0.297 / 0.436 / 0.618 | 0.25 / 0.5 / 0.67 — 0.842 / 0.864 / 0.894 |
| **chosen `min_term_coverage` / `min_cosine`** | **0.25 / 0.2359** | **0.25 / 0.8160564601421356** |
| gold kept / unanswerable filled / cost | 104 of 104 / 17 of 17 / 17 | 112 of 112 / 17 of 17 / 17 |
| non-gold candidates passing | 7130 of 7597 | 7780 of 7874 |
| before (0121): floors | 0.5 / 0.3076 | 0.5 / 0.8608 |

The table is the finding. On both models the best candidate of an
unanswerable near-miss question has *more* evidence on both signals than
the gold record of a paraphrased question (e5: unanswerable cosine p10
0.842 against paraphrase gold p50 0.838; coverage p50 0.5 against 0). No
floor pair empties an unanswerable question without cutting at least as
many answers, so the cost-optimal floors keep every judged gold record and
fill every unanswerable question; the tie-break then takes the strictest
such floors. The e5 cosine floor is not rounded because an observed cosine
lies between it and 0.8161.

## Result (evaluated split; reports regenerated after the values were fixed)

`eval/reports/latest.md` (hash, `make eval`) and `latest-e5.md` (e5-small,
`chanda-hugot eval -config eval/e5-small.yaml`), against the reports at
81d5ff1. B2 is unchanged by construction (no cut) and is the reference.
C4 equals C3 in both runs; C5 is listed in the reports (hash: a ranker was
promoted at λ 0.05, recall@10 0.6667, MRR 0.5249; e5: none promoted, C5 =
C4).

| C3 | hash before | hash after | e5 before | e5 after | B2 hash / e5 |
| --- | --- | --- | --- | --- | --- |
| recall@10 | 0.6259 | 0.6741 | 0.6259 | 0.6963 | 0.5074 / 0.563 |
| MRR | 0.5105 | 0.5179 | 0.5102 | 0.5258 | 0.3238 / 0.3552 |
| nDCG@10 | 0.5389 | 0.5548 | 0.5374 | 0.5629 | 0.3637 / 0.4002 |
| answer in context | 0.6593 | 0.7111 | 0.7037 | **0.8148** | 0.6519 / 0.7556 |
| answer in context, paraphrase (66) | 0.0152 | 0.1667 | 0.0606 | **0.3939** | 0.197 / 0.5152 |
| recall@10, paraphrase | 0.0152 | 0.1061 | 0.0303 | 0.2273 | 0.0606 / 0.2121 |
| empty results, paraphrase | 27 of 66 | 1 of 66 | 46 of 66 | 0 of 66 | 0 / 0 |
| answer in context, plain (86) | 0.8605 | 0.8605 | 0.9419 | 0.9535 | 0.9419 / 0.9651 |
| forbidden in context | 0.0508 | 0.0508 | 0.0508 | 0.0508 | 0.6864 / 0.839 |
| abstention precision | 0.0357 | **0 (1 empty)** | 0.15 | **undefined (0 empty)** | n/a |
| abstention recall | 0.0286 | **0** | 0.2571 | **0** | 0 / 0 |
| empty results (of 305) | 28 | 1 | 60 | 0 | 0 / 0 |
| context tokens per query (mean) | 162.3 | 240.4 | 112.8 | 244.0 | 789.3 / 786.4 |

With e5-small, C3 now puts the answer of 39% of the paraphrased questions
in its pre-context (6% before; B2 52%) and the answer of 81% of all
answerable questions (70% before), above B2 (76%), at less than a third of
B2's tokens and with 5% forbidden content against B2's 84%. With hash,
paraphrase answers go from 2% to 17% (B2 20%).

**The price is abstention, and it is total.** No unanswerable question
comes back empty with either model (hash: the single empty result is an
answerable question; e5: no empty result at all). Before, C3 abstained on
1 (hash) and 9 (e5) of the 35, at the cost of 27 and 51 wrongly empty
answerable results. The BASE.md target (95% precision) was not met before
and is not met now; principle 8 is not met for near-miss questions with
these signals. This is the trade the criterion makes explicit: on the
training split every unanswerable question that could be emptied costs at
least one answer. Abstaining on near misses needs a signal that separates
"the corpus answers this" from "the corpus talks about this", which neither
term coverage nor a bi-encoder cosine is; the reranker (C2, a
cross-encoder, not configured in these runs) is the candidate.

Independent check (phase-5 scenarios, `eval/scenarios`, not a calibration
input): A2 already failed on main for "Qual e o horario de limpeza da
maquina de lavar louca?" (the coffee-machine statement: coverage 0.25,
hash cosine 0.3115 ≥ 0.3076, a pass on the cosine by 0.004). With the new
hash floors all three A2 queries fail (coverage 0.25 ≥ 0.25 for the deploy
and limpeza queries, 0.333 for the VPN one; cosines 0.273 and 0.312 also
pass), A1 fails on "receita de bolo de cenoura…" (coverage 0, hash cosine
0.275 ≥ 0.2359 against the refund statement), and I1 delivers a third
block for the request the internal turns do not answer (its injection
checks still pass).
