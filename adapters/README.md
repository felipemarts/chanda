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

## What the end-to-end test checks

`e2e_test.sh` builds `chanda`, ingests a small document, and picks the mode
from the answer: `wired` when ingest succeeds (every build since the core was
wired), `unwired` when it answers `unavailable` (a shell without the core).

| Check | Wired core | Unwired core |
| --- | --- | --- |
| `context` is the first call, before the agent's first turn (client transcript) | checked | checked |
| The server runs the read and standard profiles only (its startup log) | checked | checked |
| The block is injected at the start of the first prompt (BASE.md phase 5), as one delimited `<chanda-context>` block followed by the request, and carries the seeded fact | checked | n/a |
| `log_session` then `outcome` succeed, in order | checked | n/a |
| Go example: `context` first, block injected, `log_session` then `outcome` | checked | unavailable path |
| The server's stderr shows the call order context, search, get, log_session | n/a (successful calls are not logged) | checked |

Last run against the wired core (`sh adapters/e2e_test.sh`, Python 3.11 with
`mcp==2.3.0`): every check passed. The unit tests against the stub server
cover the paths the e2e does not (used ids, Hermes outcome values, session
switches).
