# eval/lme: LongMemEval harness

A separate Go module (Go 1.27, `replace` to the main module and to
`embedders/hugot`) so it can use Chanda's hash embedder and hugot e5-small
in process without touching the main module. The main module's
`go test ./...` does not see it. No command calls an LLM, except
`contexts -cond E`, which refuses to run without `-allow-llm`.

Dataset, licence, abstention rule and the official metrics: `DATASET.md`.

## Build and data

```sh
cd eval/lme
CGO_ENABLED=0 go build -o bin/lme ./cmd/lme                       # hash runs
CGO_ENABLED=0 GOEXPERIMENT=simd go build -o bin/lme ./cmd/lme     # for e5 (model id ...+simd)
bin/lme fetch                     # data/longmemeval_s_cleaned.json at the pinned revision, SHA-256 checked
bin/lme stats [-sample ../sample.json] [-json FILE]
CGO_ENABLED=0 go test ./...       # offline: loader, sampler, metrics, embed server, MCP end to end on a fixture
```

Paths are relative to `eval/lme`. `data/`, `work/` (per-question databases,
embedding cache, logs), `out/` and `bin/` are git-ignored.

## Sample (pre-registered)

```sh
bin/lme sample -seed 42 -n 60 -min-abs 6 -out ../sample.json
```

Stratified by `question_type` (Hamilton largest remainder), 6 abstention
questions spread over the types that have them, drawn with math/rand/v2 PCG
seeded (42, 42), order shuffled once more (phase 3 takes the first 10
entries). The full algorithm is written into `eval/sample.json`;
`TestCommittedSampleReproduces` redraws it from the dataset. Quotas: KU 9
(1 abstention), MS 16 (3), SSA 7, SSP 4, SSU 8 (1), TR 16 (1).

## Phase 1: retrieval, no LLM

```sh
bin/lme retrieve -embedder hash                                   # all 500 questions
bin/lme retrieve -embedder hash -sample ../sample.json -limit 20  # smoke test
```

Flags: `-conds bm25,rag,chanda`, `-bm25-unit session|turn`, `-workers 4`,
`-budget 2000`, `-search-limit 100`, `-keep` (keep per-question Chanda
databases), `-out DIR` (default `out/phase1-<embedder>-<n>`): writes
`phase1.json`, `phase1.md`, `phase1.questions.jsonl`.

Each question gets fresh, isolated indexes:

