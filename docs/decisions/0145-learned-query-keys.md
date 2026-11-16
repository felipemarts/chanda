# 0145 Learned query keys (off by default)

The sleep study (`eval/sleep/RESULTS.md`) found that adding digested text
to the context block does not improve later answers. It takes room from
the evidence. This record tests the other way to learn from use: change
**which candidates reach the block, and in what order**, without adding any
text to the block. When an answer that used a record is judged good, the
question that led to it becomes a *key* of that record. A later question
that resembles the key finds the record through it, even if its own words
do not match the record's.

Revision of decision 0142. That decision kept the learning layer from
reading query text. Query text from the use of the memory is legitimate
production data, so keys store it. Gold answers, judge text and test
questions remain forbidden in every evaluation (protocols, leak checks).

**Off by default.** Every key below is `omitempty` and defaults to off. The
default configuration hash stays `4dd590544d868b15`.

## Keys (configuration)

| Key | Default | Meaning |
| --- | --- | --- |
| `write.learned_keys` | `false` | A positive outcome creates keys (below). |
| `read.learned_keys` | `false` | Reads use keys as an extra candidate list. |
| `read.learned_keys_weight` | 0 (= 1.0) | Weight w of the keys list in the fusion. |
| `read.learned_keys_top_k` | 0 (= 20) | Keys taken from each of the two key searches (vector, keyword). |

## Creation

When the `outcome` tool records `value > 0` for a retrieval, every id in
`used_ids` that the retrieval delivered gets one key:

