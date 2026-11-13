-- 0001_init: full version-1 schema (BASE.md "Data model").
-- Times are TEXT in core.TimeLayout (fixed-width UTC). JSON columns are TEXT.
-- Every table carries namespace. Migrations are forward-only.
--
-- Migration number ranges reserved per area so parallel work never collides:
--   0001-0009 foundation   0010-0019 write   0020-0029 read    0030-0039 embed
--   0040-0049 facts        0050-0059 prov/forget              0060-0069 sleep
--   0070-0079 rank         0080-0089 mcp/eval                 0090+     integration

CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

---------------------------------------------------------------------------
-- Raw layer

CREATE TABLE sources (
  id         TEXT PRIMARY KEY,
  namespace  TEXT NOT NULL,
  kind       TEXT NOT NULL CHECK (kind IN ('document','session','tool','web','user_statement')),
  uri        TEXT NOT NULL,
  trust_zone TEXT NOT NULL CHECK (trust_zone IN ('external','internal','user')),
  created_at TEXT NOT NULL,
  UNIQUE (namespace, kind, uri)
) STRICT;

CREATE TABLE documents (
  id              TEXT PRIMARY KEY,
  namespace       TEXT NOT NULL,
  source_id       TEXT NOT NULL REFERENCES sources(id),
  uri             TEXT NOT NULL,
  current_version INTEGER NOT NULL,
  doc_date        TEXT,           -- date declared by the document (metadata), if any
  content_hash    TEXT NOT NULL,  -- SHA-256 of the normalized full text of current_version
  updated_at      TEXT NOT NULL,
  UNIQUE (namespace, uri)
) STRICT;

CREATE TABLE events (
  id           TEXT PRIMARY KEY,            -- evt_<ULID>
  namespace    TEXT NOT NULL,
  source_id    TEXT NOT NULL REFERENCES sources(id),
  kind         TEXT NOT NULL CHECK (kind IN ('chunk','turn','statement','tool_result','web','fact_assertion')),
  content      TEXT NOT NULL,               -- normalized, secrets masked
  content_hash TEXT NOT NULL,               -- SHA-256 hex of content
  trust_zone   TEXT NOT NULL CHECK (trust_zone IN ('external','internal','user')),
  actor        TEXT NOT NULL,
  recorded_at  TEXT NOT NULL,
  valid_from   TEXT,                        -- date given in the call, if any
  doc_id       TEXT REFERENCES documents(id),
  doc_version  INTEGER,                     -- version in which this chunk first appeared
  ordinal      INTEGER,                     -- chunk position (first version) or turn index
  heading_path TEXT,                        -- "H1 > H2" for document chunks
  session_id   TEXT,
  flags        TEXT NOT NULL DEFAULT '',    -- sorted, comma-separated: instruction_like, secrets_masked
  retired_at   TEXT                         -- the only column that may change
) STRICT;
CREATE INDEX events_ns_hash   ON events(namespace, source_id, content_hash);
CREATE INDEX events_doc       ON events(doc_id);
CREATE INDEX events_ns_rec    ON events(namespace, recorded_at);
CREATE INDEX events_session   ON events(namespace, session_id);

-- Principle 1: the raw log is never edited. Only retired_at may change.
CREATE TRIGGER events_immutable BEFORE UPDATE ON events
WHEN NEW.id IS NOT OLD.id OR NEW.namespace IS NOT OLD.namespace OR NEW.source_id IS NOT OLD.source_id
  OR NEW.kind IS NOT OLD.kind OR NEW.content IS NOT OLD.content OR NEW.content_hash IS NOT OLD.content_hash
  OR NEW.trust_zone IS NOT OLD.trust_zone OR NEW.actor IS NOT OLD.actor OR NEW.recorded_at IS NOT OLD.recorded_at
  OR NEW.valid_from IS NOT OLD.valid_from OR NEW.doc_id IS NOT OLD.doc_id OR NEW.doc_version IS NOT OLD.doc_version
  OR NEW.ordinal IS NOT OLD.ordinal OR NEW.heading_path IS NOT OLD.heading_path OR NEW.session_id IS NOT OLD.session_id
  OR NEW.flags IS NOT OLD.flags
BEGIN
  SELECT RAISE(ABORT, 'immutable: events are append-only; only retired_at may change');
END;

-- Membership of chunks in each document version (kept chunks are reused across versions).
CREATE TABLE document_chunks (
  namespace TEXT NOT NULL,
  doc_id    TEXT NOT NULL REFERENCES documents(id),
  version   INTEGER NOT NULL,
  ordinal   INTEGER NOT NULL,
  event_id  TEXT NOT NULL REFERENCES events(id),
  PRIMARY KEY (doc_id, version, ordinal)
) STRICT;
CREATE INDEX document_chunks_event ON document_chunks(event_id);

