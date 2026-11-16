# Using Chanda

Chanda is a memory server for agents: one Go binary, one SQLite file, no
cgo, no network at read or write time. This guide goes from a clean clone to
a configured server, then through the day-to-day commands.

## 1. Install

Requirements: Go 1.26 (see `go.mod`) and `git`. Nothing else.

```sh
git clone https://github.com/felipemarts/chanda-mcp && cd chanda-mcp
make build            # bin/chanda, version stamped from git describe
make test             # go test ./...
bin/chanda version
```

Without `make`: `CGO_ENABLED=0 go build -o bin/chanda ./cmd/chanda`.

`make release` cross-compiles static binaries for Linux, macOS and Windows
(amd64 and arm64) into `dist/`, with a `SHA256SUMS` file.

### With the multilingual embedder (hugot)

The default embedder (`hash`) is deterministic and built in, but captures
words and character trigrams, not meaning. For semantic search build
`chanda-hugot`, a separate module (Go 1.27, fetched automatically) that is
the full chanda CLI with the `hugot` provider registered:

```sh
make hugot                                                    # bin/chanda-hugot
bin/chanda-hugot models pull e5-small ~/.local/share/chanda/models/e5-small   # the only network step
bin/chanda-hugot models pull mmarco-minilm-l12 ~/.local/share/chanda/models/mmarco   # optional reranker
bin/chanda-hugot models verify ~/.local/share/chanda/models/e5-small
```

Then use `bin/chanda-hugot` wherever this guide says `chanda`, with the
`embed` section below. See `embedders/hugot/README.md`.

## 2. Configure

Every key is optional; a missing file means the defaults
(`internal/config/config.go` lists every key). A typical `chanda.yaml`:

```yaml
db_path: /home/me/.local/share/chanda/chanda.db
namespace: default

mcp:
  profiles: [read, standard]   # add admin only for a client a person drives
  actor: agent
  max_zone: internal           # highest trust zone written through MCP

write:
  ingest_roots: [/home/me/company-docs]   # folders the MCP ingest tool may read
  extractors:                             # deterministic facts, no LLM
    - {name: header, kind: header}        # "Key: value" lines under a heading
    - {name: tables, kind: kv_table}      # two-column Markdown tables

facts:
  single_predicates: [job_title, address, current_price, owner, email, phone, status, manager]

read:
  context_default_tokens: 800
  # Context selection knobs (decision 0137); the defaults keep the block as
  # it always was and are under study (eval/selection/PROTOCOL.md):
  # context_item_max_tokens: 0      # cap per item block, label included; 0 = off
  # context_item_cap_after: 0        # the first N (best-ranked) items are exempt from the cap
  # context_truncate: head          # head or best_window (sentences that best match the query)
  # context_max_per_document: 0     # 0 = read.max_per_document; -1 = no limit
  # context_max_per_session: 0      # raw excerpts per session (log_session turns); 0 = off
  # context_show_role: false        # "role: user" in the label of a session turn

embed:
  provider: hash                # or hugot (chanda-hugot only):
  # provider: hugot
  # model_path: /home/me/.local/share/chanda/models/e5-small
  # dim: 384
  # reranker_model_path: /home/me/.local/share/chanda/models/mmarco

llm:                            # optional; without it sleep runs phase 1 only
  # endpoint: http://localhost:11434/v1
  # model: qwen2.5:7b

sleep:
  # mode: uniform                 # or guided (decision 0142): phase 2 digests only what retrieval keeps returning
  # guided:
  #   digest: extractive          # or llm (needs llm.endpoint); extractive needs no LLM
  #   top_k: 10                   # best-ranked event candidates of a retrieval that count as returned
  #   max_groups: 0               # groups digested per run; 0 = no cap
```

Thresholds that compare cosines (`write.near_dup_threshold`,
`read.min_cosine`, `sleep.merge_similarity`) depend on the embedding model;
recalibrate them when you switch from `hash` to a hugot model.

## 3. Serve

### Claude Code