- **bm25**: in-memory SQLite FTS5, tokenizer `unicode61 remove_diacritics 2`
  (Chanda's), MATCH built exactly as Chanda's keyword stage does (folded
  terms, Chanda's stopword list, at most 32, quoted, OR), ranked by
  `bm25()`. Unit `session` (default): one document per session, every turn's
  content, both roles. Unit `turn`: one document per turn, sessions ranked
  by their best turn.
- **rag**: each session cut into chunks of consecutive `role: content` lines
  of at most 1,000 runes (long turns cut at sentence ends), embedded as
  passages; the question embedded as a query; exact cosine over every chunk.
  The ranking for recall uses every chunk; the context is the top 50 chunks
  kept greedily while the rendered text stays within 2,000 tokens; "budget
  recall" is the share of evidence sessions with a chunk in that context.
- **chanda**: a real `chanda serve` over MCP stdio (this binary:
  `lme chanda serve`, i.e. `internal/cli.Run` with the caching embed
  provider registered), own database, `log_session` per haystack session in
  date order (turn `ts` = session date, session id `s<index>`, never the
  dataset id, which names evidence sessions `answer_...`), `flush`, then
  `search` with the question, `limit` 100 and `filters.as_of` = question
  date. Items map back to sessions through the ids `log_session` returned
  (and `origin.event_ids` for facts and pages).

Sessions are ranked by first appearance; metrics are in `DATASET.md`.
Latency: BM25 = the FTS5 query; RAG = query embedding + scan + budget fill;
Chanda = the `search` round trip over MCP. Chanda ingest = all
`log_session` calls + `flush`. DB size = Chanda's `stats.db_size_bytes` and
the `.db` + `-wal` + `-shm` bytes on disk.

## Contexts for phases 3/4

```sh
bin/lme contexts -cond B -embedder hash -sample ../sample.json -out out/contexts-B.jsonl
bin/lme contexts -cond D -embedder hash -sample ../sample.json -out out/contexts-D.jsonl
bin/lme contexts -cond E -embedder hash -sample ../sample.json -out out/contexts-E.jsonl \
    -allow-llm -llm-url http://127.0.0.1:PORT/v1 -llm-model deepseek-... -llm-key-env NAME
```

One JSON line per question: `question_id`, `question_type`, `abstention`,
`condition`, `question`, `question_date`, `context` (exactly what goes into
the reader prompt), `context_tokens` (ceil(runes/4)), `retrieved_session_ids`,
`gold_evidence_session_ids` and `evidence_recall_in_context` (analysis only,
never for a prompt), `ingest_ms`, `retrieval_ms`, `db_bytes`, `embed_model`,
`items`, `reason`; D/E add `chanda_tokens_used`; E adds `sleep` (the
`sleep_run` output: status, cost tokens, report) and `sleep_ms`.

Turn-level evidence (B, D and E; analysis only, never for a prompt), the
primary no-LLM measure of `eval/selection/PROTOCOL.md`:

- `evidence_turns`: the question's `has_answer: true` turns (any role);
- `evidence_turns_in_context`: how many of them reached the block;
- `evidence_turn_recall`: in / total, `null` when the question has none
  (such a question is not scored);
- `evidence_turn_matches`: per evidence turn, `session` (haystack index),
  `turn` (index in the session), `role`, `coverage`, `reached` and `by`
  (`id` or `text`);
- `context_items`: one entry per block item (Chanda) or per turn piece
  (plain RAG), in block order: `session`, `turn` (-1 when unknown), `role`
  (when known), `tokens` (ceil(runes/4) of the item as rendered: Chanda's
  label line and content; RAG's `role: content` line), `truncated`, and
  `type` for a Chanda fact or page.

Session-level `evidence_recall_in_context` overstates (a session counts if
any of its turns is in the block); use the turn-level fields.

**Matching rule** (`lme/evidence.go`). Texts are compared with every
whitespace rune removed. The coverage of an evidence turn is the longest
prefix of the turn's content that occurs contiguously in the text searched,
over the turn's (non-whitespace) runes. The turn **reaches the block when
its coverage is at least 0.6**; the full turn present is coverage 1. The
prefix form follows how the renderers lose text: Chanda truncates an item
at its end (`…`, label `| truncated`), plain RAG drops the later parts of a
long turn cut across chunks. Where the coverage is measured:

- Chanda: in the delivered text of each item whose id is the event id
  `log_session` returned for that (session, turn) (`by: id`); failing that,
  in the whole block (`by: text`), which catches a turn delivered under
  another event's id (near-duplicates collapse onto their original at read
  time). A fact or page derived from the turn does not count: the measure
  is about the turn's text.
- Plain RAG: in the rendered selected chunks of the turn's own session
  (`by: text`).

Chanda configuration overrides (D/E only): `-chanda-set key=value`,
repeatable, sets a dotted key of the per-question `chanda.yaml` on top of
the harness's file (the value is YAML: `5`, `true`, `0.4`, `[a, b]`; later
settings of a key win):

```sh
bin/lme contexts -cond D -embedder e5 -embed-url http://127.0.0.1:8089 \
    -sample ../split-tune.json -out out/D-mpd5.jsonl \
    -chanda-set read.max_per_document=5 -chanda-set read.context_min_item_tokens=40
```

The merged configuration is loaded with Chanda's own loader
(`internal/config`, unknown fields refused) before any question runs, so an
unknown key or a wrong type stops the command. `db_path` and
`sleep.snapshot_dir` belong to the harness and cannot be set. Every D/E line
records `chanda_set` (the overrides as given, in order) and
`chanda_config_hash` (Chanda's configuration hash of the file the server ran
with), so each contexts file says which settings produced it.

## Turn-level evidence report

```sh
bin/lme evidence -in out/D.jsonl -in out/B.jsonl [-json out/evidence.json]   # -json - prints JSON only
```

Per input file and question type (then `all`), over the scored questions:
`n scored`, mean turn recall, share of questions with full turn recall,
mean items, tokens per item (pooled), share of item tokens spent on
assistant turns (pooled; the denominator is the items' tokens, not the
block header or RAG session headers) and share of truncated items. Every
later file is paired with the first on the questions scored in both:
questions where A has full turn recall and B not (and the reverse, with
their ids), mean recall of each, how often each is higher, and the exact
two-sided sign test on the full-recall discordant pairs. Files written
before the turn-level fields existed are refused.

- **B**: the plain-RAG context above, in the official LongMemEval history
  layout (`### Session N:` / `Session Date:` / `Session Content:`), sessions
  in date order. The question date belongs to the reader prompt.
- **D**: Chanda `context` with `as_of` = question date and
  `budget_tokens` 2000, after ingest and flush, without sleep.
- **E**: as D with `sleep_run` (phases 1 and 2) and a flush before
  `context`; the LLM endpoint goes into the per-question `chanda.yaml`
  (point it at the cost proxy). Implemented, not run.

Per-question `chanda.yaml`: Chanda's defaults except `embed` (provider
`lme-cached`, `model_id` and `dim` of the wrapped embedder),
`read.context_max_tokens: 2000` (the default ceiling of 1,500 would refuse
the 2,000-token budget), `sleep.scheduler: false` (no background sleep in D),
`mcp.profiles` + `admin` for E only, and `llm` for E only.