-- Sleep idempotence: which run processed which event, per phase step.
CREATE TABLE event_processing (
  namespace    TEXT NOT NULL,
  event_id     TEXT NOT NULL REFERENCES events(id),
  step         TEXT NOT NULL,            -- e.g. 'extract_facts'
  sleep_run_id TEXT NOT NULL,
  processed_at TEXT NOT NULL,
  PRIMARY KEY (event_id, step)
) STRICT;

---------------------------------------------------------------------------
-- Derived layer

CREATE TABLE entities (
  id             TEXT PRIMARY KEY,        -- ent_<ULID>
  namespace      TEXT NOT NULL,
  canonical_name TEXT NOT NULL,
  type           TEXT NOT NULL DEFAULT '',
  merged_into    TEXT REFERENCES entities(id),
  created_at     TEXT NOT NULL
) STRICT;
CREATE INDEX entities_ns ON entities(namespace, canonical_name);

CREATE TABLE entity_aliases (
  namespace  TEXT NOT NULL,
  alias_norm TEXT NOT NULL,               -- lowercase, no accents, no punctuation
  entity_id  TEXT NOT NULL REFERENCES entities(id),
  PRIMARY KEY (namespace, alias_norm)
) STRICT;

CREATE TABLE entity_merge_proposals (
  id           TEXT PRIMARY KEY,          -- prp_<ULID>
  namespace    TEXT NOT NULL,
  entity_a     TEXT NOT NULL REFERENCES entities(id),
  entity_b     TEXT NOT NULL REFERENCES entities(id),
  score        REAL NOT NULL,
  reason       TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('pending','accepted','rejected')),
  sleep_run_id TEXT,
  created_at   TEXT NOT NULL,
  UNIQUE (namespace, entity_a, entity_b)
) STRICT;

CREATE TABLE predicates (
  namespace   TEXT NOT NULL,
  name        TEXT NOT NULL,
  cardinality TEXT NOT NULL CHECK (cardinality IN ('single','multi')),
  created_at  TEXT NOT NULL,
  PRIMARY KEY (namespace, name)
) STRICT;

CREATE TABLE facts (
  id            TEXT PRIMARY KEY,          -- fct_<ULID>
  namespace     TEXT NOT NULL,
  subject       TEXT NOT NULL,             -- entity id
  predicate     TEXT NOT NULL,
  value         TEXT NOT NULL,
  value_norm    TEXT NOT NULL,
  valid_from    TEXT NOT NULL,
  valid_to      TEXT,                      -- NULL = current
  recorded_at   TEXT NOT NULL,
  superseded_by TEXT REFERENCES facts(id),
  status        TEXT NOT NULL CHECK (status IN ('active','superseded','quarantined','stale_source','rejected','flagged')),
  confidence    REAL NOT NULL,
  trust_zone    TEXT NOT NULL CHECK (trust_zone IN ('external','internal','user')),
  is_single     INTEGER NOT NULL CHECK (is_single IN (0,1)), -- copy of predicate cardinality at insert
  text          TEXT NOT NULL              -- readable sentence, indexed in FTS
) STRICT;
CREATE INDEX facts_sp ON facts(namespace, subject, predicate, valid_from);
-- At most one active fact per (namespace, subject, predicate) for single predicates.
CREATE UNIQUE INDEX facts_single_active ON facts(namespace, subject, predicate)
  WHERE status = 'active' AND is_single = 1;

CREATE TABLE pages (
  id           TEXT PRIMARY KEY,           -- pg_<ULID>
  namespace    TEXT NOT NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('summary','lesson','procedure')),
  title        TEXT NOT NULL,
  body         TEXT NOT NULL,              -- concatenation of active items, in ordinal order
  version      INTEGER NOT NULL,
  sleep_run_id TEXT,
  status       TEXT NOT NULL CHECK (status IN ('active','staged','needs_rebuild','retired')),
  trust_zone   TEXT NOT NULL CHECK (trust_zone IN ('external','internal','user')),
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
) STRICT;

-- Pages change by adding items, never by full rewrite. Each item cites events.
CREATE TABLE page_items (
  id           TEXT PRIMARY KEY,           -- pgi_<ULID>
  namespace    TEXT NOT NULL,
  page_id      TEXT NOT NULL REFERENCES pages(id),
  ordinal      INTEGER NOT NULL,
  text         TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('active','staged','retired')),
  trust_zone   TEXT NOT NULL CHECK (trust_zone IN ('external','internal','user')),
  sleep_run_id TEXT,
  created_at   TEXT NOT NULL
) STRICT;
CREATE INDEX page_items_page ON page_items(page_id, ordinal);

