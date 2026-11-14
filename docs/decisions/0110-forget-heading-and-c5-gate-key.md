# 0110 Forget's byte probe and headings; the C5 gate key and the learning result

Two failures of the evaluation report (`eval/reports/latest.md`, commit
1953043) were traced to their source.

## Deletion: 13/14 → 14/14

Forgetting the entity "Abertura de chamados de TI" failed forget's own
`bytes` probe: byte strings 6, 8 and 11 (one per removed fact) were found in
`chanda.db`. A table-by-table scan after the forget located the bytes: the
first needle of each fact text is its first 16 bytes, `Abertura de cham`,
and the only rows holding them were five **surviving** events, in
`events.heading_path`. The FTS shadow tables, free pages, the WAL, usage
rows, the sleep report, the audit details and the gate questions held
nothing; the rest of each fact's text was absent from the file.

The document is titled after the entity (`# Abertura de chamados de TI`).
The header extractor takes the heading as the facts' subject, so the facts
and their header chunk are removed, while the other paragraphs under the
heading survive: an entity scope removes the facts about the entity and the
events they came from or that mention it, not every paragraph under the same
heading (decision 0050; the suite's independent probe says the same). Those
paragraphs keep the heading in `heading_path`, which is text of a surviving
record, but `survivorText` only listed `events.content`. The probe now also
skips a byte string that a surviving event's `heading_path` holds. It still
scans every other byte string of the removed texts. Regression test:
`TestEntityScopeKeepsSiblingChunkUnderTheHeading` in `internal/forget`. It
failed before the fix (`byte string 4 of fct_07 found`).

## Learning: why C5 equalled C4

C5 trained a model and the gate scored it 93.94 against a 93.94 baseline, so
nothing was promoted. Checks:

- **Features are populated.** Of 2436 training examples (291 positive, from
  297 tasks): bm25 non-zero in 2433, cosine in 2166, rrf, age and utility
  in all, time since use in 507. `rerank` is always 0 (no reranker
  configured). `entity_match` is always 0, because the read path never logs
  it. That gap is in `internal/read`, outside this area.
- **The evaluator compares the right orders.** `promotionEvaluator` scores
  the gate set at λ = 0 and at the candidate λ with the newest model as the
  term. λ is applied at read time once a model is promoted: forcing the
  promoted state changes the delivered order.
- **Root cause: the gate key could not see the order.** The C5 harness
  approved the gate split with `answers` and `as_of` only (decision 0086,
  item 1: "no record ids, which the dataset cannot know"). The gate score is
  then answer-in-context per question (and empty-result for abstention).
  That score is a set property, and the learned term only reorders what
  already passed the relevance cut. Answer-in-context was already 1.0 on
  every answerable gate question, and the 2 lost points were abstention
  questions, which ordering cannot fix. So learned and baseline tied at
  every λ, and no model could ever pass `promotion_margin`.

**Fix (supersedes 0086 item 1 on record ids).** The person who writes an
answer key can look up which records answer each question, and so can the
harness. `System.GateEvidence` cites every record of the namespace (events,
facts, pages and page items, any status, read through `get`) that is
evidence for a gold unit, by the same test as the metrics and the training
outcomes (`eval.Hits`: same corpus item, span contained). The gate key now
holds `record_ids` + `answers`, so the gate's nDCG@10 component measures the
order. This only uses gate-split gold. Evaluated questions are still never
seen, and the gate split, the dataset and the suite targets are unchanged.
Regression test: `TestC5GateKeyCitesEvidenceRecords`.

## Result, measured and published as is

After the fix, the sleep run trains the same model and the gate scores it
71.36 against a 70.05 baseline. That gain of 1.31 points is above the 1.0
margin, so the model is promoted at λ = 0.05, the first `lambda_step`.
On the evaluated questions, C5 against C4:

| Metric | C4 | C5 | Δ |
| --- | --- | --- | --- |
| nDCG@10 | 0.5329 | 0.5381 | +0.0052 |
| MRR | 0.5081 | 0.5178 | +0.0097 |
| recall@5 | 0.5963 | 0.5852 | −0.0111 |
| recall@10 | 0.6074 | 0.6000 | −0.0074 |
| answer in context | 0.6370 | 0.6333 | −0.0037 |

`gain_c5_over_c4` is +0.0052, which is positive. `gate_regression`
(GatePoints on the evaluated questions) is 0.33, which is one question.
The learning suite therefore still **fails** its "no regression" bound, and
the report says so.

The first gold item moves up in 12 questions; in q001 it enters the top 10.
It drops out of the context in 3 (q020, q079, q101), where the term swaps
the last item at the context cut (a fact for a chunk, or the reverse). It
moves down in 4 others. The term therefore carries a real but small signal
at this scale.

## Known limitations

- **Repeated passes.** The 3 passes repeat the same 99 training questions,
  so the newest 20% of retrievals held out for `eval_score` were already
  seen. The held-out AUC of 0.999 (relevance alone: 0.925) is optimistic.
  Promotion does not use it.
- **Gate size.** The gate split has 33 questions. BASE.md asks for 50 to
  100, so a 1-point margin is close to one question's weight.
- **One sleep run.** C5 runs sleep once, so λ stops at one step. A longer
  task sequence with periodic sleep would let λ climb only while the gate
  keeps improving.

These are recorded, not tuned. In particular, no λ, margin, pass count or
gate size was chosen by looking at the evaluated questions.
