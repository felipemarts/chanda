# 0097 Hermes adapter: provider, packaging and outcome values

BASE.md defers the Hermes extension interface to research
(`docs/research/hermes.md`, Hermes Agent v0.23.1). This records what the
reference adapter in `adapters/hermes/` does.

- **A MemoryProvider plugin.** It is the only Hermes surface with all three
  moments: `prefetch` (moment 1), `get_tool_schemas`/`handle_tool_call`
  (moment 2), `on_session_end(messages)` with the transcript (moment 3).
  Bridging the tools through the provider, instead of Hermes' own
  `mcp_servers:` client, is what lets the adapter see which ids were opened.
  Cost: Hermes allows one external provider at a time.
- **First turn via prefetch, once.** `context` is called on the first
  prefetched turn of a session and never again, success or failure (no
  retries). Hermes skips prefetch for trivial prompts and gives it 8 s, so the
  block lands on the first turn with a request; `context_timeout` defaults to
  7 s. Resumed sessions get no pre-context; `on_session_switch(reset=True)`
  (`/new`, `/reset`) starts a new one.
- **Outcome value.** `on_session_end` has no success flag. The plugin also
  registers the general `on_session_end` hook (fired per turn with
  `completed`, `failed`, `interrupted`) and maps the last turn's status:
  completed 1.0, failed -1.0, interrupted -0.5, unknown: no outcome. A
  completed turn only means the agent answered, but Chanda centres outcomes on
  the namespace's moving average (BASE.md stage 1), so a constant optimistic
  value carries no false credit; failures and interruptions are what move
  utility. The four values are configurable (`memory.chanda.outcome_*`, or
  `skip`).
- **Writes only from the primary agent.** `agent_context` other than
  `primary` (subagent, cron, flush) reads but never sends `log_session` or
  `outcome`.
- **Packaging.** A directory plugin must be self-contained and Hermes resolves
  its Python dependencies from PyPI, so `chanda_hermes/_client.py` is a
  byte-identical copy of the generic client, enforced by a test, rather than a
  dependency on an unpublished package. The same directory is also a pip
  package with the `hermes_agent.memory_providers` entry point.
- **Configuration** lives in `memory.chanda` of Hermes' `config.yaml` (what
  `hermes memory setup` writes when a provider does not override
  `save_config`), with `CHANDA_*` environment overrides. The spawned server's
  stderr goes to `$HERMES_HOME/chanda/serve.log`, never to the terminal.
