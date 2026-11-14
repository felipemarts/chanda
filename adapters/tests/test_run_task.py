"""run_task: the three moments, with a stub server."""

from __future__ import annotations

import json
import logging
import tempfile
import unittest

import support  # noqa: F401  (sets sys.path)
from fake_chanda import BLOCK, FakeChanda, empty_responses, wired_responses
from support import fake_chanda_command, read_log

from chanda_adapter import AgentResult, ChandaClient, run_task

REQUEST = "When is the Project Orion deadline?"


class ScriptedAgent:
    """Records what it saw and does a fixed sequence of memory calls."""

    def __init__(self, fake: FakeChanda | None = None, value: float | None = 1.0, use_tools: bool = True):
        self.fake, self.value, self.use_tools = fake, value, use_tools
        self.prompt = None
        self.calls_before_first_turn = None

    def __call__(self, prompt, tools):
        if self.fake is not None:
            self.calls_before_first_turn = self.fake.names()
        self.prompt = prompt
        if self.use_tools:
            json.loads(tools.call("chanda_search", {"query": "orion deadline"}))
            json.loads(tools.call("chanda_get", {"id": "evt_GAMMA"}))
        answer = "The deadline is 2026-12-01 (source fct_ALPHA)."
        return AgentResult(turns=[{"role": "user", "content": prompt}, {"role": "assistant", "content": answer}],
                           value=self.value)


def run(fake: FakeChanda, agent, **kw):
    client = ChandaClient(transport=fake.transport, timeout=10)
    with client:
        return run_task(agent, REQUEST, session_id="s-1", client=client, **kw)


class ThreeMoments(unittest.TestCase):
    def test_context_is_called_before_the_first_turn_and_injected(self):
        fake = FakeChanda(wired_responses())
        agent = ScriptedAgent(fake)
        report = run(fake, agent, budget_tokens=600)
        self.assertEqual(agent.calls_before_first_turn, ["context"])
        self.assertEqual(fake.calls[0][1], {"query": REQUEST, "budget_tokens": 600, "session_id": "s-1"})
        self.assertTrue(report.injected)
        self.assertTrue(agent.prompt.startswith(BLOCK))
        self.assertTrue(agent.prompt.endswith(REQUEST))
        self.assertEqual(report.retrieval_id, "ret_CTX")

    def test_finishing_logs_session_then_outcomes(self):
        fake = FakeChanda(wired_responses())
        report = run(fake, ScriptedAgent(fake))
        self.assertEqual(fake.names(), ["context", "search", "get", "log_session", "outcome", "outcome"])
        log_args = fake.calls[3][1]
        self.assertEqual(log_args["session_id"], "s-1")
        # The injected block is not logged back as session content.
        self.assertEqual(log_args["turns"][0], {"role": "user", "content": REQUEST})
        self.assertEqual(report.logged_turns, 2)
        # Context outcome: fct_ALPHA was cited; search outcome: evt_GAMMA opened, fct_ALPHA cited.
        self.assertEqual(fake.calls[4][1], {"retrieval_id": "ret_CTX", "value": 1.0, "used_ids": ["fct_ALPHA"]})
        self.assertEqual(fake.calls[5][1], {"retrieval_id": "ret_S1", "value": 1.0,
                                            "used_ids": ["evt_GAMMA", "fct_ALPHA"]})
        self.assertEqual([o.error for o in report.outcomes], [None, None])

    def test_no_relevant_memory_injects_nothing(self):
        fake = FakeChanda(empty_responses())
        agent = ScriptedAgent(fake, use_tools=False)
        report = run(fake, agent)
        self.assertFalse(report.injected)
        self.assertEqual(agent.prompt, REQUEST)
        self.assertEqual(fake.names(), ["context", "log_session", "outcome"])
        self.assertEqual(fake.calls[2][1], {"retrieval_id": "ret_EMPTY", "value": 1.0})

    def test_value_none_sends_no_outcome(self):
        fake = FakeChanda(wired_responses())
        run(fake, ScriptedAgent(fake, value=None, use_tools=False))
        self.assertEqual(fake.names(), ["context", "log_session"])


class Unavailable(unittest.TestCase):
    def test_unwired_core_runs_the_agent_without_injection(self):
        fake = FakeChanda()  # every tool answers unavailable
        agent = ScriptedAgent(fake)
        with self.assertLogs("chanda_adapter", logging.WARNING) as logs:
            report = run(fake, agent)
        self.assertEqual(agent.calls_before_first_turn, ["context"])
        self.assertEqual(agent.prompt, REQUEST)
        self.assertFalse(report.injected)
        self.assertEqual(report.context_error, "unavailable")
        self.assertIsNone(report.retrieval_id)
        # log_session is still attempted once; no retrieval id, so no outcome; nothing retried.
        self.assertEqual(fake.names(), ["context", "search", "get", "log_session"])
        self.assertEqual(report.log_error, "unavailable")
        self.assertEqual(report.outcomes, [])
        self.assertTrue(any("context failed (unavailable)" in m for m in logs.output))

    def test_server_that_cannot_start_still_runs_the_agent(self):
        agent = ScriptedAgent()
        client = ChandaClient("/nonexistent/chanda-binary", connect_timeout=10)
        with self.assertLogs("chanda_adapter", logging.WARNING):
            report = run_task(agent, REQUEST, session_id="s-1", client=client)
        self.assertEqual(agent.prompt, REQUEST)
        self.assertEqual(report.context_error, "transport")
        self.assertIsNotNone(report.result)

    def test_agent_errors_propagate_without_finishing_calls(self):
        fake = FakeChanda(wired_responses())

        def broken(prompt, tools):
            raise RuntimeError("model down")

        with self.assertRaises(RuntimeError):
            run(fake, broken)
        self.assertEqual(fake.names(), ["context"])

    def test_bad_outcome_value_is_rejected(self):
        with self.assertRaises(ValueError):
            AgentResult(turns=[], value=2.0)


class OverStdio(unittest.TestCase):
    def test_spawned_server_sees_context_first(self):
        with tempfile.TemporaryDirectory() as d:
            cmd, env, log = fake_chanda_command(d, "wired")
            seen = {}

            def agent(prompt, tools):
                seen["calls"] = [e["tool"] for e in read_log(log)]
                seen["prompt"] = prompt
                return AgentResult(turns=[("user", prompt), ("assistant", "ok fct_ALPHA")], value=0.5)

            report = run_task(agent, REQUEST, session_id="s-2",
                              client=ChandaClient(cmd, env=env, timeout=20, connect_timeout=30))
            calls = [e["tool"] for e in read_log(log)]
        self.assertEqual(seen["calls"], ["_start", "context"])
        self.assertTrue(seen["prompt"].startswith(BLOCK))
        self.assertEqual(calls, ["_start", "context", "log_session", "outcome"])
        self.assertEqual(report.outcomes[0].used_ids, ["fct_ALPHA"])


if __name__ == "__main__":
    unittest.main()
