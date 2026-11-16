#!/usr/bin/env python3
"""Cost projections of the conflict study (eval/conflicts/PROTOCOL.md,
"Smoke run and cost"), deepseek-flash prices of
eval/cmd/lmeproxy/prices.deepseek.json in the gating window.

  tune   before tune.sh's grid: pairs per history from Part 0's scan at
         each point's min_cosine (capped by sleep.conflicts.max_pairs when
         given), an assumed verdict size (expected and upper), and with
         --qa the reader and judge of every block. Histories already
         listed in the tune directory are not counted.
  test   after the smoke run (run_test.sh --smoke N): the measured cost of
         each smoke history, paid-equivalent from the tokens (a replayed
         call counts as if paid): the C1 sleep (proxy tag C1-sleep-<qid> in
         the call log) and the C0 and C1 reader and judge (their records).
         Projected total = spent so far (the cost file, every tag) + the
         histories not yet run x the mean (expected) or the maximum (upper)
         smoke cost per history. Writes the smoke summary (pairs judged,
         sleep cost and time per history) with --md/--json.

Exit status 3 when spent + the upper projection exceeds the cap.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import measures as M  # noqa: E402

PRICES = os.path.join(HERE, "..", "cmd", "lmeproxy", "prices.deepseek.json")
WINDOWS = ("offpeak", "peak")
# Assumed size of one verdict call: instructions and schema, plus 2 records
# x (1 + 2 x context_turns) turns.
VERDICT_FIXED_IN = 450
TURN_TOKENS = {"expected": 150, "upper": 450}
VERDICT_OUT = {"expected": 40, "upper": 100}
# One LongMemEval reader + judge (the 2,000-token block, the official templates).
QA_IN = {"expected": 2600, "upper": 3000}
QA_OUT = {"expected": 200, "upper": 810}


def rates(model: str = "deepseek-flash", window: str = "offpeak") -> dict:
    with open(PRICES) as f:
        return json.load(f)["models"][model][window]


def usd_tokens(r: dict, prompt: int, hit: int, completion: int) -> float:
    return ((prompt - hit) * r["input_cache_miss"] + hit * r["input_cache_hit"] + completion * r["output"]) / 1e6


def spent(cost_path: str | None) -> float:
    if cost_path and os.path.exists(cost_path):
        return float(M.load_json(cost_path).get("spent_usd") or 0)
    return 0.0


# ---------------------------------------------------------------- tune


def tune(a) -> dict:
    pre = M.load_json(a.precheck) if a.precheck and os.path.exists(a.precheck) else None
    if pre is None:
        raise SystemExit(f"{a.precheck} not found: run eval/conflicts/precheck.sh first")
    by_q = {r["question_id"]: r for r in pre["rows"]}
    max_pairs = 0
    for kv in (a.extra or "").split():
        if kv.startswith("sleep.conflicts.max_pairs="):
            max_pairs = int(kv.split("=", 1)[1])
    r = rates(window=a.window)
    ids = a.ids.split()
    rows = []
    for pt in a.grid.split():
        mc, ct, cf = pt.split(":")
        lab = f"mc{mc}-ct{ct}-cf{cf}"
        todo = [q for q in ids if not os.path.exists(os.path.join(a.tune or "", lab, q, "list.json"))]
        pairs = 0
        for q in todo:
            bm = (by_q.get(q) or {}).get("by_min_cosine") or {}
            x = bm.get(mc) or (max(bm.values(), key=lambda v: v["candidates"]) if bm else {"candidates": 0})
            n = x["candidates"]
            pairs += min(n, max_pairs) if max_pairs else n
        row = {"point": lab, "histories": len(todo), "pairs": pairs}
        for kind in ("expected", "upper"):
            tin = VERDICT_FIXED_IN + 2 * (1 + 2 * int(ct)) * TURN_TOKENS[kind]
            usd = pairs * usd_tokens(r, tin, 0, VERDICT_OUT[kind])
            if a.qa:
                usd += len(todo) * usd_tokens(r, QA_IN[kind], 0, QA_OUT[kind])
            row[kind] = usd
        rows.append(row)
    if a.qa:  # C0's reader and judge, once
        rows.append({"point": "qa-C0", "histories": len(ids), "pairs": 0,
                     **{k: len(ids) * usd_tokens(r, QA_IN[k], 0, QA_OUT[k]) for k in ("expected", "upper")}})
    s = spent(a.cost)
    tot = {k: sum(x[k] for x in rows) for k in ("expected", "upper")}
    res = {"rows": rows, "totals": tot, "spent_usd": s, "cap_usd": a.cap, "window": a.window, "over_cap": s + tot["upper"] > a.cap}
    print(f"cost projection (conflict dev grid, {a.window} prices, no replay assumed):")
    for x in rows:
        print(f"  {x['point']:24s} {x['histories']:3d} histories {x['pairs']:5d} pairs  expected ${x['expected']:.4f}  upper ${x['upper']:.4f}")
    print(f"  total expected ${tot['expected']:.4f} upper ${tot['upper']:.4f}; spent so far ${s:.4f}; cap ${a.cap:.2f}: "
          + ("OVER THE CAP" if res["over_cap"] else "within the cap"))
    return res


# ---------------------------------------------------------------- test (smoke gate)


def call_costs(calls_path: str, r: dict) -> dict[str, dict]:
    """Per tag: calls and paid-equivalent USD from the call log (forwarded and replayed)."""
    out: dict[str, dict] = {}
    if not calls_path or not os.path.exists(calls_path):
        return out
    for c in M.load_jsonl(calls_path):
        if c.get("outcome") not in ("forwarded", "replayed"):
            continue
        t = out.setdefault(c.get("tag") or "", {"calls": 0, "usd": 0.0, "replayed": 0})
        t["calls"] += 1
        t["replayed"] += c.get("outcome") == "replayed"
        t["usd"] += usd_tokens(r, int(c.get("prompt_tokens") or 0), int(c.get("cache_hit_tokens") or 0), int(c.get("completion_tokens") or 0))
    return out


def record_usd(rec: dict | None, role: str, r: dict) -> float:
    t = ((rec or {}).get(role) or {}).get("tokens") or {}
    return usd_tokens(r, int(t.get("prompt") or 0), int(t.get("cache_hit") or 0), int(t.get("completion") or 0))


def smoke_rows(work: str, ids: list[str], calls: dict, r: dict) -> list[dict]:
    recs = {}
    for c in ("C0", "C1"):
        recs[c] = {"answers": {}, "judged": {}}
        a, j = os.path.join(work, f"llm-{c}", "D.answers.jsonl"), os.path.join(work, f"llm-{c}", "D.jsonl")
        if os.path.exists(a):
            recs[c]["answers"] = {x["question_id"]: x for x in M.load_jsonl(a)}
        if os.path.exists(j):
            recs[c]["judged"] = {x["question_id"]: x for x in M.load_jsonl(j)}
    rows = []
    for q in ids:
        d = os.path.join(work, q)
        lp, sp = os.path.join(d, "C1.list.json"), os.path.join(d, "C1.sleep.json")
        done = os.path.exists(lp) and all(q in recs[c]["judged"] for c in ("C0", "C1"))
        row = {"question_id": q, "done": done}
        if os.path.exists(sp):
            row["sleep"] = M.sleep_stats(M.load_json(sp))
        if os.path.exists(lp):
            pairs = M.conflict_pairs(M.load_json(lp))
            row["candidates"] = len(pairs)
            row["judged"] = sum(1 for p in pairs if M.judged(p))
            row["verdicts"] = M.verdict_tally(pairs)
        sl = calls.get(f"C1-sleep-{q}") or {"calls": 0, "usd": 0.0, "replayed": 0}
        row["sleep_usd"], row["sleep_calls_proxy"] = sl["usd"], sl["calls"]
        row["qa_usd"] = sum(record_usd(recs[c]["answers"].get(q), "reader", r) + record_usd(recs[c]["judged"].get(q), "judge", r)
                            for c in ("C0", "C1"))
        row["usd"] = row["sleep_usd"] + row["qa_usd"]
        rows.append(row)
    return rows


def smoke_md(res: dict) -> str:
    L = ["# Conflict study: smoke run", "",
         f"{res['smoke_done']} of the first {res['smoke']} test histories finished. Paid-equivalent cost from the tokens "
         f"({res['window']} prices; replayed calls counted as paid).", "",
         "| question | candidates | judged | verdicts | sleep calls | sleep tokens in/out | sleep s | sleep US$ | reader+judge US$ |",
         "|---|---|---|---|---|---|---|---|---|"]
    for x in res["rows"]:
        s = x.get("sleep") or {}
        L.append(f"| {x['question_id']} | {x.get('candidates', '-')} | {x.get('judged', '-')} | {json.dumps(x.get('verdicts', {}))} | "
                 f"{s.get('llm_calls', '-')} | {s.get('input_tokens', '-')}/{s.get('output_tokens', '-')} | {s.get('seconds', '-')} | "
                 f"{x['sleep_usd']:.5f} | {x['qa_usd']:.5f} |")
    p = res["projection"]
    L += ["", f"Per history: mean ${p['per_history_mean']:.5f}, max ${p['per_history_max']:.5f}. "
          f"Remaining {p['remaining']} of {res['total']} histories: expected ${p['remaining_expected']:.4f}, upper ${p['remaining_upper']:.4f}. "
          f"Spent so far ${res['spent_usd']:.4f}. Projected total: expected ${p['total_expected']:.4f}, upper ${p['total_upper']:.4f}; "
          f"cap ${res['cap_usd']:.2f}: " + ("**over the cap: stop, the options go to the user (PROTOCOL.md)**" if res["over_cap"] else "within the cap") + "."]
    return "\n".join(L) + "\n"


def test(a) -> dict:
    r = rates(window=a.window)
    ids = M.set_ids(M.load_sets(a.sets), "test")
    smoke = ids[: a.smoke]
    calls = call_costs(a.calls, r)
    rows = smoke_rows(a.work, smoke, calls, r)
    done = [x for x in rows if x["done"]]
    if not done:
        raise SystemExit("no smoke history is finished: run run_test.sh --smoke first")
    per = [x["usd"] for x in done]
    finished_all = sum(1 for q in ids if os.path.exists(os.path.join(a.work, q, "C1.list.json")))
    remaining = len(ids) - max(finished_all, len(done))
    mean, mx = sum(per) / len(per), max(per)
    s = spent(a.cost)
    proj = {"per_history_mean": mean, "per_history_max": mx, "remaining": remaining,
            "remaining_expected": remaining * mean, "remaining_upper": remaining * mx,
            "total_expected": s + remaining * mean, "total_upper": s + remaining * mx}
    res = {"smoke": a.smoke, "smoke_done": len(done), "total": len(ids), "window": a.window, "spent_usd": s, "cap_usd": a.cap,
           "projection": proj, "over_cap": proj["total_upper"] > a.cap, "rows": rows}
    md = smoke_md(res)
    if a.md:
        with open(a.md, "w") as f:
            f.write(md)
    if a.json:
        with open(a.json, "w") as f:
            json.dump(res, f, indent=1)
            f.write("\n")
    print(md)
    return res


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    t = sub.add_parser("tune")
    t.add_argument("--precheck", required=True)
    t.add_argument("--grid", required=True)
    t.add_argument("--ids", required=True)
    t.add_argument("--tune")
    t.add_argument("--extra")
    t.add_argument("--qa", action="store_true")
    x = sub.add_parser("test")
    x.add_argument("--work", required=True)
    x.add_argument("--smoke", type=int, default=10)
    x.add_argument("--calls", default=os.path.join(HERE, "llm_calls.jsonl"))
    x.add_argument("--sets", default=os.path.join(HERE, "sets.json"))
    x.add_argument("--md")
    x.add_argument("--json")
    for p in (t, x):
        p.add_argument("--cost", default=os.path.join(HERE, "cost.json"))
        p.add_argument("--cap", type=float, default=2.0)
        p.add_argument("--window", choices=WINDOWS, default="offpeak")
    a = ap.parse_args(argv)
    res = tune(a) if a.cmd == "tune" else test(a)
    return 3 if res["over_cap"] else 0


if __name__ == "__main__":
    sys.exit(main())
