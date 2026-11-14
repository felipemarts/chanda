# Comparing other memory systems under the same harness

BASE.md phase 9 asks for a comparison with other memory systems. **None of
them has been run.** This directory describes how each would be plugged into
the harness so that its numbers are comparable with B0..B3 and C1..C5; until
someone runs them, every result below is "not run" and no figure is
reported or estimated.

| System | Kind | Status |
| --- | --- | --- |
| Mem0 (open-source) | LLM-extracted memories + vector store | not run |
| Graphiti | temporal knowledge graph (bi-temporal edges) | not run |
| Memor | conversation memory library | not run |
| ferrex | local memory server | not run |
| localmem-mcp | local MCP memory server | not run |

## What "the same harness" means

Every compared system implements `eval.System` (`internal/eval/system.go`):

```go
type System interface {
    Name() string
    Ingest(ctx context.Context, item eval.Item) error             // corpus order
    Retrieve(ctx context.Context, q eval.Query, budgetTokens int) ([]eval.Retrieved, error)
    Close() error
}
```

and is added to the factories of `chanda eval` (`internal/cli/eval.go`).
The harness then guarantees, for every system alike:

- **Same corpus, same order.** `eval/data/synthetic-pt` (or any dataset in
  the same format), ingested item by item. Document versions arrive as new
  items with the same id and uri; sessions arrive as turns (tool turns carry
  a tool name) plus the statements the agent asserted.
- **Same budget and estimator.** 800 tokens per query by default, counted as
  `ceil(characters / 4)` by the harness on whatever the system returns; what
  does not fit is dropped and counted as a budget violation.
- **Same reader: none.** The metrics are retrieval metrics over the
  delivered context (span-matched relevance, answer-in-context, superseded
  or injected strings in context, abstention). No LLM reader or judge, so a
  system's own LLM may be used to *build* memory, but what is scored is the
  text it would put in the prompt.
- **Same embedder where it applies.** Systems that accept an embedding
  function receive `FactoryEnv.Embedder` (the one B2 and C1..C5 use) so the
  comparison is about memory design, not embedding quality. A system that
  cannot take an external embedder is run with its own and the report says
  which model it used (`Info().EmbeddingModel`).
- **Traceability.** Each `Retrieved` carries the corpus item it comes from
  (`DocID`, or `URI` mapped by the harness). A system that loses the source
  of a memory must keep a side table from its memory ids to corpus items;
  without it, span-based recall cannot be computed and the report must say
  so instead of guessing.
- **Determinism and declaration.** Anything non-deterministic (an LLM in the
  write path, ANN indexes) is declared in `Info().Description`, with the
  model, temperature and seed, and the run is repeated to report variance.

## Adapter sketches

Each adapter lives in its own package (for example `eval/compare/mem0`) and
talks to the system through its documented interface, never through Chanda
code. Python systems are driven over a local subprocess speaking JSON lines
(one request per `Ingest`/`Retrieve`), so the Go harness stays CGO-free.

### Mem0 (open-source)

- Ingest: documents as `add(messages=[{"role": "user", "content": chunk}],
  metadata={"doc_id", "uri", "version", "date"})`, sessions as their turns
  with `user_id` = namespace and `run_id` = session id. Mem0's LLM extraction
  needs a provider: use a local model, declare it, fix temperature 0.
- Embedder: Mem0's `embedder` config accepts a custom provider; wire the
  harness embedder through the JSON bridge.
- Retrieve: `search(query, user_id, limit=50)`, then fill the budget in rank
  order with each memory's text; `DocID` from the stored metadata.
- Temporal questions: Mem0 has no `as_of`; `Query.AsOf` is ignored and the
  report must show it.

### Graphiti

- Ingest: each item as an *episode* (`add_episode`) with `reference_time` =
  item date, source description = source kind; documents chunked as the
  baselines do (`eval.ChunkItem`) so episodes stay small.
- Retrieve: hybrid `search` with `center_node` unset; facts (edges) rendered
  as their `fact` text; `as_of` mapped to Graphiti's valid-time filter when
  set.
- Needs a graph database (Neo4j or FalkorDB) and an LLM for extraction:
  both declared in the description.

### Memor

- Ingest: sessions as conversations; documents as single-message
  conversations tagged with their uri.
- Retrieve: Memor's prompt/context renderer limited to the budget; if it
  returns a single block, split it back into memories to keep `DocID`.

### ferrex

- Ingest through its CLI or MCP tools (store/remember), one call per chunk
  and per turn, with the item id and date in the metadata.
- Retrieve through its recall/search tool with the question; rank order as
  returned.

### localmem-mcp

- An MCP server: drive it with the same MCP client the adapters use
  (`adapters/generic`), calling its write tool for every chunk and turn and
  its search/recall tool per question. Its trust handling of tool and web
  content is reported as observed; the injection suite counts guidance only
  if its context marks external content.

## Own suites for other systems

The per-system suites (temporal, abstention) run on any `System`. The suites
that need a live Chanda core (deletion, injection, document update,
deduplication, determinism, learning) are defined on Chanda's API; for
another system they are "not applicable" unless an adapter implements the
equivalent probe (for example deletion through the system's own delete call,
checked by searching for the removed item's distinctive terms afterwards).

## Reporting

A comparison report is produced exactly like `eval/reports/latest.md`
(`chanda eval -systems B0,B1,B2,B3,C1,C3,mem0,... -name compare`). A system
that could not be installed or run is listed under "Not available in this
run" with the reason. Nothing is copied from other papers or benchmarks.
