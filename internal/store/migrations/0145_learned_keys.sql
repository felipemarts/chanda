-- 0145_learned_keys: learned query keys (decision 0145), off by default.
--
-- learned_keys: one key per (record, normalised query text). A positive
--   outcome makes the query text of its retrieval a key of every used
--   record the retrieval delivered (write.learned_keys). record_type and
--   record_id name the record the key points to; retrieval_id the
--   retrieval it came from. Keys are derived records: forget removes them
--   with their record, their retrieval or a scope matching their text.
--   Their vector is in embeddings under record_type 'key' (embedded with
--   the query role).
-- learned_keys_fts: the keyword index of key text, trigger-maintained like
--   the other FTS tables. record_id is the key id.
-- learned_keys_state: the runtime switch per namespace (learned_keys
--   enable/disable). No row means on.

CREATE TABLE learned_keys (
  id           TEXT PRIMARY KEY,           -- key_<ULID>
  namespace    TEXT NOT NULL,
  record_type  TEXT NOT NULL CHECK (record_type IN ('event','fact','page')),
  record_id    TEXT NOT NULL,
  retrieval_id TEXT NOT NULL REFERENCES retrievals(id),
  text         TEXT NOT NULL,              -- normalised query text
  created_at   TEXT NOT NULL,
  UNIQUE (namespace, record_type, record_id, text)
) STRICT;
CREATE INDEX learned_keys_record    ON learned_keys(record_id);
CREATE INDEX learned_keys_retrieval ON learned_keys(retrieval_id);
CREATE INDEX learned_keys_ns        ON learned_keys(namespace, id);

CREATE VIRTUAL TABLE learned_keys_fts USING fts5(
  text, record_id UNINDEXED, namespace UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
CREATE TRIGGER learned_keys_fts_ai AFTER INSERT ON learned_keys BEGIN
  INSERT INTO learned_keys_fts(text, record_id, namespace) VALUES (NEW.text, NEW.id, NEW.namespace);
END;
CREATE TRIGGER learned_keys_fts_ad AFTER DELETE ON learned_keys BEGIN
  DELETE FROM learned_keys_fts WHERE record_id = OLD.id;
END;
CREATE TRIGGER learned_keys_fts_au AFTER UPDATE OF text ON learned_keys BEGIN
  DELETE FROM learned_keys_fts WHERE record_id = OLD.id;
  INSERT INTO learned_keys_fts(text, record_id, namespace) VALUES (NEW.text, NEW.id, NEW.namespace);
END;

CREATE TABLE learned_keys_state (
  namespace  TEXT PRIMARY KEY,
  enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  updated_at TEXT NOT NULL
) STRICT;
