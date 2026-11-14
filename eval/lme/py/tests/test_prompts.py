"""prompts.py must hold the official LongMemEval templates verbatim."""

from __future__ import annotations

import ast
import hashlib
import os

import pytest

import prompts

OFFICIAL = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "official")


def _literals(path: str) -> dict[int, str]:
    src = open(path, encoding="utf-8").read()
    out = {}
    for node in ast.walk(ast.parse(src)):
        if isinstance(node, (ast.Assign, ast.AugAssign)) and isinstance(node.value, ast.Constant) \
                and isinstance(node.value.value, str):
            out[node.lineno] = node.value.value
    return out


def test_sources_are_the_pinned_files():
    for name, want in (("run_generation.py.txt", prompts.READER_SOURCE_SHA256),
                       ("evaluate_qa.py.txt", prompts.JUDGE_SOURCE_SHA256)):
        got = hashlib.sha256(open(os.path.join(OFFICIAL, name), "rb").read()).hexdigest()
        assert got == want, name


def test_reader_templates_verbatim():
    g = _literals(os.path.join(OFFICIAL, "run_generation.py.txt"))
    assert prompts.READER_NO_RETRIEVAL == g[48]
    assert prompts.READER_NO_RETRIEVAL_COT_SUFFIX == g[50]
    assert prompts.READER_HISTORY_COT == g[55]
    assert prompts.READER_HISTORY_DIRECT == g[57]
    assert prompts.READER_FACTS_COT == g[65]
    assert prompts.READER_FACTS_DIRECT == g[67]
    src = open(os.path.join(OFFICIAL, "run_generation.py.txt")).read()
    assert "gen_length = 500 if not args.cot else 800" in src


def test_judge_templates_verbatim():
    e = _literals(os.path.join(OFFICIAL, "evaluate_qa.py.txt"))
    assert prompts.JUDGE_DEFAULT == e[27]
    assert prompts.JUDGE_TEMPORAL == e[30]
    assert prompts.JUDGE_KNOWLEDGE_UPDATE == e[33]
    assert prompts.JUDGE_PREFERENCE == e[36]
    assert prompts.JUDGE_ABSTENTION == e[41]
    src = open(os.path.join(OFFICIAL, "evaluate_qa.py.txt")).read()
    assert "'max_tokens': 10" in src
    assert "label = 'yes' in eval_response.lower()" in src
    assert "abstention='_abs' in entry['question_id']" in src


def test_judge_branches():
    q, a, r = "Q?", "A", "R"
    assert prompts.judge_prompt("multi-session", q, a, r) == prompts.JUDGE_DEFAULT.format(q, a, r)
    assert prompts.judge_prompt("single-session-assistant", q, a, r) == prompts.JUDGE_DEFAULT.format(q, a, r)
    assert prompts.judge_prompt("temporal-reasoning", q, a, r) == prompts.JUDGE_TEMPORAL.format(q, a, r)
    assert prompts.judge_prompt("knowledge-update", q, a, r) == prompts.JUDGE_KNOWLEDGE_UPDATE.format(q, a, r)
    assert prompts.judge_prompt("single-session-preference", q, a, r) == prompts.JUDGE_PREFERENCE.format(q, a, r)
    # abstention overrides the type
    assert prompts.judge_prompt("temporal-reasoning", q, a, r, abstention=True) == prompts.JUDGE_ABSTENTION.format(q, a, r)
    with pytest.raises(NotImplementedError):
        prompts.judge_prompt("unknown", q, a, r)


def test_verdict_parse_is_official():
    assert prompts.parse_verdict("Yes.") is True
    assert prompts.parse_verdict(" no") is False
    # the official parser is a substring test; keep its quirks
    assert prompts.parse_verdict("yesterday") is True


def test_reader_prompt():
    p = prompts.reader_prompt("CTX", "2023/05/30 (Tue) 23:40", "What?", cot=True)
    assert p == prompts.READER_HISTORY_COT.format("CTX", "2023/05/30 (Tue) 23:40", "What?")
    assert "History Chats:\n\nCTX\n\nCurrent Date: 2023/05/30 (Tue) 23:40\nQuestion: What?\nAnswer (step by step):" in p
    p = prompts.reader_prompt("", "d", "What?", cot=True, no_retrieval_template=True)
    assert p == "What?Answer step by step."
    with pytest.raises(ValueError):
        prompts.reader_prompt("x", "d", "q", no_retrieval_template=True)