CREATE TABLE embeddings (
  namespace   TEXT NOT NULL,
  record_type TEXT NOT NULL,
  record_id   TEXT NOT NULL,
  model_id    TEXT NOT NULL,
  dim         INTEGER NOT NULL,
  vector      BLOB NOT NULL,               -- little-endian float32 x dim, L2-normalized
  PRIMARY KEY (record_type, record_id, model_id)
) STRICT;
CREATE INDEX embeddings_ns_model ON embeddings(namespace, model_id);

-- Embedding queue (write step 7): drained by the indexer; flush waits on it.
CREATE TABLE embed_queue (
  namespace   TEXT NOT NULL,
  record_type TEXT NOT NULL,
  record_id   TEXT NOT NULL,
  enqueued_at TEXT NOT NULL,
  PRIMARY KEY (record_type, record_id)
) STRICT;

-- Full-text indexes. Maintained by triggers so that every insert/delete of a
-- record is reflected (forget relies on this). unicode61 + diacritic removal.
CREATE VIRTUAL TABLE events_fts USING fts5(
  content, record_id UNINDEXED, namespace UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
CREATE VIRTUAL TABLE facts_fts USING fts5(
  text, record_id UNINDEXED, namespace UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
CREATE VIRTUAL TABLE pages_fts USING fts5(
  title, body, record_id UNINDEXED, namespace UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER events_fts_ai AFTER INSERT ON events BEGIN
  INSERT INTO events_fts(content, record_id, namespace) VALUES (NEW.content, NEW.id, NEW.namespace);
END;
CREATE TRIGGER events_fts_ad AFTER DELETE ON events BEGIN
  DELETE FROM events_fts WHERE record_id = OLD.id;
END;
CREATE TRIGGER facts_fts_ai AFTER INSERT ON facts BEGIN
  INSERT INTO facts_fts(text, record_id, namespace) VALUES (NEW.text, NEW.id, NEW.namespace);
END;
CREATE TRIGGER facts_fts_ad AFTER DELETE ON facts BEGIN
  DELETE FROM facts_fts WHERE record_id = OLD.id;
END;
CREATE TRIGGER facts_fts_au AFTER UPDATE OF text ON facts BEGIN
  DELETE FROM facts_fts WHERE record_id = OLD.id;
  INSERT INTO facts_fts(text, record_id, namespace) VALUES (NEW.text, NEW.id, NEW.namespace);
END;
CREATE TRIGGER pages_fts_ai AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(title, body, record_id, namespace) VALUES (NEW.title, NEW.body, NEW.id, NEW.namespace);
END;
CREATE TRIGGER pages_fts_ad AFTER DELETE ON pages BEGIN
  DELETE FROM pages_fts WHERE record_id = OLD.id;
END;
CREATE TRIGGER pages_fts_au AFTER UPDATE OF title, body ON pages BEGIN
  DELETE FROM pages_fts WHERE record_id = OLD.id;
  INSERT INTO pages_fts(title, body, record_id, namespace) VALUES (NEW.title, NEW.body, NEW.id, NEW.namespace);
END;

-- Provenance graph. derived -> source. Every fact, page, page item and vector
-- must reach an event through these edges (principle 4).
CREATE TABLE provenance (
  namespace    TEXT NOT NULL,
  derived_type TEXT NOT NULL,
  derived_id   TEXT NOT NULL,
  source_type  TEXT NOT NULL,
  source_id    TEXT NOT NULL,
  relation     TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  PRIMARY KEY (derived_type, derived_id, source_type, source_id, relation)
) STRICT;
CREATE INDEX provenance_source ON provenance(source_type, source_id);

---------------------------------------------------------------------------
-- Learning layer

CREATE TABLE retrievals (
  id         TEXT PRIMARY KEY,             -- ret_<ULID>
  namespace  TEXT NOT NULL,
  session_id TEXT,
  mode       TEXT NOT NULL CHECK (mode IN ('search','context')),
  query_text TEXT NOT NULL,
  params     TEXT NOT NULL,                -- JSON: filters, budget, as_of, config hash
  candidates TEXT NOT NULL,                -- JSON: [{id,type,bm25,cosine,rerank,rrf,utility,learned,final,features}]
  delivered  TEXT NOT NULL,                -- JSON: [id,...] in delivered order
  reason     TEXT NOT NULL DEFAULT '',     -- e.g. no_relevant_memory
  ts         TEXT NOT NULL
) STRICT;
CREATE INDEX retrievals_ns_ts ON retrievals(namespace, ts);

CREATE TABLE outcomes (
  id           TEXT PRIMARY KEY,           -- out_<ULID>
  namespace    TEXT NOT NULL,
  retrieval_id TEXT NOT NULL REFERENCES retrievals(id),
  signal       TEXT NOT NULL,              -- 'task_outcome'
  value        REAL NOT NULL CHECK (value >= -1 AND value <= 1),
  used_ids     TEXT NOT NULL,              -- JSON array
  actor        TEXT NOT NULL,
  ts           TEXT NOT NULL
) STRICT;
CREATE INDEX outcomes_retrieval ON outcomes(retrieval_id);

CREATE TABLE utility (
  namespace   TEXT NOT NULL,
  record_type TEXT NOT NULL,
  record_id   TEXT NOT NULL,
  n_shown     INTEGER NOT NULL DEFAULT 0,
  n_used      INTEGER NOT NULL DEFAULT 0,
  n_success   INTEGER NOT NULL DEFAULT 0,
  alpha       REAL NOT NULL DEFAULT 1,     -- Beta positive side
  beta        REAL NOT NULL DEFAULT 1,     -- Beta negative side
  score       REAL NOT NULL DEFAULT 0.5,
  updated_at  TEXT NOT NULL,
  PRIMARY KEY (record_type, record_id)
) STRICT;

CREATE TABLE flags (
  id          TEXT PRIMARY KEY,            -- flg_<ULID>
  namespace   TEXT NOT NULL,
  record_type TEXT NOT NULL,
  record_id   TEXT NOT NULL,
  reason      TEXT NOT NULL,
  actor       TEXT NOT NULL,
  ts          TEXT NOT NULL
) STRICT;
CREATE INDEX flags_record ON flags(record_type, record_id);

CREATE TABLE rank_state (
  namespace     TEXT PRIMARY KEY,
  enabled       INTEGER NOT NULL DEFAULT 1,
  lambda        REAL NOT NULL DEFAULT 0,
  outcome_mean  REAL NOT NULL DEFAULT 0,   -- moving average for centring outcomes
  outcome_n     INTEGER NOT NULL DEFAULT 0,
  active_model  TEXT,
  updated_at    TEXT NOT NULL
) STRICT;

CREATE TABLE ranker_models (
  id             TEXT PRIMARY KEY,         -- mdl_<ULID>
  namespace      TEXT NOT NULL,
  kind           TEXT NOT NULL CHECK (kind IN ('logreg','mlp')),
  status         TEXT NOT NULL CHECK (status IN ('shadow','promoted','retired')),
  weights        TEXT NOT NULL,            -- JSON
  feature_names  TEXT NOT NULL,            -- JSON
  n_examples     INTEGER NOT NULL,
  eval_score     REAL,
  baseline_score REAL,
  created_at     TEXT NOT NULL
) STRICT;

---------------------------------------------------------------------------
-- Audit layer

CREATE TABLE sleep_runs (
  id         TEXT PRIMARY KEY,             -- run_<ULID>
  namespace  TEXT NOT NULL,
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  phases     TEXT NOT NULL,                -- JSON array
  budget     TEXT NOT NULL,                -- JSON
  inputs     TEXT NOT NULL DEFAULT '{}',   -- JSON
  outputs    TEXT NOT NULL DEFAULT '{}',   -- JSON
  snapshot   TEXT,                         -- snapshot file path
  status     TEXT NOT NULL CHECK (status IN ('running','completed','partial','reverted','failed')),
  report     TEXT NOT NULL DEFAULT '',
  cost       TEXT NOT NULL DEFAULT '{}'    -- JSON: tokens in/out, llm calls
) STRICT;

CREATE TABLE gate_questions (
  id          TEXT PRIMARY KEY,
  namespace   TEXT NOT NULL,
  question    TEXT NOT NULL,
  expected    TEXT NOT NULL,               -- JSON: expected record ids and/or answer substrings
  status      TEXT NOT NULL CHECK (status IN ('proposed','approved','retired')),
  created_at  TEXT NOT NULL
) STRICT;

CREATE TABLE audit_log (
  id        INTEGER PRIMARY KEY,           -- monotonically increasing
  ts        TEXT NOT NULL,
  namespace TEXT NOT NULL,
  actor     TEXT NOT NULL,
  action    TEXT NOT NULL,
  target    TEXT NOT NULL,
  details   TEXT NOT NULL,                 -- canonical JSON; never stores forgotten content
  prev_hash TEXT NOT NULL,
  hash      TEXT NOT NULL
) STRICT;
CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_log BEGIN
  SELECT RAISE(ABORT, 'immutable: audit_log is append-only');
END;
CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_log BEGIN
  SELECT RAISE(ABORT, 'immutable: audit_log is append-only');
END;

CREATE TABLE tombstones (
  id             TEXT PRIMARY KEY,         -- tmb_<ULID>
  namespace      TEXT NOT NULL,
  ts             TEXT NOT NULL,
  scope          TEXT NOT NULL,            -- JSON description of the scope, without content
  requested_by   TEXT NOT NULL,
  removed_counts TEXT NOT NULL,            -- JSON
  content_hashes TEXT                      -- JSON; only if forget.tombstone_keeps_hash
) STRICT;
