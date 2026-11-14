# 0021 Read path: interpretations of BASE.md

Points where BASE.md is silent or ambiguous, and what `internal/read` does.

- **Explicit `as_of` selects the fact that held then.** "Only active facts
  valid at `as_of`" would make time travel impossible, since a fact that held
  in March is `superseded` today. When the caller gives `as_of`, a
  `superseded` fact whose validity window contains it is eligible, in both
  modes. Without `as_of`, `context` never returns superseded facts, and
  `search` only with `include_history` (which then ignores the validity
  window). Quarantined, rejected, flagged and stale_source facts are never
  returned.
- **`flags` table.** A record with a row in `flags` leaves every read, whatever
  its type (events cannot change status): `NOT EXISTS (SELECT 1 FROM flags
  WHERE record_id = <id>)` in the keyword query, and a `flags` lookup by
  `record_id` in the rule filter (vector hits). Ids are globally unique, so
  `record_type` is not compared. Migration `0020_read` indexes
  `flags(record_id)`.
- **Retrieval log shape.** `retrievals.candidates` elements carry exactly the
  keys internal/rank reads: `id, type, bm25, cosine, rerank, rrf, utility,
  learned, final, zone, age_seconds, features` (null when a stage gave no
  value; `utility` is the term used, 0.5 when neutral). `features` holds only
  numbers (`kw_rank, vec_rank, coverage, evidence, relevance, has_utility,
  delivered, position`). One extra key, `dropped`, names why a candidate left
  the list (rank ignores unknown keys). Gate-set queries (`Gate: true`) log
  `params.gate = true`.
- **Inherited `instruction_like`.** A fact or page whose origin events carry
  `instruction_like` inherits the mark: shown in `search`, excluded from
  `context`.
- **Keyword top-K after cheap filters.** The FTS query joins the base table
  and already excludes retired events, inactive facts and pages, other
  namespaces and unrequested zones, so ineligible rows do not use up the
  top 50. The full rule filter still runs afterwards (vector hits need it).
- **Near-duplicates.** Records joined by `duplicate_of` edges (transitively)
  form a group; the newest eligible member (`recorded_at`, pages
  `updated_at`, then id) represents it, inheriting the best list positions of
  the group, even if it was not itself a hit.
- **Diversity key.** An item's document is the smallest `doc_id` among its
  origin events. Items without a document (statements, session turns) are not
  capped.
- **Logging a query is audited.** The `retrievals` row is a mutation of the
  learning layer, so it is written with an audit entry (action `retrieve`,
  details: mode and counts, never the query text) in the same transaction.
  Candidates unknown in the namespace are not logged.
- **`get`** shows a record whatever its status (it is inspection by id). With
  `expand_neighbors`, document chunks use the current version if the chunk is
  in it, else the latest version containing it; session turns use the
  session order.
- **Learned score** is computed and logged only (shadow mode); it never
  changes the order.
