"""eval/conflicts/suite/score.py on a synthetic work directory built from
the real pairs.json (no database, no LLM)."""

from __future__ import annotations

import json
import os

import conflicts_fixtures as F

score = F.load_module("conflicts_suite_score_t", os.path.join("suite", "score.py"))
PAIRS = json.load(open(os.path.join(F.CONF, "suite", "pairs.json")))["pairs"]


def work(tmp_path, false_controls: int, verdicts=True):
    for p in PAIRS:
        d = tmp_path / p["id"]
        o, n = f"evt_{p['id']}o", f"evt_{p['id']}n"
        meta = {"turns": [{"key": "s0t0", "session": 0, "turn": 0, "event_id": o}, {"key": "s0t1", "session": 0, "turn": 1, "event_id": "evt_r"},
                          {"key": "s1t0", "session": 1, "turn": 0, "event_id": n}, {"key": "s1t1", "session": 1, "turn": 1, "event_id": "evt_r"}]}
        F.write_json(str(d / "db" / "study.json"), meta)
        idx = int(p["id"][1:])
        detected = p["kind"] == "update" and idx <= 18 or p["kind"] == "control" and idx <= 10
        F.write_json(str(d / "queue.json"), F.listing([F.pair(o, n, status="pending")] if detected else []))
        if not verdicts:
            continue
        pairs, sups = [], []
        if detected:
            if p["kind"] == "update":
                v = "supersedes" if idx <= 15 else "uncertain"
                pairs.append(F.pair(o, n, verdict=v))
                if v == "supersedes":
                    sups.append(F.sup(o, n))
            else:
                v = "supersedes" if idx <= false_controls else "coexist"
                pairs.append(F.pair(o, n, verdict=v))
                if v == "supersedes":
                    sups.append(F.sup(o, n))
        F.write_json(str(d / "after.json"), F.listing(pairs, sups))
        F.write_json(str(d / "sleep.json"), F.sleep_result(calls=1 if detected else 0))


def test_score_counts(tmp_path):
    work(tmp_path, false_controls=2)
    res = score.build(str(tmp_path), PAIRS, "e5", {"by_tag": {"conflicts-suite": {"cost_usd": 0.002}}})
    u, c = res["updates"], res["controls"]
    assert res["pairs"] == 40 and res["verdicts_ran"]
    assert u["n"] == 20 and u["detected"] == 18 and u["applied"] == 15 and u["verdicts_detected"] == {"supersedes": 15, "uncertain": 3}
    assert c["detected"] == 10 and c["false_supersessions"] == 2 and c["verdicts_detected"] == {"coexist": 8, "supersedes": 2}
    assert not res["stop"] and res["sleep"]["cost_usd"] == 0.002 and res["sleep"]["llm_calls"] == 28
    assert "false supersessions 2/20" in score.markdown(res)


def test_stop_above_two(tmp_path):
    work(tmp_path, false_controls=3)
    out = tmp_path / "out"
    out.mkdir()
    rc = score.main(["--work", str(tmp_path), "--md", str(out / "r.md"), "--json", str(out / "r.json")])
    assert rc == 4
    assert json.load(open(out / "r.json"))["stop"] and "STOP" in open(out / "r.md").read()


def test_detection_only(tmp_path):
    work(tmp_path, false_controls=0, verdicts=False)
    res = score.build(str(tmp_path), PAIRS, "hash", {})
    assert not res["verdicts_ran"] and res["updates"]["detected"] == 18 and res["updates"]["verdicts_detected"] == {"no sleep run": 18}
    assert not res["stop"] and "detection only" in score.markdown(res)
