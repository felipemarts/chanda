"""eval/conflicts/report.py, tune.py, precheck.py and project_cost.py on
synthetic work directories."""

from __future__ import annotations

import json
import os

import conflicts_fixtures as F

report = F.load_module("conflicts_report_t", "report.py")
tune = F.load_module("conflicts_tune_t", "tune.py")
precheck = F.load_module("conflicts_precheck_t", "precheck.py")
cost = F.load_module("conflicts_project_cost_t", "project_cost.py")


def history(work, q, prefix, c0_reached, c1_reached, dropped=(), pairs=(), sups=(), sleep=None):
    d = os.path.join(str(work), q["question_id"])
    meta = F.meta_for(q, prefix)
    F.write_json(os.path.join(d, "base", "study.json"), meta)
    F.write_jsonl(os.path.join(d, "C0.ask.jsonl"), [F.ask_line(q, meta, set(c0_reached))])
    F.write_jsonl(os.path.join(d, "C1.ask.jsonl"), [F.ask_line(q, meta, set(c1_reached), dropped)])
    F.write_json(os.path.join(d, "C1.list.json"), F.listing(pairs, sups))
    F.write_json(os.path.join(d, "C0.list.json"), F.listing([F.pair(p["older_id"], p["newer_id"], status="pending") for p in pairs]))
    F.write_json(os.path.join(d, "C1.sleep.json"), sleep or F.sleep_result())
    return meta


def fixture(tmp_path, n_fixed=6, hidden_n=1, acc_drop=False):
    """n_fixed stale-measure KU questions whose outdated turn C1 no longer
    delivers (correct supersession), hidden_n questions with hidden evidence,
    one other-type question untouched."""
    data, ids = {}, []
    for i in range(n_fixed):
        q = F.ku_question(qid=f"ku{i}")
        data[q["question_id"]] = q
        ids.append(q["question_id"])
        p = f"k{i}"
        history(tmp_path, q, p, {"s0t0", "s2t0"}, {"s2t0"}, dropped=[f"evt_{p}00"],
                pairs=[F.pair(f"evt_{p}00", f"evt_{p}20", verdict="supersedes", conf=0.9)], sups=[F.sup(f"evt_{p}00", f"evt_{p}20")])
    for i in range(hidden_n):
        q = F.ku_question(qid=f"hid{i}")
        data[q["question_id"]] = q
        ids.append(q["question_id"])
        p = f"h{i}"
        history(tmp_path, q, p, {"s0t0", "s2t0"}, {"s0t0"}, dropped=[f"evt_{p}20"],
                pairs=[F.pair(f"evt_{p}00", f"evt_{p}20", verdict="supersedes", conf=0.75)], sups=[F.sup(f"evt_{p}20", f"evt_{p}00", conf=0.75)])
    o = F.other_question()
    data[o["question_id"]] = o
    ids.append(o["question_id"])
    history(tmp_path, o, "o", {"s0t0"}, {"s0t0"}, pairs=[F.pair("evt_o00", "evt_o10", verdict="coexist")])
    for c in ("C0", "C1"):
        rows = []
        for j, qid in enumerate(ids):
            v = True
            if c == "C1" and acc_drop and j < 6:
                v = False
            rows.append({"question_id": qid, "question_type": data[qid]["question_type"], "answer": "x", "verdict": v,
                         "reader": {"tokens": {"prompt": 2500, "cache_hit": 0, "completion": 150}},
                         "judge": {"tokens": {"prompt": 200, "cache_hit": 0, "completion": 2}}})
        F.write_jsonl(os.path.join(str(tmp_path), f"llm-{c}", "D.jsonl"), rows)
        F.write_jsonl(os.path.join(str(tmp_path), f"llm-{c}", "D.answers.jsonl"), rows)
    calls = os.path.join(str(tmp_path), "calls.jsonl")
    F.write_jsonl(calls, [{"tag": f"C1-sleep-{qid}", "outcome": "forwarded", "prompt_tokens": 3000, "cache_hit_tokens": 0, "completion_tokens": 100}
                          for qid in ids])
    return data, ids, calls


