"""End-to-end check of the generic adapters against a real `chanda serve`.

Run by adapters/e2e_test.sh, which builds chanda, seeds it when the core is
wired, and runs the Go example first. This script runs the Python adapter
with a scripted agent and asserts:

- `context` is the first call, made before the agent's first turn (seen from
  the client's call transcript and, when the call fails, from the server's
  stderr log, which logs every failed tool call);
- wired core: the block is injected at the start of the first prompt and
  carries the seeded fact; log_session and outcome follow, in that order;
- unwired core: context answers `unavailable`, the agent runs with the bare
  request, log_session is attempted once, no outcome is sent;
- the server registered only the read and standard profiles;
- the Go example report (--go-report) shows the same moments.

Prints one line per check and exits 1 when any check fails.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "generic"))

from chanda_adapter import AgentResult, ChandaClient, run_task  # noqa: E402

ADMIN_TOOLS = {"forget", "promote", "sleep_run", "verify", "ranking_reset"}

failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'} {name}" + (f": {detail}" if detail and not ok else ""))
    if not ok:
        failures.append(name)


def server_tool_lines(path: str) -> list[str]:
    """Tools named in the server's 'tool failed' log lines, in order."""
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return re.findall(r'msg="tool failed" tool=(\w+)', f.read())


def registered_tools(path: str) -> list[str]:
    with open(path, encoding="utf-8") as f:
        m = re.search(r'msg="serving MCP over stdio" tools=([\w,]+)', f.read())
    return m.group(1).split(",") if m else []


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--chanda", required=True)
    ap.add_argument("--config", required=True)
    ap.add_argument("--serve-log", required=True)
    ap.add_argument("--mode", choices=["wired", "unwired"], required=True)
    ap.add_argument("--request", required=True)
    ap.add_argument("--expect", default="", help="text the pre-context must contain (wired)")
    ap.add_argument("--go-report", default="")
    ns = ap.parse_args()

    transcript: list[dict] = []
    seen: dict = {}

    def agent(prompt, tools):
        seen["client_calls"] = [c["tool"] for c in transcript]
        seen["server_calls"] = server_tool_lines(ns.serve_log)
        seen["prompt"] = prompt
        found = tools.search(ns.request) if ns.mode == "wired" else json.loads(
            tools.call("chanda_search", {"query": ns.request}))
        items = (found.get("items") if ns.mode == "wired" else []) or []
        if items:
            tools.get(items[0]["id"])
        else:
            tools.call("chanda_get", {"id": "evt_00000000000000000000000000"})
        cited = f" [{items[0]['id']}]" if items else ""
        return AgentResult(turns=[("user", prompt), ("assistant", f"Here is what I found.{cited}")], value=1.0)

    with open(ns.serve_log, "a", encoding="utf-8") as errlog:
        client = ChandaClient(ns.chanda, config=ns.config, errlog=errlog, on_call=transcript.append,
                              timeout=30, connect_timeout=30)
        report = run_task(agent, ns.request, session_id="e2e-session", client=client)

    calls = [c["tool"] for c in transcript]
    check("context is the first call, before the first turn", seen.get("client_calls") == ["context"],
          f"calls before first turn: {seen.get('client_calls')}")
    tools = registered_tools(ns.serve_log)
    check("server runs the read and standard profiles only",
          "context" in tools and "log_session" in tools and not ADMIN_TOOLS & set(tools), f"tools: {tools}")

    if ns.mode == "unwired":
        check("server log shows context before the first turn", seen.get("server_calls") == ["context"],
              f"server log at first turn: {seen.get('server_calls')}")
        check("context answered unavailable", report.context_error == "unavailable", str(report.context_error))
        check("agent ran with the bare request (nothing injected)",
              not report.injected and seen.get("prompt") == ns.request, repr(seen.get("prompt")))
        check("call order: context, search, get, log_session; no outcome",
              calls == ["context", "search", "get", "log_session"], str(calls))
        check("server log order matches", server_tool_lines(ns.serve_log) == calls,
              str(server_tool_lines(ns.serve_log)))
    else:
        prompt = seen.get("prompt") or ""
        check("pre-context injected at the start of the first prompt",
              report.injected and prompt.endswith(ns.request) and not prompt.startswith(ns.request), repr(prompt[:200]))
        # The block may quote the request itself (the Go example logged the
        # same request as a session turn just before): look inside the block.
        end = prompt.find("</chanda-context>")
        block = prompt[:end] if prompt.startswith("<chanda-context") and end > 0 else ""
        check("pre-context is one delimited block, then the request",
              bool(block) and prompt[end + len("</chanda-context>"):].strip().endswith(ns.request), repr(prompt[:200]))
        check("pre-context carries the seeded fact", ns.expect in block, repr(block[:600]))
        check("log_session and outcome follow, in order",
              calls[:1] == ["context"] and "log_session" in calls and "outcome" in calls
              and calls.index("log_session") < calls.index("outcome"), str(calls))
        check("every finishing call succeeded",
              report.log_error is None and all(o.error is None for o in report.outcomes),
              f"log_error={report.log_error} outcomes={[(o.retrieval_id, o.error) for o in report.outcomes]}")

    if ns.go_report:
        with open(ns.go_report, encoding="utf-8") as f:
            go = json.load(f)
        go_calls = [c["tool"] for c in go.get("calls", [])]
        check("go example: context first", go_calls[:1] == ["context"], str(go_calls))
        if ns.mode == "unwired":
            check("go example: unavailable, nothing injected, log_session attempted",
                  go.get("context_error") == "unavailable" and not go.get("injected")
                  and go_calls == ["context", "log_session"], json.dumps(go))
        else:
            check("go example: block injected, log_session then outcome",
                  go.get("injected") and ns.expect in go.get("prompt", "")
                  and go_calls == ["context", "log_session", "outcome"], json.dumps(go))

    print(f"e2e ({ns.mode} core): {'FAILED ' + ', '.join(failures) if failures else 'ok'}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
