"""Prompts (verbatim from PROTOCOL.md), verdict parsing, and locomo_qa.py
against a loopback fake server. No network."""

from __future__ import annotations

import json
import os
import re

import pytest

import locomo_prompts as P
import locomo_qa

PROTOCOL = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "PROTOCOL.md")


def protocol_block(after: str) -> str:
    """The first fenced code block after the line starting with `after`."""
    text = open(PROTOCOL, encoding="utf-8").read()
    i = text.index("\n" + after)
    m = re.compile(r"```\n(.*?)\n```", re.S).search(text, i)
    return m.group(1)


def test_prompts_are_the_protocols_verbatim():
    assert P.READER == protocol_block("Reader (")
    assert P.JUDGE == protocol_block("Judge (")
    assert "max_tokens 200" in open(PROTOCOL).read() and P.READER_MAX_TOKENS == 200
    assert "max_tokens 5" in open(PROTOCOL).read() and P.JUDGE_MAX_TOKENS == 5


def test_reader_prompt_rendering():
    ctx = '<chanda-context retrieval_id="ret_1">\n[excerpt evt_1 | zone: internal]\nJon: a {brace} here\n</chanda-context>'
    p = P.reader_prompt("Jon", "Gina", ctx, "When was {x}?")
    assert p.startswith("Below are memories retrieved from a long conversation between Jon and Gina. Each memory may carry a date.\n\n<chanda-context")
    assert "Jon: a {brace} here" in p
    assert p.endswith('answer "Not mentioned".\n\nQuestion: When was {x}?\nAnswer:')
    assert "{speaker_a}" not in p and "{context}" not in p


def test_judge_prompt_rendering():
    p = P.judge_prompt("Where?", "Lisbon", "In Lisbon, Portugal")
    assert "Question: Where?\nGold answer: Lisbon\nGenerated answer: In Lisbon, Portugal\n\n" in p
    assert p.endswith("Reply with one word: CORRECT or WRONG.")


@pytest.mark.parametrize("raw,want", [
    ("CORRECT", (True, "exact")),
    ("WRONG", (False, "exact")),
    ("correct.", (True, "exact")),
    (" Wrong", (False, "exact")),
    ("**CORRECT**", (True, "exact")),
    ("INCORRECT", (False, "exact")),
    ("The answer is CORRECT", (True, "fallback")),
    ("Verdict: WRONG", (False, "fallback")),
    ("CORRECT or WRONG", (True, "exact")),
    ("Grade: CORRECT or WRONG", (False, "unparsed")),
    ("", (False, "unparsed")),
    ("Yes", (False, "unparsed")),
])
def test_parse_verdict(raw, want):
    assert P.parse_verdict(raw) == want


def ask_line(qid: str, ctx: str = "ctx", gold: str = "Lisbon") -> dict:
    return {"question_id": qid, "condition": "S1", "half": "test", "sample_id": "conv-x", "index": 1, "category": 4,
            "category_name": "single-hop", "question": f"Where? {qid}", "gold": gold, "speaker_a": "Ana",
            "speaker_b": "Bruno", "context": ctx, "retrieval_id": "ret_" + qid, "items": [{"id": "evt_1"}],
            "sleep_items": 1, "sleep_tokens": 10, "sleep_share": 0.1,
            "evidence": {"scored": True, "reached": 1, "full": True, "reached_or_cited": 1, "full_or_cited": True,
                         "turns": [{"key": "D1:1"}]}}


def test_locomo_qa_end_to_end(tmp_path):
    from fakes import FakeServer

    def reply(body):
        prompt = body["messages"][0]["content"]
        if prompt.startswith("You are grading"):
            assert body["max_tokens"] == 5
            return "CORRECT" if "Generated answer: Lisbon" in prompt else "WRONG"
        assert body["max_tokens"] == 200 and body["temperature"] == 0
        return "Lisbon" if "q1" in prompt else "Not mentioned"

    srv = FakeServer(chat_reply=reply)
    try:
        ask = tmp_path / "ask.jsonl"
        ask.write_text("".join(json.dumps(ask_line(q)) + "\n" for q in ("q1", "q2")))
        out = tmp_path / "answers.jsonl"
        argv = ["--ask", str(ask), "--out", str(out), "--tag", "S1-test", "--proxy", srv.url, "--workers", "1"]
        assert locomo_qa.main(argv) == 0
        recs = {r["question_id"]: r for r in map(json.loads, out.read_text().splitlines())}
        assert recs["q1"]["verdict"] is True and recs["q1"]["answer"] == "Lisbon" and recs["q1"]["verdict_parse"] == "exact"
        assert recs["q2"]["verdict"] is False and recs["q2"]["judge_raw"] == "WRONG"
        assert recs["q1"]["reader"]["tag"] == "S1-test-reader" and recs["q1"]["judge"]["tag"] == "S1-test-judge"
        assert recs["q1"]["retrieval_id"] == "ret_q1" and recs["q1"]["gold"] == "Lisbon"
        assert "context" not in recs["q1"] and "reader_prompt" not in recs["q1"]
        assert sorted(set(srv.chat_paths)) == ["/tag/S1-test-judge/v1/chat/completions", "/tag/S1-test-reader/v1/chat/completions"]
        # Resume: nothing more is called.
        n = len(srv.chat_bodies)
        assert locomo_qa.main(argv) == 0 and len(srv.chat_bodies) == n
    finally:
        srv.close()


def test_locomo_qa_fake_and_budget(tmp_path):
    ask = tmp_path / "ask.jsonl"
    ask.write_text(json.dumps(ask_line("q1")) + "\n")
    out = tmp_path / "a.jsonl"
    assert locomo_qa.main(["--ask", str(ask), "--out", str(out), "--tag", "dry", "--fake"]) == 0
    r = json.loads(out.read_text())
    assert r["fake"] and r["answer"] == "Not mentioned" and r["verdict"] is False
    big = tmp_path / "big.jsonl"
    big.write_text(json.dumps(ask_line("q9", ctx="x" * 8004)) + "\n")
    with pytest.raises(SystemExit):
        locomo_qa.main(["--ask", str(big), "--out", str(tmp_path / "b.jsonl"), "--tag", "t", "--fake"])
