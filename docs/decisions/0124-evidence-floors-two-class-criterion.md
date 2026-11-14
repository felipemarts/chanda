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
