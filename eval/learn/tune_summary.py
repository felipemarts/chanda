"""Summary of the development tuning grid (eval/learn/tune.sh), no LLM.

Input: <tune>/<variant>/sets.txt (the variant's overrides, one key=value
per line) and <tune>/<variant>/<conv>.test.ask.jsonl (the dev test half
asked on that variant's copy of the sleep study's base database).

Per variant, pooled over the dev conversations and per conversation:
full evidence in the block (scored questions whose every valid evidence
turn reached the block) and mean turn recall (reached / turns, averaged
over scored questions). Per K variant: the share of test questions where
a key brought into the block a record U0 did not deliver
(items[].key_rank > 0, i.e. features.key_rank in the retrieval log), and
how many of those records were evidence turns.

Selection (a suggestion; the freeze is decided by hand, decision 0147):
the most full-evidence questions, then the higher mean turn recall, then
the value closer to Chanda's defaults (U1: smaller lambda, then larger
min_exposures; K: weight closer to 1, then top_k closer to 20).

  python3 eval/learn/tune_summary.py --tune eval/learn/work/tune --md eval/learn/tune/grid.md --json eval/learn/tune/grid.json
  python3 eval/learn/tune_summary.py --tune eval/learn/work/tune --best-u1     prints "lambda min_exposures"
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))


import common  # noqa: E402


def _learn_report():
    # eval/learn/report.py under its own name (eval/sleep has a report.py too).
    import importlib.util

    spec = importlib.util.spec_from_file_location("learn_report", os.path.join(HERE, "report.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


key_brought = _learn_report().key_brought

DEV = ("conv-30", "conv-47", "conv-49")


def read_sets(path: str) -> dict[str, str]:
    out = {}
    if os.path.exists(path):
        for line in open(path):
            line = line.split("#", 1)[0].strip()
            if "=" in line:
                k, v = line.split("=", 1)
                out[k.strip()] = v.strip()
    return out


def evidence(lines: list[dict]) -> dict:
    sc = [l for l in lines if (l.get("evidence") or {}).get("scored")]
    rec = [l["evidence"]["reached"] / len(l["evidence"]["turns"]) for l in sc if l["evidence"].get("turns")]
    return {"questions": len(lines), "scored": len(sc), "full": sum(1 for l in sc if l["evidence"]["full"]),
            "mean_turn_recall": sum(rec) / len(rec) if rec else None}


def kind_of(name: str) -> str:
    return "U0" if name == "U0" else name.split("-", 1)[0]


def collect(tune: str, convs: list[str]) -> dict:
    out = {}
    if not os.path.isdir(tune):
        return out
    for name in sorted(os.listdir(tune)):
        vd = os.path.join(tune, name)
        if not os.path.isdir(vd):
            continue
        asks = {}
        for conv in convs:
            p = os.path.join(vd, f"{conv}.test.ask.jsonl")
            if os.path.exists(p):
                asks[conv] = common.read_jsonl(p)
        if asks:
            out[name] = {"kind": kind_of(name), "sets": read_sets(os.path.join(vd, "sets.txt")), "asks": asks}
    return out


def summarise(variants: dict, convs: list[str]) -> dict:
    res = {"conversations": convs, "variants": {}}
    u0 = variants.get("U0")
    for name, v in variants.items():
        row = {"kind": v["kind"], "sets": v["sets"], "complete": all(c in v["asks"] for c in convs),
               "pooled": evidence([l for c in convs for l in v["asks"].get(c, [])]),
               "by_conversation": {c: evidence(v["asks"][c]) for c in convs if c in v["asks"]}}
        if v["kind"] == "K" and u0:
            kl = [l for c in convs for l in v["asks"].get(c, [])]
            ul = [l for c in convs for l in u0["asks"].get(c, [])]
            kb = key_brought(kl, ul)
            kb.pop("per_question")
            row["key_brought"] = kb
        res["variants"][name] = row
    res["best_U1"] = best(res, "U1")
    res["best_K"] = best(res, "K", require=lambda s: float(s.get("ranking.utility_lambda", 0)) > 0)
    return res


def fnum(s: dict, k: str, default: float) -> float:
    try:
        return float(s.get(k, default))
    except ValueError:
        return default


def best(res: dict, kind: str, require=None) -> str | None:
    cands = [(n, r) for n, r in res["variants"].items() if r["kind"] == kind and r["complete"] and (require is None or require(r["sets"]))]
    if not cands:
        return None

    def key(item):
        n, r = item
        p, s = r["pooled"], r["sets"]
        if kind == "U1":
            tie = (fnum(s, "ranking.utility_lambda", 0), -fnum(s, "ranking.min_exposures", 5))
        else:
            w = fnum(s, "read.learned_keys_weight", 1) or 1.0
            tie = (abs(w - 1), abs(fnum(s, "read.learned_keys_top_k", 20) - 20))
        return (-p["full"], -(p["mean_turn_recall"] or 0), *tie, n)

    return sorted(cands, key=key)[0][0]


def pct(x) -> str:
    return "-" if x is None else f"{100 * x:.1f}%"


def markdown(res: dict) -> str:
    convs = res["conversations"]
    L = ["# Learning study: development tuning grid (no LLM)", "",
         f"Dev conversations {', '.join(convs)}, test half of `eval/sleep/split-locomo.json`, asked on a copy of the sleep "
         "study's base database per variant (`eval/learn/tune.sh`). Full evidence: scored questions whose every valid "
         "evidence turn reached the block; recall: mean share of evidence turns reached.", "",
         "| Variant | Overrides | Full evidence (pooled) | Mean turn recall | " + " | ".join(f"{c} full / recall" for c in convs)
         + " | Key brought a non-U0 record (share) | Records, of them evidence |",
         "| --- | --- | --- | --- | " + "--- | " * len(convs) + "--- | --- |"]
    order = sorted(res["variants"], key=lambda n: ({"U0": 0, "U1": 1, "K": 2}.get(res["variants"][n]["kind"], 3), n))
    for n in order:
        r = res["variants"][n]
        p = r["pooled"]
        sets = ", ".join(f"{k}={v}" for k, v in r["sets"].items()) or "-"
        per = " | ".join(f"{r['by_conversation'][c]['full']}/{r['by_conversation'][c]['scored']} / {pct(r['by_conversation'][c]['mean_turn_recall'])}"
                         if c in r["by_conversation"] else "-" for c in convs)
        kb = r.get("key_brought")
        kbs = f"{kb['questions_key_brought']}/{kb['questions']} ({pct(kb['share'])})" if kb else "-"
        kbr = f"{kb['records_key_brought']}, {kb['evidence_records']}" if kb else "-"
        mark = " **(best U1)**" if n == res["best_U1"] else (" **(best K)**" if n == res["best_K"] else "")
        L.append(f"| {n}{mark}{'' if r['complete'] else ' (incomplete)'} | {sets} | {p['full']}/{p['scored']} | "
                 f"{pct(p['mean_turn_recall'])} | {per} | {kbs} | {kbr} |")
    L += ["", "Selection rule (a suggestion; the freeze is decided by hand): most full-evidence questions, then higher mean "
          "turn recall, then closer to Chanda's defaults. K is U1's best plus keys (PROTOCOL.md: K = U1 plus learned keys); "
          "the keys-alone variant (λ 0) is reported, not selectable."]
    bu, bk = res["best_U1"], res["best_K"]
    if bu and bk:
        s = {**res["variants"][bu]["sets"], **res["variants"][bk]["sets"]}
        keys = ("ranking.utility_lambda", "ranking.min_exposures", "read.learned_keys_weight", "read.learned_keys_top_k")
        defaults = {"read.learned_keys_weight": "1", "read.learned_keys_top_k": "20"}
        L += ["", "Suggested `eval/learn/frozen.set` (every key explicit; `run_learn.sh` refuses a missing one):", "", "```"]
        L += [f"{k}={s.get(k, defaults.get(k, '?'))}" for k in keys] + ["```"]
    return "\n".join(x.rstrip() for x in L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tune", required=True)
    ap.add_argument("--convs", help="space-separated (default: conv-30 conv-47 conv-49)")
    ap.add_argument("--md")
    ap.add_argument("--json")
    ap.add_argument("--best-u1", action="store_true", help="print the best U1's lambda and min_exposures, and exit")
    args = ap.parse_args(argv)
    convs = args.convs.split() if args.convs else list(DEV)
    res = summarise(collect(args.tune, convs), convs)
    if args.best_u1:
        b = res["best_U1"]
        if not b:
            print("no complete U1 variant", file=sys.stderr)
            return 1
        s = res["variants"][b]["sets"]
        print(s["ranking.utility_lambda"], s["ranking.min_exposures"])
        return 0
    md = markdown(res)
    if args.json:
        os.makedirs(os.path.dirname(os.path.abspath(args.json)), exist_ok=True)
        with open(args.json, "w") as f:
            json.dump(res, f, indent=1)
    if args.md:
        os.makedirs(os.path.dirname(os.path.abspath(args.md)), exist_ok=True)
        with open(args.md, "w") as f:
            f.write(md)
    else:
        print(md)
    return 0


if __name__ == "__main__":
    sys.exit(main())
