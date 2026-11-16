"""eval/conflicts/measures.py on synthetic questions, ask lines and tool
outputs."""

from __future__ import annotations

import conflicts_fixtures as F

M = F.load_module("conflicts_measures_t", "measures.py")


def test_statistics():
    assert round(M.sign_test(24, 10), 3) == 0.024
    assert M.sign_test(6, 0) == 0.03125 and M.sign_test(0, 0) == 1.0
    assert M.sign_test(5, 0) == 0.0625
    lo, hi = M.wilson(248, 300)
    assert (round(100 * lo, 1), round(100 * hi, 1)) == (78.0, 86.5)
    assert M.wilson(0, 10)[0] == 0.0 and M.wilson(10, 10)[1] == 1.0
    p = M.paired_flags({"a": True, "b": True, "c": False, "x": True}, {"a": False, "b": True, "c": True})
    assert (p["n"], p["first_true"], p["second_true"], p["first_only"], p["second_only"]) == (3, 2, 2, 1, 1)
    assert M.summary([1, 2, 6]) == {"n": 3, "mean": 3.0, "median": 2, "max": 6, "total": 9}


def test_needed_and_stale():
    q = F.ku_question()
    assert M.ku_keys(q) == ("s0t0", "s2t0")
    assert M.needed_keys(q) == ["s2t0"]
    old = F.ku_question(question="Where did I live before?")
    assert M.needed_keys(old) == ["s0t0"]
    both = F.ku_question(question="Where did I live before, and where do I live now?")
    assert M.needed_keys(both) == ["s0t0", "s2t0"]
    same = F.ku_question(d0="2023/06/02 (Fri) 08:00", d1="2023/06/02 (Fri) 10:00")
    assert M.needed_keys(same) == ["s0t0", "s2t0"]
    o = F.other_question()
    assert M.needed_keys(o) == ["s0t0"] and M.ku_keys(o) is None
    meta = F.meta_for(q, "a")
    assert M.stale(q, F.ask_line(q, meta, {"s0t0", "s2t0"})) is True
    assert M.stale_without_current(q, F.ask_line(q, meta, {"s0t0", "s2t0"})) is False
    assert M.stale_without_current(q, F.ask_line(q, meta, {"s0t0"})) is True
    assert M.stale(q, F.ask_line(q, meta, {"s2t0"})) is False
    # Outside the stale measure: old-value and same-day questions, other types.
    for x in (old, same, o):
        assert M.stale(x, F.ask_line(x, F.meta_for(x, "b"), {"s0t0"})) is None


def test_hidden_counts_only_superseded_drops():
    q = F.ku_question()
    meta = F.meta_for(q, "a")
    c0 = F.ask_line(q, meta, {"s0t0", "s2t0"})
    # The needed (current) turn is missing from C1, dropped as superseded: hidden.
    c1 = F.ask_line(q, meta, {"s0t0"}, dropped=["evt_a20"])
    assert M.hidden(q, c0, c1) == [{"key": "s2t0", "event_id": "evt_a20"}]
    # Missing for another reason (not in the superseded drops): not hidden.
    assert M.hidden(q, c0, F.ask_line(q, meta, {"s0t0"}, dropped=["evt_a00"])) == []
    # Not in C0's block: not hidden either.
    assert M.hidden(q, F.ask_line(q, meta, {"s0t0"}), c1) == []
    # The outdated turn dropped is not needed evidence for a "now" question.
    assert M.hidden(q, c0, F.ask_line(q, meta, {"s2t0"}, dropped=["evt_a00"])) == []


