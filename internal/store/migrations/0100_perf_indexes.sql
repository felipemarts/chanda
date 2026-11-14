-- 0100_perf_indexes: indexes found by the 100k-record measurement
-- (docs/decisions/0100-performance.md).
--
-- The read path collapses near-duplicates by looking up 'duplicate_of'
-- edges where a candidate is the derived or the source end, by id alone.
-- Neither the primary key (derived_type first) nor provenance_source
-- (source_type first) serves that, so every read scanned the whole
-- provenance table. Partial indexes keep the cost to the duplicate edges.
CREATE INDEX provenance_dup_derived ON provenance(derived_id) WHERE relation = 'duplicate_of';
CREATE INDEX provenance_dup_source ON provenance(source_id) WHERE relation = 'duplicate_of';

-- The provenance checker lists every vector of a namespace by key. Without
-- a covering index it read every row, vector blob included (about 1 KB per
-- vector), and sorted them.
CREATE INDEX embeddings_ns_key ON embeddings(namespace, record_type, record_id, model_id);
