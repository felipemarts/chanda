from __future__ import annotations

import json
import os

import pytest

import common
import data
import estimate


def test_prior_reader_cost_by_hand():
    res = estimate.prior(estimate.main.__globals__["argparse"].Namespace(
        prices=estimate.DEFAULT_PRICES, model="deepseek-flash", n=60, smoke_n=10, cap=8.0,
        dataset=None, sample=None, conditions=["B"], ctx_tokens=2000, reader_out=350, judge_out=2,
        mem0_out=400, mem0_existing=300, mem0_overhead=150, cache_hit=0.9, e_in_factor=1.15,
        e_out_ratio=0.1, e_budget_tokens=200000, tok_ratio=1.0))
    row = {r["role"]: r for r in res["rows"]}["reader"]
    # off-peak: miss 0.15/M, out 0.60/M
    want = (row["miss_per_q"] * 0.15 + 350 * 0.60) / 1e6 * 60
    assert row["offpeak_usd"] == pytest.approx(want, rel=1e-3)
    assert row["peak_usd"] == pytest.approx(2 * row["offpeak_usd"], rel=1e-9)
    assert 2000 < row["miss_per_q"] < 2200


def test_prior_cli_with_dataset(tmp_path, capsys):
    ds, sample = data.write(str(tmp_path))
    assert estimate.main(["prior", "--dataset", ds, "--sample", sample, "--json"]) == 0
    res = json.loads(capsys.readouterr().out)
    roles = {(r["condition"], r["role"]) for r in res["rows"]}
    assert ("C", "ingest") in roles and ("E", "sleep") in roles and ("A", "reader") in roles
    c = [r for r in res["rows"] if r["role"] == "ingest"][0]
    assert c["calls_per_q"] == pytest.approx(4 / 3)
    assert res["total_peak_usd"] == pytest.approx(2 * res["total_offpeak_usd"])


def test_measured_projection(tmp_path, capsys):
    log = os.path.join(tmp_path, "calls.jsonl")
    for i in range(20):  # 10 smoke questions x 2 extraction calls
        common.append_jsonl(log, {"tag": "C-ingest", "outcome": "forwarded", "cache_hit_tokens": 1000,
                                  "cache_miss_tokens": 1000, "completion_tokens": 100, "cost_usd": 0.0002})
    common.append_jsonl(log, {"tag": "C-ingest", "outcome": "refused"})
    assert estimate.main(["measured", "--log", log, "--json"]) == 0
    res = json.loads(capsys.readouterr().out)
    row = res["rows"][0]
    assert row["calls"] == 20 and row["calls_per_q"] == 2
    per_q = 2 * (1000 * 0.003 + 1000 * 0.15 + 100 * 0.6) / 1e6
    assert res["projected_full_offpeak_usd"] == pytest.approx(per_q * 60)
    assert res["remaining_if_smoke_reused_offpeak_usd"] == pytest.approx(per_q * 50)
    assert res["billed_so_far_usd"] == pytest.approx(0.004)
