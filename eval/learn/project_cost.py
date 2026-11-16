"""Cost projection for the learning study (eval/learn/PROTOCOL.md), printed
by run_learn.sh before any paid step. The arithmetic is the sleep study's
(eval/sleep/project_cost.py: LoCoMo reader + judge per question, prompts
of eval/sleep/locomo_prompts.py, every input token a cache miss, no replay
assumed) with this study's counts:

  base.use   the use half once, on the base database (569 questions)
  <C>.test   the test half once per condition, C in B, U0, U1, K (584 each)

No other step calls an LLM: outcomes, copies, keys and sleep phase 1 make
no call. Both price windows are projected (off-peak and peak); the gate
uses --window (off-peak unless the run allows peak hours). Questions
already answered in the work directory (answers files, final or .part)
are not counted again.

Exit status 3 when spent so far (the cost file) plus the UPPER projection
in the gating window exceeds the cap.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SLEEP = os.path.join(HERE, "..", "sleep")
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))
sys.path.insert(0, SLEEP)

import importlib.util  # noqa: E402

import common  # noqa: E402
import locomo_prompts as LP  # noqa: E402


def _sleep_cost():
    # eval/sleep/project_cost.py under its own name (this file has the same name).
    spec = importlib.util.spec_from_file_location("sleep_project_cost", os.path.join(SLEEP, "project_cost.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


SP = _sleep_cost()

CONDS = ("B", "U0", "U1", "K")
WINDOWS = ("offpeak", "peak")


def answered(out_dir: str | None, label: str) -> int:
    if not out_dir:
        return 0
    p = os.path.join(out_dir, f"{label}.answers.jsonl")
    return max(SP.answered(p), SP.answered(p + ".part"))


def project(split: str, work: str | None, model: str, reader_out: int, convs: list[str] | None = None) -> list[dict]:
    with open(split) as f:
        sp = json.load(f)
    tpl_r = common.est_tokens(LP.READER.format(speaker_a="", speaker_b="", context="", question=""))
    tpl_j = common.est_tokens(LP.JUDGE.format(question="", gold="", answer=""))
    q_tok, gold_tok, ctx = 15, 8, 2000
    rows = []
    for c in sp["conversations"]:
        conv = c["sample_id"]
        if convs and conv not in convs:
            continue
        od = os.path.join(work, conv, "out") if work else None
        steps = [("base.use", c["use"])] + [(f"{k}.test", c["test"]) for k in CONDS]
        for label, nq in steps:
            left = max(0, nq - answered(od, label))
            for w in WINDOWS:
                r = SP.rates(model, w)
                for kind, rout in (("expected", reader_out), ("upper", LP.READER_MAX_TOKENS)):
                    tin = tpl_r + ctx + q_tok + tpl_j + q_tok + gold_tok + rout
                    tout = rout + (2 if kind == "expected" else LP.JUDGE_MAX_TOKENS)
                    rows.append({"step": label, "conv": conv, "window": w, "kind": kind, "questions": left,
                                 "usd": left * SP.usd(r, tin, tout)})
    return rows


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--split", default=os.path.join(HERE, "split-locomo.json"))
    ap.add_argument("--work", help="work directory (answered questions are not counted)")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"), help="the proxy's cost file (spent so far)")
    ap.add_argument("--cap", type=float, default=4.0)
    ap.add_argument("--window", choices=WINDOWS, default="offpeak", help="price window the gate uses")
    ap.add_argument("--convs", help="space-separated subset (dry runs)")
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--reader-out", type=int, default=30, help="expected LoCoMo reader output tokens")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args(argv)
    rows = project(args.split, args.work, args.model, args.reader_out, args.convs.split() if args.convs else None)
    spent = 0.0
    if args.cost and os.path.exists(args.cost):
        with open(args.cost) as f:
            spent = float(json.load(f).get("spent_usd") or 0)
    tot = {w: {k: sum(x["usd"] for x in rows if x["window"] == w and x["kind"] == k) for k in ("expected", "upper")} for w in WINDOWS}
    res = {"rows": rows, "totals": tot, "spent_usd": spent, "cap_usd": args.cap, "window": args.window,
           "questions": sum(x["questions"] for x in rows if x["window"] == "offpeak" and x["kind"] == "upper"),
           "over_cap": spent + tot[args.window]["upper"] > args.cap}
    if args.json:
        print(json.dumps(res, indent=1))
    else:
        agg: dict[tuple, float] = {}
        for x in rows:
            k = (x["step"].split(".", 1)[0] if x["step"] != "base.use" else "base.use", x["window"], x["kind"])
            agg[k] = agg.get(k, 0.0) + x["usd"]
        print(f"cost projection (learning study, {res['questions']} questions to answer, no replay assumed):")
        print(f"  {'step':10s} {'off-peak expected':>18s} {'upper':>8s} {'peak expected':>14s} {'upper':>8s}")
        for step in ["base.use"] + list(CONDS):
            print(f"  {step:10s} {agg.get((step, 'offpeak', 'expected'), 0):18.3f} {agg.get((step, 'offpeak', 'upper'), 0):8.3f}"
                  f" {agg.get((step, 'peak', 'expected'), 0):14.3f} {agg.get((step, 'peak', 'upper'), 0):8.3f}")
        print(f"  {'TOTAL':10s} {tot['offpeak']['expected']:18.3f} {tot['offpeak']['upper']:8.3f}"
              f" {tot['peak']['expected']:14.3f} {tot['peak']['upper']:8.3f}")
        print(f"  spent so far ${spent:.4f} (cost file), cap ${args.cap:.2f}, gate on the {args.window} upper bound: "
              + ("OVER THE CAP: ask before running" if res["over_cap"] else "within the cap"))
    return 3 if res["over_cap"] else 0


if __name__ == "__main__":
    sys.exit(main())
