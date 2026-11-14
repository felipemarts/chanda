"""Cost estimates for the LongMemEval battery, from the DeepSeek price table
(eval/cmd/lmeproxy/prices.deepseek.json).

  prior     a-priori estimate (before any LLM call) from the prompt template
            sizes, the 2,000-token context budget, the measured LongMemEval S
            session sizes and Mem0's measured extraction prompt; every
            assumption is a flag and is printed with the result.
  measured  projection from the lmeproxy call log of the smoke run: mean cost
            per call and calls per question for each tag, scaled to N
            questions; tokens are re-priced at off-peak and at peak rates.

Tokens are estimated as ceil(chars/4) (Chanda's estimator); --tok-ratio
scales that to the provider's tokenizer if it is known to differ.
"""

from __future__ import annotations

import argparse
import collections
import json
import os
import sys

import common
import prompts

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_PRICES = os.path.join(HERE, "..", "..", "cmd", "lmeproxy", "prices.deepseek.json")

# Measured on longmemeval_s_cleaned.json (500 questions; see PROVIDER.md):
S_SESSIONS_PER_Q = 47.7
S_CHARS_PER_SESSION = 10251
S_TURNS_PER_SESSION = 10.3
MEM0_SYSTEM_PROMPT_CHARS = 33667  # mem0ai 2.2.1 ADDITIVE_EXTRACTION_PROMPT


def load_prices(path: str, model: str) -> dict:
    with open(path) as f:
        pt = json.load(f)
    return pt["models"][model]


def cost(rate: dict, hit: float, miss: float, out: float) -> float:
    return (hit * rate["input_cache_hit"] + miss * rate["input_cache_miss"] + out * rate["output"]) / 1e6


def mem0_prompt_chars() -> int:
    try:
        os.environ.setdefault("MEM0_TELEMETRY", "False")
        from mem0.configs.prompts import ADDITIVE_EXTRACTION_PROMPT

        return len(ADDITIVE_EXTRACTION_PROMPT)
    except Exception:  # noqa: BLE001 - mem0 not installed: use the measured constant
        return MEM0_SYSTEM_PROMPT_CHARS


def haystack_stats(dataset: str | None, sample: str | None) -> dict:
    if not dataset:
        return {"source": "constants measured on the full S file", "sessions_per_q": S_SESSIONS_PER_Q,
                "chars_per_session": S_CHARS_PER_SESSION, "turns_per_session": S_TURNS_PER_SESSION}
    data = common.load_dataset(dataset)
    ids = common.load_sample_ids(sample) if sample else list(data)
    sess = chars = turns = 0
    for qid in ids:
        q = data[qid]
        sess += len(q["haystack_sessions"])
        for s in q["haystack_sessions"]:
            turns += len(s)
            chars += sum(len(t["content"]) for t in s)
    return {"source": f"{dataset} ({'sample' if sample else 'all'}: {len(ids)} questions)",
            "sessions_per_q": sess / len(ids), "chars_per_session": chars / sess, "turns_per_session": turns / sess}


def prior(args) -> dict:
    rates = load_prices(args.prices, args.model)
    hs = haystack_stats(args.dataset, args.sample)
    r = args.tok_ratio
    tok_session = hs["chars_per_session"] / 4 * r
    sys_tok = mem0_prompt_chars() / 4 * r
    last_k = tok_session * min(10, hs["turns_per_session"]) / hs["turns_per_session"]
    reader_tpl = common.est_tokens(prompts.READER_HISTORY_COT.format("", "2023/05/30 (Tue) 23:40", "")) * r
    judge_tpl = common.est_tokens(prompts.JUDGE_DEFAULT.format("", "", "")) * r
    q_tok, gold_tok = 85 / 4 * r, 52 / 4 * r  # mean question / answer chars on S

    calls = collections.OrderedDict()  # (cond, role) -> (calls, hit, miss, out) per question
    for c in args.conditions:
        ctx = 0 if c == "A" else args.ctx_tokens * r
        calls[(c, "reader")] = (1, 0, reader_tpl + ctx + q_tok, args.reader_out)
        calls[(c, "judge")] = (1, 0, judge_tpl + q_tok + gold_tok + args.reader_out, args.judge_out)
    if "C" in args.conditions:
        n = hs["sessions_per_q"]
        hit = sys_tok * args.cache_hit
        miss = (sys_tok - hit) + tok_session + last_k + args.mem0_existing + args.mem0_overhead
        calls[("C", "ingest")] = (n, hit * n, miss * n, args.mem0_out * n)
    if "E" in args.conditions:
        hay = hs["sessions_per_q"] * tok_session
        e_in = min(hay * args.e_in_factor, args.e_budget_tokens)
        calls[("E", "sleep")] = (None, 0, e_in, e_in * args.e_out_ratio)

    rows = []
    for (c, role), (ncalls, hit, miss, out) in calls.items():
        rows.append({"condition": c, "role": role, "calls_per_q": ncalls, "hit_per_q": round(hit),
                     "miss_per_q": round(miss), "out_per_q": round(out),
                     "offpeak_usd": cost(rates["offpeak"], hit, miss, out) * args.n,
                     "peak_usd": cost(rates["peak"], hit, miss, out) * args.n})
    assumptions = {
        "questions": args.n, "conditions": args.conditions, "model": args.model, "haystack": hs,
        "tokens_per_session": round(tok_session), "tok_ratio_vs_chars_div_4": r,
        "reader": f"official CoT template ({round(reader_tpl)} tok) + context {args.ctx_tokens} tok (B-E; A empty) + question; "
                  f"output {args.reader_out} tok (max_tokens {prompts.GEN_LENGTH_COT}); no cache hits",
        "judge": f"official template ({round(judge_tpl)} tok) + question + gold + answer ({args.reader_out} tok); output {args.judge_out} tok",
        "mem0": f"one extraction call per session (Memory.add, infer=True); system prompt {round(sys_tok)} tok of which "
                f"{args.cache_hit:.0%} cache hits; + session {round(tok_session)} + last 10 messages {round(last_k)} + "
                f"existing memories {args.mem0_existing} + sections {args.mem0_overhead}; output {args.mem0_out} tok",
        "chanda_sleep": f"input = min(haystack tokens x {args.e_in_factor}, sleep.budget_tokens {args.e_budget_tokens}); "
                        f"output = {args.e_out_ratio:.0%} of input",
        "smoke": "the 10-question smoke run is a subset of the 60; reusing its outputs (the scripts resume) costs nothing extra",
    }
    tot_off = sum(x["offpeak_usd"] for x in rows)
    tot_peak = sum(x["peak_usd"] for x in rows)
    return {"rows": rows, "total_offpeak_usd": tot_off, "total_peak_usd": tot_peak,
            "smoke_extra_if_not_reused_offpeak_usd": tot_off * args.smoke_n / args.n, "cap_usd": args.cap,
            "assumptions": assumptions}


