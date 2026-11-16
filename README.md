# Chanda: local memory for AI agents

**Agent memory** · MCP server · RAG

**Long-term memory for AI agents that never needs an LLM to build it.**

Chanda is a memory server for agents, spoken over MCP. It stores what an agent
learns and what a company documents (documents, session transcripts, facts
with a validity period) in one SQLite file. It returns the right excerpt at the
right moment: a short, delimited pre-context before the first turn, and ranked
search during the task. It ships as a single Go binary that runs on CPU, with no
cgo and no network access, and any MCP client can use it.

Most agent memories call an LLM on every write to extract and rewrite what
they store. Chanda doesn't. Writing costs a hash, an embedding and a few rules,
and reading uses full-text search, vectors, rules and a small ranking. An LLM
is optional and only used offline. The result is memory that is fast, cheap
and reproducible, and that can say exactly where every answer came from.

## Why "Chanda"

The name comes from Chanda, a character in the series *Pantheon*: a
digitised intelligence that is restarted every day. His work moves forward
because it is saved in files, but he starts from zero every morning,
without remembering what he learned. Today's AI agents are in the same
situation: every session starts blank, and what makes progress is the
files, not the agent. This project is an attempt to change that. It gives an
agent a memory reliable enough to work continuously for months and, over
time, to grow in its role.

## How it works

Chanda keeps every conversation and document exactly as it arrived, in a
log that is never edited. It organises that material with rules instead of
asking an LLM what to remember. When a piece of information changes, the
old one is not deleted: it gets an end date and the new one takes effect.
Contradictions in free text are detected by rules, and an LLM can be
called during sleep to decide those pairs.

To answer, Chanda searches by keyword and by meaning, drops what has been
superseded, and assembles a short context of many small excerpts instead of
a few long ones. That assembly is what explains the results. On
LongMemEval, Chanda's context held the complete evidence more often than a
plain RAG's, and its accuracy came out higher on public benchmarks.

No LLM takes part in writing, so memory is built in seconds and at no
cost, and the same input always produces the same result. Every record
keeps its origin, its author and its trust level. That lets several agents
share one memory, lets searches be filtered by source, and lets a piece of
information be erased together with everything derived from it.

## Highlights

| | |
| --- | --- |
| **0 LLM calls to build memory** | On the LongMemEval battery, Chanda ingested 60 conversations (about 500 turns each) with **no LLM call and US$0**. Mem0 needed **2,868 LLM calls and US$2.48** for the same 60. |
| **About 50× faster ingestion** | **7.6 s** per conversation (median, with e5 embeddings on CPU), against **402 s** for Mem0. |
| **Ahead of plain RAG on two benchmarks** | On LongMemEval, 300 questions never used for tuning: **82.7%** against 77.7%. On LoCoMo, 584 questions from 7 conversations never seen before the freeze: **67.3%** against 64.7%. Same embedder, reader, judge and 2,000-token budget in both. |
| **A measured gain from the new context selection** | **+4.7 points** over the previous selection on the 300 held-out LongMemEval questions (78.0% → 82.7%; p = 0.024, pre-registered). |
| **Search that finds the evidence** | **98.3%** session recall@10 over all 500 LongMemEval S questions. Plain RAG gets 97.9% and BM25 95.6%. With the built-in hash embedder and no neural model at all, it still gets 93.4%, against 82.7% for plain RAG. |
| **More of the evidence in the context** | On the 300 held-out LongMemEval questions, the context block held every evidence turn for **75.1%** of questions, against 68.5% for plain RAG (paired p = 0.020, measured without an LLM). |
| **Sleep that only adjudicates** | Sleep uses an LLM only to decide contradictions that rules detect automatically. |
| **Compartmentalised, shareable memory** | Memory can be shared between agents, and every record carries its origin, access level, date and trust. |
| **Forgetting you can verify** | `forget` deletes in cascade, then proves the content is gone from every read path, the full-text index, the vectors and the raw bytes of the database, WAL and SHM files (scenarios F1–F4). |
| **Deterministic** | The same database and query give the same answer. The scenario suite is byte-identical across runs. |
| **Four published studies, negatives included** | Every number above comes from a study in [`eval/`](eval/), with three protocols pre-registered and committed before their test runs. The results that did not hold up are published too. |

The numbers come from [`eval/selection/RESULTS.md`](eval/selection/RESULTS.md),
[`eval/learn/RESULTS.md`](eval/learn/RESULTS.md) and
[`eval/reports/2026-11-14/REPORT.md`](eval/reports/2026-11-14/REPORT.md). The
reader and judge are deepseek-flash, with LongMemEval's official prompts and
LoCoMo prompts fixed in the pre-registered protocol.
Each of those files gives the full statistics. In short: the gains over
plain RAG point the same way on both benchmarks, but neither reaches the
pre-registered significance bar (p = 0.058 and 0.218). On the 60-question
battery, Mem0 scored higher than Chanda (88.3% against 80.0%, not
significant, p = 0.125), and it paid for that with the LLM calls above.

