"""Generic Chanda adapter: deliver memory to any agent at three moments.

See adapters/generic/README.md. Typical use::

    from chanda_adapter import AgentResult, ChandaClient, run_task

    def agent(prompt, tools):
        ...  # call your model with prompt; expose tools.schemas() / tools.call()
        return AgentResult(turns=[("user", request), ("assistant", answer)], value=1.0)

    with ChandaClient(config="chanda.yaml") as client:
        report = run_task(agent, request, session_id="s-42", client=client)
"""

from .client import (
    CODE_TRANSPORT,
    DEFAULT_PROFILES,
    TOOL_SCHEMAS,
    ChandaClient,
    ChandaError,
    ChandaTools,
    Usage,
    compose_prompt,
    normalize_turns,
    parse_envelope,
    strip_block,
)
from .run import AgentResult, OutcomeReport, TaskReport, run_task

__all__ = [
    "AgentResult",
    "CODE_TRANSPORT",
    "ChandaClient",
    "ChandaError",
    "ChandaTools",
    "DEFAULT_PROFILES",
    "OutcomeReport",
    "TOOL_SCHEMAS",
    "TaskReport",
    "Usage",
    "compose_prompt",
    "normalize_turns",
    "parse_envelope",
    "run_task",
    "strip_block",
]
