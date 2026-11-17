-- 0153_source_labels: a short human-readable name for a source (decision 0153).
--
-- An agent may name the source it writes from (source.label of remember,
-- assert_fact and ingest). The label is shown next to the source in the
-- context block and in origins. It never changes the trust zone, which stays
-- bound to the channel. The latest label of a source wins; forget removes
-- the label with its source.
CREATE TABLE source_labels (
  source_id  TEXT PRIMARY KEY REFERENCES sources(id),
  namespace  TEXT NOT NULL,
  label      TEXT NOT NULL,
  actor      TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;
