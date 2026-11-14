-- 0060_sleep: indexes for the scans sleep runs (owned by internal/sleep).
CREATE INDEX sleep_facts_ns_status       ON facts(namespace, status, id);
CREATE INDEX sleep_runs_ns_started       ON sleep_runs(namespace, started_at);
CREATE INDEX sleep_proposals_ns_status   ON entity_merge_proposals(namespace, status, id);
CREATE INDEX sleep_pages_ns_kind_status  ON pages(namespace, kind, status, id);
CREATE INDEX sleep_event_processing_step ON event_processing(namespace, step, event_id);
