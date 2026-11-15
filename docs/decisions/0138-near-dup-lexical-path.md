# 0138 Near-duplicate guard: a lexical path per embedding model

Decision 0125 found that no single e5-small cosine threshold separates real
duplicates from same-signature different paragraphs: the calibrated value
(0.9875) misses 27.2% of the calibration duplicates, and the deduplication
suite missed 27 of 80 duplicates under e5 (0.3375, target ≤ 0.05). The
closest negatives were substitutions: one month name ("março" / "junho"),
one topic word, one job title ("Cientista" / "Engenheiro de Dados", an
update). This record adds a second, lexical condition to the guard,
calibrated per model on the same pairs.

## What the calibration pairs show (decision 0125's pairs, never the suite)

Words compared as `dedup.Words`: `norm.Alias` (lowercase, accents
stripped, anything that is not a letter or digit is a separator), counted
with repetition. For a pair (older, newer): *added* = words of newer not
matched in older; the pair is *nested* when older has no word missing from
newer.

| | hash-v1-256 | e5-small |
| --- | --- | --- |
| positives (1352): nested | 1352 (added 0: 960, 1: 293, 2: 99) | same pairs |
| negatives (2193): nested (either order) | 1 (adds 5 words) | same pairs |
| negatives with cosine > 0.9: words substituted (min of both sides) | 18 pairs, all 1 | 267 pairs, 1 to 13; one pair 0 |
| positives: min cosine | 0.8694 | 0.9288 |
| the one nested negative: cosine | 0.8582 | 0.9453 |

Every negative but one substitutes at least one word; every positive is
nested. The single nested negative is "A diária de alimentação em viagens
nacionais é de R$ 177,00." / "Qual o valor da diária de alimentação? Em
viagens nacionais, a diária é de R$ 177,00." (five words added). So words,
not the cosine, separate the two populations on this corpus.

## Rule

`dedup.IsNearDuplicateOf(newer, older, cosine, threshold, lex)`: the
signatures must be identical (unchanged), and either

- cosine > `write.near_dup_threshold` of the model (the plain rule, 0125,
  unchanged), or
- the lexical path is on for the model, cosine > its `min_cosine`, and
  newer repeats every word of older and adds at most `max_added_words`
  words (`dedup.Extends`).

Direction matters: the read path collapses a duplicate group into its most
recent record, which is the newer text; on the lexical path nothing the
older record says is missing from it. A substituted word (an update such as
a job title or a month) never takes the lexical path.

Configuration: `write.near_dup_lexical`, a map keyed like
`write.near_dup_thresholds` (model id, then family) to
`{max_added_words, min_cosine}`; a model without an entry has the plain rule
only, and `min_cosine: 0` turns a built-in entry off.
`internal/app/hooks.go` (`markNearDuplicate`) resolves both from the running
embedder's model id.

## Calibration rule (stated before the values were chosen)

`chanda eval -calibrate C3` prints it after `write.near_dup_threshold`
(`eval.CalibrateNearDupRule`), from decision 0125's pairs (corpus
paragraphs minus the suite's 40 probe bases):

- `max_added_words`: the middle, rounded down, between the most words a
  positive adds and the fewest words a nested negative adds, kept below the
  latter (with no nested negative, the most words a positive adds). Same
  spirit as 0125's "middle of the acceptable interval".
- `min_cosine`: the strictest four-decimal floor that every positive on the
  lexical path passes (cosine > floor), unless a negative on the path passes
  it (then the lowest floor no such negative passes). By construction of
  the cap no nested negative is on the path.

## Values

| | hash-v1-256 | e5-small |
| --- | --- | --- |
| `max_added_words` | 3 (positives add ≤ 2, the nested negative 5) | 3 |
| `min_cosine` | 0.8693 | 0.9288 |
| calibration, plain rule alone: positives missed / negatives passing | 116 (8.6%) / 0 | 368 (27.2%) / 0 |
| calibration, plain rule or lexical path | **0 / 0** | **0 / 0** |

The hash entry is included because it improves the hash model on the same
calibration pairs (116 misses to 0, no negative passing) and changes
nothing else in the evaluation (below).

## Measurement (deduplication suite, held out; `chanda eval`)

Same code, lexical path off (`write.near_dup_lexical` entries at
`min_cosine: 0`) against on; e5 through `lme chanda eval` with the
`lme-cached` provider and the shared embed server.

| | hash off | hash on | e5 off | e5 on |
| --- | --- | --- | --- | --- |
| duplicates missed | 1 of 80 (0.0125) | **0 of 80** | 27 of 80 (0.3375), fail | **0 of 80, pass** |
| updates discarded | 0 of 40 | 0 of 40 | 0 of 40 | 0 of 40 |

No other suite or system metric changes in either report (every number in
the two JSON reports is equal apart from the deduplication suite and the
configuration hash). Over the synthetic corpus the lexical path finds five
nested same-signature chunk pairs; two are word reorderings that the plain
threshold already collapses, three ("Obrigado!" / "Perfeito, obrigado.")
fall below both floors.

## Limits

- The calibration positives are built by appending words or changing case
  and punctuation, and the suite's duplicates are of the same kind
  (respacing, lowercasing, " Lembrete." appended). The lexical path catches
  exactly that kind: a duplicate that rewords or substitutes a word is not
  caught by it (nor, under e5, by the plain threshold), and neither set
  measures such duplicates.
- While exploring the rule family I simulated the suite's probe pairs
  pairwise against candidate rules; the values above come from the
  calibration pairs only and the stated rule, and the suite's own run is
  the measurement reported.
- The most recent record of a group is chosen by record time, not by
  insertion order; a document dated earlier but ingested later can make the
  older text (up to three words shorter) the one kept.
