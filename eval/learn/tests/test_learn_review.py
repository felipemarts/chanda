"""eval/learn/review.py: the differing questions, the seeded sample of
judge-approved answers and the evidence texts, on synthetic fixtures."""

from __future__ import annotations

import json
import random

from helpers import load_module, write_json, write_jsonl

review = load_module("learn_review", "review.py")
CONVS = ["conv-a", "conv-b"]
N = 15  # test questions per conversation


def locomo(tmp):
    data = []
    for conv in CONVS:
        data.append({"sample_id": conv, "conversation": {
            "speaker_a": "Ana", "speaker_b": "Bruno", "session_1_date_time": "1:56 pm on 8 May, 2023",
            "session_1": [{"speaker": "Ana", "dia_id": f"D1:{i}", "text": f"{conv} turn {i}"} for i in range(1, N + 2)]
            + [{"speaker": "Bruno", "dia_id": "D1:99", "text": "a photo", "blip_caption": "a grey cat"}]},
            "qa": [], "observation": {"x": "NEVER READ"}})
    p = tmp / "locomo.json"
    write_json(p, data)
    return p


def verdicts(c, conv, i):
    if c == "U0":
        return i % 3 != 0          # 10 approved of 15
    if c == "K":
        return i % 3 != 0 or i == 3  # differs from U0 on q3 only
    return i % 2 == 0              # B: 8 approved of 15


def make(tmp):
    w = tmp / "work"
    for conv in CONVS:
        od = w / conv / "out"
        for c in ("U0", "K", "B"):
            write_jsonl(od / f"{c}.test.answers.jsonl", [
                {"question_id": f"{conv}-q{i}", "verdict": verdicts(c, conv, i), "question": f"Q{i} | pipe?", "gold": f"G{i}",
                 "answer": f"{c} says\n{i}", "category": 4, "category_name": "single-hop"} for i in range(N)])
            write_jsonl(od / f"{c}.test.ask.jsonl", [
                {"question_id": f"{conv}-q{i}", "evidence": {"turns": [{"key": f"D1:{i + 1}"}] + ([{"key": "D1:99"}] if i == 3 else []),
                                                             "invalid": ["D"] if i == 4 else []}} for i in range(N)])
    return w


def test_differing_and_sample(tmp_path):
    w = make(tmp_path)
    d = review.collect(str(w), CONVS)
    assert review.differing(d, CONVS) == ["conv-a-q3", "conv-b-q3"]
    s = review.sample_approved(d, CONVS)
    assert [c for c, _ in s] == ["U0"] * 10 + ["B"] * 10
    assert all(d["answers"][c][q]["verdict"] is True for c, q in s)
    assert len({q for c, q in s if c == "U0"}) == 10 and len({q for c, q in s if c == "B"}) == 10
    # Same draw as by hand: one Random(45), U0's sorted pool first, then B's.
    rng = random.Random(45)
    key = lambda q: (CONVS.index(q.rsplit("-q", 1)[0]), int(q.rsplit("-q", 1)[1]))
    u0 = sorted((f"{c}-q{i}" for c in CONVS for i in range(N) if verdicts("U0", c, i)), key=key)
    b = sorted((f"{c}-q{i}" for c in CONVS for i in range(N) if verdicts("B", c, i)), key=key)
    want = [("U0", q) for q in rng.sample(u0, 10)] + [("B", q) for q in rng.sample(b, 10)]
    assert s == want
    assert review.sample_approved(d, CONVS) == s  # deterministic
    assert review.sample_approved(d, CONVS, seed=46) != s


def test_small_pools_and_render(tmp_path):
    w = make(tmp_path)
    p = locomo(tmp_path)
    out = tmp_path / "review.md"
    assert review.main(["--work", str(w), "--locomo", str(p), "--convs", "conv-a", "--out", str(out)]) == 0
    md = out.read_text()
    assert "## 1. Test questions where K and U0 differ (1)" in md
    assert "- **U0** (WRONG): U0 says 3" in md and "- **K** (CORRECT): K says 3" in md
    assert "`D1:4` (1:56 pm on 8 May, 2023) Ana: conv-a turn 4" in md
    assert "Bruno: a photo [shares a photo: a grey cat]" in md
    assert "Q3 \\| pipe?" in md  # table-safe, one line
    assert "invalid evidence ids (not scored): D" in md
    assert "NEVER READ" not in md and "conv-b" not in md
    # One conversation: U0 has 10 approved, B 8; all of B's are taken.
    assert "(18: U0 then B)" in md
    d = review.collect(str(w), ["conv-a"])
    assert sum(1 for c, _ in review.sample_approved(d, ["conv-a"]) if c == "B") == 8
