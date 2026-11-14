-- 0050_promotions: zone raises made by the promote tool (BASE.md "Trust zones":
-- only promote, called by a human actor, raises a record's zone). The
-- provenance checker accepts a zone above the minimum of a record's origins
-- only up to the highest zone recorded here for that record.

CREATE TABLE promotions (
  id          TEXT PRIMARY KEY,            -- prm_<ULID>
  namespace   TEXT NOT NULL,
  record_type TEXT NOT NULL,
  record_id   TEXT NOT NULL,
  from_zone   TEXT NOT NULL CHECK (from_zone IN ('external','internal','user')),
  to_zone     TEXT NOT NULL CHECK (to_zone IN ('external','internal','user')),
  actor       TEXT NOT NULL,
  audit_id    INTEGER NOT NULL,
  ts          TEXT NOT NULL
) STRICT;
CREATE INDEX promotions_record ON promotions(record_type, record_id);