- id `key_<ULID>`, namespace, `record_type`, `record_id`, `retrieval_id`,
  `text` (the retrieval's `query_text`, normalised like an event), created
  at;
- its embedding, the query text embedded with the *query* role, so that
  key and query compare as two queries. It goes through the embedding
  queue under record type `key`, and the queue is drained like every other
  (phase 1, flush);
- provenance edges `key → retrieval` (relation `derived_from`) and `key →
  record` (relation `points_to`).

The key of the same normalised text for the same record is created once.
Only outcomes the session agent sends create keys: the `outcome` tool, with
its current rate limit (`ranking.outcome_rate_per_minute`). Sleep and
admin paths never create them, except `rebuild` below, which replays
outcomes already recorded.

## Read

With `read.learned_keys` on (and the runtime switch on, below), step 2/3 of
the read path also searches the keys:

- **vector**: the query vector against key vectors, top `top_k`;
- **keyword**: the query terms against an FTS5 index of key text, top
  `top_k`.

Each key hit maps to its record. A record's key rank is the best rank
among its keys in either search, which gives one extra ranked list of
records. The list enters the fusion with weight w: the record's RRF gains
w/(k + key rank). The normalisation of relevance (`maxRRF`) does not
change, so a read without key hits scores exactly as before.

A record that only keys reached becomes a candidate and goes through every
later step as usual: rule filter, near-duplicate collapse, rerank, utility,
cut, selection. For the evidence cut, the key's own match also counts: the
cosine of the query to the matched key, and the coverage of the query
terms in the key text. Without that, a record found only through a key
whose words differ from the question would always fail the floor.

A key never appears in a result or in the block. Retrieval logs record the
key rank of a candidate under `features.key_rank` (0 when none).

Determinism: no randomness. The same database and the same query give the
same result, with ties broken by id as everywhere else.

## Control

The admin tool `learned_keys` (and the CLI `chanda keys`) takes these
actions, each audited:

- `disable` / `enable`: a runtime switch per namespace, stored and audited.
  Reads use keys only when both the configuration and the switch are on;
  the switch starts on.
- `reset`: deletes every key of the namespace.
- `rebuild`: creates the missing keys of every positive outcome already
  recorded, by the creation rule above. It is deterministic and calls no
  LLM. The evaluation uses it to give a copied database its keys.
- `stats`: the number of keys.

## Forget

Keys are derived records. They are removed when their record is forgotten,
when their retrieval is removed (forget already removes retrievals whose
query text, candidates or delivered list touch the scope), and when a
term or entity scope matches their text. Tests cover each case.

## Implementation notes

Where the rule above leaves a choice, or the code differs from a literal
reading, the implementation does this:

- **Storage** (migration `0145_learned_keys.sql`). `learned_keys` (id
  `key_<ULID>`, `core.PrefixKey`), unique on (namespace, record_type,
  record_id, text), with a foreign key to `retrievals`;
  `learned_keys_fts`, trigger-maintained like the other FTS tables
  (`record_id` is the key id); `learned_keys_state` holds the runtime
  switch per namespace (no row means on). `key` and `retrieval` are
  record types of provenance edges and vectors only (`core.RecKey`,
  `core.RecRetrieval`). They have no id-prefix entry, so `get`, `flag`,
  `promote` and forget's record scope never route to a key. The
  provenance checker knows both as edge endpoints.
- **Text.** The key text is the retrieval's `query_text` through write
  step 1 (`normalize.Text`). The dedupe is on that exact text, so case
  differences make two keys. Only records of type event, fact or page
  that exist in the namespace get a key. Other used ids count as
  `skipped`.
- **Creation** runs inside the outcome transaction, after the outcome's
  own audit entry, for the credited ids (used and delivered). One more
  audit entry, `keys_create` (target: the outcome id), holds counts only:
  created, existing, skipped. `outcome` returns `keys_created`
  (omitempty). The App wakes the indexer. Keys are queued under record
  type `key`. The indexer embeds queued keys with `EmbedQuery` (the
  passages of the same batch go through `Embed` in one call, as before).
  A key has a vector only after the queue is drained: `flush` or the
  background indexer.
- **Key rank.** A record's key rank is the smallest rank, among its keys,
  in the vector list or the keyword list of keys, each 1-based over key
  hits. Two records can share a key rank. The final order still breaks
  ties by id. Key hits whose record type is outside the search's `types`
  filter are dropped before they become candidates. Every other rule
  (namespace, zone, retired, flagged, status) is the rule filter's, and
  key-only records reach it like any other.
- **Vector search over records.** Without a type filter the record
  vector search now passes `core.RecordTypes()` (every record type with
  an id prefix) instead of no filter, so key vectors never take a slot.
  With no key vectors in the index the result is identical.
- **Evidence.** A record's own `coverage`, `cosine` and logged
  `evidence` do not change. The cut keeps a candidate when its own
  evidence passes (as before) or its key evidence passes: best key
  coverage ≥ the model's `MinTermCoverage`, or best query-to-key cosine
  (vector hits only) ≥ its `MinCosine` (`read.Floors`).
- **Log.** `features.key_rank` (0 when none), and with a key hit
  `key_coverage` and `key_cosine` (vector hit only), are logged on every
  candidate of a read that searched the keys (configuration and switch
  on). The params then hold `key_records`. A read with keys off logs
  exactly what it logged before, without `key_rank`. This keeps off reads
  byte-identical.
- **Failures.** A failed key vector search adds the notice
  `learned_keys_vector_unavailable`, and the keyword side still counts.
  A scoring read inside a transaction (the sleep gate) searches keys by
  keyword only, as it does for records.
- **Control.** MCP tool `learned_keys` (admin profile, the 19th tool),
  input `{namespace?, action}`. The output always carries the counts
  after the action: `enabled`, `keys`, `records`, `embedded`, `pending`.
  `reset` adds `removed`. `rebuild` adds `outcomes`, `created` and
  `existing`. Audit actions: `keys_toggle` (enabled_from, enabled_to),
  `keys_reset` (keys, embeddings, provenance_edges) and `keys_rebuild`
  (outcomes, created, existing, skipped). `stats` writes nothing. When
  `read.learned_keys` is off, every call adds the notice
  `learned_keys_read_off`. `reset` deletes the vectors, queue rows and
  edges of the keys and keeps the switch. `rebuild` ignores
  `write.learned_keys`: it is the explicit path that gives a copied
  database its keys. It replays outcomes with value > 0 in id order,
  using used ids ∩ delivered ids of each retrieval. `ranking_reset` does
  not touch keys. CLI: `chanda keys enable|disable|reset|rebuild|stats`.
- **Forget.** Keys go when their record goes, when their retrieval goes,
  or when their text matches. Matching text means a term found only in
  removed events, or a probe byte string (the rule already applied to
  retrieval query text), or a used term of the entity scope, or every
  term of the search scope. The last two are matched over
  `learned_keys_fts`. Keys are deleted before retrievals (foreign key),
  with their vectors, queue rows and edges. `learned_keys_fts` is in the
  scrubbed and probed FTS tables. `removed.keys` (report and API) and
  `keys` in the tombstone counts are always present, 0 when nothing was
  removed.
- **Stats.** The `stats` tool's embedding count includes key vectors.
