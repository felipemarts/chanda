-- 0070_rank: learning-layer additions (internal/rank, decision 0070).
--
-- utility.last_used_at: time of the last outcome that credited the record
--   (feature "time since last use" of the stage-2 ranker).
-- rank_state.reset_at: ranking_reset watermark; RecomputeUtility and training
--   ignore outcomes and flags at or before it, so a reset survives a recompute.
-- One outcome per retrieval, so an agent cannot pump credit by repeating it.

ALTER TABLE utility ADD COLUMN last_used_at TEXT;
ALTER TABLE rank_state ADD COLUMN reset_at TEXT;

CREATE UNIQUE INDEX outcomes_one_per_retrieval ON outcomes(retrieval_id);
CREATE INDEX outcomes_ns_ts ON outcomes(namespace, ts);
CREATE INDEX flags_ns_ts ON flags(namespace, ts);
CREATE INDEX utility_ns ON utility(namespace, record_id);
CREATE INDEX ranker_models_ns ON ranker_models(namespace, created_at);
