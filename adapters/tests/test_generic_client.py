"""ChandaClient, ChandaTools, Usage and transcript helpers."""

from __future__ import annotations

import json
import tempfile
import types
import unittest

import support  # noqa: F401  (sets sys.path)
from fake_chanda import BLOCK, READ, FakeChanda, err, ok, wired_responses
from support import fake_chanda_command, read_log

from chanda_adapter import (
    CODE_TRANSPORT,
    ChandaClient,
    ChandaError,
    ChandaTools,
    Usage,
    compose_prompt,
    normalize_turns,
    parse_envelope,
    strip_block,
)


def client_for(fake: FakeChanda, **kw) -> ChandaClient:
    return ChandaClient(transport=fake.transport, timeout=10, **kw).start()


class EnvelopeParsing(unittest.TestCase):
    def test_ok_envelope_returns_data(self):
        fake = FakeChanda(wired_responses())
        with client_for(fake) as c:
            data = c.context("when is the Orion deadline?")
        self.assertEqual(data["retrieval_id"], "ret_CTX")
        self.assertEqual(data["text"], BLOCK)

    def test_not_ok_raises_with_stable_code(self):
        fake = FakeChanda({"get": err("not_found", "record fct_X not found")})
        with client_for(fake) as c:
            with self.assertRaises(ChandaError) as cm:
                c.get("fct_X")
            env = c.call_envelope("get", {"id": "fct_X"})
        self.assertEqual(cm.exception.code, "not_found")
        self.assertEqual(cm.exception.tool, "get")
        self.assertFalse(env["ok"])
        self.assertEqual(env["error"]["code"], "not_found")

    def test_unwired_core_answers_unavailable(self):
        with client_for(FakeChanda()) as c:
            with self.assertRaises(ChandaError) as cm:
                c.context("anything")
        self.assertEqual(cm.exception.code, "unavailable")

    def test_falls_back_to_text_content(self):
        env = ok({"x": 1})
        result = types.SimpleNamespace(structured_content=None,
                                       content=[types.SimpleNamespace(text="not json"),
                                                types.SimpleNamespace(text=json.dumps(env))])
        self.assertEqual(parse_envelope(result)["data"], {"x": 1})

    def test_accepts_sdk1_field_name(self):
        result = types.SimpleNamespace(structuredContent=ok({"y": 2}), content=[])
        self.assertEqual(parse_envelope(result)["data"], {"y": 2})

    def test_garbage_is_a_transport_error(self):
        result = types.SimpleNamespace(structured_content={"nope": 1}, content=[types.SimpleNamespace(text="hi")])
        with self.assertRaises(ChandaError) as cm:
            parse_envelope(result, "search")
        self.assertEqual(cm.exception.code, CODE_TRANSPORT)


class Calls(unittest.TestCase):
    def test_drops_none_arguments_and_adds_namespace(self):
        fake = FakeChanda(wired_responses())
        with client_for(fake, namespace="team") as c:
            c.search("orion", limit=None, filters=None)
            c.call("search", query="x", namespace="other")
        self.assertEqual(fake.calls[0], ("search", {"query": "orion", "namespace": "team"}))
        self.assertEqual(fake.calls[1][1]["namespace"], "other")

    def test_tool_outside_profiles_is_transport_error(self):
        fake = FakeChanda(wired_responses(), tools=READ)  # read profile only
        with client_for(fake) as c:
            with self.assertRaises(ChandaError) as cm:
                c.log_session("s1", [{"role": "user", "content": "hi"}])
        self.assertEqual(cm.exception.code, CODE_TRANSPORT)
        self.assertIn("unknown tool", cm.exception.message)

    def test_outcome_value_out_of_range_is_refused_before_sending(self):
        fake = FakeChanda(wired_responses())
        with client_for(fake) as c:
            for bad in (1.5, -2, True, "1"):
                with self.assertRaises(ValueError):
                    c.outcome("ret_CTX", bad, [])
        self.assertEqual(fake.calls, [])

    def test_outcome_sorts_and_dedups_used_ids(self):
        fake = FakeChanda(wired_responses())
        with client_for(fake) as c:
            c.outcome("ret_CTX", 1, ["b", "a", "b"])
        self.assertEqual(fake.calls[0][1], {"retrieval_id": "ret_CTX", "value": 1.0, "used_ids": ["a", "b"]})

    def test_on_call_sees_every_call_in_order(self):
        seen = []
        fake = FakeChanda(wired_responses())
        with client_for(fake, on_call=seen.append) as c:
            c.context("q")
            with self.assertRaises(ChandaError):
                c.call("explain", retrieval_id="r", record_id="x")  # no canned answer: unavailable
        self.assertEqual([(e["tool"], e["ok"], e["code"]) for e in seen],
                         [("context", True, None), ("explain", False, "unavailable")])

    def test_spawn_failure_is_transport_error(self):
        c = ChandaClient("/nonexistent/chanda-binary", connect_timeout=10)
        with self.assertRaises(ChandaError) as cm:
            c.start()
        self.assertEqual(cm.exception.code, CODE_TRANSPORT)
        with self.assertRaises(ChandaError):
            c.context("q")

    def test_serve_arguments_select_read_and_standard_profiles(self):
        c = ChandaClient("chanda", config="/etc/chanda.yaml")
        self.assertEqual(c.args, ["serve", "--config", "/etc/chanda.yaml", "--profiles", "read,standard"])

    def test_stdio_spawns_server_with_profiles(self):
        with tempfile.TemporaryDirectory() as d:
            cmd, env, log = fake_chanda_command(d, "wired")
            with ChandaClient(cmd, env=env, timeout=20, connect_timeout=30) as c:
                self.assertEqual(c.context("q")["retrieval_id"], "ret_CTX")
            entries = read_log(log)
        self.assertEqual(entries[0], {"tool": "_start", "args": {"command": "serve", "profiles": "read,standard",
                                                                  "config": ""}})
        self.assertEqual([e["tool"] for e in entries[1:]], ["context"])


