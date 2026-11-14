"""Hermes MemoryProvider plugin, with a stub server.

Set HERMES_SRC to a Hermes Agent checkout to also check the provider against
the real ``agent.memory_provider.MemoryProvider`` ABC (run in a subprocess).
"""

from __future__ import annotations

import filecmp
import json
import logging
import os
import subprocess
import sys
import tempfile
import unittest

import support  # noqa: F401  (sets sys.path)
from fake_chanda import BLOCK, FakeChanda, wired_responses
from support import ADAPTERS, HERE

import chanda_hermes
from chanda_hermes import ChandaProvider, Settings, default_client, load_settings, register
from chanda_hermes._client import ChandaClient

QUERY = "When is the Project Orion deadline?"


def provider_for(fake: FakeChanda, settings: Settings | None = None) -> ChandaProvider:
    def factory(s, errlog):
        return ChandaClient(transport=fake.transport, timeout=10, errlog=errlog)
    return ChandaProvider(settings or Settings(), client_factory=factory)


def session_messages(user_content: str = QUERY) -> list[dict]:
    """An OpenAI-style transcript as Hermes passes to on_session_end."""
    return [
        {"role": "system", "content": "You are Hermes."},
        {"role": "user", "content": user_content},
        {"role": "assistant", "content": None,
         "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "chanda_search"}}]},
        {"role": "tool", "tool_call_id": "c1", "content": "{\"ok\": true}"},
        {"role": "assistant", "content": "The deadline is 2026-12-01 [fct_ALPHA]."},
    ]


class Lifecycle(unittest.TestCase):
    def setUp(self):
        self.home = tempfile.TemporaryDirectory()
        self.addCleanup(self.home.cleanup)

    def start(self, fake, settings=None, **kw):
        p = provider_for(fake, settings)
        p.initialize("s1", hermes_home=self.home.name, platform="cli", **kw)
        self.addCleanup(p.shutdown)
        return p

    def test_prefetch_injects_only_on_the_first_turn(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake, Settings(budget_tokens=900))
        self.assertEqual(p.prefetch(QUERY, session_id="s1"), BLOCK)
        self.assertEqual(p.recall_status().count, 2)
        self.assertEqual(p.prefetch("and the budget?", session_id="s1"), "")
        self.assertIsNone(p.recall_status())
        self.assertEqual(fake.calls, [("context", {"query": QUERY, "budget_tokens": 900, "session_id": "s1"})])

    def test_resumed_session_gets_no_pre_context(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake, resumed=True, message_count=6)
        self.assertEqual(p.prefetch(QUERY), "")
        self.assertEqual(fake.calls, [])

    def test_tools_are_routed_through_the_provider(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake)
        names = [s["name"] for s in p.get_tool_schemas()]
        self.assertEqual(names, ["chanda_search", "chanda_get", "chanda_timeline"])
        env = json.loads(p.handle_tool_call("chanda_search", {"query": "orion"}))
        self.assertEqual(env["data"]["retrieval_id"], "ret_S1")
        p.handle_tool_call("chanda_get", {"id": "evt_GAMMA"})
        self.assertEqual(fake.names(), ["search", "get"])
        self.assertEqual(fake.calls[0][1]["session_id"], "s1")

    def test_session_end_logs_turns_then_reports_outcomes(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake)
        block = p.prefetch(QUERY)
        p.handle_tool_call("chanda_search", {"query": "orion"})
        p.handle_tool_call("chanda_get", {"id": "evt_GAMMA"})
        p.note_turn(session_id="s1", completed=True, failed=False, interrupted=False)
        # Even if a Hermes build stored the appended block, it is not logged back.
        p.on_session_end(session_messages(QUERY + "\n\n" + block))
        self.assertEqual(fake.names(), ["context", "search", "get", "log_session", "outcome", "outcome"])
        self.assertEqual(fake.calls[3][1], {"session_id": "s1", "turns": [
            {"role": "user", "content": QUERY},
            {"role": "assistant", "content": "The deadline is 2026-12-01 [fct_ALPHA]."},
        ]})
        self.assertEqual(fake.calls[4][1], {"retrieval_id": "ret_CTX", "value": 1.0, "used_ids": ["fct_ALPHA"]})
        self.assertEqual(fake.calls[5][1], {"retrieval_id": "ret_S1", "value": 1.0,
                                            "used_ids": ["evt_GAMMA", "fct_ALPHA"]})
        p.on_session_end(session_messages())  # idempotent: shutdown after a session end
        self.assertEqual(len(fake.calls), 6)

    def test_outcome_value_policy(self):
        cases = [
            ({"completed": True}, 1.0),
            ({"completed": False, "failed": True}, -1.0),
            ({"completed": False, "interrupted": True}, -0.5),
            ({}, None),  # no status seen: no outcome by default
        ]
        for status, want in cases:
            with self.subTest(status=status):
                fake = FakeChanda(wired_responses())
                p = self.start(fake)
                p.prefetch(QUERY)
                if status:
                    p.note_turn(session_id="s1", **status)
                p.on_session_end(session_messages())
                outcomes = [a for n, a in fake.calls if n == "outcome"]
                self.assertEqual([o["value"] for o in outcomes], [want] if want is not None else [])

    def test_configured_values_and_other_sessions_status(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake, Settings(outcome_completed=0.3, outcome_unknown=0.0))
        p.prefetch(QUERY)
        p.note_turn(session_id="another-session", completed=True)  # not ours: ignored
        self.assertEqual(p.outcome_value(), 0.0)
        p.note_turn(session_id="s1", completed=True)
        self.assertEqual(p.outcome_value(), 0.3)

    def test_non_primary_context_reads_but_never_writes(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake, agent_context="cron")
        self.assertEqual(p.prefetch(QUERY), BLOCK)
        p.note_turn(session_id="s1", completed=True)
        p.on_session_end(session_messages())
        self.assertEqual(fake.names(), ["context"])

    def test_new_session_after_reset_gets_pre_context_again(self):
        fake = FakeChanda(wired_responses())
        p = self.start(fake)
        p.prefetch(QUERY)
        p.on_session_end(session_messages())
        p.on_session_switch("s2", parent_session_id="s1", reset=True)
        self.assertEqual(p.prefetch("new topic"), BLOCK)
        p.on_session_switch("s3", parent_session_id="s2", reset=False)  # /resume: continues a conversation
        self.assertEqual(p.prefetch("more"), "")
        p.on_session_end(session_messages())
        contexts = [a for n, a in fake.calls if n == "context"]
        self.assertEqual([c["session_id"] for c in contexts], ["s1", "s2"])
        self.assertEqual([a["session_id"] for n, a in fake.calls if n == "log_session"], ["s1", "s3"])

    def test_unwired_core_injects_nothing_and_does_not_retry(self):
        fake = FakeChanda()  # every tool answers unavailable
        p = self.start(fake)
        with self.assertLogs("chanda_hermes", logging.WARNING):
            self.assertEqual(p.prefetch(QUERY), "")
        self.assertEqual(p.prefetch("second turn"), "")
        p.note_turn(session_id="s1", completed=True)
        with self.assertLogs("chanda_hermes", logging.WARNING):
            p.on_session_end(session_messages())
        self.assertEqual(fake.names(), ["context", "log_session"])  # no retrieval id: no outcome

    def test_server_that_cannot_start_leaves_hermes_running(self):
        p = ChandaProvider(Settings(command="/nonexistent/chanda", connect_timeout=5))
        with self.assertLogs("chanda_hermes", logging.WARNING):
            p.initialize("s1", hermes_home=self.home.name)
        self.addCleanup(p.shutdown)
        self.assertEqual(p.prefetch(QUERY), "")
        env = json.loads(p.handle_tool_call("chanda_search", {"query": "x"}))
        self.assertEqual(env["error"]["code"], "unavailable")
        p.on_session_end(session_messages())
        self.assertTrue(os.path.exists(os.path.join(self.home.name, "chanda", "serve.log")))


class Setup(unittest.TestCase):
    def test_settings_from_config_and_environment(self):
        s = load_settings({"command": "/opt/chanda", "namespace": "work", "budget_tokens": "1200",
                           "outcome_interrupted": "skip", "unknown_key": 1},
                          {"CHANDA_NAMESPACE": "env-ns", "CHANDA_CONFIG": "/etc/chanda.yaml"})
        self.assertEqual((s.command, s.namespace, s.config, s.budget_tokens), ("/opt/chanda", "env-ns",
                                                                               "/etc/chanda.yaml", 1200))
        self.assertIsNone(s.outcome_interrupted)
        self.assertEqual(load_settings(None, {}), Settings())

    def test_bad_values_are_refused(self):
        for raw in ({"outcome_completed": 2}, {"outcome_failed": "bad"}, {"budget_tokens": -1},
                    {"context_timeout": 0}):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                load_settings(raw, {})

    def test_spawned_server_uses_read_and_standard_profiles(self):
        c = default_client(Settings(command="chanda", config="/etc/chanda.yaml", namespace="work"), None)
        self.assertEqual(c.args, ["serve", "--config", "/etc/chanda.yaml", "--profiles", "read,standard"])
        self.assertEqual(c.namespace, "work")
        self.assertEqual(default_client(Settings(url="http://127.0.0.1:8765/mcp"), None).url,
                         "http://127.0.0.1:8765/mcp")

    def test_availability_needs_the_binary_or_a_url(self):
        self.assertFalse(ChandaProvider(Settings(command="/nonexistent/chanda")).is_available())
        self.assertIn("not found", ChandaProvider(Settings(command="/nonexistent/chanda")).unavailable_reason())
        self.assertTrue(ChandaProvider(Settings(command=sys.executable)).is_available())
        self.assertTrue(ChandaProvider(Settings(command="/nonexistent", url="http://h/mcp")).is_available())

    def test_register_adds_hook_and_a_new_provider_each_load(self):
        class Ctx:
            def __init__(self):
                self.hooks, self.providers = [], []

            def register_hook(self, name, cb):
                self.hooks.append((name, cb))

            def register_memory_provider(self, p):
                self.providers.append(p)

        a, b = Ctx(), Ctx()
        register(a)
        register(b)
        self.assertEqual([h[0] for h in a.hooks], ["on_session_end"])
        self.assertEqual(a.providers[0].name, "chanda")
        self.assertIsNot(a.providers[0], b.providers[0])
        self.assertEqual(a.hooks[0][1].__self__, a.providers[0])

    def test_vendored_client_matches_the_generic_one(self):
        generic = os.path.join(ADAPTERS, "generic", "chanda_adapter", "client.py")
        vendored = os.path.join(ADAPTERS, "hermes", "chanda_hermes", "_client.py")
        self.assertTrue(filecmp.cmp(generic, vendored, shallow=False),
                        "copy adapters/generic/chanda_adapter/client.py to adapters/hermes/chanda_hermes/_client.py")

    def test_plugin_metadata(self):
        with open(os.path.join(ADAPTERS, "hermes", "chanda_hermes", "plugin.yaml"), encoding="utf-8") as f:
            text = f.read()
        self.assertIn("name: chanda\n", text)
        with open(os.path.join(ADAPTERS, "hermes", "pyproject.toml"), encoding="utf-8") as f:
            py = f.read()
        self.assertIn('[project.entry-points."hermes_agent.memory_providers"]\nchanda = "chanda_hermes:register"', py)


@unittest.skipUnless(os.environ.get("HERMES_SRC"), "set HERMES_SRC to a Hermes Agent checkout")
class AgainstHermesABC(unittest.TestCase):
    def test_provider_implements_the_real_interface(self):
        code = (
            "import sys, json\n"
            "from agent.memory_provider import MemoryProvider, RecallStatus\n"
            "sys.path.insert(0, sys.argv[1])\n"
            "import support\n"
            "from fake_chanda import FakeChanda, wired_responses\n"
            "from chanda_hermes import ChandaProvider, Settings\n"
            "from chanda_hermes._client import ChandaClient\n"
            "fake = FakeChanda(wired_responses())\n"
            "p = ChandaProvider(Settings(), client_factory=lambda s, e: ChandaClient(transport=fake.transport))\n"
            "assert isinstance(p, MemoryProvider)\n"
            "p.initialize('s1', hermes_home=sys.argv[2], platform='cli', agent_context='primary')\n"
            "assert p.prefetch('q')\n"
            "assert isinstance(p.recall_status(), RecallStatus)\n"
            "p.note_turn(session_id='s1', completed=True)\n"
            "p.on_session_end([{'role': 'user', 'content': 'q'}])\n"
            "p.shutdown()\n"
            "print(json.dumps(fake.names()))\n"
        )
        with tempfile.TemporaryDirectory() as home:
            out = subprocess.run([sys.executable, "-c", code, HERE, home], cwd=os.environ["HERMES_SRC"],
                                 env={**os.environ, "PYTHONPATH": os.environ["HERMES_SRC"]},
                                 capture_output=True, text=True, timeout=120)
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertEqual(json.loads(out.stdout.strip().splitlines()[-1]), ["context", "log_session", "outcome"])


if __name__ == "__main__":
    unittest.main()
