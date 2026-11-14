# 0125 Near-duplicate and vector merge thresholds per embedding model

`write.near_dup_threshold` (0.95) and `sleep.merge_similarity` (0.92)
compare a cosine of the embedding model with a fixed number, chosen for the
hash embedder. Under e5-small the deduplication suite missed 21% of real
duplicates (`latest-e5.md` at 81d5ff1), against 3.75% with hash: e5 scores
a paragraph and the same paragraph with a courtesy word appended lower than
0.95, and it scores different entity names (same template, other person)
far higher than hash does.

## Mechanism

Same as `read.evidence_floors` (0121): `write.near_dup_thresholds` and
`sleep.merge_similarities`, maps keyed by model id or family (`e5-small`
covers both build flavours), with the scalar as the fallback
(`config.perModel`).

- Merge: `sleep.proposeMerges` uses
  `Sleep.MergeVectorSimilarity(Embed.ModelID)` for `vector_similarity`
  proposals only. Name (edit) similarity does not depend on the model and
  keeps `sleep.merge_similarity`.
- Near-duplicate guard: its consumer is `internal/app/hooks.go`
  (`markNearDuplicate`), outside this work's area, and it reads the scalar.
  So `config.Load` resolves the scalar: when the file does not set
  `write.near_dup_threshold`, it becomes `Write.NearDup(Embed.ModelID)`
  (also in `Default()`); a value set in the file wins. Both resolve through
  the *configured* `embed.model_id`; the embedder factories reject a
  configured id that disagrees with the model (0031, 0034), so it is
  reliable when set. With `embed.model_id` empty the scalar default
  applies. The exact equivalent of the evidence floors (resolution from the
  running embedder) is a one-line change in `markNearDuplicate`:
  `a.e.Cfg.Write.NearDup(a.emb.ModelID())`; the load-time resolution can
  then go.

## Calibration data (never the evaluated questions)

`-calibrate C3` prints both thresholds after the evidence floors
(`eval.CalibrateNearDup`, `eval.CalibrateMerge`). The pairs come from the
corpus and from the entities C3 extracts from it; no question of any split
is used.

- Near-duplicate guard. Paragraphs: every distinct chunk text of at least
  8 words **except the 40 the deduplication suite builds its probes from**
  (`eval.DedupProbeBases`, now shared with the suite), so the suite stays a
  held-out measurement of the chosen value. Positives (1352): three real
  duplicates of each paragraph (first letter's case swapped; a courtesy
  sentence appended, none of them the suite's "Lembrete."; final period
  removed or added), kept only when `dedup.Signature` is unchanged, since
  only then does the threshold decide. Negatives (2193): every pair of
  paragraphs with identical signatures whose words differ (the guard would
  collapse them on the cosine alone); pairs with the same words in another
  order are duplicates, not negatives. The guard collapses when cosine >
  threshold.
- Vector merge proposals. Entities: the live entities of C3 after ingesting
  the corpus. Positives (191): each name against its variants written
  otherwise (lowercase without accents; "Last, First"). Negatives (4753):
  every pair of entities with different folded names and compatible types
  (the pairs sleep compares). A proposal is made when cosine ≥ threshold.

## Rule (stated before the values were chosen)

A threshold is acceptable when no negative pair passes it and at most 5% of
the positive pairs miss it (BASE.md: real duplicates not collapsed up to
5%). The current value is kept when acceptable; otherwise the middle of the
acceptable interval; when none is acceptable, the lowest four-decimal value
that no negative passes (a hidden distinct record or a false merge proposal
is worse than a missed duplicate), with the misses reported.

## Values

| | hash-v1-256 | e5-small |
| --- | --- | --- |
| near-dup positives: min / 5th percentile | 0.8694 / 0.9255 | 0.9288 / 0.9481 |
| near-dup negatives: max | 0.9369 | 0.9874 |
| at 0.95: positives missed / negatives passing | 232 / 0 | 88 / 139 |
| **`write.near_dup_thresholds`** | **0.937** (no acceptable value) | **0.9875** (no acceptable value) |
| at the chosen value: missed / passing | 116 (8.6%) / 0 | 368 (27.2%) / 0 |
| merge positives: min / 5th percentile | 1 / 1 | 0.9221 / 0.9479 |
| merge negatives: max | 0.7755 | 0.9719 |
| at 0.92: missed / passing | 0 / 0 | 0 / 142 |
| **`sleep.merge_similarities`** | 0.92 kept (no entry) | **0.972** (no acceptable value) |
| at the chosen value: missed / passing | 0 / 0 | 70 / 0 |

The closest negatives explain the e5 values. Near-duplicates: "Notícias
de março de 2026 da equipe de Goiânia." / "… de junho de 2026 …" (0.9874:
month names are not dates for the signature, so only the cosine separates
two different newsletters), two "Riscos: …" lines that differ in one topic
word (0.9869), and the same person's directory entry before and after a job
title change, "Cargo: Cientista de Dados" / "Cargo: Engenheiro de Dados"
(0.9866): an **update**. At 0.95, 139 such pairs would be collapsed under
e5. Merges: "Beatriz Espíndola" / "Gabriela Espíndola" (0.9719), "Carla
Kowalski" / "Helena Kowalski": e5 places different people with the same
surname closer than a name and its "Last, First" form, so at 0.92 it would
propose 142 false merges. With hash, the closest negatives are templated
minutes that differ in one word (0.9369), and 0.95 misses 17% of the
positives (short paragraphs plus a courtesy sentence), so the data moves
the hash value too, from 0.95 to 0.937.

**What this does to the deduplication suite.** For e5 the rule chooses not
to collapse distinct records over catching duplicates, so the suite's
duplicate miss rate under e5 does not improve; it gets worse (see the
measurement below). No single cosine threshold of e5-small separates real
duplicates from same-signature different paragraphs on this corpus; the
lever is the signature (month names as dates, role words), not the number.

## Measurement (deduplication suite, held out from the calibration)

| | hash before (0.95) | hash after (0.937) | e5 before (0.95) | e5 after (0.9875) |
| --- | --- | --- | --- | --- |
| duplicates missed | 3 of 80 (0.0375), pass | 1 of 80 (0.0125), pass | 17 of 80 (0.2125), fail | **27 of 80 (0.3375), fail** |
| updates discarded | 0 of 40 | 0 of 40 | 0 of 40 | 0 of 40 |

The suite's updates change a number, so the signature, not the threshold,
protects them; its duplicates are what the threshold decides, and under e5
the chosen value misses more of them. That is the stated rule doing what it
says (no distinct record hidden on the calibration pairs, at the cost of
missed duplicates), not an improvement. Merge proposals have no suite; the
counts above are the evidence.

The e5 report's version reads `…e42b77512857+dirty`: the hugot binary was
built from e42b775 with this record still untracked in the worktree (Go
stamps untracked files as modified); the code is e42b775's.