class ToolBridge(unittest.TestCase):
    def test_schemas_name_the_three_agent_tools(self):
        names = [s["name"] for s in ChandaTools.schemas()]
        self.assertEqual(names, ["chanda_search", "chanda_get", "chanda_timeline"])

    def test_get_marks_record_opened_and_search_records_delivery(self):
        fake = FakeChanda(wired_responses())
        with client_for(fake) as c:
            tools = ChandaTools(c, session_id="s1")
            out = json.loads(tools.call("chanda_search", {"query": "orion", "namespace": "evil"}))
            tools.call("chanda_get", {"id": "evt_GAMMA"})
        self.assertTrue(out["ok"])
        self.assertEqual(fake.calls[0], ("search", {"query": "orion", "session_id": "s1"}))
        self.assertEqual(tools.usage.opened, {"evt_GAMMA"})
        self.assertEqual(tools.usage.delivered, {"ret_S1": ["evt_GAMMA", "fct_ALPHA"]})

    def test_failed_get_is_not_counted(self):
        fake = FakeChanda({"get": err("not_found")})
        with client_for(fake) as c:
            tools = ChandaTools(c)
            env = json.loads(tools.call("chanda_get", {"id": "fct_NOPE"}))
        self.assertEqual(env["error"]["code"], "not_found")
        self.assertEqual(tools.usage.opened, set())

    def test_unknown_tool_and_missing_client_answer_envelopes(self):
        env = json.loads(ChandaTools(None).call("chanda_get", {"id": "x"}))
        self.assertEqual(env["error"]["code"], "unavailable")
        with client_for(FakeChanda(wired_responses())) as c:
            env = json.loads(ChandaTools(c).call("chanda_forget", {}))
        self.assertEqual(env["error"]["code"], "invalid_argument")


class UsagePlan(unittest.TestCase):
    def test_context_always_reported_search_only_when_used(self):
        u = Usage()
        u.add_retrieval("ret_CTX", [{"id": "fct_A"}, {"id": "evt_B"}], context=True)
        u.add_retrieval("ret_S1", [{"id": "evt_C"}])
        u.add_retrieval("ret_S2", [{"id": "evt_D"}, {"id": "fct_A"}])
        u.open("evt_D")
        u.cite_from(["The deadline is in fct_A."])
        self.assertEqual(u.plan(), [("ret_CTX", ["fct_A"]), ("ret_S2", ["evt_D", "fct_A"])])

    def test_used_ids_are_a_subset_of_delivered(self):
        u = Usage()
        u.add_retrieval("ret_CTX", [{"id": "fct_A"}], context=True)
        u.open("fct_OTHER")
        self.assertEqual(u.plan(), [("ret_CTX", [])])

    def test_no_retrieval_no_outcome(self):
        self.assertEqual(Usage().plan(), [])


class Transcript(unittest.TestCase):
    def test_normalize_keeps_user_and_assistant_text(self):
        msgs = [
            {"role": "system", "content": "you are hermes"},
            {"role": "user", "content": [{"type": "text", "text": "hello"}, {"type": "image_url"}]},
            {"role": "assistant", "content": None, "tool_calls": [{"id": "1"}]},
            {"role": "tool", "content": "{\"ok\": true}", "tool_call_id": "1"},
            {"role": "assistant", "content": "done", "ts": "2026-11-13T10:00:00Z"},
            ("user", "thanks"),
        ]
        self.assertEqual(normalize_turns(msgs), [
            {"role": "user", "content": "hello"},
            {"role": "assistant", "content": "done", "ts": "2026-11-13T10:00:00Z"},
            {"role": "user", "content": "thanks"},
        ])
        self.assertEqual(len(normalize_turns(msgs, roles=("user", "assistant", "tool"))), 4)

    def test_injected_block_is_stripped_from_user_turns(self):
        prompt = compose_prompt(BLOCK, "When is the deadline?")
        self.assertTrue(prompt.startswith(BLOCK))
        turns = strip_block([{"role": "user", "content": prompt}, {"role": "assistant", "content": BLOCK}], BLOCK)
        self.assertEqual(turns[0]["content"], "When is the deadline?")
        self.assertEqual(turns[1]["content"], BLOCK)  # assistant text is the agent's own
        self.assertEqual(compose_prompt("", "q"), "q")


if __name__ == "__main__":
    unittest.main()
