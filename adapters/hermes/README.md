# Chanda memory provider for Hermes Agent

A Hermes Agent [memory provider plugin](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/developer-guide/memory-provider-plugin.md)
(checked against Hermes v0.23.1, `agent/memory_provider.py`) that delivers
Chanda memory at the three moments of BASE.md, from the environment rather
than the model:

| Moment | Hermes surface | Chanda call |
| --- | --- | --- |
| Before the first turn | `prefetch()` on the first prefetched turn of a session | `context` with the user's message; Hermes appends `data.text` to that turn |
| During the task | provider tools `chanda_search`, `chanda_get`, `chanda_timeline` | `search`, `get`, `timeline` (routed through the provider, so opened ids are known) |
| On finishing | `on_session_end(messages)` | `log_session` with the user/assistant turns, then `outcome` |

The provider spawns `chanda serve --profiles read,standard` over stdio when
Hermes initializes it (one process per agent, gone with Hermes); the admin
tools are never exposed. Only one external memory provider can be active in
Hermes at a time.

## Install

1. Install the `chanda` binary and check it: `chanda version`.

   ```sh
   CGO_ENABLED=0 go install github.com/felipemarts/chanda/cmd/chanda@latest
   # or, from a clone: go build -o ~/.local/bin/chanda ./cmd/chanda
   ```

2. Install the plugin, either way:

   - **Directory plugin** (recommended by Hermes for user installs): copy the
     package directory as `chanda` into the profile's plugins folder.

     ```sh
     cp -R adapters/hermes/chanda_hermes "$HERMES_HOME/plugins/chanda"   # HERMES_HOME defaults to ~/.hermes
     ```

     `plugin.yaml` declares the Python dependency (`mcp>=2.3.0,<3`; Hermes
     already pins `mcp==2.3.0`), which Hermes prepares on activation.

   - **Package** (owner-managed Hermes environments): install into the Python
     environment Hermes runs in; the entry point is
     `[project.entry-points."hermes_agent.memory_providers"] chanda = "chanda_hermes:register"`.

     ```sh
     pip install ./adapters/hermes
     ```

3. Activate it: `hermes config set memory.provider chanda` (or
   `hermes memory setup`, which prompts for `command`, `config` and
   `namespace`). Restart Hermes.

## Configuration

`memory.chanda` in the profile's `config.yaml`; the environment variables in
the second column override it.

| Key | Env | Default | Meaning |
| --- | --- | --- | --- |
| `command` | `CHANDA_BIN` | `chanda` | binary spawned as `chanda serve --profiles read,standard` |
| `config` | `CHANDA_CONFIG` | empty | `chanda.yaml` passed with `--config` (empty: `./chanda.yaml` or defaults) |
| `url` | `CHANDA_URL` | empty | connect to a running `chanda serve --http ... --profiles read,standard` at this URL (e.g. `http://127.0.0.1:8765/mcp`) instead of spawning |
| `namespace` | `CHANDA_NAMESPACE` | empty | memory namespace; empty uses the one in `chanda.yaml` |
| `budget_tokens` | `CHANDA_BUDGET_TOKENS` | `0` | `context` budget; 0 uses Chanda's default (800, ceiling 1500) |
| `context_timeout` | | `7` | seconds for `context` (Hermes stops waiting for prefetch at 8 s) |
| `call_timeout` | | `20` | seconds for every other call |
| `connect_timeout` | | `15` | seconds to spawn the server and finish the MCP handshake |
| `outcome_completed` | | `1.0` | outcome value when the last turn completed |
| `outcome_failed` | | `-1.0` | when the last turn failed |
| `outcome_interrupted` | | `-0.5` | when the user interrupted the last turn |
| `outcome_unknown` | | `skip` | when no turn status was seen; `skip` sends no outcome |

Outcome values must be in [-1, 1] or `skip`; a malformed value makes the
provider unavailable with the reason, rather than falling back silently.

```yaml
memory:
  provider: chanda
  chanda:
    command: /usr/local/bin/chanda
    config: /home/me/.config/chanda/chanda.yaml
    namespace: work
```

The spawned server's stderr goes to `$HERMES_HOME/chanda/serve.log`.

## Behaviour

- **First turn only.** Hermes calls `prefetch` every turn; the provider calls
  `context` once per session, on the first prefetched turn, and returns ""
  afterwards. Hermes skips prefetch for trivial prompts ("hi", "ok", `/cmd`),
  so the block lands on the first turn that carries a request. A resumed
  session (`resumed=True`, `/resume`, `/branch`) gets no pre-context; `/new`
  and `/reset` start over. A failed `context` is logged and not retried.
- **Outcome value policy.** `on_session_end` carries no success flag, so the
  plugin also registers the general `on_session_end` hook, which Hermes fires
  at the end of every turn with `completed`/`failed`/`interrupted`; the status
  of the session's last turn picks the value from the table above. The
  `context` retrieval always gets an outcome (when a value applies); a
  `chanda_search` retrieval only when one of its records was used.
- **used_ids.** A delivered record is used when the agent opened it with
  `chanda_get` or its id appears in an assistant message. Ids are always a
  subset of what the retrieval delivered.
- **What is logged.** User and assistant messages with text; the system
  prompt, tool calls and tool results are left out, and the injected block is
  removed from user messages.
- **Subagents, cron, flush.** Non-primary contexts (`agent_context` other than
  `primary`) may read (pre-context, tools) but never call `log_session` or
  `outcome`.
- **Chanda unavailable.** If the binary is missing, the server does not start
  or the core is not wired, Hermes keeps running: a warning is logged, nothing
  is injected, and the tools answer an envelope with `ok: false` and the code
  (`unavailable` or `transport`). Nothing is retried.

Policies and their reasons: `docs/decisions/0095-agent-adapters.md`,
`0096-adapter-outcome-and-used-ids.md`, `0097-hermes-adapter.md`.

## Development

`chanda_hermes/_client.py` is a byte-identical copy of
`adapters/generic/chanda_adapter/client.py` (a directory plugin must be
self-contained); a test fails when they differ. After editing the generic
client:

```sh
cp adapters/generic/chanda_adapter/client.py adapters/hermes/chanda_hermes/_client.py
```

Tests (`adapters/tests/test_hermes_provider.py`) run with a stub server and do
not need Hermes. With a Hermes checkout, `HERMES_SRC=/path/to/hermes-agent`
also checks the provider against the real `MemoryProvider` ABC.
