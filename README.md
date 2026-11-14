# Chanda

Chanda is a memory server for agents, spoken over MCP. It stores what an agent
learns and what a company documents (documents, session transcripts, facts
with a validity period) in one SQLite file, and returns the right excerpt at
the right moment: a short, delimited pre-context before the first turn, and
ranked search during the task. It is a single Go binary that runs on CPU,
without cgo and without network access; any MCP client can use it.

Reads and writes are deterministic and never call an LLM: writing costs a
hash, an embedding and rules, and reading uses full-text search, vectors,
rules and a small learned ranking. Every derived record points to the raw
events it came from, and its trust zone (`user`, `internal`, `external`) is
fixed by the input channel, so content from tools or the web never becomes
guidance or a trusted fact. Facts are superseded, never overwritten;
`forget` deletes in cascade and proves the content is gone. The full
specification is in [BASE.md](BASE.md); decisions that refine it are in
[docs/decisions/](docs/decisions/), error codes in [docs/errors.md](docs/errors.md).

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
api, closeFn, err := local.Open(ctx, "chanda.yaml") // github.com/felipemarts/chanda-mcp/pkg/chanda/local
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
| admin | `forget`, `promote`, `sleep_run`, `verify`, `ranking_reset` |

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
