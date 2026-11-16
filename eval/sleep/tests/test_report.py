"""report.py statistics and tables on a synthetic work directory; the cost
projection's arithmetic. No network."""

from __future__ import annotations

import json
import os

import project_cost
import report


def test_sign_test_matches_the_selection_study():
    # eval/selection/RESULTS.md: 24 vs 10 -> 0.024, 35 vs 20 -> 0.058, 27 vs 26 -> 1.0
    assert round(report.sign_test(24, 10), 3) == 0.024
    assert round(report.sign_test(35, 20), 3) == 0.058
    assert report.sign_test(27, 26) == 1.0
    assert report.sign_test(0, 0) == 1.0
    assert report.sign_test(5, 0) == 0.0625


def test_wilson_matches_the_selection_study():
    lo, hi = report.wilson(248, 300)  # 82.7% [78.0, 86.5]
    assert (round(100 * lo, 1), round(100 * hi, 1)) == (78.0, 86.5)
    assert report.wilson(0, 10)[0] == 0.0 and report.wilson(10, 10)[1] == 1.0


def write_jsonl(path, rows):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


def test_build_on_a_synthetic_work_dir(tmp_path):
    w = tmp_path / "locomo"
    verdicts = {"B": [1, 0, 0, 1], "S0": [1, 0, 0, 0], "S1": [1, 1, 1, 0], "S2": [1, 1, 0, 0], "S3": [0, 0, 0, 0]}
    for conv in report.CONVS:
        od = w / conv / "out"
        write_jsonl(str(od / "base.use.answers.jsonl"), [{"question_id": f"{conv}-u{i}", "verdict": i % 2 == 0, "category": 4} for i in range(4)])
        for c, vs in verdicts.items():
            write_jsonl(str(od / f"{c}.test.answers.jsonl"),
                        [{"question_id": f"{conv}-q{i}", "verdict": bool(v), "category": 1 + i % 4, "verdict_parse": "exact"} for i, v in enumerate(vs)])
            write_jsonl(str(od / f"{c}.test.ask.jsonl"),
                        [{"question_id": f"{conv}-q{i}", "context_tokens": 1000, "sleep_items": int(c in ("S1", "S2")),
                          "sleep_tokens": 200 if c in ("S1", "S2") else 0, "sleep_share": 0.2 if c in ("S1", "S2") else 0.0,
                          "evidence": {"scored": i != 3, "full": i == 0, "full_or_cited": i <= 1, "reached": 1, "reached_or_cited": 2,
                                       "turns": [{}, {}]}} for i in range(4)])
            if c != "B":
                write_jsonl(str(od / f"{c}.ceiling.answers.jsonl"), [{"question_id": f"{conv}-u{i}", "verdict": True, "category": 4} for i in range(4)])
                tok = {"S2": (1000, 200), "S3": (1100, 100)}.get(c, (0, 0))
                guided = {"digest": "llm" if c == "S2" else "extractive", "candidate_groups": 5, "groups": 3, "groups_digested": 3,
                          "groups_skipped": 0, "groups_left": 0, "lines_written": 9, "lines_discarded": {"unsupported_name": 2}, "fichas": ["pg_1"]}
                with open(od / f"{c}.sleep.json", "w") as f:
                    json.dump({"output": {"status": "completed", "phases": [1] if c == "S0" else [1, 2],
                                          "cost": {"input_tokens": tok[0], "output_tokens": tok[1], "llm_calls": 3 if tok[0] else 0}},
                               "counts": {"guided": guided} if c in ("S1", "S2") else {}, "wall_ms": 1500}, f)
    cost = {"spent_usd": 0.1, "cap_usd": 2, "total": {"replayed": 4}, "by_tag": {"S2-sleep-conv-30": {"cost_usd": 0.01}}}
    res = report.build(str(w), cost)
    ta = res["test_accuracy"]
    assert ta["S1"]["total"]["correct"] == 9 and ta["S0"]["total"]["n"] == 12
    assert ta["S1"]["by_category"]["multi-hop"]["n"] == 3
    p = res["paired_vs_S0"]["S1"]
    assert (p["first_only"], p["second_only"], p["n"]) == (6, 0, 12) and round(p["p"], 4) == 0.0312 and p["rule_met"]
    assert res["paired_vs_S0"]["S2"]["rule_met"] is False  # 3 vs 0: p = 0.25
    assert "rule_met" not in res["paired_vs_S0"]["B"]
    assert res["full_evidence_test"]["S0"] == {"scored": 9, "full": 3, "full_or_cited": 6, "turns": 18, "turns_reached": 9,
                                               "turns_reached_or_cited": 18}
    s2 = res["sleep"]["S2"]
    assert s2["conv-30"]["cost_source"] == "proxy" and s2["conv-47"]["cost_source"] == "priced"
    assert s2["conv-30"]["lines_discarded"] == 2 and s2["conv-30"]["groups_digested"] == 3
    assert res["sleep"]["S0"]["conv-30"]["lines_written"] is None
    g = res["gain_per_dollar"]
    assert round(g["S1"]["gain_points"], 1) == 50.0 and g["S1"]["points_per_usd"] is None
    assert g["S2"]["sleep_usd"] > 0.01 and g["S2"]["points_per_usd"] > 0
    assert res["sleep_share_test"]["S1"]["pooled_share"] == 0.2 and res["sleep_share_test"]["S0"]["blocks_with_sleep_items"] == 0
    assert res["ceiling_use_half"]["S3"]["total"]["accuracy"] == 1.0 and res["use_half_before_sleep"]["correct"] == 6
    md = report.markdown(res)
    assert "| S1 vs S0 | 12 | 6 | 0 | 0.031 | **met** |" in md
    assert "not established" in md and "no correction" in md
    assert "## Ceiling" in md


def test_cost_projection(tmp_path, capsys):
    cost = tmp_path / "cost.json"
    cost.write_text(json.dumps({"spent_usd": 0.0}))
    rc = project_cost.main(["locomo", "--cost", str(cost), "--json"])
    res = json.loads(capsys.readouterr().out)
    assert rc == 0 and 0.3 < res["expected_usd"] < res["upper_usd"] < 2.0
    qa = [r for r in res["rows"] if r["step"] == "S1.test" and r["kind"] == "upper"]
    assert sum(r["questions"] for r in qa) == 197
    cost.write_text(json.dumps({"spent_usd": 1.9}))
    assert project_cost.main(["locomo", "--cost", str(cost)]) == 3
    assert "ask before running" in capsys.readouterr().out
    assert project_cost.main(["bench", "--cost", str(tmp_path / "none.json")]) == 0
