# 0050 Forget, provenance and zones

Answers the two phase-6 open questions in BASE.md and records where
`internal/prov` and `internal/forget` go beyond the specification.

## Open questions

- **Tombstone hash.** By default the tombstone does not keep content hashes.
  It keeps them (the `events.content_hash` of removed events) only when
  `forget.tombstone_keeps_hash` is set. This is a deliberate residue that
  blocks re-ingestion.
- **Snapshots.** Old snapshots are deleted, not rewritten. Forget deletes every
  file named in `sleep_runs.snapshot` and every regular file in
  `sleep.snapshot_dir`, then clears `sleep_runs.snapshot`. Any snapshot that
  exists when forget runs may hold the content, and rewriting a full database
  copy is not worth the risk. Relative paths resolve against the database's
  directory. The database, WAL, SHM and journal files are never deleted, and
  the snapshot directory is ignored when it is the database directory. Sleep
  must treat a missing snapshot as "cannot revert".

## Provenance

- Only `derived_from`, `confirmed_by` and `cites` are derivation (origin)
  edges. `supersedes`, `duplicate_of`, `mentions` and unknown relations are
  never followed for origins, zones, descendants or the checker.
- A page reaches events through its items and through edges named on the page.
- Promotions are stored in `promotions` (migration 0050). The checker accepts
  a zone above `MinZone(origins)` up to the highest promoted zone. Events
  cannot be promoted (their zone is immutable), and promote never lowers a zone.
- `prov.Verify` checks the audit chain and the graph of every namespace.
  `prov.Check(ns)` checks one namespace.

## Forget

- **A derived record's id as scope** (fact, page, page item) resolves to its
  origin events. The record itself is removed too. Otherwise sleep would
  rebuild it from the raw log.
- **Entities** are shared vocabulary. An entity is removed when it is the scope
  (with the entities merged into it), or when only removed records referred to
  it (`mentions` edges, `facts.subject`, `merged_into`). This keeps names from
  outliving the content.
- **Entity scope** resolves to the events that mention the entity, plus the
  origin events of facts whose subject is the entity or that mention it.
- **Facts** are removed when all of their origins are forgotten. A fact with
  another live origin survives and loses only the edges. A surviving fact
  that a removed fact had superseded keeps its closed status
  (`superseded_by` becomes NULL). Forgetting a newer value does not assert
  that the old value is current again.
- **Page items** are removed when any of their origins is forgotten, because
  their text cannot be split by origin. A page with any forgotten origin gets
  `status = needs_rebuild`. Its body is recomputed from the remaining active
  items, and its vector is deleted. A page left with no items is deleted. The
  title is kept until the next sleep rebuilds the page.
- **Usage.** Retrievals are removed when their candidates, delivered list or
  params cite a removed id (as a JSON string). They are also removed when the
  query text holds a term that only removed events contained, or one of the
  probe byte strings. Their outcomes go with them, and so do outcomes whose
  `used_ids` cite a removed id. Gate questions whose `expected` cites a
  removed id are deleted. Removed ids in sleep run inputs, outputs and
  reports become `[forgotten]`.
- **Date range** is `[from, to)` over `events.recorded_at`. Search match ANDs
  every token of the query. Each token is quoted, so FTS5 syntax is never
  interpreted.
- **References probe** scans every column of every table except `audit_log`
  and `tombstones`. Those keep ids, never content, as the proof of what
  happened. A table added by another area that keeps removed ids fails the
  probe, and that is intended: forget must learn to clean it.
- **Byte probe** skips byte strings that a surviving record still holds
  (another namespace, a duplicate in another source, a surviving fact's text).
  The report counts what it skipped.
- If a probe fails, `Forget` returns the report together with a
  `core.CodeIntegrity` error. A trigger-maintained FTS entry that survives its
  row aborts the transaction before commit.
