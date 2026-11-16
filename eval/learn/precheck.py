#!/usr/bin/env python3
"""Part 0 of the learning study (eval/learn/PRECHECK.md), no LLM:
1. S0 test blocks of the sleep study against the same test half asked on a
   copy of the base database without phase 1 (lme study ask, e5).
2. Overlap ceiling: share of test-half questions with at least one evidence
   turn marked used (outcomes.used_ids) by some use-half question.
Inputs: eval/sleep/work/locomo/<conv>/{base,out}, eval/learn/work/precheck."""
import json, sqlite3, sys
W = "eval/sleep/work/locomo"
P = "eval/learn/work/precheck"
def L(p): return [json.loads(l) for l in open(p)]
res = {"blocks": {}, "overlap": {}}
for conv in ("conv-30", "conv-47", "conv-49"):
    s0 = {x["question_id"]: x for x in L(f"{W}/{conv}/out/S0.test.ask.jsonl")}
    np1 = {x["question_id"]: x for x in L(f"{P}/{conv}/nophase1.test.ask.jsonl")}
    same_ids = same_text = 0
    for q, a in s0.items():
        b = np1[q]
        ia = [(i["id"], i["text"]) for i in a["items"]]
        ib = [(i["id"], i["text"]) for i in b["items"]]
        same_ids += [i for i, _ in ia] == [i for i, _ in ib]
        same_text += ia == ib
    res["blocks"][conv] = {"n": len(s0), "same_items_and_order": same_ids, "same_items_and_text": same_text}
    db = sqlite3.connect(f"{W}/{conv}/base/chanda.db")
    used_any, used_pos, delivered = set(), set(), set()
    for v, u, d in db.execute("SELECT o.value, o.used_ids, r.delivered FROM outcomes o JOIN retrievals r ON r.id = o.retrieval_id"):
        ids = set(json.loads(u))
        used_any |= ids
        if v > 0:
            used_pos |= ids
        delivered |= set(json.loads(d))
    n = scored = hit_any = hit_pos = hit_deliv = 0
    for q, a in s0.items():
        evs = [t.get("event_id") for t in a["evidence"]["turns"] if t.get("event_id")]
        n += 1
        if not evs:
            continue
        scored += 1
        hit_any += any(e in used_any for e in evs)
        hit_pos += any(e in used_pos for e in evs)
        hit_deliv += any(e in delivered for e in evs)
    res["overlap"][conv] = {"test": n, "scored": scored, "used_any": hit_any, "used_positive": hit_pos,
                            "delivered_any": hit_deliv, "outcomes": db.execute("SELECT count(*) FROM outcomes").fetchone()[0],
                            "used_ids_total": len(used_any)}
tot = {k: sum(v[k] for v in res["overlap"].values()) for k in ("test", "scored", "used_any", "used_positive", "delivered_any")}
res["overlap"]["total"] = tot
json.dump(res, sys.stdout, indent=1)
print()
