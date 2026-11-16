"""Synthetic fixtures for the conflict study's tests: LongMemEval-shaped
questions, study.json turn maps, ask lines and conflicts tool outputs. No
dataset, no network."""

from __future__ import annotations

import importlib.util
import json
import os

CONF = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def load_module(name: str, rel: str):
    spec = importlib.util.spec_from_file_location(name, os.path.join(CONF, rel))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def turn(role, content, has=False):
    t = {"role": role, "content": content}
    if has:
        t["has_answer"] = True
    return t


def ku_question(qid="ku1", question="Where do I live now?", d0="2023/01/10 (Tue) 10:00", d1="2023/06/02 (Fri) 10:00", qtype="knowledge-update"):
    """Three sessions: s0 the outdated statement, s1 filler, s2 the current one."""
    return {
        "question_id": qid, "question_type": qtype, "question": question, "answer": "Botafogo",
        "question_date": "2023/07/01 (Sat) 10:00",
        "haystack_dates": [d0, "2023/03/01 (Wed) 10:00", d1],
        "haystack_sessions": [
            [turn("user", "I live in Vila Mariana, Sao Paulo.", True), turn("assistant", "Nice neighbourhood.")],
            [turn("user", "Recommend a book."), turn("assistant", "Try Dom Casmurro.")],
            [turn("user", "I moved to Botafogo in Rio last month.", True), turn("assistant", "Congratulations on the move.")],
        ],
    }


def other_question(qid="o1"):
    return {
        "question_id": qid, "question_type": "single-session-user", "question": "What degree did I get?", "answer": "Business Administration",
        "question_date": "2023/07/01 (Sat) 10:00",
        "haystack_dates": ["2023/01/01 (Sun) 10:00", "2023/02/01 (Wed) 10:00"],
        "haystack_sessions": [
            [turn("user", "I graduated in Business Administration.", True), turn("assistant", "Well done.")],
            [turn("user", "I now study law at night."), turn("assistant", "Good luck.")],
        ],
    }


def meta_for(q: dict, prefix: str) -> dict:
    turns = []
    for s, sess in enumerate(q["haystack_sessions"]):
        for t, _ in enumerate(sess):
            turns.append({"key": f"s{s}t{t}", "session": s, "turn": t, "session_id": f"s{s:03d}", "event_id": f"evt_{prefix}{s}{t}"})
    return {"source": "longmemeval", "question_id": q["question_id"], "turns": turns, "steps": []}


def ask_line(q: dict, meta: dict, reached: set[str], dropped=()) -> dict:
    ev = {t["key"]: t["event_id"] for t in meta["turns"]}
    turns = []
    for s, sess in enumerate(q["haystack_sessions"]):
        for t, tr in enumerate(sess):
            if tr.get("has_answer"):
                k = f"s{s}t{t}"
                turns.append({"key": k, "event_id": ev[k], "coverage": 1.0 if k in reached else 0.0, "reached": k in reached})
    return {"question_id": q["question_id"], "condition": "D", "context": "...", "context_tokens": 100, "question": q["question"],
            "gold": q["answer"], "question_type": q["question_type"], "items": [],
            "evidence": {"turns": turns, "scored": bool(turns), "reached": sum(t["reached"] for t in turns), "full": all(t["reached"] for t in turns)},
            "dropped_superseded": list(dropped)}


def listing(pairs=(), sups=()):
    """A conflicts list output in a plausible shape: pairs with a nested
    verdict, supersession rows; wrapped as `lme study conflicts -out` saves it."""
    return {"db": "x", "tool": "conflicts", "action": "list",
            "output": {"pending": [p for p in pairs if p.get("status") == "pending"],
                       "judged": [p for p in pairs if p.get("status") != "pending"],
                       "supersessions": list(sups), "counts": {"pending": 0}}}


def pair(o, n, status="judged", verdict=None, conf=0.9, reason=None):
    p = {"older_id": o, "newer_id": n, "cosine": 0.84, "differs": "entities", "shared": ["live"], "status": status}
    if verdict:
        p["verdict"] = {"verdict": verdict, "current": "B", "confidence": conf}
        p["reason"] = reason or ("applied" if verdict == "supersedes" else verdict)
    return p


def sup(o, n, conf=0.9, reverted=None):
    return {"id": f"sup_{o}_{n}", "old_id": o, "new_id": n, "confidence": conf, "reverted_at": reverted}


def write_json(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        json.dump(obj, f)


def write_jsonl(path, rows):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


def sleep_result(calls=3, tin=3000, tout=120, wall=2500.0):
    return {"db": "x", "condition": "C1", "phases": [1, 2], "wall_ms": wall,
            "output": {"run_id": "slp_1", "status": "completed", "phases": [1, 2], "cost": {"llm_calls": calls, "input_tokens": tin, "output_tokens": tout}},
            "counts": {"conflicts_judged": calls}}