def test_tool_output_normalisation():
    lst = F.listing(pairs=[F.pair("evt_1", "evt_2", verdict="supersedes", conf=0.92), F.pair("evt_3", "evt_4", status="pending"),
                           F.pair("evt_5", "evt_6", verdict="coexist")],
                    sups=[F.sup("evt_1", "evt_2"), F.sup("evt_7", "evt_8", reverted="2024-01-01T00:00:00Z")])
    ps = {(p["older_id"], p["newer_id"]): p for p in M.conflict_pairs(lst)}
    assert set(ps) == {("evt_1", "evt_2"), ("evt_3", "evt_4"), ("evt_5", "evt_6")}
    assert ps[("evt_1", "evt_2")]["verdict"] == "supersedes" and ps[("evt_1", "evt_2")]["confidence"] == 0.92
    assert ps[("evt_1", "evt_2")]["applied"] and not M.judged(ps[("evt_3", "evt_4")])
    assert M.verdict_tally(list(ps.values())) == {"coexist": 1, "supersedes": 1}
    sups = M.supersessions(lst)
    assert len(sups) == 2 and [s["old_id"] for s in M.applied_supersessions(lst)] == ["evt_1"]
    # A flat verdict string and a JSON-string verdict are read too.
    flat = {"output": {"pairs": [{"older_id": "a", "newer_id": "b", "status": "judged", "verdict": "uncertain", "confidence": 0.5,
                                  "reason": "uncertain:low_confidence"},
                                 {"older_id": "c", "newer_id": "d", "verdict": '{"verdict":"unrelated","current":"A","confidence":0.8}'}]},
            "tool": "conflicts"}
    fp = {p["older_id"]: p for p in M.conflict_pairs(flat)}
    assert fp["a"]["verdict"] == "uncertain" and not fp["a"]["applied"] and fp["c"]["verdict"] == "unrelated"
    # Without supersession rows, pairs marked applied stand for them.
    assert [s["old_id"] for s in M.applied_supersessions({"output": {"pairs": [F.pair("x", "y", verdict="supersedes")]}, "tool": "conflicts"})] == ["x"]
    # Refusals of a scan are not pairs; scan_lists keeps them apart.
    scan = {"tool": "conflicts", "output": {"candidates": [{"older_id": "a", "newer_id": "b", "cosine": 0.81}],
                                            "near_dup_refusals": [{"older_id": "a", "newer_id": "c", "cosine": 0.97, "reason": "numbers"}],
                                            "candidate_count": 1, "refusal_count": 1}}
    assert [p["newer_id"] for p in M.conflict_pairs(scan)] == ["b"]
    s = M.scan_lists(scan)
    assert len(s["candidates"]) == 1 and s["near_dup_refusals"][0]["reason"] == "numbers" and s["counts"]["candidate_count"] == 1


def test_ku_pair_and_classes():
    q = F.ku_question()
    meta = F.meta_for(q, "a")
    ev = M.turn_events(meta)
    good = F.listing(pairs=[F.pair("evt_a00", "evt_a20", verdict="supersedes")], sups=[F.sup("evt_a00", "evt_a20")])
    k = M.ku_pair_verdict(q, ev, M.conflict_pairs(good), M.applied_supersessions(good))
    assert k["candidate"] and k["judged"] and k["applied"] and k["correct"] is True
    cls = M.classify_applied(q, ev, M.applied_supersessions(good))
    assert [c["class"] for c in cls] == ["correct"]
    # Reversed: the current record superseded by the outdated one.
    bad = F.listing(pairs=[F.pair("evt_a00", "evt_a20", verdict="supersedes")], sups=[F.sup("evt_a20", "evt_a00")])
    k = M.ku_pair_verdict(q, ev, M.conflict_pairs(bad), M.applied_supersessions(bad))
    assert k["correct"] is False and k["applied_reversed"]
    c = M.classify_applied(q, ev, M.applied_supersessions(bad))[0]
    assert c["class"] == "wrong" and any("needed" in w for w in c["why"]) and any("reversed" in w for w in c["why"])
    # An evidence turn superseded by an unrelated record: wrong; two filler turns: unverified.
    odd = M.classify_applied(q, ev, [{"old_id": "evt_a00", "new_id": "evt_a10", "confidence": 0.9, "reverted": False},
                                     {"old_id": "evt_a10", "new_id": "evt_a11", "confidence": 0.9, "reverted": False}])
    assert [c["class"] for c in odd] == ["wrong", "unverified"]
    # Uncertain verdict: judged, not applied, not correct.
    unc = F.listing(pairs=[F.pair("evt_a00", "evt_a20", verdict="uncertain")])
    k = M.ku_pair_verdict(q, ev, M.conflict_pairs(unc), M.applied_supersessions(unc))
    assert k["judged"] and not k["applied"] and k["correct"] is False
    # Same-day pairs are not scored; non-qualifying questions have none.
    same = F.ku_question(d0="2023/06/02 (Fri) 08:00", d1="2023/06/02 (Fri) 10:00")
    assert M.ku_pair_verdict(same, M.turn_events(F.meta_for(same, "a")), M.conflict_pairs(good), M.applied_supersessions(good))["correct"] is None
    assert M.ku_pair_verdict(F.other_question(), {}, [], []) is None


def test_history_measures_texts():
    q = F.ku_question()
    meta = F.meta_for(q, "a")
    c0 = F.ask_line(q, meta, {"s0t0", "s2t0"})
    c1 = F.ask_line(q, meta, {"s0t0"}, dropped=["evt_a20"])
    lst = F.listing(pairs=[F.pair("evt_a00", "evt_a20", verdict="supersedes", conf=0.8)], sups=[F.sup("evt_a20", "evt_a00", conf=0.8)])
    r = M.history_measures(q, meta, c0, c1, lst, F.sleep_result())
    assert r["stale_c0"] is True and r["stale_c1"] is True and r["judged"] == 1
    h = r["hidden"][0]
    assert h["key"] == "s2t0" and "Botafogo" in h["old"]["text"] and "Vila Mariana" in h["new"]["text"] and h["confidence"] == 0.8
    assert r["sleep"]["llm_calls"] == 3 and r["sleep"]["seconds"] == 2.5
    assert r["applied"][0]["class"] == "wrong"
