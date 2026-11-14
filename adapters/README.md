# Agent adapters

Agents rarely ask for memory on their own (BASE.md, "Why MCP alone is not
enough"), so the environment that runs the agent delivers it at three
moments: `context` before the first turn, `search`/`get`/`timeline` during
the task, `log_session` and `outcome` on finishing.

| Path | What |
| --- | --- |
| `generic/` | Python package `chanda_adapter` (`ChandaClient`, `run_task`) for any agent, and a Go example in `generic/go/` (go-sdk client) |
| `hermes/` | Hermes Agent memory provider plugin `chanda_hermes` |
| `tests/` | Python unit tests for both, with a stub MCP server (`tests/fake_chanda.py`) |
| `e2e_test.sh`, `e2e_agent.py` | End-to-end test against a real `chanda serve` |

Both adapters run `chanda serve --profiles read,standard`: never the admin
tools. Every call is sent once; if Chanda is unavailable the agent still runs
without a pre-context. Decisions: `docs/decisions/0095` to `0097`.

## Running the tests

```sh
python3.11 -m venv .venv && .venv/bin/pip install "mcp==2.3.0" pytest
.venv/bin/python -m pytest adapters/tests      # or: .venv/bin/python -m unittest discover -s adapters/tests
go test ./adapters/...                          # Go example, in-memory against internal/mcp
CHANDA_E2E_PYTHON=.venv/bin/python sh adapters/e2e_test.sh
HERMES_SRC=/path/to/hermes-agent .venv/bin/python -m pytest adapters/tests/test_hermes_provider.py   # + real MemoryProvider ABC
```

## What the end-to-end test checks, and what needs the wired core

`e2e_test.sh` builds `chanda`, tries to ingest a small document, and picks the
mode from the answer: `wired` when ingest succeeds, `unwired` when it answers
`unavailable` (the state of `chanda serve` until integration wires the core).

| Check | Unwired core (today) | Wired core |
| --- | --- | --- |
| `context` is the first call, before the agent's first turn (client transcript) | checked | checked |
| The server runs the read and standard profiles only (its startup log) | checked | checked |
| The server's stderr shows `context` before the first turn and the call order context, search, get, log_session | checked (the server logs every failed call) | not available: successful calls are not logged; the client transcript is used |
| `context` answers `unavailable`; the agent runs with the bare request; no outcome is sent | checked | n/a |
| The block is injected at the start of the first prompt and carries the seeded fact | **needs the wired core** | checked |
| `log_session` then `outcome` succeed, in order | **needs the wired core** | checked |
| Go example: same moments | checked (unavailable path) | **needs the wired core** for injection and outcome |

The positive paths (injection, used ids, outcome per retrieval, Hermes outcome
values, session switches) are covered today by the unit tests against the
stub server, including one run where the stub is spawned over stdio as a
stand-in `chanda` binary. Integration should re-run `sh adapters/e2e_test.sh`
once the core is wired: it switches to the wired checks by itself.
