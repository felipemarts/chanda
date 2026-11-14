-- 0030_embed_generations: change counter per (namespace, model_id) of the
-- embeddings table (internal/embed, decision 0030).
--
-- The exact vector index keeps vectors in memory. Any package may delete
-- embeddings rows with plain SQL (forget does), so every insert, update and
-- delete bumps this counter by trigger; the index compares it before each
-- search and reloads when it moved. Holds no record content.

CREATE TABLE embedding_generations (
  namespace TEXT NOT NULL,
  model_id  TEXT NOT NULL,
  gen       INTEGER NOT NULL,
  PRIMARY KEY (namespace, model_id)
) STRICT;

CREATE TRIGGER embeddings_gen_ai AFTER INSERT ON embeddings BEGIN
  INSERT INTO embedding_generations(namespace, model_id, gen) VALUES (NEW.namespace, NEW.model_id, 1)
  ON CONFLICT(namespace, model_id) DO UPDATE SET gen = gen + 1;
END;

CREATE TRIGGER embeddings_gen_ad AFTER DELETE ON embeddings BEGIN
  INSERT INTO embedding_generations(namespace, model_id, gen) VALUES (OLD.namespace, OLD.model_id, 1)
  ON CONFLICT(namespace, model_id) DO UPDATE SET gen = gen + 1;
END;

CREATE TRIGGER embeddings_gen_au AFTER UPDATE ON embeddings BEGIN
  INSERT INTO embedding_generations(namespace, model_id, gen) VALUES (OLD.namespace, OLD.model_id, 1)
  ON CONFLICT(namespace, model_id) DO UPDATE SET gen = gen + 1;
  INSERT INTO embedding_generations(namespace, model_id, gen) VALUES (NEW.namespace, NEW.model_id, 1)
  ON CONFLICT(namespace, model_id) DO UPDATE SET gen = gen + 1;
END;
