# 0010 Write path: details BASE.md leaves open

Phase 1 of `internal/write` fixes these points:

- **Normalization trims every line.** Runs of whitespace become one space,
  lines are trimmed and blank runs collapse to one blank line. Markdown
  indentation (nested lists, indented code) is lost in the stored text. Fenced
  code keeps its lines and stays one chunk.
- **Hashes cover masked text.** `content_hash` and `documents.content_hash`
  are SHA-256 of the normalized text after secrets are masked, so a stored
  hash never depends on a secret.
- **Length rules apply to whole writes.** `min_chars`/`max_chars` apply to a
  `remember` text, a session turn or a whole document, not to each chunk.
- **Secrets are masked before chunking**, so a key block longer than a chunk
  is never split past the mask. A chunk gets `secrets_masked` when it holds
  `[REDACTED]` and the document had a mask.
- **Chunks are paragraphs.** ATX headings (`#`) set `heading_path`; each
  paragraph is a chunk. Setext headings are plain text. A document with only
  headings is one chunk. Chunks are matched to the previous version by
  content hash only: a moved paragraph is kept with its first `heading_path`
  and `ordinal`; `document_chunks` holds its position in each version.
- **Stale facts.** When chunks are retired, an `active` or `quarantined` fact
  with a provenance edge to one of them becomes `stale_source` only if no
  edge to a live event is left (same idea as `forget`: a fact with another
  origin survives). Superseded, rejected and flagged facts are not touched.
  Facts reached only through pages are left to sleep phase 1.
- **Duplicates.** `remember`: same source and hash among live events. Session
  turns also match `session_id` and position, so re-logging a session is
  idempotent and a repeated "thanks" later in the session is a new turn.
  A re-ingested document with the same hash is `unchanged`. Duplicates are
  audited (`duplicate`); admission rejections write nothing and are not audited.
- **Channels.** `remember` requires an explicit `source_kind`; there is no
  default zone. Tool turns use the source `tool:<name>` (external).
- **Audit actions** added: `source_create` and `document_version`.
- **Front matter.** Only `date:` is read (into `documents.doc_date`); no key
  can set a zone. Chunk events keep `valid_from` NULL; the document date stays
  on `documents`.
