# 0031 Built-in hash embedder until a multilingual model is integrated

BASE.md names hugot for embeddings; whether a small multilingual model runs
on it in pure Go is an open question (research pending in
`docs/research/embeddings.md`). Until then the default provider is `hash`, a
deterministic lexical embedder in pure Go, so phase 3 can be built and
measured end to end:

- Text is folded (NFD, combining marks dropped, lowercase) and split like
  FTS5 unicode61. Each word yields the word, a 5-rune truncation stem and its
  boundary-padded character 3- and 4-grams.
- Features are signed-hashed (FNV-1a 64, fixed seed) into `embed.dim`
  buckets with sublinear tf (1 + ln tf), then L2-normalized.
- Measured cosines (dim 256): identical text 1.0; "preço"/"preços" 0.74;
  "o preço do produto subiu"/"os preços dos produtos subiram" 0.58;
  unrelated sentences 0.04-0.11.

It is a baseline, not a semantic model: synonyms without shared morphology
score near zero. Evaluation reports must declare it (`model_id`
`hash-v1-<dim>`), and B2 must use the same embedder as C1.

The model id is derived from the algorithm version and the dimension; a
configured `embed.model_id` that disagrees is rejected instead of being
stored as a misleading label. A golden test freezes the output; changing the
feature extraction requires bumping `HashVersion` (a model change, handled by
the migration flow: `Backfill`, `Flush`, `CompleteMigration`).

`provider: hugot` returns `unavailable` until the integration lands.
