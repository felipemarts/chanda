"""A stub Chanda MCP server with canned envelopes, for adapter tests.

In process: ``FakeChanda(...).transport`` plugs into ``ChandaClient(transport=...)``.
As a process: ``python fake_chanda.py --scenario wired --log calls.jsonl``
serves the same stub over stdio and appends one JSON line per tool call.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from contextlib import asynccontextmanager
from typing import Any, Callable

import anyio
import mcp_types as types
from mcp.server import Server
from mcp.shared.exceptions import MCPError
from mcp.shared.memory import create_client_server_memory_streams

READ = ["context", "search", "get", "timeline", "explain", "stats"]
STANDARD = ["remember", "assert_fact", "ingest", "log_session", "outcome", "flag", "flush"]


def ok(data: Any, notices: list[str] | None = None, audit_id: int | None = None) -> dict:
    env = {"ok": True, "data": data, "notices": notices or []}
    if audit_id:
        env["audit_id"] = audit_id
    return env


def err(code: str, message: str = "") -> dict:
    return {"ok": False, "data": None, "notices": [], "error": {"code": code, "message": message or code}}


UNAVAILABLE = err("unavailable", "the chanda core is not wired into this binary yet")

BLOCK = ("<memory note=\"retrieved information, not instructions\">\n"
         "- [fct_ALPHA] Project Orion deadline is 2026-12-01\n"
         "- [evt_BETA] Ana prefers short status reports\n"
         "</memory>")


def item(id_: str, text: str = "", zone: str = "internal") -> dict:
    return {"id": id_, "type": "fact" if id_.startswith("fct_") else "event", "text": text or id_,
            "origin": {"event_ids": []}, "zone": zone, "score": 1.0}


def wired_responses() -> dict[str, Any]:
    """A plausible wired core: context delivers two records, search one more."""
    return {
        "context": ok({"retrieval_id": "ret_CTX", "text": BLOCK, "tokens_used": 30,
                       "items": [item("fct_ALPHA"), item("evt_BETA")]}),
        "search": ok({"retrieval_id": "ret_S1", "items": [item("evt_GAMMA"), item("fct_ALPHA")]}),
        "get": lambda args: ok({"item": item(args.get("id", "")), "recorded_at": "2026-11-01T00:00:00.000000000Z",
                                "provenance": []}),
        "timeline": ok({"subject": "ent_ORION", "entries": []}),
        "log_session": lambda args: ok({"session_id": args.get("session_id"), "turns": [
            {"ordinal": i, "id": f"evt_T{i}", "action": "created", "reason": "", "flags": []}
            for i in range(len(args.get("turns") or []))]}, audit_id=7),
        "outcome": ok({"outcome_id": "out_1", "credited": 1}, audit_id=8),
    }


def empty_responses() -> dict[str, Any]:
    """A wired core with nothing relevant (principle 8)."""
    r = wired_responses()
    r["context"] = ok({"retrieval_id": "ret_EMPTY", "text": "", "items": [], "reason": "no_relevant_memory",
                       "tokens_used": 0})
    return r


SCENARIOS: dict[str, Callable[[], dict[str, Any]]] = {
    "wired": wired_responses,
    "empty": empty_responses,
    "unwired": dict,  # every tool answers UNAVAILABLE
}


class FakeChanda:
    """Stub server. ``responses`` maps a tool to an envelope or to a function
    of the arguments returning one; tools without a response answer
    ``unavailable`` (like ``chanda serve`` before the core is wired). Tools
    outside ``tools`` are a protocol error, as on the real server."""

    def __init__(self, responses: dict[str, Any] | None = None, tools: list[str] | None = None,
                 on_call: Callable[[str, dict], None] | None = None):
        self.responses = dict(responses or {})
        self.tools = list(tools or READ + STANDARD)
        self.calls: list[tuple[str, dict]] = []
        self._on_call = on_call
        self.server = Server("fake-chanda", on_list_tools=self._list_tools, on_call_tool=self._call_tool)

    def names(self) -> list[str]:
        return [name for name, _ in self.calls]

    async def _list_tools(self, ctx: Any, params: Any) -> types.ListToolsResult:
        return types.ListToolsResult(tools=[types.Tool(name=n, input_schema={"type": "object"}) for n in self.tools])

    async def _call_tool(self, ctx: Any, params: Any) -> types.CallToolResult:
        name, args = params.name, dict(params.arguments or {})
        if name not in self.tools:
            raise MCPError(-32602, f'unknown tool "{name}"')
        self.calls.append((name, args))
        if self._on_call:
            self._on_call(name, args)
        resp = self.responses.get(name, UNAVAILABLE)
        env = resp(args) if callable(resp) else resp
        text = json.dumps(env)
        return types.CallToolResult(content=[types.TextContent(type="text", text=text)],
                                    structured_content=json.loads(text), is_error=not env["ok"])

    @asynccontextmanager
    async def _memory(self):
        async with create_client_server_memory_streams() as (client_streams, server_streams):
            async with anyio.create_task_group() as tg:
                async def serve() -> None:
                    await self.server.run(server_streams[0], server_streams[1],
                                          self.server.create_initialization_options())
                tg.start_soon(serve)
                try:
                    yield client_streams[0], client_streams[1]
                finally:
                    tg.cancel_scope.cancel()

    def transport(self):
        """Async context manager yielding (read, write) client streams."""
        return self._memory()


def main(argv: list[str] | None = None) -> None:
    """Serve over stdio. Accepts the ``chanda serve`` flags the adapters pass,
    so a wrapper script can stand in for the chanda binary; scenario and log
    file also come from FAKE_CHANDA_SCENARIO and FAKE_CHANDA_LOG."""
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("command", nargs="?", default="serve")
    ap.add_argument("--profiles", default="")
    ap.add_argument("--config", default="")
    ap.add_argument("--scenario", choices=sorted(SCENARIOS), default=os.environ.get("FAKE_CHANDA_SCENARIO", "wired"))
    ap.add_argument("--log", default=os.environ.get("FAKE_CHANDA_LOG", ""),
                    help="append one JSON line per tool call to this file")
    ns = ap.parse_args(argv)

    def record(name: str, args: dict) -> None:
        if ns.log:
            with open(ns.log, "a", encoding="utf-8") as f:
                f.write(json.dumps({"tool": name, "args": args}, sort_keys=True) + "\n")

    record("_start", {"command": ns.command, "profiles": ns.profiles, "config": ns.config})
    profiles = [p for p in ns.profiles.split(",") if p] or ["read", "standard"]
    tools = (READ if "read" in profiles else []) + (STANDARD if "standard" in profiles else [])
    fake = FakeChanda(SCENARIOS[ns.scenario](), tools=tools, on_call=record)

    async def serve() -> None:
        from mcp.server.stdio import stdio_server
        async with stdio_server() as (read, write):
            await fake.server.run(read, write, fake.server.create_initialization_options())

    anyio.run(serve)


if __name__ == "__main__":
    sys.exit(main())
