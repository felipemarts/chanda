"""Synthetic fixtures for the eval/learn tests (no dataset, no network)."""

from __future__ import annotations

import importlib.util
import json
import os

LEARN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def load_module(name: str, file: str):
    spec = importlib.util.spec_from_file_location(name, os.path.join(LEARN, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def write_jsonl(path, rows):
    os.makedirs(os.path.dirname(str(path)), exist_ok=True)
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


def write_json(path, obj):
    os.makedirs(os.path.dirname(str(path)), exist_ok=True)
    with open(path, "w") as f:
        json.dump(obj, f)


CONVS = ["conv-a", "conv-b"]
# Verdicts of the 6 test questions of each conversation.
VERDICTS = {"B": [1, 0, 0, 0, 0, 0], "U0": [1, 1, 1, 0, 0, 0], "U1": [1, 1, 0, 1, 0, 0], "K": [1, 1, 1, 1, 1, 1]}


def ev(eid, reached):
    return {"key": f"D1:{eid[-1]}", "event_id": eid, "reached": reached}


def test_line(conv, i, cond):
    """Ask line of test question i under cond. Evidence and items:
    q0: evidence evt_k (missing in U0); K brings evt_k by a key (evidence).
    q1: K delivers evt_b by a key, U0 delivered it too (not new).
    q2: K brings evt_x by a key, not evidence (evidence evt_y, reached).
    q3: evidence evt_k missing in U0, U0 wrong: the tight ceiling.
    q4: evidence evt_z, used only in a negative outcome.
    q5: evidence evt_d, delivered to a use question, never used."""
    items = {0: ["evt_a"], 1: ["evt_b"], 2: ["evt_y"], 3: ["evt_a"], 4: ["evt_z"], 5: ["evt_d"]}[i]
    evid = {0: [ev("evt_k", False)], 1: [ev("evt_b", True)], 2: [ev("evt_y", True)], 3: [ev("evt_k", False)],
            4: [ev("evt_z", True)], 5: [ev("evt_d", True)]}[i]
    its = [{"id": x, "type": "event"} for x in items]
    kc = 0
    if cond == "K":
        if i == 0:
            its.append({"id": "evt_k", "type": "event", "key_rank": 3})
            evid = [ev("evt_k", True)]
        if i == 1:
            its[0]["key_rank"] = 2
        if i == 2:
            its.append({"id": "evt_x", "type": "event", "key_rank": 1})
        kc = 2 if i < 3 else 0
    reached = sum(1 for t in evid if t["reached"])
    return {"question_id": f"{conv}-q{i}", "condition": cond, "items": its, "key_candidates": kc,
            "retrieval_ms": {"B": 1.0, "U0": 10.0, "U1": 11.0, "K": 20.0}[cond] + i,
            "evidence": {"scored": True, "turns": evid, "reached": reached, "full": reached == len(evid)}}


def make_work(tmp, verdicts=VERDICTS):
    w = tmp / "work"
    for conv in CONVS:
        od = w / conv / "out"
        for c, vs in verdicts.items():
            write_jsonl(od / f"{c}.test.answers.jsonl",
                        [{"question_id": f"{conv}-q{i}", "verdict": bool(v), "category": 1 + i % 4, "verdict_parse": "exact",
                          "question": f"question {i}?", "gold": f"gold {i}", "answer": f"{c} answer {i}"} for i, v in enumerate(vs)])
            write_jsonl(od / f"{c}.test.ask.jsonl", [test_line(conv, i, c) for i in range(len(vs))])
        write_jsonl(od / "base.use.ask.jsonl", [{"question_id": f"{conv}-u0", "items": [{"id": "evt_k"}, {"id": "evt_d"}]},
                                                {"question_id": f"{conv}-u1", "items": [{"id": "evt_z"}]}])
        write_jsonl(od / "base.use.answers.jsonl", [{"question_id": f"{conv}-u0", "verdict": True}, {"question_id": f"{conv}-u1", "verdict": False}])
        write_jsonl(od / "base.use.outcomes.jsonl", [{"question_id": f"{conv}-u0", "value": 1, "used_ids": ["evt_k"]},
                                                     {"question_id": f"{conv}-u1", "value": -1, "used_ids": ["evt_z"]}])
        leak = {"ok": True, "key_table": "learned_keys", "keys": 1, "failures": [], "gold_in_use_question": [],
                "test_question_inside_use_question": []}
        write_json(od / "base.leak.json", leak)
        for c in ("U0", "U1", "K"):
            l2 = dict(leak)
            if c == "K":
                l2["gold_in_use_question"] = [{"table": "learned_keys", "column": "text", "row": "key_1", "match": "Lisbon",
                                               "via": "Where does Bruno run in Lisbon?"}]
            write_json(od / f"{c}.leak.json", l2)
        for c in ("U1", "K"):
            write_json(od / f"{c}.sleep.json", {"output": {"status": "completed", "cost": {"llm_calls": 0}}})
    return w
