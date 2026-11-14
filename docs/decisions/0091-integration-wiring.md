# 0091 Integration: how the areas are wired

`internal/app` implements `chanda.API` over one `env.Env`. Choices made while
wiring areas that were built in isolation:

## Write path order (BASE.md steps 7 to 10)

One `write.EventHook` runs inside the write transaction for every new event,
in this order (contradiction before duplication, principle 7):

1. **Facts.** `facts.AfterEvent` runs the deterministic extractors and asserts
   each candidate with the event as its only origin. Skipped for
   `fact_assertion` events (their fact is asserted by the call itself).
2. **Embedding queue.** Every fact *inserted* by step 1 (any action but
   `confirmed`) goes into `embed_queue`; `facts` owns no queue.
3. **Near-duplicates, last.** Candidates are the top
   `write.near_dup_candidates` (default 10) live events of the same namespace
   **and the same zone** by FTS5 BM25 over the new event's tokens (quoted,
   OR-joined), queried through the transaction: the single connection
   forbids `ExactIndex.Search` inside a transaction. The new text and the
   candidates are embedded in memory with the configured `Embedder`; the
   candidate with the highest cosine that `dedup.IsNearDuplicate` accepts
   (cosine above `write.near_dup_threshold`, identical numbers, dates,
   entities and negations) gets `Guard.Mark(new -> older)`. The same-zone
   rule keeps a low-trust copy from becoming the representative of a
   higher-zone record at read time. Only events are compared; facts are
   reconciled by value in step 1.
4. **Per-write integrity check.** `prov.Check` is whole-namespace, too costly
   for every write. Instead every fact created by this write must have at
   least one `derived_from` edge to an event of its namespace, and a zone no
   higher than the lowest zone of those events; otherwise the write fails
   with `integrity` and rolls back. The full `prov.Check` runs in `verify`
   (and later in sleep).

The checker was also changed to compute a record's zone over `derived_from`
and `cites` only: a `confirmed_by` edge is evidence and never lowers a zone
(decision 0040), but the checker counted it and reported confirmed facts as
violations.

## assert_fact

The call writes a raw event of kind `fact_assertion` whose content is
`"<subject> <predicate>: <value>"` (normalized, admitted and audited like any
event), then `facts.Assert` runs with that event as origin, in the same
transaction (`write.RememberInput.OnEvent`, run before the hooks). Without
`valid_from` the fact is dated by the call. An exact duplicate of an earlier
assertion from the same source reuses that event as origin, so A -> B -> A
still supersedes B. A fact whose text would hold a secret is refused
(`rejected`, `secret_in_fact`): the event would be masked but the fact
value would not.

`fact_assertion` events are not read candidates (search and context): the
fact is the readable record, and the raw event would bring a superseded
value back. `get` still shows them.

## Channels: zone ceiling, actor, ingest roots

`chanda.Actor` gained `Remote` and `MaxZone`. The MCP server sets
`Remote: true, MaxZone: mcp.max_zone`; the CLI sets `Human: true, MaxZone:
user`. A call without an actor is `api`, ceiling `mcp.max_zone`. A `user`
ceiling on a non-human actor is lowered to `internal` with a notice. The zone
of every write is `MinZone(ZoneForSource(kind), ceiling)` (decision 0090);
a clamped write gets a `zone_clamped` notice. An exact duplicate must also
match the zone, so a capped copy never aliases a higher-zone event. The
outcome tool uses the ceiling as the caller's zone.

A remote caller may ingest only under `write.ingest_roots` (resolved with
symlinks; empty = refused with `permission_denied`). The folder walk never
follows symlinks. The CLI and Go callers may ingest anywhere.

Defaults: `remember` and `assert_fact` without a source kind use `session`
(uri = `session_id` when given), `ingest` uses `document`.

## Read wiring

`read.Deps{Embedder, VectorIndex: ExactIndex, Reranker: nil, Utility:
rank.Ranker, Learned: adapter over rank.Learned}`. The read path's λ term is
the stage-1 utility, while decision 0070 says it should be the promoted
model's score; λ stays 0 until a model is promoted (sleep/eval), so the two
agree today and must be reconciled before promotion goes live.
Retrievals are audited under the caller's actor and their audit id is
returned. `search` ignores `filters.source_uri` (notice).

## Other mappings

- `forget`: a failed probe answers `ok: true` with `verification.passed:
  false` (docs/errors.md). `root_events` counts removed events. The
  `ExactIndex` cache never serves removed vectors (generation counter;
  tested).
- `promote`: to the current zone answers `conflict`.
- `verify`: the audit chain (global) and `prov.Check` of the namespace;
  violation kinds are listed in notices, record ids in `orphans`.
- `flush`: drains the queue in the caller's goroutine (`RunOnce` until
  empty, or `timeout_ms`).
- `sleep_run`: `unavailable` with the notice "sleep not wired yet".
- `log_session`: `Turn.name` names the tool of a tool turn; `Turn.ts` is
  not stored (the raw log is dated by the clock).
- `timeline` gained `as_of`: only the facts that held at that date.
