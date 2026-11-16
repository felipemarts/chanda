"""eval/learn/report.py statistics and tables on a synthetic work directory;
the cost projection's arithmetic. No network, no dataset."""

from __future__ import annotations

import json
import sqlite3

from helpers import CONVS, load_module, make_work, write_jsonl

report = load_module("learn_report", "report.py")
cost = load_module("learn_project_cost", "project_cost.py")


def test_statistics_are_the_sleep_studys():
    assert round(report.sign_test(24, 10), 3) == 0.024
    assert round(report.sign_test(35, 20), 3) == 0.058
    assert report.sign_test(6, 0) == 0.03125 and report.sign_test(0, 0) == 1.0
    lo, hi = report.wilson(248, 300)
    assert (round(100 * lo, 1), round(100 * hi, 1)) == (78.0, 86.5)
    assert report.quantile([1.0, 2.0, 3.0, 4.0], 0.5) == 2.0 and report.quantile([1.0, 2.0, 3.0, 4.0], 0.95) == 4.0


def build(tmp_path, **kw):
    w = make_work(tmp_path, **kw)
    calls = tmp_path / "calls.jsonl"
    write_jsonl(calls, [{"tag": "base-use-reader", "price_window": "offpeak", "outcome": "forwarded"},
                        {"tag": "K-test-judge", "price_window": "peak", "outcome": "forwarded"},
                        {"tag": "U0-test-reader", "price_window": "peak", "outcome": "replayed"}])
    c = {"spent_usd": 0.5, "cap_usd": 4, "total": {"replayed": 1},
         "by_tag": {"K-test-judge": {"calls": 1, "cost_usd": 0.01, "unverified_usd": 0.001, "replayed": 0}}}
    return w, report.build(str(w), CONVS, c, str(calls))


def test_claims_and_decisions(tmp_path):
    _, res = build(tmp_path)
    ta = res["test_accuracy"]
    assert ta["U0"]["total"]["correct"] == 6 and ta["U0"]["total"]["n"] == 12 and ta["K"]["total"]["accuracy"] == 1.0
    assert ta["B"]["by_conversation"]["conv-a"]["correct"] == 1 and ta["U0"]["by_category"]["multi-hop"]["n"] == 4
    a, b = res["claims"]["A"], res["claims"]["B"]
    assert (a["first"], a["second"], a["first_only"], a["second_only"], a["n"]) == ("U0", "B", 4, 0, 12)
    assert round(a["p"], 3) == 0.125 and not a["established"] and "not established" in a["decision"]
    assert (b["first_only"], b["second_only"]) == (6, 0) and b["p"] == 0.03125 and b["established"]
    assert "Claim B" in b["decision"] and "**established**" in b["decision"]
    s = res["secondary"]
    assert (s["U1 vs U0"]["first_only"], s["U1 vs U0"]["second_only"], s["U1 vs U0"]["p"]) == (2, 2, 1.0)
    assert (s["K vs U1"]["first_only"], s["K vs U1"]["second_only"]) == (6, 0)
    md = report.markdown(res)
    assert "No correction for multiple tests is applied" in md
    assert report.NOT_B not in md  # claim B established here
    assert "| A: Chanda is more accurate than plain RAG | U0 vs B | 12 | 4 | 0 | 0.125 | not established |" in md
    assert "**Too good:**" in md  # a gain of 6 over a tight ceiling of 2


def test_claim_b_not_established_text(tmp_path):
    v = {"B": [1, 0, 0, 0, 0, 0], "U0": [1, 1, 1, 0, 0, 0], "U1": [1, 1, 1, 0, 0, 0], "K": [1, 1, 1, 0, 0, 1]}
    _, res = build(tmp_path, verdicts=v)
    b = res["claims"]["B"]
    assert (b["first_only"], b["second_only"], b["established"]) == (2, 0, False)
    md = report.markdown(res)
    assert report.NOT_B in md and "keys and digestion stay off by default" in md
    assert "**Too good:**" not in md  # 2 does not exceed the tight ceiling of 2


