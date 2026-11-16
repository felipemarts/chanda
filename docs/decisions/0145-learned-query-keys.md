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
