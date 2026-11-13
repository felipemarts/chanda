# 0004 Schema additions to the BASE.md data model

The data model in BASE.md lists main fields only. `0001_init.sql` adds:

- `events`: `valid_from`, `heading_path`, `session_id`, `flags`. A trigger
  rejects any `UPDATE` except of `retired_at` (stricter than content and hash
  only, matching "Only `retired_at` may change").
- `document_chunks`: chunk membership per document version, because a kept
  chunk belongs to several versions while its event row is immutable.
- `event_processing`: "each event stores the run that processed it" without
  mutating the raw log.
- `facts.is_single`: the cardinality copied at insert, so the partial unique
  index can enforce one active fact for `single` predicates.
- `page_items`: pages change by adding items; each item carries provenance.
- `embed_queue`, `entity_merge_proposals`, `flags`, `rank_state`,
  `ranker_models`, `gate_questions`, `meta`.
- FTS5 tables are maintained by triggers, so deletes always reach the index.
- `audit_log` rejects `UPDATE` and `DELETE` by trigger.
- Ids are `<prefix>_<ULID>` (`evt_`, `fct_`, `pg_`, ...) so `get` can route by id.
