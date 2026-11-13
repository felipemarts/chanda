# Research: Hermes Agent integration (and a generic adapter)

Source inspected: `NousResearch/hermes-agent`, `main` at commit `bf0073dadb470c8e5f28b19ca17f8bba40411724`
(2026-11-13); latest release `v0.23.1` (2026-11-10). Python (`requires-python >=3.11,<3.15`), MIT.
Cloned shallow; every path below is relative to the repo root. Docs live in `website/docs/`.

## 1. What Hermes offers

| Mechanism | Where | Fits Chanda? |
| --- | --- | --- |
| **Memory provider plugin** (`MemoryProvider` ABC) | `agent/memory_provider.py`, `agent/memory_manager.py`, `plugins/memory/__init__.py`, guide: `website/docs/developer-guide/memory-provider-plugin.md` | Yes, best fit |
| General plugin hooks (`ctx.register_hook`) | `hermes_cli/plugins.py::VALID_HOOKS`, `website/docs/user-guide/features/hooks.md` | Partly: `pre_llm_call` (with `is_first_turn`) fits moment 1, but `on_session_end` carries no messages |
| Native MCP client (`mcp_servers:` in `config.yaml`) | `tools/mcp_tool*.py`, `website/docs/user-guide/features/mcp.md`, `.../reference/mcp-config-reference.md` | Moment 2 only |
| Gateway event hooks (`~/.hermes/hooks/*/HOOK.yaml`) | `hooks.md` | Messaging gateway only, not needed |

### 1.1 MemoryProvider (the extension point)

- Lifecycle (`MemoryManager`): `initialize` -> per turn `prefetch` / `sync_turn` -> tools -> `on_session_end` ->
  `shutdown`. Abstract: `name` (property), `is_available()`, `initialize(session_id, **kwargs)`, `get_tool_schemas()`.
- `prefetch(self, query: str, *, session_id: str = "") -> str`: once per user turn, before the tool loop
  (`agent/turn_context.py::_memory_turn_start_and_prefetch`). The text is appended to that turn's user message (API
  copy only), not the system prompt. Limits: 8 s timeout (`_EXTERNAL_PREFETCH_TIMEOUT_S`, `agent/memory_manager.py`),
  exceptions swallowed, skipped for trivial prompts (`is_trivial_prompt`: empty, `/cmd`, "hi", "ok"). On success Hermes
  shows a recall line built from `recall_status()`. It runs every turn, so "first turn only" is the provider's job;
  `initialize` receives `resumed`/`message_count` (read with `kwargs.get`, older Hermes omits them).
- `get_tool_schemas() -> list[dict]` (OpenAI function schema) + `handle_tool_call(tool_name, args, **kwargs) -> str`
  (JSON string) expose provider tools to the model.
- `on_session_end(self, messages: list[dict])`: OpenAI-style messages with assistant `tool_calls` and `tool` results.
  Fired by `AIAgent.shutdown_memory_provider` (idempotent) and on session rotation (`/new`, compression), then
  `shutdown()`. No success flag is passed.
- `initialize` kwargs: `hermes_home` (profile storage, never hardcode `~/.hermes`), `platform`, `agent_context`
  (`primary|subagent|cron|flush`: skip writes when not primary), `agent_identity`, `cwd`. Background work must use
  `agent.memory_provider.spawn_context_thread`.
- Registration: module `register(ctx)` calls `ctx.register_memory_provider(new_instance)`. The collector context
  (`plugins/memory/__init__.py::_ProviderCollector`) also forwards `register_hook`, `register_tool`, `register_skill`.
- Only ONE external provider is active at a time (built-in MEMORY.md/USER.md stays on).
- Packaging: no new in-tree providers (`CONTRIBUTING.md`, "Ship as a Standalone Plugin"). Install as a directory
  `$HERMES_HOME/plugins/chanda/` (`__init__.py`, `plugin.yaml`), `./.hermes/plugins/chanda/` (needs
  `HERMES_ENABLE_PROJECT_PLUGINS=1`), or a pip entry point
  `[project.entry-points."hermes_agent.memory_providers"] chanda = "chanda_hermes:register"`.
  Activate: `hermes config set memory.provider chanda` or `hermes memory setup`. `plugin.yaml` fields: `name`,
  `version`, `description`, `requires_env`, `external_dependencies` (`name/install/check`), `python_dependencies`.
  `mcp==2.3.0` is already a Hermes dependency. Structural references: `plugins/memory/byterover/` (wraps a CLI).

### 1.2 Plugin hooks (general plugin system)

