"""eval/learn/tune_summary.py on a synthetic tuning directory."""

from __future__ import annotations

from helpers import load_module, write_jsonl

tune = load_module("learn_tune_summary", "tune_summary.py")
DEV = ["conv-30", "conv-47", "conv-49"]


def line(conv, i, full, keyed=None, ev_ids=()):
    items = [{"id": f"evt_{i}"}]
    if keyed:
        items.append({"id": keyed, "key_rank": 1})
    turns = [{"key": "D1:1", "event_id": e, "reached": full} for e in (ev_ids or [f"evt_{i}"])]
    return {"question_id": f"{conv}-q{i}", "items": items, "key_candidates": 1 if keyed else 0,
            "evidence": {"scored": True, "turns": turns, "reached": len(turns) if full else 0, "full": full}}


def variant(root, name, sets, fulls, keyed=None):
    vd = root / name
    vd.mkdir(parents=True)
    (vd / "sets.txt").write_text("".join(f"{k}={v}\n" for k, v in sets.items()))
    for conv in DEV:
        write_jsonl(vd / f"{conv}.test.ask.jsonl",
                    [line(conv, i, f, keyed=(keyed if i == 0 else None), ev_ids=(["evt_new"] if keyed and i == 0 else ())) for i, f in enumerate(fulls)])


def test_grid_summary_and_selection(tmp_path, capsys):
    t = tmp_path / "tune"
    variant(t, "U0", {"ranking.utility_lambda": 0}, [1, 0, 0])
    variant(t, "U1-l0.1-m1", {"ranking.utility_lambda": 0.1, "ranking.min_exposures": 1}, [1, 1, 0])
    variant(t, "U1-l0.2-m5", {"ranking.utility_lambda": 0.2, "ranking.min_exposures": 5}, [1, 1, 0])
    variant(t, "U1-l0.1-m5", {"ranking.utility_lambda": 0.1, "ranking.min_exposures": 5}, [1, 1, 0])
    variant(t, "U1-l0.3-m2", {"ranking.utility_lambda": 0.3, "ranking.min_exposures": 2}, [1, 0, 0])
    ks = {"ranking.utility_lambda": 0.1, "ranking.min_exposures": 5, "read.learned_keys": "true"}
    variant(t, "K-w0.5-k10", {**ks, "read.learned_keys_weight": 0.5, "read.learned_keys_top_k": 10}, [1, 1, 1], keyed="evt_new")
    variant(t, "K-w1-k20", {**ks, "read.learned_keys_weight": 1, "read.learned_keys_top_k": 20}, [1, 1, 1], keyed="evt_new")
    variant(t, "K-alone", {"ranking.utility_lambda": 0, "read.learned_keys": "true"}, [1, 1, 1, 1], keyed="evt_new")
    res = tune.summarise(tune.collect(str(t), DEV), DEV)
    v = res["variants"]
    assert v["U0"]["pooled"] == {"questions": 9, "scored": 9, "full": 3, "mean_turn_recall": 1 / 3}
    # Ties on full and recall go to the smaller lambda, then the larger min_exposures.
    assert res["best_U1"] == "U1-l0.1-m5"
    # Ties among K go to weight 1, top k 20; keys alone (lambda 0) is not selectable.
    assert res["best_K"] == "K-w1-k20"
    kb = v["K-w1-k20"]["key_brought"]
    assert (kb["questions"], kb["questions_key_brought"], kb["records_key_brought"], kb["evidence_records"]) == (9, 3, 3, 3)
    assert v["K-alone"]["key_brought"]["questions"] == 9  # q3 has no U0 counterpart
    md = tune.markdown(res)
    assert "U1-l0.1-m5 **(best U1)**" in md and "K-w1-k20 **(best K)**" in md
    assert "ranking.utility_lambda=0.1\nranking.min_exposures=5\nread.learned_keys_weight=1\nread.learned_keys_top_k=20" in md
    assert tune.main(["--tune", str(t), "--best-u1"]) == 0
    assert capsys.readouterr().out.split() == ["0.1", "5"]


def test_incomplete_variant_is_not_selected(tmp_path):
    t = tmp_path / "tune"
    variant(t, "U1-l0.1-m1", {"ranking.utility_lambda": 0.1, "ranking.min_exposures": 1}, [1, 0])
    variant(t, "U1-l0.2-m1", {"ranking.utility_lambda": 0.2, "ranking.min_exposures": 1}, [1, 1])
    (t / "U1-l0.2-m1" / "conv-49.test.ask.jsonl").unlink()
    res = tune.summarise(tune.collect(str(t), DEV), DEV)
    assert res["best_U1"] == "U1-l0.1-m1" and not res["variants"]["U1-l0.2-m1"]["complete"]
    assert "(incomplete)" in tune.markdown(res)
    assert tune.main(["--tune", str(tmp_path / "none"), "--best-u1"]) == 1