def test_primary_measures_and_decision(tmp_path):
    data, ids, calls = fixture(tmp_path)
    res = report.build(str(tmp_path), data, ids, {"spent_usd": 0.1, "cap_usd": 2}, calls)
    s = res["primary"]["stale"]
    # 7 stale-measure questions: all stale in C0; the 6 fixed ones are not in C1.
    assert (s["n"], s["first_true"], s["second_true"], s["first_only"], s["second_only"]) == (7, 7, 1, 6, 0)
    assert round(s["p"], 5) == 0.03125
    h = res["primary"]["hidden"]
    assert h["questions"] == 1 and h["cases"][0]["question_id"] == "hid0" and h["cases"][0]["verdict"] == "supersedes"
    assert "Botafogo" in h["cases"][0]["old"]["text"]
    se = res["secondary"]
    assert se["accuracy"]["C0"]["correct"] == 8 and se["accuracy"]["C1"]["correct"] == 8 and se["accuracy"]["paired"]["p"] == 1.0
    assert se["applied_by_class"] == {"correct": 6, "wrong": 1}
    assert se["ku_pairs"]["correct"] == 6 and se["ku_pairs"]["reversed"] == 1 and se["ku_pairs"]["scored"] == 7
    assert se["by_type"]["knowledge-update"]["verdicts"] == {"supersedes": 7} and se["by_type"]["single-session-user"]["verdicts"] == {"coexist": 1}
    assert se["sleep"]["llm_calls"]["mean"] == 3 and se["sleep"]["cost_usd"]["n"] == 8 and se["sleep"]["cost_usd"]["max"] > 0
    assert len(res["examples"]["correct"]) == 5 and len(res["examples"]["wrong"]) == 1
    d = res["decision"]
    # Only 8 of 107 questions: the rule is not evaluated, whatever the conditions say.
    assert not d["complete"] and not d["established"]
    assert [c["holds"] for c in d["conditions"]] == [True, True, True]
    md = report.markdown(res)
    assert "incomplete" in md and "Five correct supersessions" in md and "hid0" in md


def test_decision_conditions(tmp_path, monkeypatch):
    monkeypatch.setattr(report, "TOTAL", 8)
    data, ids, calls = fixture(tmp_path)
    d = report.build(str(tmp_path), data, ids, {}, calls)["decision"]
    assert d["complete"] and d["established"]
    assert report.NOT_ESTABLISHED not in report.markdown(report.build(str(tmp_path), data, ids, {}, calls))


def test_decision_fails(tmp_path, monkeypatch):
    # Too few discordant stale pairs (p = 0.0625 > 0.05), 3 hidden questions,
    # C1 six answers worse (p = 0.03125).
    monkeypatch.setattr(report, "TOTAL", 9)
    data, ids, calls = fixture(tmp_path, n_fixed=5, hidden_n=3, acc_drop=True)
    res = report.build(str(tmp_path), data, ids, {}, calls)
    d = res["decision"]
    assert d["complete"] and not d["established"]
    assert [c["holds"] for c in d["conditions"]] == [False, False, False]
    assert report.NOT_ESTABLISHED in report.markdown(res)


def test_tune_summary(tmp_path):
    data, ids, _ = fixture(tmp_path / "test")
    dev, tdir = tmp_path / "dev", tmp_path / "tune"
    lab = tune.label("0.80:1:0.7")
    for qid in ids:
        src = tmp_path / "test" / qid
        F.write_json(str(dev / qid / "base" / "study.json"), json.load(open(src / "base" / "study.json")))
        (dev / qid / "D2.ask.jsonl").write_text((src / "C0.ask.jsonl").read_text())
        d = tdir / lab / qid
        d.mkdir(parents=True)
        (d / "ask.jsonl").write_text((src / "C1.ask.jsonl").read_text())
        (d / "list.json").write_text((src / "C1.list.json").read_text())
        (d / "sleep.json").write_text((src / "C1.sleep.json").read_text())
    p = tune.point(str(tdir), str(dev), data, ids + ["missing1"], lab, {"by_tag": {f"conflicts-dev-{lab}-sleep": {"cost_usd": 0.01, "calls": 24}}})
    assert p["histories"] == 8 and p["missing"] == ["missing1"]
    assert p["stale"]["first_true"] == 7 and p["stale"]["second_true"] == 1 and p["hidden_questions"] == 1
    assert p["ku_pairs"]["verdicts"] == {"supersedes": 7} and p["ku_pairs"]["correct"] == 6
    assert p["sleep"]["cost_usd"] == 0.01 and "accuracy" not in p
    assert lab in tune.markdown([p])