`VALID_HOOKS` (`hermes_cli/plugins.py`) include `pre_llm_call`, `post_llm_call`, `on_session_start`,
`on_session_end`, `on_session_finalize`, `pre_tool_call`, `post_tool_call`; callbacks take `**kwargs`, 30 s timeout.
- `pre_llm_call(session_id, user_message, conversation_history, is_first_turn, model, platform, ...)`: a returned
  string or `{"context": ...}` is injected into the user message. No trivial-prompt gate.
- `on_session_end` fires at EVERY turn finalization with `completed`, `failed`, `interrupted`, no message body.
  `on_session_finalize` (teardown) has no messages either. A hooks-only plugin must buffer history from
  `post_llm_call`.

### 1.3 MCP client config (`~/.hermes/config.yaml`)

```yaml
mcp_servers:
  chanda:
    command: chanda
    args: ["serve", "--config", "/path/chanda.yaml"]   # stdio; or url: http://127.0.0.1:8765/mcp
    tools: { include: [search, get, timeline] }
```
Tools register as `mcp_<server>_<tool>`; `${ENV}` substitution works; `/reload-mcp` re-reads. Hermes only discovers
them and never calls them by itself: the "agents don't ask" problem from BASE.md.

## 2. Recommendation

**One standalone `MemoryProvider` plugin (shipped from `adapters/hermes/`), talking to `chanda serve` over stdio MCP
with the `mcp` Python SDK** (`ClientSession`/`stdio_client`, as Hermes does in `tools/mcp_tool_transport.py`).
1. It is the only surface giving all three moments in one object: `prefetch`, `get_tool_schemas`/`handle_tool_call`,
   `on_session_end(messages)` (full transcript for `log_session`).
2. Bridging `search/get/timeline` through the provider (not `mcp_servers:`) shows which ids the agent opened, so
   `outcome.used_ids` is filled deterministically (BASE.md open item), with one `chanda` process.
3. Stdio needs no port or auth and dies with Hermes. Use HTTP only to share one running Chanda between agents.

Trade-offs:
- One external provider at a time (conflicts with Honcho etc.). Fallback "Option B": `mcp_servers.chanda` plus a
  hooks-only plugin (`pre_llm_call`, `post_llm_call` buffer, `on_session_finalize`); `used_ids` less exact.
- `prefetch` can be skipped (trivial prompt, 8 s, exception). For strict determinism move step 1 to `pre_llm_call`
  (`is_first_turn`); the skeleton keeps `prefetch` for the recall indicator.
- Assumed, not yet in BASE.md: `context` returns `data.text` and `data.retrieval_id`.

## 3. Shared stdio client (used by both adapters)

```python
# chanda_client.py: sync facade over the async MCP SDK (provider/hook methods are sync, called from threads)
import asyncio, json, threading
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

class ChandaClient:
    def __init__(self, command="chanda", args=("serve",), namespace="default", env=None):
        self.params = StdioServerParameters(command=command, args=list(args), env=env)
        self.ns, self.loop, self.ready, self.err = namespace, asyncio.new_event_loop(), threading.Event(), None

    def start(self, timeout=30):
        threading.Thread(target=self.loop.run_until_complete, args=(self._main(),), daemon=True).start()
        if not self.ready.wait(timeout) or self.err:
            raise RuntimeError(f"chanda not ready: {self.err}")

    async def _main(self):
        self.stop = asyncio.Event()
        try:
            async with stdio_client(self.params) as (r, w), ClientSession(r, w) as self.s:
                await self.s.initialize()
                self.ready.set()
                await self.stop.wait()
        except Exception as e:
            self.err = e
            self.ready.set()

    def call(self, tool, timeout=20, **args) -> dict:
        res = asyncio.run_coroutine_threadsafe(
            self.s.call_tool(tool, {"namespace": self.ns, **args}), self.loop).result(timeout)
        env = res.structuredContent or json.loads(res.content[0].text)
        if not env.get("ok"):
            raise RuntimeError(f"chanda {tool}: {env}")
        return env["data"]

    def close(self):
        self.loop.call_soon_threadsafe(self.stop.set)
```

## 4. Hermes adapter skeleton (`adapters/hermes/chanda/__init__.py`)

`plugin.yaml`: `name: chanda`, `version: 0.1.0`, `external_dependencies: [{name: chanda, check: "chanda --version"}]`,
`python_dependencies: [mcp]`.

