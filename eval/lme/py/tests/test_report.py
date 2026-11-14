from __future__ import annotations

import csv
import os

import pytest

import report


@pytest.mark.parametrize("k,n,lo,hi", [
    # Reference values for n=10: statsmodels proportion_confint(k, 10,
    # method="wilson"), alpha 0.05 (e.g. 5/10 -> 0.236593, 0.763407). For
    # n=60: the roots of (p - k/n)^2 = z^2 p (1 - p) / n, solved separately.
    (0, 10, 0.0, 0.27753),
    (1, 10, 0.01788, 0.40415),
    (5, 10, 0.23659, 0.76341),
    (10, 10, 0.72247, 1.0),
    (30, 60, 0.37735, 0.62265),
    (45, 60, 0.62768, 0.84223),
])
def test_wilson_known_values(k, n, lo, hi):
    got = report.wilson(k, n)
    assert got[0] == pytest.approx(lo, abs=6e-5)
    assert got[1] == pytest.approx(hi, abs=6e-5)


def test_wilson_closed_form_symmetry():
    for n in (1, 7, 60):
        for k in range(n + 1):
            lo, hi = report.wilson(k, n)
            lo2, hi2 = report.wilson(n - k, n)
            assert lo == pytest.approx(1 - hi2)
            assert 0 <= lo <= k / n <= hi <= 1


def test_percentile():
    assert report.percentile([5, 1, 3, 2, 4], 50) == 3
    assert report.percentile(list(range(1, 101)), 95) == 95
    assert report.percentile([None, None], 50) is None


def _rec(qid, qtype, cond, verdict, answer="a"):
    return {"question_id": qid, "question_type": qtype, "condition": cond, "question": "Q " + qid,
            "gold_answer": "G", "answer": answer, "verdict": verdict, "context_tokens": 100,
            "retrieval_latency_ms": 10.0, "ingest_seconds": 1.0,
            "reader": {"tokens": {"prompt": 10, "cache_hit": 0, "completion": 5}},
            "judge": {"tokens": {"prompt": 7, "cache_hit": 0, "completion": 1}}}


def test_summarize_and_review(tmp_path):
    by = {
        "A": [_rec("q1", "multi-session", "A", True), _rec("q2_abs", "multi-session", "A", False)],
        "B": [_rec("q1", "multi-session", "B", True, "b1"), _rec("q2_abs", "multi-session", "B", True, "b2")],
    }
    cost = {"spent_usd": 0.5, "cap_usd": 8, "by_tag": {
        "A-reader": {"cost_usd": 0.1}, "A-judge": {"cost_usd": 0.01, "unverified_usd": 0.001},
        "B-reader": {"cost_usd": 0.2}, "C-ingest": {"cost_usd": 3}}}
    s = report.summarize(by, cost)
    a = s["conditions"]["A"]
    assert a["total"]["correct"] == 1 and a["total"]["n"] == 2
    assert a["by_type"]["abstention"]["n"] == 1 and a["by_type"]["abstention"]["correct"] == 0
    assert a["cost_usd"] == pytest.approx(0.111)
    assert a["tokens"]["reader"]["prompt"] == 20
    assert s["conditions"]["B"]["total"]["accuracy"] == 1.0
    md = report.markdown(s)
    assert "| **total** |" in md and "50.0%" in md

    path = os.path.join(tmp_path, "review.csv")
    report.write_review(path, by, order=["q2_abs", "q1"])
    rows = list(csv.DictReader(open(path)))
    assert [r["question_id"] for r in rows] == ["q2_abs", "q1"]
    assert rows[0]["abstention"] == "yes"
    assert rows[0]["answer_B"] == "b2" and rows[0]["verdict_A"] == "no" and rows[0]["verdict_B"] == "yes"
    assert list(rows[0].keys()) == ["question_id", "question_type", "abstention", "question", "gold_answer",
                                    "answer_A", "verdict_A", "answer_B", "verdict_B"]