def test_precheck(tmp_path):
    q = F.ku_question(qid="ku0")
    o = F.other_question()
    for x, pre in ((q, "a"), (o, "b")):
        meta = F.meta_for(x, pre)
        d = tmp_path / x["question_id"]
        F.write_json(str(d / "base" / "study.json"), meta)
        F.write_jsonl(str(d / "D2.ask.jsonl"), [F.ask_line(x, meta, {"s0t0", "s2t0"} if x is q else {"s0t0"})])
        for mc, cands in (("0.75", 4), ("0.80", 2)):
            out = {"candidates": [{"older_id": f"evt_{pre}0{i}", "newer_id": f"evt_{pre}1{i}", "cosine": 0.8} for i in range(cands)],
                   "near_dup_refusals": [{"older_id": "e1", "newer_id": "e2", "cosine": 0.97, "reason": "numbers"}], "candidates_total": cands}
            if x is q and mc == "0.75":
                out["candidates"].append({"older_id": "evt_a00", "newer_id": "evt_a20", "cosine": 0.77})
                out["near_dup_refusals"].append({"older_id": "evt_a20", "newer_id": "evt_a00", "cosine": 0.95, "reason": "dates"})
            F.write_json(str(d / f"scan-{mc}.json"), {"tool": "conflicts", "action": "scan", "output": out})
    res = precheck.build(str(tmp_path), {"ku0": q, o["question_id"]: o}, ["ku0", o["question_id"], "gone"], ["0.75", "0.80"])
    assert res["missing"] == ["gone"] and res["questions"] == 2
    a = res["aggregate"]
    assert a["0.75"]["qualifying_ku"] == {"n": 1, "pair_candidate": 1, "refusal_links_pair": 1, "same_event": 0}
    assert a["0.80"]["qualifying_ku"]["pair_candidate"] == 0
    assert a["0.75"]["knowledge_update"]["candidates"]["max"] == 5 and a["0.75"]["other"]["candidates"]["max"] == 4
    assert a["0.75"]["knowledge_update"]["refusals_by_reason"] == {"dates": 1, "numbers": 1}
    assert res["d2"] == {"stale_measure_questions": 1, "outdated_delivered": 1, "outdated_without_current": 0, "current_delivered": 1}
    row = [r for r in res["rows"] if r["question_id"] == "ku0"][0]
    assert row["by_min_cosine"]["0.75"]["refusal_link_reasons"] == ["dates"]
    assert "0.75" in precheck.markdown(res)


def test_smoke_projection(tmp_path):
    data, ids, calls = fixture(tmp_path)
    # The fixture's ids stand in for the first test questions.
    sets = {"test": {"knowledge_update": ids + [f"later{i}" for i in range(99)], "other": []}}
    F.write_json(str(tmp_path / "sets.json"), sets)
    r = cost.rates(window="offpeak")
    per = cost.usd_tokens(r, 3000, 0, 100) + 2 * (cost.usd_tokens(r, 2500, 0, 150) + cost.usd_tokens(r, 200, 0, 2))
    a = type("A", (), {"work": str(tmp_path), "smoke": 8, "calls": calls, "sets": str(tmp_path / "sets.json"), "window": "offpeak",
                       "cost": None, "cap": 2.0, "md": str(tmp_path / "smoke.md"), "json": None})
    res = cost.test(a)
    assert res["smoke_done"] == 8 and res["projection"]["remaining"] == 99
    assert abs(res["projection"]["per_history_mean"] - per) < 1e-12
    assert abs(res["projection"]["total_upper"] - 99 * per) < 1e-9 and not res["over_cap"]
    assert res["rows"][0]["judged"] == 1 and "within the cap" in open(tmp_path / "smoke.md").read()
    a.cap = 0.01
    assert cost.test(a)["over_cap"]


def test_tune_projection(tmp_path):
    pre = {"rows": [{"question_id": "q1", "by_min_cosine": {"0.80": {"candidates": 10}, "0.75": {"candidates": 30}}},
                    {"question_id": "q2", "by_min_cosine": {"0.80": {"candidates": 4}}}]}
    F.write_json(str(tmp_path / "pre.json"), pre)
    a = type("A", (), {"precheck": str(tmp_path / "pre.json"), "grid": "0.80:1:0.7 0.75:2:0.7", "ids": "q1 q2", "tune": str(tmp_path),
                       "extra": "sleep.conflicts.max_pairs=20", "qa": False, "cost": None, "cap": 2.0, "window": "offpeak"})
    res = cost.tune(a)
    assert [x["pairs"] for x in res["rows"]] == [14, 24] and not res["over_cap"]