def measured(args) -> dict:
    rates = load_prices(args.prices, args.model)
    by = collections.defaultdict(lambda: {"calls": 0, "hit": 0, "miss": 0, "out": 0, "billed": 0.0})
    for r in common.read_jsonl(args.log):
        if r.get("outcome") != "forwarded":
            continue
        b = by[r["tag"]]
        b["calls"] += 1
        b["hit"] += r.get("cache_hit_tokens", 0)
        b["miss"] += r.get("cache_miss_tokens", 0)
        b["out"] += r.get("completion_tokens", 0)
        b["billed"] += r.get("cost_usd", 0) + r.get("unverified_usd", 0)
    rows = []
    for tag, b in sorted(by.items()):
        per_q = {k: b[k] / args.smoke_n for k in ("calls", "hit", "miss", "out")}
        rows.append({
            "tag": tag, "calls": b["calls"], "billed_usd": b["billed"], "calls_per_q": per_q["calls"],
            "mean_in_per_call": (b["hit"] + b["miss"]) / b["calls"], "mean_out_per_call": b["out"] / b["calls"],
            "cache_hit_share": b["hit"] / max(1, b["hit"] + b["miss"]),
            "offpeak_usd": cost(rates["offpeak"], per_q["hit"], per_q["miss"], per_q["out"]) * args.n,
            "peak_usd": cost(rates["peak"], per_q["hit"], per_q["miss"], per_q["out"]) * args.n,
        })
    billed = sum(r["billed_usd"] for r in rows)
    off = sum(r["offpeak_usd"] for r in rows)
    return {"rows": rows, "smoke_questions": args.smoke_n, "questions": args.n, "billed_so_far_usd": billed,
            "projected_full_offpeak_usd": off, "projected_full_peak_usd": sum(r["peak_usd"] for r in rows),
            "remaining_if_smoke_reused_offpeak_usd": off * (args.n - args.smoke_n) / args.n,
            "cap_usd": args.cap}


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="mode", required=True)
    for name in ("prior", "measured"):
        p = sub.add_parser(name)
        p.add_argument("--prices", default=DEFAULT_PRICES)
        p.add_argument("--model", default=common.DEFAULT_MODEL)
        p.add_argument("--n", type=int, default=60)
        p.add_argument("--smoke-n", type=int, default=10)
        p.add_argument("--cap", type=float, default=8.0)
        p.add_argument("--json", action="store_true")
    p = sub.choices["prior"]
    p.add_argument("--dataset")
    p.add_argument("--sample")
    p.add_argument("--conditions", type=lambda s: s.split(","), default=list("ABCDE"))
    p.add_argument("--ctx-tokens", type=int, default=2000)
    p.add_argument("--reader-out", type=int, default=350)
    p.add_argument("--judge-out", type=int, default=2)
    p.add_argument("--mem0-out", type=int, default=400)
    p.add_argument("--mem0-existing", type=int, default=300)
    p.add_argument("--mem0-overhead", type=int, default=150)
    p.add_argument("--cache-hit", type=float, default=0.9)
    p.add_argument("--e-in-factor", type=float, default=1.15)
    p.add_argument("--e-out-ratio", type=float, default=0.10)
    p.add_argument("--e-budget-tokens", type=int, default=200000)
    p.add_argument("--tok-ratio", type=float, default=1.0)
    sub.choices["measured"].add_argument("--log", required=True, help="lmeproxy JSONL log of the smoke run")
    args = ap.parse_args(argv)
    res = prior(args) if args.mode == "prior" else measured(args)
    if args.json:
        print(json.dumps(res, indent=2))
        return 0
    for row in res["rows"]:
        name = row.get("tag") or f"{row['condition']}-{row['role']}"
        print(f"{name:12s} off-peak ${row['offpeak_usd']:.3f}  peak ${row['peak_usd']:.3f}")
    if args.mode == "prior":
        print(f"TOTAL        off-peak ${res['total_offpeak_usd']:.2f}  peak ${res['total_peak_usd']:.2f}  (cap ${res['cap_usd']:.2f})")
        for k, v in res["assumptions"].items():
            print(f"  {k}: {v}")
    else:
        print(f"billed so far ${res['billed_so_far_usd']:.4f}; projected {res['questions']} q: off-peak "
              f"${res['projected_full_offpeak_usd']:.2f}, peak ${res['projected_full_peak_usd']:.2f}; "
              f"remaining if smoke reused ${res['remaining_if_smoke_reused_offpeak_usd']:.2f} (cap ${res['cap_usd']:.2f})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