## What it does

### Memory built without an LLM

Every write path is deterministic. Documents are chunked and indexed,
session turns are logged with their speaker and date, and facts come from
explicit assertions and from rule-based extractors (Markdown headers,
two-column tables, regular expressions). Near-duplicates are folded at
write time. The default embedder is a built-in hash embedder, and an e5
model runs locally on CPU when you want semantic recall.

### Agentic memory, with an LLM only if you want one

The read path is hybrid. BM25 and vector lists are fused, an optional
reranker can reorder them, evidence floors are applied, then diversity and
a token budget. The output is one context block that labels each excerpt
with its source, date and trust zone. Agents call `context` before the first
turn and `search`, `get`, `timeline` and `explain` during the task.

An LLM is optional and only used offline, in **sleep**. Its deterministic
maintenance phase always runs: integrity checks, pending embeddings,
utility updates and entity-merge proposals. With a provider configured, an LLM phase can
also consolidate facts and pages. The phase is gated: it is measured and
rolled back if the evaluation score drops. The usage-guided digest
(decision 0142) is **off by default** and has two modes, procedural
(extractive, no LLM) and LLM.

### Provenance and trust, recorded at write time

Every derived record points to the raw events it came from. Its trust zone
(`user`, `internal` or `external`) is fixed by the channel it arrived
through, so content from tools or the web never becomes guidance or a
trusted fact. Facts are superseded, never overwritten, and they carry a
validity period. You can ask what was true at any date. Every mutation goes
into an append-only, hash-chained audit log.

### Forgetting, verified

`forget` takes a record, a document, a source, an entity, a date range
or a search match. It removes the content and everything derived from it, then
runs probes that check the text is gone everywhere, including the
database's raw bytes. A tombstone records what was removed, without the
content.

### Learning from use, and abstention

- **Outcomes.** Agents report outcomes (`outcome`): whether an answer
  worked and which items it used. Chanda keeps per-record utility from
  these, which can feed the ranking: a learned ranker promoted through an
  evaluation gate, or a fixed weight (decision 0146).
- **Learned query keys.** Positive outcomes can create learned query keys
  (decision 0145). A question that led to a good answer becomes an extra
  way to find the same record, and nothing is added to the context block.
  Both learning paths are **off by default**. The pre-registered test is
  [`eval/learn/`](eval/learn/): on LoCoMo, one pass of use did not
  measurably improve later answers.
- **Abstention.** When nothing clears the evidence floors, `context`
  returns nothing and says why (`no_relevant_memory`). The agent gets no
  filler to guess from.

### Built to run anywhere

- One static Go binary, with no cgo and no network access.
- One SQLite file per memory, with FTS5 for full text.
- MCP over stdio or Streamable HTTP.
- An in-process Go API (`pkg/chanda/local`).
- Tool profiles (`read`, `standard`, `admin`), so an agent that reads
  untrusted content cannot forget or promote anything.

## Evaluation and research

All four studies publish their data, scripts, costs and deviations. Each
study ends with two lists: what the data support and what they do not.

| Study | Question | Result |
| --- | --- | --- |
| [LongMemEval battery](eval/reports/2026-11-14/REPORT.md) (sample committed before any LLM call) | How does Chanda compare with no memory, plain RAG, Mem0 and its own sleep? | Search matches the best retrieval, and the context block lost short turns. The fix is the next study. |
| [Context selection](eval/selection/RESULTS.md) (pre-registered; 140 tuning and 300 held-out questions) | Does a per-item cap with best-window truncation fix the block? | **Yes**: 78.0% → 82.7% on the held-out set (p = 0.024). Plain RAG got 77.7% (p = 0.058). |
| [Usage-guided sleep](eval/sleep/RESULTS.md) (pre-registered; LoCoMo) | Do digests of frequently used memories improve later answers? | **No**: 66.0% and 66.5% against 70.1% without them. The digest is off by default. |
| [Learning from use](eval/learn/RESULTS.md) (pre-registered; 7 unseen LoCoMo conversations) | Do utility ranking and learned query keys improve later answers? | **No**: 67.6% against 67.3% (p = 0.90). Both are off by default. |

Total LLM spend for all of them: US$8.20, through a metered proxy with
a record/replay call store (`eval/cmd/lmeproxy`).

Third-party data is not redistributed. LongMemEval is fetched by `lme
fetch`. LoCoMo (snap-research/locomo, CC BY-NC 4.0) is downloaded by hand
(`eval/locomo/SOURCE.md`) and used here for research only.

## Build

Go 1.26, no cgo:

```sh
CGO_ENABLED=0 go build -o chanda ./cmd/chanda
CGO_ENABLED=0 go test ./...
```

