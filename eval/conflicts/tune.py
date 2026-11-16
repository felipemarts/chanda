#!/usr/bin/env python3
"""Summary of the conflict study's development grid (tune.sh): per grid
point, over the development histories, C0 = the D2 block on the base
(precheck.sh) and C1 = the block after the point's maintenance sleep.

- stale delivered: stale-measure questions (kuturns.stale_measure) whose
  block holds the outdated turn, C0 and C1, with the exact paired sign test;
- hidden evidence: needed turns (kuturns.needed for knowledge-update
  questions, has_answer turns otherwise) in C0's block, missing from C1's
  and dropped as "superseded" in C1's retrieval log;
- verdict tally of the judged pairs, and on the knowledge-update
  (outdated, current) pairs: candidate, judged, applied, correct;
- applied supersessions by class (measures.classify_applied);
- sleep calls, tokens, time per history, and the point's cost (proxy tag
  conflicts-dev-<point>-sleep);
- accuracy C0 and C1 when tune.sh --qa ran the reader and judge.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import measures as M  # noqa: E402


def label(pt: str) -> str:
    mc, ct, cf = pt.split(":")
    return f"mc{mc}-ct{ct}-cf{cf}"


def verdicts_of(path: str) -> dict[str, bool]:
    if not os.path.exists(path):
        return {}
    return {r["question_id"]: bool(r["verdict"]) for r in M.load_jsonl(path)}


def point(tune: str, dev: str, data: dict, ids: list[str], lab: str, cost: dict) -> dict:
    rows, missing = [], []
    for qid in ids:
        d = os.path.join(tune, lab, qid)
        if not os.path.exists(os.path.join(d, "list.json")):
            missing.append(qid)
            continue
        q = data[qid]
        meta = M.load_json(os.path.join(dev, qid, "base", "study.json"))
        c0 = M.load_jsonl(os.path.join(dev, qid, "D2.ask.jsonl"))[0]
        c1 = M.load_jsonl(os.path.join(d, "ask.jsonl"))[0]
        sl = M.load_json(os.path.join(d, "sleep.json")) if os.path.exists(os.path.join(d, "sleep.json")) else None
        rows.append(M.history_measures(q, meta, c0, c1, M.load_json(os.path.join(d, "list.json")), sl))
    return summarise(rows, missing, lab, cost, verdicts_of(os.path.join(tune, "qa-C0", "D.jsonl")),
                     verdicts_of(os.path.join(tune, f"qa-{lab}", "D.jsonl")))


def summarise(rows: list[dict], missing: list[str], lab: str, cost: dict, acc0: dict, acc1: dict) -> dict:
    st = [r for r in rows if r["stale_measure"]]
    stale = M.paired_flags({r["question_id"]: bool(r["stale_c0"]) for r in st}, {r["question_id"]: bool(r["stale_c1"]) for r in st})
    hid = [r for r in rows if r.get("hidden")]
    tally: dict[str, int] = {}
    for r in rows:
        for k, v in r["verdicts"].items():
            tally[k] = tally.get(k, 0) + v
    ku = [r["ku_pair"] for r in rows if r["ku_pair"]]
    classes: dict[str, int] = {}
    for r in rows:
        for s in r["applied"]:
            classes[s["class"]] = classes.get(s["class"], 0) + 1
    sleep = [r["sleep"] for r in rows if r["sleep"]]
    tag = (cost.get("by_tag") or {}).get(f"conflicts-dev-{lab}-sleep") or {}
    res = {"point": lab, "histories": len(rows), "missing": missing,
           "stale": stale, "hidden_questions": len(hid), "hidden_turns": sum(len(r["hidden"]) for r in hid),
           "hidden_cases": [{"question_id": r["question_id"], "hidden": r["hidden"]} for r in hid],
           "candidates": M.summary([r["candidates"] for r in rows]), "judged": M.summary([r["judged"] for r in rows]),
           "verdicts": dict(sorted(tally.items())), "applied_by_class": dict(sorted(classes.items())),
           "ku_pairs": {"qualifying": len(ku), "same_event": sum(bool(k["same_event"]) for k in ku), "candidate": sum(k["candidate"] for k in ku),
                        "judged": sum(k["judged"] for k in ku), "applied": sum(bool(k["applied"]) for k in ku),
                        "reversed": sum(bool(k.get("applied_reversed")) for k in ku),
                        "correct": sum(k["correct"] is True for k in ku), "scored": sum(k["correct"] is not None for k in ku),
                        "verdicts": {}},
           "sleep": {"llm_calls": M.summary([s["llm_calls"] for s in sleep]), "input_tokens": M.summary([s["input_tokens"] for s in sleep]),
                     "output_tokens": M.summary([s["output_tokens"] for s in sleep]), "seconds": M.summary([s["seconds"] for s in sleep]),
                     "cost_usd": tag.get("cost_usd"), "calls_by_proxy": tag.get("calls"), "replayed": tag.get("replayed")},
           "rows": rows}
    for k in ku:
        if k["judged"]:
            v = k["verdict"] or "invalid"
            res["ku_pairs"]["verdicts"][v] = res["ku_pairs"]["verdicts"].get(v, 0) + 1
    if acc0 and acc1:
        p = M.paired_flags(acc0, acc1)
        res["accuracy"] = {"n": p["n"], "c0_correct": p["first_true"], "c1_correct": p["second_true"],
                           "c0_only": p["first_only"], "c1_only": p["second_only"], "p": p["p"]}
    return res


def markdown(points: list[dict]) -> str:
    L = ["# Conflict study: development grid", "",
         "C0 = the D2 block on the base (no sleep); C1 = after the point's maintenance sleep. "
         "Stale: stale-measure questions whose block holds the outdated turn. Hidden: needed turns in C0's block, "
         "dropped as superseded in C1's.", "",
         "| point | histories | stale C0 -> C1 (p) | hidden (questions / turns) | candidates mean / max | judged mean / max | "
         "verdicts | KU pair: candidate / judged / applied / correct | applied by class | sleep calls mean | cost US$ | accuracy C0 / C1 (p) |",
         "|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for p in points:
        s, k = p["stale"], p["ku_pairs"]
        acc = p.get("accuracy")
        acc_s = f"{acc['c0_correct']}/{acc['n']} / {acc['c1_correct']}/{acc['n']} ({acc['p']:.3f})" if acc else "-"
        cost = p["sleep"]["cost_usd"]
        L.append(f"| {p['point']} | {p['histories']} | {s['first_true']}/{s['n']} -> {s['second_true']}/{s['n']} ({s['p']:.3f}) | "
                 f"{p['hidden_questions']} / {p['hidden_turns']} | {p['candidates']['mean']} / {p['candidates']['max']} | "
                 f"{p['judged']['mean']} / {p['judged']['max']} | {json.dumps(p['verdicts'])} | "
                 f"{k['candidate']} / {k['judged']} / {k['applied']} / {k['correct']} of {k['qualifying']} | {json.dumps(p['applied_by_class'])} | "
                 f"{p['sleep']['llm_calls']['mean']} | {cost if cost is not None else '-'} | {acc_s} |")
    for p in points:
        if p["hidden_cases"]:
            L += ["", f"## Hidden evidence, {p['point']}", ""]
            for c in p["hidden_cases"]:
                for h in c["hidden"]:
                    L.append(f"- {c['question_id']} {h['key']}: {((h.get('old') or {}).get('text') or '')[:160]!r} superseded by "
                             f"{((h.get('new') or {}).get('text') or '')[:160]!r} (verdict {h.get('verdict')}, confidence {h.get('confidence')})")
        if p["missing"]:
            L += ["", f"{p['point']}: not finished for {', '.join(p['missing'])}."]
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tune", required=True)
    ap.add_argument("--dev", required=True, help="precheck work directory (bases, D2 blocks)")
    ap.add_argument("--data", required=True)
    ap.add_argument("--ids")
    ap.add_argument("--grid", required=True)
    ap.add_argument("--cost", help="the proxy's cost file")
    ap.add_argument("--md")
    ap.add_argument("--json")
    a = ap.parse_args(argv)
    ids = a.ids.split() if a.ids else M.set_ids(M.load_sets(), "development")
    want = set(ids)
    data = {q["question_id"]: q for q in M.load_json(a.data) if q["question_id"] in want}
    cost = M.load_json(a.cost) if a.cost and os.path.exists(a.cost) else {}
    points = [point(a.tune, a.dev, data, ids, label(pt), cost) for pt in a.grid.split()]
    if a.json:
        with open(a.json, "w") as f:
            json.dump({"points": points}, f, indent=1)
            f.write("\n")
    md = markdown(points)
    if a.md:
        with open(a.md, "w") as f:
            f.write(md)
    else:
        print(md)
    return 0


if __name__ == "__main__":
    sys.exit(main())