```python
import json, os, shutil
from agent.memory_provider import MemoryProvider
from .chanda_client import ChandaClient

def _schema(name, desc, req, **props):
    return {"name": f"chanda_{name}", "description": desc, "parameters": {
        "type": "object", "required": req, "properties": {k: {"type": t} for k, t in props.items()}}}

TOOLS = [
    _schema("search", "Search long-term memory (ranked list).", ["query"], query="string", limit="integer"),
    _schema("get", "Fetch one memory record by id, with origins.", ["id"], id="string"),
    _schema("timeline", "History of a subject/attribute.", ["subject"], subject="string", predicate="string"),
]

class ChandaProvider(MemoryProvider):
    @property
    def name(self): return "chanda"

    def is_available(self): return bool(shutil.which(os.environ.get("CHANDA_BIN", "chanda")))

    def initialize(self, session_id, **kw):
        self.sid = session_id
        self.passive = kw.get("agent_context", "primary") != "primary"   # subagent/cron/flush: no writes
        self.first = not kw.get("resumed", False)
        self.retrieval_id, self.used, self.last_ok = None, set(), None
        self.c = ChandaClient(os.environ.get("CHANDA_BIN", "chanda"),
                              ("serve", *filter(None, [os.environ.get("CHANDA_ARGS")])),
                              os.environ.get("CHANDA_NAMESPACE", "default"))
        self.c.start()

    # moment 1: before the first turn
    def prefetch(self, query, *, session_id=""):
        if not self.first or self.passive:
            return ""
        self.first = False
        d = self.c.call("context", query=query, budget_tokens=int(os.environ.get("CHANDA_BUDGET", "1500")), timeout=7)
        self.retrieval_id = d.get("retrieval_id")
        return f"<chanda-memory>\n{d['text']}\n</chanda-memory>" if d.get("text") else ""

    # moment 2: during the task
    def get_tool_schemas(self): return TOOLS

    def handle_tool_call(self, tool_name, args, **kw):
        tool = tool_name.removeprefix("chanda_")
        d = self.c.call(tool, **args)
        if tool == "get":
            self.used.add(args["id"])              # opened record counts as used
        return json.dumps(d)

    # hook state: the per-turn on_session_end HOOK carries completed/failed; the provider method does not
    def note_turn(self, completed=False, failed=False, interrupted=False, **_):
        self.last_ok = completed and not failed and not interrupted

    # moment 3: on finishing
    def on_session_end(self, messages):
        if self.passive:
            return
        turns = [{"role": m["role"], "content": m["content"]} for m in messages
                 if m.get("role") in ("user", "assistant") and isinstance(m.get("content"), str)]
        self.c.call("log_session", session_id=self.sid, turns=turns)
        if self.retrieval_id:
            self.c.call("outcome", retrieval_id=self.retrieval_id,
                        value=1.0 if self.last_ok else -0.5 if self.last_ok is False else 0.0,
                        used_ids=sorted(self.used))

    def shutdown(self): self.c.close()

def register(ctx):
    p = ChandaProvider()
    ctx.register_hook("on_session_end", p.note_turn)   # general hook, forwarded by the collector
    ctx.register_memory_provider(p)
```

Config: `hermes config set memory.provider chanda`; env `CHANDA_BIN`, `CHANDA_NAMESPACE`, `CHANDA_ARGS` (e.g.
`--config /path/chanda.yaml`). Add `get_config_schema()`/`save_config()` for `hermes memory setup`, and run
`log_session` via `spawn_context_thread` if exit latency matters.

To verify while implementing: how the Standard profile is selected (`log_session`/`outcome` are not in Read; BASE.md
puts the profile in `chanda.yaml`); the exact `context` response fields; the `outcome.value` policy (1.0 / -0.5 /
0.0 above, range -1..1); whether `used_ids` should also include context records the final answer cites.

## 5. Generic adapter pattern (any agent, `adapters/generic/`)

Same client, three calls, no agent-specific API. The agent runtime only needs two seams: "wrap the first prompt" and
"run on exit". Works for CLI agents (wrap the launcher), SDK agents (wrap `run()`), or anything that can prepend text
to a prompt.

```python
# adapters/generic/run_with_chanda.py
from chanda_client import ChandaClient

def run_task(agent_run, request: str, session_id: str, namespace="default", budget=1500):
    c = ChandaClient("chanda", ("serve",), namespace); c.start()
    try:
        # 1. before the first turn: deterministic, from the environment, not from the model
        ctx = c.call("context", query=request, budget_tokens=budget)
        prompt = f"{ctx.get('text','')}\n\n---\n{request}" if ctx.get("text") else request
        # 2. during: expose c.call("search"|"get"|"timeline", ...) as tools, or point the agent at
        #    `chanda serve` through its own MCP config (no code needed for this step alone)
        transcript, ok = agent_run(prompt, tools=lambda name, **a: c.call(name, **a))
        # 3. on finishing
        c.call("log_session", session_id=session_id,
               turns=[{"role": r, "content": t} for r, t in transcript])
        if ctx.get("retrieval_id"):
            c.call("outcome", retrieval_id=ctx["retrieval_id"], value=1.0 if ok else -1.0, used_ids=[])
    finally:
        c.close()
```

