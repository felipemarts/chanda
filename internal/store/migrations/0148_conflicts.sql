-- 0148_conflicts: conflict resolution between raw records (decision 0148),
-- off by default (sleep.conflicts.enabled).
--
-- conflict_candidates: the queue that write-time detection fills (no LLM).
--   One row per pair of live events, stored once whichever event arrived
--   second: older_id and newer_id are ordered by effective time
--   (valid_from, else recorded_at). differs is the signature component
--   that differs (numbers, dates, entities, negations); shared is the JSON
--   array of shared entities and content terms. Phase 2 judges pending
--   pairs: status becomes judged, with the model's answer (model_verdict,
--   model_current A|B, confidence), the verdict after the rules (verdict)
--   and why it was or was not applied (reason). Events are never changed.
-- supersessions: the effect of an applied verdict. old_id is outdated by
--   new_id from ends_at (the new record's effective time). Staged and
--   promoted through the sleep gate like other phase 2 writes (status
--   staged, then active); revert keeps the row with reverted_at. pair_id
--   names the judged pair (NULL once the queue is reset).

CREATE TABLE conflict_candidates (
  id            TEXT PRIMARY KEY,           -- cfl_<ULID>
  namespace     TEXT NOT NULL,
  older_id      TEXT NOT NULL,
  newer_id      TEXT NOT NULL,
  cosine        REAL NOT NULL,
  differs       TEXT NOT NULL CHECK (differs IN ('numbers','dates','entities','negations')),
  shared        TEXT NOT NULL,              -- JSON array, sorted
  status        TEXT NOT NULL CHECK (status IN ('pending','judged')),
  model_verdict TEXT,
  model_current TEXT,
  confidence    REAL,
  verdict       TEXT,
  reason        TEXT,
  sleep_run_id  TEXT,
  judged_at     TEXT,
  created_at    TEXT NOT NULL,
  UNIQUE (namespace, older_id, newer_id)
) STRICT;
CREATE INDEX conflict_candidates_older  ON conflict_candidates(older_id);
CREATE INDEX conflict_candidates_newer  ON conflict_candidates(newer_id);
CREATE INDEX conflict_candidates_status ON conflict_candidates(namespace, status, id);

CREATE TABLE supersessions (
  id           TEXT PRIMARY KEY,            -- sup_<ULID>
  namespace    TEXT NOT NULL,
  old_id       TEXT NOT NULL,
  new_id       TEXT NOT NULL,
  pair_id      TEXT,
  sleep_run_id TEXT NOT NULL,
  ends_at      TEXT NOT NULL,
  confidence   REAL NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('staged','active')),
  created_at   TEXT NOT NULL,
  reverted_at  TEXT
) STRICT;
CREATE INDEX supersessions_old ON supersessions(old_id);
CREATE INDEX supersessions_new ON supersessions(new_id);
CREATE INDEX supersessions_ns  ON supersessions(namespace, id);
