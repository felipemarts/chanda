# Generic Chanda adapter

Agents rarely ask for memory on their own, so the runtime that runs the agent
has to deliver it (BASE.md, "Why MCP alone is not enough"). This adapter does
that for any agent, at three moments:

1. **Before the first turn**: call `context` with the user's request and put
   the returned `data.text` block in front of the first prompt.
2. **During the task**: give the agent `search`, `get` and `timeline` as the
   function tools `chanda_search`, `chanda_get`, `chanda_timeline`.
3. **On finishing**: call `log_session` with the turns and `outcome` with the
   retrieval id, the task value in [-1, 1] and the ids the agent used.

Calls 1 and 3 come from the adapter, never from the model. Every call is sent
once (no retries). When Chanda is unavailable (binary missing, server down,
core not wired) the agent still runs: a warning is logged and nothing is
injected.

## Python

Python 3.11+, one dependency: the official `mcp` SDK (`mcp>=2.3.0,<3`, the
range that includes the `mcp==2.3.0` Hermes Agent pins).

```sh
python3.11 -m venv .venv && . .venv/bin/activate
pip install ./adapters/generic
```

```python
from chanda_adapter import AgentResult, ChandaClient, run_task

def agent(prompt, tools):
    # prompt = pre-context block + "\n\n" + request (or just the request).
    # Give your model tools.schemas() (OpenAI function schemas) and route its
    # calls through tools.call(name, args), which returns the envelope as JSON.
    answer = my_model(prompt, tools=tools.schemas(), on_tool=tools.call)
    return AgentResult(turns=[("user", prompt), ("assistant", answer)], value=1.0)

with ChandaClient(config="chanda.yaml") as client:   # spawns: chanda serve --config chanda.yaml --profiles read,standard
    report = run_task(agent, "When is the Orion deadline?", session_id="s-42", client=client)
```

- `ChandaClient(command="chanda", config=None, profiles=("read","standard"), url=None, namespace=None, ...)`
  spawns `chanda serve` over stdio, or connects to `chanda serve --http` with
  `url="http://127.0.0.1:8765/mcp"` (that server's profiles are its own setting:
  start it with `--profiles read,standard`). Typed helpers: `context`, `search`,
  `get`, `timeline`, `log_session`, `outcome`; each returns `data` and raises
  `ChandaError` (`.code` is the stable code from `docs/errors.md`, or
  `transport` when no envelope came back: spawn failure, timeout, a tool outside
  the profiles). `call_envelope(tool, args)` returns the raw envelope.
- `run_task(agent_fn, request, *, session_id, client=None, budget_tokens=None, as_of=None)`
  returns a `TaskReport` (prompt, whether a block was injected, the context
  error code, the outcome calls made).
- The transcript logged is the user and assistant turns, with the injected
  block removed, so memory never re-ingests its own retrieval.
- `used_ids`: a delivered record counts as used when the agent opened it with
  `chanda_get` or its id appears in an assistant turn. One `outcome` goes to the
  `context` retrieval (always, even with no used ids); one more to each
  `search` retrieval whose records were used. See
  `docs/decisions/0096-adapter-outcome-and-used-ids.md`.

## Go

`adapters/generic/go` is a runnable example with the go-sdk client:

```sh
go build -o chanda ./cmd/chanda
go run ./adapters/generic/go --chanda ./chanda --request "When is the Orion deadline?" --value 1
```

It prints a JSON report (`prompt`, `injected`, `context_error`, `calls`). Its
`RunTask` reports one outcome, for the `context` retrieval.

## Tests

```sh
python3.11 -m pytest adapters/tests     # or: python3.11 -m unittest discover -s adapters/tests
go test ./adapters/...
sh adapters/e2e_test.sh                 # builds chanda and runs both adapters against it
```

The Python tests use an in-process stub server (`adapters/tests/fake_chanda.py`)
with canned envelopes; one test spawns the stub over stdio as a stand-in
`chanda` binary.
