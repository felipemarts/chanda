-- 0020_read: read-path indexes (internal/read).
--
-- The read path excludes flagged records with
--   NOT EXISTS (SELECT 1 FROM flags f WHERE f.record_id = <id>)
-- inside the keyword query, for every FTS hit. flags_record is keyed by
-- (record_type, record_id), so a lookup by record_id alone needs its own index.
CREATE INDEX flags_record_id ON flags(record_id);