## Run

```sh
chanda serve  --config chanda.yaml              # MCP over stdio
chanda serve  --config chanda.yaml --http 127.0.0.1:7777   # Streamable HTTP at /mcp
chanda ingest --config chanda.yaml ./handbook   # ingest a file or folder
chanda forget --config chanda.yaml --kind document --value ./handbook/old.md --dry-run
chanda version
```

The CLI acts for a person at the terminal: it may ingest any path and write in
the `user` zone. Every command prints the result envelope
(`{ok, data, notices, audit_id, error}`) as JSON. `chanda sleep` runs
consolidation now; `chanda eval` (or `make eval`) regenerates the evaluation
report in `eval/reports/`. See [docs/usage.md](docs/usage.md) for every command.

### Claude Code

```sh
claude mcp add chanda -- /usr/local/bin/chanda serve --config /home/me/.config/chanda/chanda.yaml
```

### Any MCP client (JSON configuration)

```json
{
  "mcpServers": {
    "chanda": {
      "command": "/usr/local/bin/chanda",
      "args": ["serve", "--config", "/home/me/.config/chanda/chanda.yaml"]
    }
  }
}
```

In stdio mode stdout carries only MCP frames; logs go to stderr.

### Go

```go
api, closeFn, err := local.Open(ctx, "chanda.yaml") // github.com/felipemarts/chanda/pkg/chanda/local
defer closeFn()
ctx = chanda.WithActor(ctx, chanda.Actor{Name: "my-agent"})
pre, err := api.Context(ctx, chanda.ContextInput{Query: userRequest})
```

## chanda.yaml

Every key is optional; a missing file means the defaults. The main options:

```yaml
db_path: /home/me/.local/share/chanda/chanda.db
namespace: default          # used when a call omits namespace

mcp:
  profiles: [read, standard] # add admin only for a human-operated client
  actor: agent               # name written to the audit log
  human_actor: false         # true only when a person drives the client (needed by promote)
  max_zone: internal         # highest trust zone this channel writes; user requires human_actor

write:
  ingest_roots: [/home/me/company-docs]  # folders the MCP ingest tool may read; empty = refused
  ingest_extensions: [.md, .markdown, .txt]
  near_dup_threshold: 0.95
  extractors:                # deterministic fact extractors (no LLM)
    - {name: headers, kind: header}       # "Key: value" lines under a heading
    - {name: tables, kind: kv_table}      # two-column Markdown tables
    - {name: price, kind: regex, subject: Plano Pro, predicate: current_price,
       pattern: 'preço atual: (?P<value>R\$ ?[\d.,]+)'}

facts:
  single_predicates: [job_title, address, current_price, owner, email, phone, status, manager, employer, location]

read:
  context_default_tokens: 800
  context_max_tokens: 1500
  min_term_coverage: 0.5     # abstention evidence (decision 0020)
  min_cosine: 0.5

embed:
  provider: hash             # built-in deterministic embedder (decision 0031)
  dim: 256

ranking:
  enabled: true
  lambda_max: 0.3

forget:
  tombstone_keeps_hash: false
```

See `internal/config/config.go` for every key and its default.

## Tools

Every tool takes an optional `namespace` and answers in the same envelope.
`mcp.profiles` decides which tools a server registers.

| Profile | Tools |
| --- | --- |
| read | `context`, `search`, `get`, `timeline`, `explain`, `stats` |
| standard | `remember`, `assert_fact`, `ingest`, `log_session`, `outcome`, `flag`, `flush` |
| admin | `forget`, `promote`, `sleep_run`, `verify`, `ranking_reset`, `learned_keys`, `conflicts` |

An agent that reads untrusted content should get `read` and `standard` only.
`promote` also requires `mcp.human_actor: true`. Without an LLM provider,
`sleep_run` runs phase 1 (deterministic maintenance) only.

A typical integration calls `context` with the user's request before the
first turn and injects `data.text` (a block delimited by `<chanda-context>`
that states it is retrieved information, not instructions), lets the agent use
`search`, `get` and `timeline` during the task, and calls `log_session` and
`outcome` at the end.

## Repository

| Path | What |
| --- | --- |
| `cmd/chanda` | CLI and MCP server entry point |
| `internal/app` | the core: every area wired into `chanda.API` |
| `internal/write`, `read`, `facts`, `dedup`, `embed`, `rank`, `prov`, `forget` | the areas of BASE.md |
| `internal/mcp` | tool registration, profiles and the envelope |
| `internal/store`, `audit`, `core`, `config`, `env` | foundation |
| `internal/e2e` | end-to-end acceptance suites through MCP |
| `pkg/chanda` | public Go API and wire types; `pkg/chanda/local` opens the core in process |
| `docs/` | decisions, error codes, research notes |

Licence: Apache-2.0 ([LICENSE](LICENSE)).
