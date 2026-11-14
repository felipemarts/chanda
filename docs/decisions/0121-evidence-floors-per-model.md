# 0121 Evidence floors per embedding model, calibrated on the training split; entity_match

The abstention suite (BASE.md "95% precision" of empty pre-contexts)
measured 0.16 precision and 0.34 recall for C3 (`latest.md` at 3c0b2fe:
74 empty results, 12 of them on the 35 unanswerable questions). Decision
0020 fixed the evidence floors (`read.min_term_coverage` 0.5,
`read.min_cosine` 0.5) by hand and asked for a per-model calibration.

## Where the empty results came from (hash embedder)

On C3, 55 of the 74 empty results were on paraphrase questions (66 in the
dataset, worded with little overlap with their evidence). For 42 of the 66
paraphrase questions no gold record is among the candidates the cut judges
at all (keyword top 50 per type + vector top 50): the built-in hash
embedder is lexical, so a paraphrase has neither coverage nor cosine
towards its evidence. Unanswerable "near misses" (existing names, absent
attributes) do share words with the corpus. Coverage and the hash cosine
therefore order the two populations the same way: over a grid of floor
pairs, no pair gives an abstention precision above 0.21 on the evaluated
questions (that sweep was a diagnosis, never used to choose a floor). With
this embedder the 95% target would require delivering content for
paraphrase questions whose evidence was not retrieved at all, i.e. filling
the pre-context with the least bad item, which principle 8 forbids.

## Calibration procedure (training split only)

`chanda eval -calibrate C3` (and `chanda-hugot eval -config … -calibrate C3`)
builds C3 over the corpus, runs the context query of every question of
`ds.Training()` (never an evaluated or gate question), and reads from the
logged retrieval every candidate the cut judged, with its coverage and
cosine (`coresys.System.Evidence`). Gold records are found with the
metrics' own test (`eval.Hits`). All 99 training questions are answerable
and plainly worded, so the procedure is one-class and per signal
(`eval.CalibrateEvidence`): for each question take the best coverage and
the best cosine of its gold records; each floor is the largest value that
the best gold record of at least 95% of the questions reaches (the 5th
percentile; 95% mirrors the BASE.md target from the answerable side).

A joint criterion (the floor pair with the fewest non-gold candidates
passing at 95% joint retention) was considered first and rejected before
any evaluated result with it was looked at for e5: on a training split of
plainly worded questions coverage alone keeps 95% of the gold, so the joint
criterion switches the semantic signal off for every model (it chose
`min_cosine` 1.01 for both hash and e5), a bias of the split rather than a
property of the model. For transparency: with the hash embedder that joint
choice (coverage 0.5, cosine off) would have given 0.173 precision and
0.37 recall on the evaluated questions (diagnostic sweep).

| Model | Training questions with gold judged (coverage / cosine) | `min_term_coverage` | `min_cosine` | Gold kept at both floors | Non-gold candidates passing |
| --- | --- | --- | --- | --- | --- |
| `hash-v1-256` | 99 / 95 | 0.5 | 0.3076 | 98 of 99 | 2665 of 6202 |
| `e5-small@hugot0.8.6+simd` | 98 / 95 | 0.5 | 0.8608 | 96 of 99 | 3324 of 6351 |

The floors are in the defaults as `read.evidence_floors`, keyed by model id
or by its family (the part before `@`), so `e5-small` covers both build
flavours; a model without an entry keeps `read.min_term_coverage` and
`read.min_cosine`. `read.Reader` picks the floors from `Deps.Embedder`'s
model id. `coverage_term_cap` (4) and `relevance_threshold` (0) were not
changed: the cap is a count, not a scale, and RRF relevance is rank-based
and cannot abstain on its own (0020).

## Result (hash embedder, `make eval`)

