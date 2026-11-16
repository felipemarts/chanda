"""Cost projection for the sleep study, printed by the drivers before any
paid step. Prices: eval/cmd/lmeproxy/prices.deepseek.json, off-peak rates
(the proxy runs -offpeak-only), every input token a cache miss.

  locomo  the PROTOCOL procedure: the use half once on the base database,
          the test half for B, S0, S1, S2, S3, the use half again (ceiling)
          for S0..S3; sleep LLM calls for S2 and S3 (S3's token budget is
          S2's spend, so it is projected equal to S2).
  bench   the 9 LongMemEval tuning questions: official CoT reader and judge
          before sleep and after each of X and L; L's sleep.

Questions already answered in the work directory (answers files) are not
counted again (the scripts resume), and a finished sleep result replaces
the sleep estimate with its measured tokens. Replayed calls (the store)
would cost nothing; the projection does not assume any.

Exit status 3 when spent so far (the cost file) plus the UPPER projection
exceeds the cap: the protocol says to ask before running.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))
sys.path.insert(0, HERE)

import common  # noqa: E402
import locomo_prompts as LP  # noqa: E402

PRICES = os.path.join(HERE, "..", "cmd", "lmeproxy", "prices.deepseek.json")
CONVS = ("conv-30", "conv-47", "conv-49")
SLEEP_BUDGET_TOKENS = 200_000  # Chanda's sleep.budget_tokens default: the ceiling of one run
SLEEP_OUT_SHARE = 0.15          # assumed output share of a sleep run's tokens


def rates(model: str, window: str = "offpeak") -> dict:
    with open(PRICES) as f:
        return json.load(f)["models"][model][window]


def usd(r: dict, tin: float, tout: float) -> float:
    return (tin * r["input_cache_miss"] + tout * r["output"]) / 1e6


def answered(path: str) -> int:
    return len(common.done_ids(path)) if os.path.exists(path) else 0


def sleep_tokens(path: str) -> int | None:
    if not os.path.exists(path):
        return None
    with open(path) as f:
        c = (json.load(f).get("output") or {}).get("cost") or {}
    return int(c.get("input_tokens", 0)) + int(c.get("output_tokens", 0))


def locomo(args) -> dict:
    with open(args.split) as f:
        sp = json.load(f)
    n = {c["sample_id"]: (c["use"], c["test"]) for c in sp["conversations"]}
    r = rates(args.model)
    tpl_r = common.est_tokens(LP.READER.format(speaker_a="", speaker_b="", context="", question=""))
    tpl_j = common.est_tokens(LP.JUDGE.format(question="", gold="", answer=""))
    q_tok, gold_tok = 15, 8
    rows = []

    def qa(label: str, conv: str, nq: int, out_dir: str | None, ctx: int = 2000) -> None:
        left = nq - (answered(os.path.join(out_dir, f"{label}.answers.jsonl")) if out_dir else 0)
        for kind, rout in (("expected", args.reader_out), ("upper", LP.READER_MAX_TOKENS)):
            tin = tpl_r + ctx + q_tok + tpl_j + q_tok + gold_tok + rout
            tout = rout + (2 if kind == "expected" else LP.JUDGE_MAX_TOKENS)
            rows.append({"step": label, "conv": conv, "kind": kind, "questions": left, "usd": left * usd(r, tin, tout)})

    for conv in CONVS:
        use, test = n[conv]
        od = os.path.join(args.work, conv, "out") if args.work else None
        qa("base.use", conv, use, od)
        for c in ("B", "S0", "S1", "S2", "S3"):
            qa(f"{c}.test", conv, test, od)
        for c in ("S0", "S1", "S2", "S3"):
            qa(f"{c}.ceiling", conv, use, od)
        s2 = sleep_tokens(os.path.join(od, "S2.sleep.json")) if od else None
        for c in ("S2", "S3"):
            done = sleep_tokens(os.path.join(od, f"{c}.sleep.json")) if od else None
            for kind in ("expected", "upper"):
                if done is not None:
                    tok = 0  # already spent; in the cost file
                elif c == "S3" and s2 is not None:
                    tok = s2
                else:
                    tok = SLEEP_BUDGET_TOKENS if kind == "upper" else args.sleep_expected_tokens
                rows.append({"step": f"{c}.sleep", "conv": conv, "kind": kind, "tokens": tok,
                             "usd": usd(r, tok * (1 - SLEEP_OUT_SHARE), tok * SLEEP_OUT_SHARE)})
    return finish(args, rows)


def bench(args) -> dict:
    r = rates(args.model)
    import prompts as LME  # eval/lme/py/prompts.py

    tpl_r = common.est_tokens(LME.READER_HISTORY_COT.format("", "2023/05/30 (Tue) 23:40", ""))
    tpl_j = common.est_tokens(LME.JUDGE_TEMPORAL.format("", "", ""))
    n = args.questions
    rows = []
    for label in ("before", "X", "L"):
        od = os.path.join(args.work, f"llm-{label}") if args.work else None
        left = n - (answered(os.path.join(od, "D.jsonl")) if od else 0)
        for kind, rout in (("expected", args.bench_reader_out), ("upper", LME.GEN_LENGTH_COT)):
            tin = tpl_r + 2000 + 25 + tpl_j + 25 + 15 + rout
            rows.append({"step": f"{label}.qa", "kind": kind, "questions": left,
                         "usd": left * usd(r, tin, rout + LME.JUDGE_MAX_TOKENS)})
    for kind in ("expected", "upper"):
        tok = n * (SLEEP_BUDGET_TOKENS if kind == "upper" else args.bench_sleep_expected_tokens)
        rows.append({"step": "L.sleep", "kind": kind, "tokens": tok,
                     "usd": usd(r, tok * (1 - SLEEP_OUT_SHARE), tok * SLEEP_OUT_SHARE)})
    return finish(args, rows)


def finish(args, rows: list[dict]) -> dict:
    spent = 0.0
    if args.cost and os.path.exists(args.cost):
        with open(args.cost) as f:
            spent = float(json.load(f).get("spent_usd") or 0)
    exp = sum(x["usd"] for x in rows if x["kind"] == "expected")
    up = sum(x["usd"] for x in rows if x["kind"] == "upper")
    return {"rows": rows, "expected_usd": exp, "upper_usd": up, "spent_usd": spent, "cap_usd": args.cap,
            "over_cap": spent + up > args.cap}


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("mode", choices=["locomo", "bench"])
    ap.add_argument("--split", default=os.path.join(HERE, "split-locomo.json"))
    ap.add_argument("--work", help="work directory (resumed steps are not counted)")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"), help="the proxy's cost file (spent so far)")
    ap.add_argument("--cap", type=float, default=2.0)
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--reader-out", type=int, default=30, help="expected LoCoMo reader output tokens")
    ap.add_argument("--sleep-expected-tokens", type=int, default=60_000, help="expected S2 sleep tokens per conversation")
    ap.add_argument("--questions", type=int, default=9, help="bench questions")
    ap.add_argument("--bench-reader-out", type=int, default=300)
    ap.add_argument("--bench-sleep-expected-tokens", type=int, default=15_000)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args(argv)
    res = locomo(args) if args.mode == "locomo" else bench(args)
    if args.json:
        print(json.dumps(res, indent=1))
    else:
        agg: dict[str, list[float]] = {}
        for x in res["rows"]:
            k = x["step"].split(".", 1)[-1] if args.mode == "locomo" else x["step"]
            agg.setdefault(k, [0.0, 0.0])[0 if x["kind"] == "expected" else 1] += x["usd"]
        print(f"cost projection ({args.mode}, off-peak, no replay assumed):")
        for k, (e, u) in agg.items():
            print(f"  {k:10s} expected ${e:.3f}   upper ${u:.3f}")
        print(f"  TOTAL      expected ${res['expected_usd']:.3f}   upper ${res['upper_usd']:.3f}")
        print(f"  spent so far ${res['spent_usd']:.4f} (cost file), cap ${res['cap_usd']:.2f}: "
              + ("OVER THE CAP with the upper projection: ask before running" if res["over_cap"] else "within the cap"))
    return 3 if res["over_cap"] else 0


if __name__ == "__main__":
    sys.exit(main())
