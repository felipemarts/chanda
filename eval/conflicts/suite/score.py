#!/usr/bin/env python3
"""Score of the Portuguese suite (run_suite.sh). Per pair directory
<work>/<pair>/: db/study.json (turn -> event; the older user turn is s0t0,
the newer one s1t0), queue.json (conflicts list after ingestion) and, when
the verdicts ran, after.json (conflicts list after the sleep) and
sleep.json.

- detected: the (older user turn, newer user turn) pair is in the queue;
- verdict: the pair's verdict after the sleep;
- applied: an active supersession old = older turn, new = newer turn;
- false supersession (controls): any active supersession in the pair's
  database; for updates, a supersession other than older -> newer is
  counted as wrong too.

Exit status 4 when more than 2 false supersessions are applied among the
controls (PROTOCOL.md, Stops).
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))

import measures as M  # noqa: E402

MAX_FALSE = 2


def score_pair(p: dict, d: str) -> dict:
    meta = M.load_json(os.path.join(d, "db", "study.json"))
    ev = M.turn_events(meta)
    old, new = ev.get("s0t0", ""), ev.get("s1t0", "")
    r = {"id": p["id"], "kind": p["kind"], "category": p["category"], "expected": p["expected"],
         "older_event": old, "newer_event": new, "older": p["older"]["turns"][0]["content"], "newer": p["newer"]["turns"][0]["content"],
         "same_event": bool(old) and old == new}
    qp = os.path.join(d, "queue.json")
    queue = M.conflict_pairs(M.load_json(qp)) if os.path.exists(qp) else []
    pq = M.find_pair(queue, old, new) if old and new and old != new else None
    r["queue_pairs"] = len(queue)
    r["detected"] = pq is not None
    if pq:
        r["cosine"] = pq.get("cosine")
        r["differs"], r["shared"] = pq["raw"].get("differs"), pq["raw"].get("shared")
    ap = os.path.join(d, "after.json")
    r["judged_run"] = os.path.exists(ap)
    if r["judged_run"]:
        after = M.load_json(ap)
        pairs = M.conflict_pairs(after)
        pa = M.find_pair(pairs, old, new) if old and new and old != new else None
        r["verdict"] = pa.get("verdict") if pa and M.judged(pa) else None
        r["confidence"] = pa.get("confidence") if pa else None
        r["reason"] = pa.get("reason") if pa else None
        sup = M.applied_supersessions(after)
        r["applied"] = any(s["old_id"] == old and s["new_id"] == new for s in sup)
        r["other_supersessions"] = [{"old_id": s["old_id"], "new_id": s["new_id"]} for s in sup
                                    if not (s["old_id"] == old and s["new_id"] == new)]
        r["false_supersession"] = (p["kind"] == "control" and bool(sup)) or bool(r["other_supersessions"])
        r["verdicts_all"] = M.verdict_tally(pairs)
    sp = os.path.join(d, "sleep.json")
    if os.path.exists(sp):
        r["sleep"] = M.sleep_stats(M.load_json(sp))
    return r


def build(work: str, pairs: list[dict], embedder: str, cost: dict) -> dict:
    rows, missing = [], []
    for p in pairs:
        d = os.path.join(work, p["id"])
        if not os.path.exists(os.path.join(d, "db", "study.json")):
            missing.append(p["id"])
            continue
        rows.append(score_pair(p, d))
    up = [r for r in rows if r["kind"] == "update"]
    ct = [r for r in rows if r["kind"] == "control"]
    judged_run = bool(rows) and all(r["judged_run"] for r in rows)

    def tally(rs):
        t: dict[str, int] = {}
        for r in rs:
            if r.get("detected"):
                v = r.get("verdict") or ("not judged" if r.get("judged_run") else "no sleep run")
                t[v] = t.get(v, 0) + 1
        return dict(sorted(t.items()))

    res = {"embedder": embedder, "pairs": len(rows), "missing": missing, "verdicts_ran": judged_run,
           "updates": {"n": len(up), "detected": sum(r["detected"] for r in up), "verdicts_detected": tally(up),
                       "applied": sum(bool(r.get("applied")) for r in up),
                       "wrong_supersessions": sum(bool(r.get("other_supersessions")) for r in up)},
           "controls": {"n": len(ct), "detected": sum(r["detected"] for r in ct), "verdicts_detected": tally(ct),
                        "false_supersessions": sum(bool(r.get("false_supersession")) for r in ct)},
           "sleep": {"llm_calls": sum((r.get("sleep") or {}).get("llm_calls", 0) for r in rows),
                     "input_tokens": sum((r.get("sleep") or {}).get("input_tokens", 0) for r in rows),
                     "output_tokens": sum((r.get("sleep") or {}).get("output_tokens", 0) for r in rows),
                     "cost_usd": ((cost.get("by_tag") or {}).get("conflicts-suite") or {}).get("cost_usd")},
           "rows": rows}
    res["stop"] = res["controls"]["false_supersessions"] > MAX_FALSE
    return res


def markdown(res: dict) -> str:
    u, c = res["updates"], res["controls"]
    L = [f"# Conflict study: Portuguese suite ({res['embedder']})", "",
         f"{res['pairs']} pairs" + (f" (missing: {', '.join(res['missing'])})" if res["missing"] else "") +
         ("" if res["verdicts_ran"] else "; **detection only, the verdicts have not run (run_suite.sh --paid)**") + ".", "",
         f"- Updates: detected {u['detected']}/{u['n']}; verdicts of the detected pairs {json.dumps(u['verdicts_detected'])}; "
         f"applied {u['applied']}/{u['n']}; other (wrong) supersessions in {u['wrong_supersessions']} pairs.",
         f"- Controls: detected {c['detected']}/{c['n']}; verdicts {json.dumps(c['verdicts_detected'])}; "
         f"**false supersessions {c['false_supersessions']}/{c['n']}** (stop above {MAX_FALSE}).",
         f"- Sleep: {res['sleep']['llm_calls']} calls, {res['sleep']['input_tokens']}+{res['sleep']['output_tokens']} tokens, "
         f"US$ {res['sleep']['cost_usd'] if res['sleep']['cost_usd'] is not None else '-'} (tag conflicts-suite).", ""]
    if res["stop"]:
        L += [f"**STOP: more than {MAX_FALSE} false supersessions among the controls (PROTOCOL.md, Stops). The test does not run.**", ""]
    L += ["| pair | kind | category | detected | cosine | verdict | confidence | reason | applied | false/other |",
          "|---|---|---|---|---|---|---|---|---|---|"]
    for r in res["rows"]:
        cos = f"{r['cosine']:.3f}" if isinstance(r.get("cosine"), (int, float)) else "-"
        L.append(f"| {r['id']} | {r['kind']} | {r['category']} | {'yes' if r['detected'] else 'no'} | {cos} | {r.get('verdict') or '-'} | "
                 f"{r.get('confidence') if r.get('confidence') is not None else '-'} | {r.get('reason') or '-'} | "
                 f"{'yes' if r.get('applied') else 'no'} | {'yes' if r.get('false_supersession') else 'no'} |")
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True)
    ap.add_argument("--pairs", default=os.path.join(HERE, "pairs.json"))
    ap.add_argument("--embedder", default="e5")
    ap.add_argument("--cost")
    ap.add_argument("--md")
    ap.add_argument("--json")
    a = ap.parse_args(argv)
    pairs = M.load_json(a.pairs)["pairs"]
    cost = M.load_json(a.cost) if a.cost and os.path.exists(a.cost) else {}
    res = build(a.work, pairs, a.embedder, cost)
    if a.json:
        with open(a.json, "w") as f:
            json.dump(res, f, indent=1, ensure_ascii=False)
            f.write("\n")
    md = markdown(res)
    if a.md:
        with open(a.md, "w") as f:
            f.write(md)
    print(md)
    if res["stop"]:
        print(f"STOP: {res['controls']['false_supersessions']} false supersessions among the {res['controls']['n']} controls "
              f"(more than {MAX_FALSE}): the protocol stops here.", file=sys.stderr)
        return 4
    return 0


if __name__ == "__main__":
    sys.exit(main())
