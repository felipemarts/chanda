"""run_task: the three moments of BASE.md "Why MCP alone is not enough".

1. Before the first turn, call ``context`` with the user's request and put
   the returned block in front of the first prompt.
2. During the task, the agent gets ``search``/``get``/``timeline`` as tools.
3. On finishing, call ``log_session`` with the turns and ``outcome`` with the
   retrieval id, the task value and the ids the agent used.

The adapter, not the model, makes calls 1 and 3. Every call is sent once; when
Chanda is unavailable the agent still runs, without a pre-context.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any, Callable

from .client import (
    CODE_TRANSPORT,
    ChandaClient,
    ChandaError,
    ChandaTools,
    check_value,
    compose_prompt,
    normalize_turns,
    strip_block,
)

log = logging.getLogger("chanda_adapter")


@dataclass
class AgentResult:
    """What the agent returns: its transcript and how the task went.

    ``turns`` are OpenAI-style message dicts or ``(role, content)`` pairs;
    ``value`` is the outcome in [-1, 1], or None to send no outcome.
    """

    turns: list[Any]
    value: float | None = None

    def __post_init__(self) -> None:
        if self.value is not None:
            self.value = check_value(self.value)


@dataclass
class OutcomeReport:
    retrieval_id: str
    value: float
    used_ids: list[str]
    error: str | None = None  # Chanda code when the call failed


@dataclass
class TaskReport:
    """What run_task did, for logs and tests."""

    prompt: str = ""
    injected: bool = False
    retrieval_id: str | None = None
    context_error: str | None = None  # Chanda code (or "transport") when context failed
    result: AgentResult | None = None
    logged_turns: int = 0
    log_error: str | None = None
    outcomes: list[OutcomeReport] = field(default_factory=list)


AgentFn = Callable[[str, ChandaTools], AgentResult]


def run_task(
    agent_fn: AgentFn,
    request: str,
    *,
    session_id: str,
    client: ChandaClient | None = None,
    budget_tokens: int | None = None,
    as_of: str | None = None,
    logger: logging.Logger | None = None,
) -> TaskReport:
    """Run ``agent_fn(prompt, tools)`` with Chanda's three moments around it.

    ``client`` defaults to a new :class:`ChandaClient` (``chanda serve`` over
    stdio with the read and standard profiles), closed on return; a client
    passed in is started if needed and left open. Exceptions from agent_fn
    propagate and skip the finishing calls.
    """
    lg = logger or log
    owned = client is None
    if client is None:
        client = ChandaClient()
    report = TaskReport()
    try:
        connected = _connect(client, lg)
        tools = ChandaTools(client if connected else None, session_id=session_id)

        # 1. Before the first turn.
        block = ""
        if not connected:
            report.context_error = CODE_TRANSPORT
        else:
            try:
                data = client.context(request, budget_tokens=budget_tokens, as_of=as_of, session_id=session_id)
                block = data.get("text") or ""
                report.retrieval_id = data.get("retrieval_id") or None
                tools.usage.add_retrieval(report.retrieval_id, data.get("items"), context=True)
            except ChandaError as e:
                report.context_error = e.code
                lg.warning("chanda context failed (%s); running without pre-context: %s", e.code, e.message)
        report.prompt = compose_prompt(block, request)
        report.injected = bool(block)

        # 2. During the task.
        result = agent_fn(report.prompt, tools)
        if not isinstance(result, AgentResult):
            raise TypeError(f"agent_fn must return AgentResult, got {type(result).__name__}")
        report.result = result
        if not connected:
            return report

        # 3. On finishing.
        turns = strip_block(normalize_turns(result.turns), block)
        tools.usage.cite_from(t["content"] for t in turns if t["role"] == "assistant")
        if turns:
            try:
                client.log_session(session_id, turns)
                report.logged_turns = len(turns)
            except ChandaError as e:
                report.log_error = e.code
                lg.warning("chanda log_session failed (%s): %s", e.code, e.message)
        if result.value is not None:
            for rid, used in tools.usage.plan():
                o = OutcomeReport(rid, result.value, used)
                try:
                    client.outcome(rid, result.value, used)
                except ChandaError as e:
                    o.error = e.code
                    lg.warning("chanda outcome failed (%s): %s", e.code, e.message)
                report.outcomes.append(o)
        return report
    finally:
        if owned:
            client.close()


def _connect(client: ChandaClient, lg: logging.Logger) -> bool:
    try:
        client.start()
        return True
    except ChandaError as e:
        lg.warning("chanda unavailable (%s); running without memory: %s", e.code, e.message)
        return False
