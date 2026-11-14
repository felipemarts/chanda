# 0102 Re-ingesting a folder retires the documents that left it

BASE.md step 6 retires chunks that disappeared from a document. A file
deleted from an ingested folder was never retired: its chunks stayed live and
its facts stayed active.

When `IngestPath` walks a **folder** to the end (no error), every document of
the same source (namespace, kind and source uri) whose uri is under
`<source uri>/` and was not seen in the walk is retired, one transaction per
document, in uri order:

- the document gets a new, empty version (no `document_chunks` rows,
  `content_hash = ''`, which no normalized text hashes to);
- every live chunk of its previous version gets `retired_at` (`retire`
  audit per chunk);
- facts left without a live origin become `stale_source` through the
  existing `markStale` (a fact also stated by a live chunk elsewhere stays);
- one `document_retire` audit entry (source id, version, counts).

The result lists it with action `retired`, reason `missing_from_folder`
(`PathResult.Removed` counts them). A file still present but refused by
admission is "seen" and not retired. A single-file ingest never retires
anything. A file that comes back writes the next version with fresh chunk
events (nothing is "kept" from the empty version).

Scope is by source, so ingesting a different folder under the same explicit
source uri retires the first folder's documents: the source uri names the
collection.