def test_keys_ceiling_latency_sizes_calls(tmp_path):
    w, _ = build(tmp_path)
    for conv in CONVS:
        for c in ("K", "K.nokeys"):
            p = w / conv / c
            p.mkdir(parents=True)
            db = sqlite3.connect(p / "chanda.db")
            db.execute("CREATE TABLE t (x TEXT)")
            db.executemany("INSERT INTO t VALUES (?)", [("x" * 1000,)] * (200 if c == "K" else 5))
            db.commit()
            db.close()
    res = report.build(str(w), CONVS, None, str(tmp_path / "no-calls.jsonl"))
    kb = res["keys"]["key_brought"]
    assert (kb["questions"], kb["questions_key_brought"], kb["records_key_brought"], kb["evidence_records"],
            kb["questions_with_evidence_brought"], kb["questions_with_key_candidates"]) == (12, 4, 4, 2, 2, 6)
    assert round(kb["share"], 4) == round(4 / 12, 4)
    pc = res["keys"]["per_conversation"]["conv-a"]
    assert pc["keys_created"] == 1 and pc["positive_outcomes"] == 1 and pc["key_brought"]["questions_key_brought"] == 2
    t = res["overlap_ceiling"]["total"]
    assert t == {"test": 12, "scored": 12, "used_any": 6, "used_positive": 4, "delivered_any": 8, "missing_evidence": 4,
                 "missing_used_positive": 4, "wrong_U0": 6, "tight": 2}
    assert res["overlap_ceiling"]["tight_question_ids"] == ["conv-a-q3", "conv-b-q3"]
    assert res["too_good"] == {"gain_questions": 6, "tight_ceiling_questions": 2, "flag": True}
    lat = res["latency"]["U0"]
    assert (lat["n"], lat["mean_ms"], lat["p50_ms"], lat["p95_ms"]) == (12, 12.5, 12.0, 15.0)
    fe = res["full_evidence_test"]
    assert (fe["U0"]["full"], fe["K"]["full"], fe["U0"]["scored"]) == (8, 10, 12)
    sz = res["db_bytes"]["conv-a"]
    assert sz["K_compact"] > sz["K_nokeys_compact"] > 0 and sz["U0"] is None
    lc = res["llm_calls"]
    assert lc["call_log"] is None and lc["learning_steps_llm_free"]
    assert set(lc["sleep_llm_calls"]) == {"U1/conv-a", "U1/conv-b", "K/conv-a", "K/conv-b"}
    lk = res["leak_checks"]
    assert lk["all_ok"] and len(lk["per_check"]) == 8 and len(lk["listed_gold_in_use_question"]) == 2


def test_call_log_tags_and_peak(tmp_path):
    w, res = build(tmp_path)
    lc = res["llm_calls"]
    assert lc["calls"] == 3 and lc["other_tags"] == {} and lc["peak_calls"] == {"K-test-judge": 1} and lc["learning_steps_llm_free"]
    md = report.markdown(res)
    assert "**The learning steps made zero LLM calls.**" in md and "Deviation: 1 calls were made in peak hours" in md
    assert "| K-test-judge | 1 | 0 | 0.0110 |" in md
    # A call from a learning step (any non reader/judge tag) is caught.
    calls = tmp_path / "calls2.jsonl"
    write_jsonl(calls, [{"tag": "U1-sleep-conv-a", "price_window": "offpeak", "outcome": "forwarded"}])
    res2 = report.build(str(w), CONVS, None, str(calls))
    assert not res2["llm_calls"]["learning_steps_llm_free"] and res2["llm_calls"]["other_tags"] == {"U1-sleep-conv-a": 1}
    # ... and so is a sleep run that reports LLM calls.
    with open(w / "conv-b" / "out" / "K.sleep.json", "w") as f:
        json.dump({"output": {"cost": {"llm_calls": 2}}}, f)
    assert not report.build(str(w), CONVS, None, None)["llm_calls"]["learning_steps_llm_free"]


def test_failed_leak_check_is_reported(tmp_path):
    w = make_work(tmp_path)
    with open(w / "conv-a" / "out" / "K.leak.json", "w") as f:
        json.dump({"ok": False, "failures": ["1 test question text in the database"], "keys": 1}, f)
    res = report.build(str(w), CONVS, None, None)
    assert not res["leak_checks"]["all_ok"]
    assert "- K/conv-a: 1 test question text in the database" in report.markdown(res)


def test_cost_projection(tmp_path, capsys):
    c = tmp_path / "cost.json"
    c.write_text(json.dumps({"spent_usd": 0.0}))
    assert cost.main(["--cost", str(c), "--json"]) == 0
    res = json.loads(capsys.readouterr().out)
    assert res["questions"] == 569 + 4 * 584
    off, peak = res["totals"]["offpeak"], res["totals"]["peak"]
    assert 0.5 < off["expected"] < off["upper"] < 4.0 and abs(peak["upper"] - 2 * off["upper"]) < 1e-9
    # Answered questions (final or .part) are not counted again.
    w = tmp_path / "w"
    write_jsonl(w / "conv-26" / "out" / "K.test.answers.jsonl.part", [{"question_id": f"q{i}"} for i in range(77)])
    assert cost.main(["--cost", str(c), "--work", str(w), "--json"]) == 0
    assert json.loads(capsys.readouterr().out)["questions"] == 569 + 4 * 584 - 77
    # The gate uses the chosen window's upper bound.
    c.write_text(json.dumps({"spent_usd": 2.0}))
    assert cost.main(["--cost", str(c)]) == 0
    assert cost.main(["--cost", str(c), "--window", "peak"]) == 3
    assert "ask before running" in capsys.readouterr().out
