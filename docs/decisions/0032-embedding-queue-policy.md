# 0032 Embedding queue and vector lifecycle

Details BASE.md leaves open, as implemented by `embed.Indexer` and
`embed.ExactIndex`:

- **What is embedded.** `events.content`, `facts.text` (every status, since
  history is searchable on request), `pages.title + "\n\n" + body` and
  `page_items.text`. Retired records (events with `retired_at`, pages and
  page items with status `retired`) are not indexed; their queue rows are
  dropped. Vectors they already have are kept: the read filter excludes
  retired records, and only `forget` deletes.
- **No provenance edges for vectors.** An embedding is keyed by
  (record_type, record_id, model_id); the integrity checker should treat a
  vector as reaching its record's origins. `core.RelEmbeddingOf` stays unused.
- **Batches.** The queue drains in (enqueued_at, record_type, record_id)
  order, `embed.batch_size` rows per transaction. Text is read and embedded
  outside the transaction, so a slow model never holds the single
  connection; the commit re-checks each row and skips any whose queue row or
  text changed meanwhile (it stays queued for the next batch).
- **Remove** deletes a record's vectors for every model and its pending
  queue row (what forget needs).
- **Model migration.** Vectors of a new model coexist with the old ones;
  `Search` uses only the requested model. `Backfill` enqueues live records
  missing a vector for the current model; `CompleteMigration` drops the old
  model only when `MissingFor` is empty.
- **Audit.** Actions `embed_index` (one entry per namespace per batch, with
  counts), `embed_remove`, `embed_drop_model`, `embed_backfill`, actor
  `system:embed`. They should join the action list in `internal/audit`.
- **Reranker.** Not provided. The read path treats a nil `core.Reranker` as
  disabled until a pure-Go cross-encoder is confirmed (BASE.md open question,
  before phase 5).
