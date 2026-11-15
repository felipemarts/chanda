# 0140 Abstention under e5: question-level signals on the training split (not adopted)

With the evidence floors of decision 0124, C3 never returns an empty
pre-context under e5-small (abstention precision undefined, target 95%):
on the training split every unanswerable near-miss question has a
candidate with more coverage and more cosine than the gold of a paraphrased
question. Decision 0124 showed that no pair of per-item floors separates
them. This record checks whether a *question-level* signal does: return an
empty pre-context for the whole query when the signal says so, and deliver
what the cut delivers today otherwise. The cut was not changed.

## Data (training split only)

`chanda eval -calibrate C3 -calibrate-dump FILE` (new flag) writes, for each
of the 149 training questions, every candidate the cut judged with what the
read path logged (coverage, cosine, BM25, relevance, list ranks, delivered
or not) and whether its text holds one of the question's answers. Runs:
hash in process; e5 through `lme chanda eval -config eval/e5-lme.yaml`.
132 answerable questions (all with answers), 17 unanswerable.

Baseline answer in context on training (answer text in a delivered item):
hash 0.7273, e5 0.7652. Empty results today: hash 1 (answerable), e5 0.

Signals per question, from the judged candidates in final order:
`best_cos` (max cosine), `top_cos` (cosine of the first candidate),
`top3_cos` (mean of the three best cosines), `cos_margin` (best minus
second cosine), `cos_gap5` (best minus fifth), `best_cov` (max coverage),
`top_cov` (coverage of the first candidate), `cov_of_bestcos` (query-term
coverage of the best-cosine item), `best_rel`, `rel_margin` (RRF relevance
and best-minus-second). Rule family: empty when the signal is below (or
above) a threshold.

## One signal: the trade-off curve (training, in-sample, optimistic)

Precision = unanswerable among the questions emptied; recall = of the 17
unanswerable; drop = points of answer in context lost.

| e5-small | best precision with drop ≤ 2 pts (recall) | at recall ≥ 9/17 | at 17/17 |
| --- | --- | --- | --- |
| best_cos | 0.20 (2/17) | 0.27, drop 8.3 | 0.17, drop 40.2 |
| top_cos | 0.31 (5/17) | 0.33, drop 2.3 | 0.18, drop 34.1 |
| top3_cos | none | 0.26, drop 9.1 | 0.18, drop 37.1 |
| cos_margin | none | 0.17, drop 39.4 | 0.16, drop 43.2 |
| cos_gap5 | 0.25 (2/17) | 0.18, drop 22.0 | 0.16, drop 42.4 |
| best_cov | none | 0.28, drop 9.1 | 0.11, drop 76.5 |
| top_cov | none | 0.23, drop 18.9 | 0.11, drop 76.5 |
| cov_of_bestcos | none | 0.22, drop 20.5 | 0.11, drop 76.5 |
| best_rel | none | 0.20, drop 17.4 | 0.11, drop 76.5 |
| rel_margin | none | 0.13, drop 31.8 | 0.12, drop 75.8 |

| hash-v1-256 | best precision with drop ≤ 2 pts (recall) | at recall ≥ 9/17 | at 17/17 |
| --- | --- | --- | --- |
| best_cos | 0.15 (2/17) | 0.21, drop 11.4 | 0.12, drop 63.6 |
| top_cos | 0.21 (3/17) | 0.21, drop 9.1 | 0.12, drop 63.6 |
| best_cov | 0.18 (7/17) | 0.28, drop 6.1 | 0.11, drop 72.7 |
| top_cov | 0.21 (9/17) | 0.24, drop 12.1 | 0.11, drop 72.7 |
| cos_margin / cos_gap5 / rel_margin | none | 0.18 / 0.23 / 0.21 | 0.16 / 0.16 / 0.12 |

No single signal comes near 95%: the best in-sample precision of any of
them at a useful recall is about a third. The margin between the best and
second candidates is *smaller* for paraphrased questions than for near
misses (e5 p50 0.004 for both, plain 0.011), and query-term coverage of the
best item is higher for near misses (p50 0.33) than for paraphrases (0).

## Two signals: in-sample separation that does not hold out of sample

A conjunction of two signals (every ordered pair above, each with a
threshold and a direction; several million rules) finds, in-sample, rules that
empty only unanswerable questions. The best one for e5 is "the candidates
cover the query (best_cov > 0.25) but the first one is semantically weak
(top_cos < 0.8527)": 7 of 17 unanswerable emptied, no answerable question
emptied, no answer lost. It is the near-miss pattern (the words are there,
the meaning is not), so it was worth testing as a rule.

Out of sample, on the training split itself (stratified 5-fold
cross-validation: the rule and its thresholds chosen on four folds,
measured on the fifth, repeated with 10 shuffles):

| rule chosen on the training folds | emptied unanswerable / answerable | precision | recall | answer in context lost |
| --- | --- | --- | --- | --- |
| e5: best pair of all signals (4 shuffles) | 26 / 9 | 0.74 | 0.38 | 0.38 pts |
| e5: fixed family best_cov > c ∧ top_cos < s, threshold at the nearest answerable | 70 / 16 | 0.81 | 0.41 | 0.38 pts |
| e5: same family, threshold at the midpoint | 67 / 16 | 0.81 | 0.39 | 0.38 pts |
| e5: same family, threshold just above the emptied unanswerable | 58 / 10 | 0.85 | 0.34 | 0.38 pts |
| e5: best_cov > c ∧ best_cos < s | 44 / 12 | 0.79 | 0.26 | 0.83 pts |
| hash: best pair of all signals (4 shuffles) | 7 / 20 | 0.26 | 0.10 | 1.33 pts |

The in-sample 7 of 7 is the fit of two thresholds to 17 points; held out,
about one emptied question in five is answerable. With the 270 answerable
and 35 unanswerable evaluated questions that would be roughly 0.8
precision, not 95%.

## Decision

Not adopted, for either model. The bar was ≥ 95% precision of empty
pre-contexts on the training split with at most 2 points of answer in
context lost there; the only rules that reach 95% do so in-sample with two
thresholds fitted to 17 questions, and they fall to 0.74–0.85 precision
under cross-validation on the same split. The read path and the floors are
unchanged; hash behaviour is unchanged. The evaluated questions were not
used for any of this (no evaluated-split run of these rules was made).

What remains open: the e5 pattern above is real but noisy (cross-validated
precision about 0.8 at about 0.4 recall, losing 0.4 points of answer in
context). A larger set of unanswerable training questions would tell
whether it can be calibrated to a precision worth shipping; a cross-encoder
(C2) remains the candidate for "the corpus answers this" against "the
corpus talks about this" (0124).

The dump flag stays: `-calibrate-dump FILE` (with `-calibrate`), JSONL, one
line per training question; `eval.EvidenceCandidate` now carries BM25 and
the logged features for it.