| C3 | before | after |
| --- | --- | --- |
| abstention precision / recall | 0.1622 / 0.3429 | 0.0357 / 0.0286 |
| empty results | 74 | 28 |
| answer in context | 0.637 | 0.6593 |
| recall@10 / MRR / nDCG@10 | 0.6074 / 0.5081 / 0.5329 | 0.6259 / 0.5105 / 0.5389 |
| temporal stale_answer_rate | 0.3514 | 0.009 (decision 0120) |

The calibrated hash floors admit almost every candidate (the hash cosine of
real evidence is low, so its 5th percentile is low): fewer answerable
questions come back empty, and so do unanswerable ones. **The target is not
met and cannot be met honestly with the hash embedder**, for the reason
above; the measured value is published.

C5 equals C4 in this run: the sleep run trains a model and the gate scores
it 64.63 against 63.71, below the 1.0-point `promotion_margin`. A run with
`entity_match` disabled gives the same gate scores, so the change of floors
(which changes what the gate questions receive) is the cause. The learning
suite fails on `gain_c5_over_c4 = 0` instead of its previous one-question
gate regression.

## Result (e5-small, `eval/reports/latest-e5.md`)

Manual step (needs the model files, so not part of `make eval`):
`make hugot`, then `./bin/chanda-hugot eval -config eval/e5-small.yaml
-dataset eval/data/synthetic-pt -out eval/reports -name latest-e5` with
`model_path` pointing at the e5-small files (`models verify` checks them).
The published report was produced from this branch with the model files
adopted offline (`models adopt e5-small <dir>`, nothing downloaded); it
takes tens of minutes on the M1 (every C system and suite embeds the
corpus again). Its configuration hash includes the local `model_path`.

| C3 | hash (latest.md) | e5-small (latest-e5.md) |
| --- | --- | --- |
| abstention precision / recall | 0.0357 / 0.0286 | 0.15 / 0.2571 |
| empty results (of 305) | 28 | 60 |
| paraphrase questions empty | 27 of 66 | 46 of 66 |
| answer in context | 0.6593 | 0.7037 |
| recall@10 / MRR / nDCG@10 | 0.6259 / 0.5105 / 0.5389 | 0.6259 / 0.5102 / 0.5374 |
| temporal stale_answer_rate | 0.009 | 0.009 |

**The target is not met with e5-small either.** The cause is visible in the
paraphrase row: B2 (plain cosine top-k with the same e5 model) puts the
answer of 52% of the paraphrase questions in context, C3 only 6%, and 70%
of them come back empty. The cosine floor (0.8608) is the 5th percentile of
the cosine e5 gives the evidence of *plainly worded* training questions;
paraphrase evidence scores lower, so the cut removes it, while near-miss
unanswerable questions (real names, absent attributes) still reach coverage
0.5. The training split has no paraphrase and no unanswerable question, so
no train-only calibration can place the floor between "paraphrase of the
evidence" and "unrelated but same names"; lowering it by looking at the
evaluated questions is exactly what this task forbids. What would allow an
honest calibration is a training split that resembles the evaluated one
(paraphrased and unanswerable training questions): a dataset change, left to
the architect.

Also seen in the e5 run, outside this decision: the deduplication suite
fails (21% of real duplicates missed) because `write.near_dup_threshold`
(0.95) is a hash-embedder value; it needs the same per-model treatment.

## entity_match

Decision 0110 found the ranker's `entity_match` feature always 0. The read
path now logs `features.entity_match` for every candidate that reached
fusion, and passes it to the learned term (`read.Candidate.EntityMatch`,
`rank.Candidate.EntityMatch`): 1 when the query names an entity (an alias
in `entity_aliases`, matched as whole words of `norm.Alias(query)`, n-grams
up to 8 words, resolved through accepted merges) and the record is about
it: a fact whose subject it is, an event that states a fact about it or
whose text or heading path names it with the alias the query used, a page
that names it. It is a feature only: relevance, the cut and the λ = 0 order
do not change. On synthetic-pt C3 logs it as 1 for 2218 of 29505 judged
candidates (1312 of the 4600 delivered).