## Embeddings, cache and the e5 topology

The cache (`work/embcache.sqlite`, SQLite WAL, shared by processes) is keyed
by (model_id, role, sha256(text)): the role is in the key because e5 embeds
a text differently as a query and as a passage. The caching provider
reports the wrapped embedder's model id and dimension, so Chanda stores the
same model id as `chanda-hugot` would. Default: on for e5, off for hash.

At most one e5 process may run on the 8 GB machine, so e5 always goes
through one embed server and every other process embeds over HTTP:

```sh
bin/lme embedserve -embedder e5 -e5-dir /path/to/e5-small -addr 127.0.0.1:8089 &
bin/lme retrieve -embedder e5 -embed-url http://127.0.0.1:8089 -workers 4
```

## OpenAI-compatible embedding server (Mem0)

`bin/lme embedserve -embedder hash|e5 -addr 127.0.0.1:PORT [-role query|passage] [-cache FILE|off]`
serves `POST /v1/embeddings` (`input`: string or array of strings;
`encoding_format` float or base64; `dimensions` must equal the model's;
token-id inputs refused), `GET /v1/models` (with `dim`), loopback only.
The response `model` is Chanda's model id (`hash-v1-256` or
`e5-small@hugot0.8.6+simd`).

Prefix: e5 embeds queries as `"query: " + text` and stored text as
`"passage: " + text`; the hash embedder has no prefix. An OpenAI request has
no role, so the server takes it from the non-standard field `input_type`
(`query`/`passage`, also `search_query`/`search_document`), else from a
`:query`/`:passage` suffix on `model`, else from `-role`, default **query**.
Why query: Mem0's OpenAI embedder sends memories and searches through the
same call with no role, and e5's model card asks for `"query: "` on both
sides when the two sides cannot be told apart (symmetric use). Chanda,
which knows the role, stores passages and searches with queries; with e5
that is a difference between conditions C and D to state in the report.