```sh
claude mcp add chanda -- /path/to/bin/chanda serve --config /home/me/.config/chanda/chanda.yaml
```

### Any MCP client (JSON)

```json
{
  "mcpServers": {
    "chanda": {
      "command": "/path/to/bin/chanda",
      "args": ["serve", "--config", "/home/me/.config/chanda/chanda.yaml"]
    }
  }
}
```

Over stdio, stdout carries only MCP frames and logs go to stderr.
`chanda serve --http 127.0.0.1:7777` serves Streamable HTTP at `/mcp`.
If the environment variable named by `mcp.http_token_env` (default
`CHANDA_MCP_TOKEN`) is set, every request needs `Authorization: Bearer
<token>`; without a token the server refuses to listen on a non-loopback
address.

An agent should call `context` with the user's request before its first
turn, inject the returned block as is, and end the task with `log_session`
and `outcome`. `adapters/` has a generic Python adapter, a Go example and a
Hermes plugin that do exactly that (`sh adapters/e2e_test.sh` checks it
against a real server).

## 4. Ingest

```sh
chanda ingest --config chanda.yaml /home/me/company-docs          # a folder (.md, .markdown, .txt)
chanda ingest --config chanda.yaml --kind web --uri https://example.com/page page.md
```

Re-ingesting a changed file creates a new version: unchanged paragraphs are
kept, removed ones are retired and the facts derived from them leave the
current state. The CLI acts for a person: it may ingest any path and write
in the `user` zone. The source kind fixes the trust zone (`document` and
`session` are internal, `tool` and `web` external).

## 5. Forget

```sh
chanda forget --config chanda.yaml --kind document --value /home/me/company-docs/old.md --dry-run
chanda forget --config chanda.yaml --kind document --value /home/me/company-docs/old.md
chanda forget --config chanda.yaml --kind entity --value "Ana Ribeiro"
chanda forget --config chanda.yaml --kind search --query "projeto Nightjar"
chanda forget --config chanda.yaml --kind date_range --from 2026-03-01 --to 2026-03-02
```

`--dry-run` lists what would go. A real run deletes in cascade, scrubs the
file and reports its absence probes (`verification.passed`); a failed probe
is reported as data, so check it.

## 6. Sleep, the gate set and learned ranking

```sh
chanda sleep --config chanda.yaml                 # phase 1 (and 2 with an llm.endpoint)
chanda gate list --config chanda.yaml --status proposed
chanda gate approve --config chanda.yaml gq_...   # or: gate retire gq_...
chanda gate add --config chanda.yaml --question "Qual o prazo de reembolso?" --answer "45 dias"
chanda ranking off --config chanda.yaml           # or: ranking on
```

Sleep consolidates memory and proposes gate questions; only a person
approves them. A learned ranker is promoted only when it does not lose on
the approved gate set, so with no approved question nothing is ever
promoted. `serve` also runs sleep on its own schedule (`sleep.scheduler`,
on by default; idle time, volume and interval triggers).

## 7. Evaluate

```sh
make eval                     # or: sh scripts/reproduce.sh
bin/chanda eval -h            # flags: -dataset -systems -budget -config -date -out -name
bin/chanda eval -systems B1,C1,C3 -name quick -out /tmp/reports
```

`make eval` writes `eval/reports/latest.md`, `.json` and
`.questions.jsonl`: baselines B0..B3 and Chanda configurations C1..C5 on the
synthetic Portuguese set, at the same 800-token budget and with the same
embedder, plus the eight own suites of BASE.md. Configurations that cannot
run (C2 without a reranker) are listed with the reason. With chanda-hugot
and `embed.reranker_model_path` set, `bin/chanda-hugot eval -config
chanda.yaml` compares everything on the hugot model and runs C2.
See `docs/decisions/0085-evaluation-harness.md` and
`docs/decisions/0086-evaluation-systems-and-suites.md`; how other memory
systems would be compared is in `eval/compare/README.md`.

`make dataset` regenerates `eval/data/synthetic-pt` from its seed; a test
fails if the committed files differ from the generator output.
