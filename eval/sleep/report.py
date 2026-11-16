"""RESULTS tables of the LoCoMo sleep transfer test (eval/sleep/PROTOCOL.md
"Measures" and "Decision rule"), from the work directory run_locomo.sh
fills:

  <work>/<conv>/out/base.use.{ask,answers}.jsonl      use half, before sleep
  <work>/<conv>/out/<C>.sleep.json                    C in S0..S3
  <work>/<conv>/out/<C>.test.{ask,answers}.jsonl      C in B, S0..S3
  <work>/<conv>/out/<C>.ceiling.{ask,answers}.jsonl   C in S0..S3

Writes markdown (--md) and JSON (--json). Accuracy has Wilson 95%
intervals; paired comparisons use the exact two-sided sign test on the
questions only one condition answered correctly. A condition missing from
some conversation is reported on the conversations it has, and marked.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))

import common  # noqa: E402

Z95 = 1.959963984540054
CONVS = ("conv-30", "conv-47", "conv-49")
CHANDA = ("S0", "S1", "S2", "S3")
CONDS = ("B",) + CHANDA
CATS = {1: "multi-hop", 2: "temporal", 3: "open-domain", 4: "single-hop"}
PRICES = os.path.join(HERE, "..", "cmd", "lmeproxy", "prices.deepseek.json")


def wilson(k: int, n: int, z: float = Z95) -> tuple[float, float]:
    if n <= 0:
        return (0.0, 1.0)
    p = k / n
    den = 1 + z * z / n
    c = (p + z * z / (2 * n)) / den
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return (0.0 if k == 0 else max(0.0, c - h), 1.0 if k == n else min(1.0, c + h))


def sign_test(b: int, c: int) -> float:
    """Exact two-sided sign test: b and c discordant pairs, p under Bin(n, 1/2)."""
    n = b + c
    if n == 0:
        return 1.0
    k = min(b, c)
    tail = sum(math.comb(n, i) for i in range(k + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def acc(recs: list[dict]) -> dict:
    n = len(recs)
    k = sum(1 for r in recs if r["verdict"])
    lo, hi = wilson(k, n)
    return {"n": n, "correct": k, "accuracy": k / n if n else None, "wilson95": [lo, hi]}


def fmt_acc(a: dict) -> str:
    if not a["n"]:
        return "-"
    return f"{100 * a['accuracy']:.1f}% [{100 * a['wilson95'][0]:.1f}, {100 * a['wilson95'][1]:.1f}] ({a['correct']}/{a['n']})"


def load(path: str) -> list[dict] | None:
    return common.read_jsonl(path) if os.path.exists(path) else None


def load_json(path: str):
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


def collect(work: str) -> dict:
    data = {"test": {c: {} for c in CONDS}, "ceiling": {c: {} for c in CHANDA}, "use": {}, "sleep": {c: {} for c in CHANDA},
            "test_ask": {c: {} for c in CONDS}}
    for conv in CONVS:
        od = os.path.join(work, conv, "out")
        u = load(os.path.join(od, "base.use.answers.jsonl"))
        if u is not None:
            data["use"][conv] = u
        for c in CONDS:
            a = load(os.path.join(od, f"{c}.test.answers.jsonl"))
            if a is not None:
                data["test"][c][conv] = a
            k = load(os.path.join(od, f"{c}.test.ask.jsonl"))
            if k is not None:
                data["test_ask"][c][conv] = k
        for c in CHANDA:
            a = load(os.path.join(od, f"{c}.ceiling.answers.jsonl"))
            if a is not None:
                data["ceiling"][c][conv] = a
            s = load_json(os.path.join(od, f"{c}.sleep.json"))
            if s is not None:
                data["sleep"][c][conv] = s
    return data


def pooled(by_conv: dict) -> list[dict]:
    return [r for conv in CONVS for r in by_conv.get(conv, [])]


def accuracy_table(by_cond: dict) -> dict:
    out = {}
    for c, by_conv in by_cond.items():
        recs = pooled(by_conv)
        if not recs:
            continue
        row = {"total": acc(recs), "conversations": sorted(by_conv), "by_category": {}}
        for k, name in CATS.items():
            row["by_category"][name] = acc([r for r in recs if r.get("category") == k])
        out[c] = row
    return out


def paired(a: list[dict], b: list[dict]) -> dict:
    va = {r["question_id"]: bool(r["verdict"]) for r in a}
    vb = {r["question_id"]: bool(r["verdict"]) for r in b}
    common_ids = sorted(set(va) & set(vb))
    only_a = sum(1 for q in common_ids if va[q] and not vb[q])
    only_b = sum(1 for q in common_ids if vb[q] and not va[q])
    return {"n": len(common_ids), "first_only": only_a, "second_only": only_b, "p": sign_test(only_a, only_b)}


def evidence_table(asks: dict) -> dict:
    out = {}
    for c, by_conv in asks.items():
        recs = [r for r in pooled(by_conv) if (r.get("evidence") or {}).get("scored")]
        if not recs:
            continue
        full = sum(1 for r in recs if r["evidence"]["full"])
        full2 = sum(1 for r in recs if r["evidence"].get("full_or_cited"))
        turns = sum(len(r["evidence"]["turns"]) for r in recs)
        reached = sum(r["evidence"]["reached"] for r in recs)
        reached2 = sum(r["evidence"].get("reached_or_cited", r["evidence"]["reached"]) for r in recs)
        out[c] = {"scored": len(recs), "full": full, "full_or_cited": full2, "turns": turns, "turns_reached": reached,
                  "turns_reached_or_cited": reached2}
    return out


def share_table(asks: dict) -> dict:
    out = {}
    for c, by_conv in asks.items():
        recs = pooled(by_conv)
        if not recs:
            continue
        blk = sum(r.get("context_tokens", 0) for r in recs)
        slp = sum(r.get("sleep_tokens", 0) for r in recs)
        shares = [r.get("sleep_share", 0.0) for r in recs]
        out[c] = {"blocks": len(recs), "blocks_with_sleep_items": sum(1 for r in recs if r.get("sleep_items", 0) > 0),
                  "mean_share": sum(shares) / len(shares), "max_share": max(shares),
                  "pooled_share": slp / blk if blk else 0.0}
    return out


def tag_cost(cost: dict | None, tag: str) -> float | None:
    if not cost:
        return None
    t = (cost.get("by_tag") or {}).get(tag)
    if t is None:
        return None
    return float(t.get("cost_usd", 0)) + float(t.get("unverified_usd", 0))


def priced(tin: int, tout: int) -> float:
    with open(PRICES) as f:
        r = json.load(f)["models"]["deepseek-flash"]["offpeak"]
    return (tin * r["input_cache_miss"] + tout * r["output"]) / 1e6


def sleep_table(sleep: dict, cost: dict | None) -> dict:
    out = {}
    for c, by_conv in sleep.items():
        for conv, s in sorted(by_conv.items()):
            o = s.get("output") or {}
            k = o.get("cost") or {}
            counts = s.get("counts") or {}
            g = counts.get("guided") or {}
            tin, tout = int(k.get("input_tokens", 0)), int(k.get("output_tokens", 0))
            billed = tag_cost(cost, f"{c}-sleep-{conv}")
            disc = g.get("lines_discarded") or {}
            out.setdefault(c, {})[conv] = {
                "status": o.get("status"), "phases": o.get("phases"), "set": s.get("chanda_set"),
                "budget": s.get("budget"), "llm_calls": int(k.get("llm_calls", 0)), "input_tokens": tin, "output_tokens": tout,
                "cost_usd": billed if billed is not None else priced(tin, tout), "cost_source": "proxy" if billed is not None else "priced",
                "seconds": (s.get("wall_ms") or 0) / 1000, "digest": g.get("digest"),
                "candidate_groups": g.get("candidate_groups"), "groups": g.get("groups"),
                "groups_digested": g.get("groups_digested"), "groups_skipped": g.get("groups_skipped"),
                "groups_left": g.get("groups_left"), "lines_written": g.get("lines_written"),
                "lines_discarded": sum(disc.values()) if disc else (0 if g else None), "discarded_by_reason": disc,
                "fichas": len(g.get("fichas") or []), "facts_created": o.get("created"),
            }
    return out


def build(work: str, cost: dict | None) -> dict:
    d = collect(work)
    res = {"work": os.path.abspath(work), "conversations": list(CONVS)}
    res["test_accuracy"] = accuracy_table(d["test"])
    res["paired_vs_S0"] = {}
    s0 = pooled(d["test"]["S0"])
    for c in ("S1", "S2", "S3", "B"):
        other = pooled(d["test"][c])
        if s0 and other:
            p = paired(other, s0)
            p["accuracy"] = acc(other)["accuracy"]
            p["accuracy_S0"] = acc([r for r in s0 if r["question_id"] in {x["question_id"] for x in other}])["accuracy"]
            if c in ("S1", "S2"):
                p["rule_met"] = p["accuracy"] > p["accuracy_S0"] and p["p"] < 0.05
            res["paired_vs_S0"][c] = p
    res["full_evidence_test"] = evidence_table(d["test_ask"])
    res["sleep"] = sleep_table(d["sleep"], cost)
    res["sleep_cost_total"] = {c: sum(v["cost_usd"] for v in res["sleep"].get(c, {}).values()) for c in CHANDA}
    res["gain_per_dollar"] = {}
    ta = res["test_accuracy"]
    for c in ("S1", "S2"):
        if c in ta and "S0" in ta:
            gain = 100 * (ta[c]["total"]["accuracy"] - ta["S0"]["total"]["accuracy"])
            usd = res["sleep_cost_total"].get(c, 0.0)
            res["gain_per_dollar"][c] = {"gain_points": gain, "sleep_usd": usd,
                                         "points_per_usd": gain / usd if usd > 0 else None}
    res["sleep_share_test"] = share_table(d["test_ask"])
    res["ceiling_use_half"] = accuracy_table(d["ceiling"])
    if d["use"]:
        res["use_half_before_sleep"] = acc(pooled(d["use"]))
    unparsed = 0
    for by in list(d["test"].values()) + list(d["ceiling"].values()) + [d["use"]]:
        unparsed += sum(1 for r in pooled(by) if r.get("verdict_parse") == "unparsed")
    res["unparsed_verdicts"] = unparsed
    if cost:
        res["proxy_spent_usd"] = cost.get("spent_usd")
        res["proxy_cap_usd"] = cost.get("cap_usd")
        res["proxy_replayed_calls"] = (cost.get("total") or {}).get("replayed")
    return res


def markdown(res: dict) -> str:
    L = []
    ta = res["test_accuracy"]
    L += ["## Accuracy on the test half (3 conversations pooled)", "",
          "| Condition | Total | " + " | ".join(f"{n} ({k})" for k, n in CATS.items()) + " | Conversations |",
          "| --- | --- | " + "--- | " * len(CATS) + "--- |"]
    for c in CONDS:
        if c in ta:
            row = ta[c]
            L.append(f"| {c} | {fmt_acc(row['total'])} | " + " | ".join(fmt_acc(row["by_category"][n]) for n in CATS.values())
                     + f" | {', '.join(row['conversations'])}{'' if len(row['conversations']) == 3 else ' (incomplete)'} |")
    L += ["", "Accuracy with Wilson 95% interval (correct/n).", ""]
    L += ["## Paired against S0 (exact two-sided sign test)", "",
          "| Pair | n | first only | S0 only | p | Decision rule (higher than S0 and p < 0.05) |", "| --- | --- | --- | --- | --- | --- |"]
    for c, p in res["paired_vs_S0"].items():
        rule = "—" if "rule_met" not in p else ("**met**" if p["rule_met"] else "not established")
        L.append(f"| {c} vs S0 | {p['n']} | {p['first_only']} | {p['second_only']} | {p['p']:.3f} | {rule} |")
    L += ["", "S1 and S2 are each tested on their own; no correction for the two tests is applied.", ""]
    L += ["## Full evidence in the block (test half, no LLM)", "",
          "| Condition | Scored | Full evidence | Full, crediting citing fichas | Turns reached | Turns reached or cited |",
          "| --- | --- | --- | --- | --- | --- |"]
    for c, e in res["full_evidence_test"].items():
        L.append(f"| {c} | {e['scored']} | {e['full']} | {e['full_or_cited']} | {e['turns_reached']}/{e['turns']} | {e['turns_reached_or_cited']}/{e['turns']} |")
    L += ["", "## Sleep per mode and conversation", "",
          "| Condition | Conversation | Status | LLM calls | Tokens in/out | Cost US$ | Time s | Groups (cand./merged/digested/skipped/left) | Lines written | Discarded (by reason) | Fichas |",
          "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
    for c, by in res["sleep"].items():
        for conv, s in by.items():
            groups = "/".join("-" if s[k] is None else str(s[k]) for k in ("candidate_groups", "groups", "groups_digested", "groups_skipped", "groups_left"))
            disc = "-" if s["lines_discarded"] is None else f"{s['lines_discarded']} {json.dumps(s['discarded_by_reason'], sort_keys=True) if s['discarded_by_reason'] else ''}".strip()
            L.append(f"| {c} | {conv} | {s['status']} | {s['llm_calls']} | {s['input_tokens']}/{s['output_tokens']} | "
                     f"{s['cost_usd']:.4f}{'' if s['cost_source'] == 'proxy' else ' (priced)'} | {s['seconds']:.1f} | {groups} | "
                     f"{'-' if s['lines_written'] is None else s['lines_written']} | {disc} | {s['fichas']} |")
    L += ["", "## Gain per dollar", "", "| Condition | Gain over S0 (points) | Sleep cost US$ | Points per US$ |", "| --- | --- | --- | --- |"]
    for c, g in res["gain_per_dollar"].items():
        ppd = "n/a (no LLM cost)" if g["points_per_usd"] is None else f"{g['points_per_usd']:.1f}"
        L.append(f"| {c} | {g['gain_points']:+.1f} | {g['sleep_usd']:.4f} | {ppd} |")
    L += ["", "## Share of the block taken by sleep items (facts and pages, test half)", "",
          "| Condition | Blocks | With sleep items | Mean share | Max share | Pooled share |", "| --- | --- | --- | --- | --- | --- |"]
    for c, s in res["sleep_share_test"].items():
        L.append(f"| {c} | {s['blocks']} | {s['blocks_with_sleep_items']} | {100 * s['mean_share']:.1f}% | {100 * s['max_share']:.1f}% | {100 * s['pooled_share']:.1f}% |")
    L += ["", "## Ceiling: use half asked again after sleep (memorisation, not transfer)", "",
          "| Condition | Accuracy |", "| --- | --- |"]
    if "use_half_before_sleep" in res:
        L.append(f"| before sleep (base) | {fmt_acc(res['use_half_before_sleep'])} |")
    for c in CHANDA:
        if c in res["ceiling_use_half"]:
            L.append(f"| {c} | {fmt_acc(res['ceiling_use_half'][c]['total'])} |")
    L += ["", f"Unparsed judge verdicts (scored WRONG): {res['unparsed_verdicts']}."]
    if "proxy_spent_usd" in res:
        L.append(f"Proxy spend: US${res['proxy_spent_usd']:.4f} of the US${res['proxy_cap_usd']} cap; replayed calls {res['proxy_replayed_calls']}.")
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True, help="eval/sleep/work/locomo")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"))
    ap.add_argument("--md", help="markdown output (default: stdout)")
    ap.add_argument("--json", help="JSON output")
    args = ap.parse_args(argv)
    cost = load_json(args.cost) if args.cost else None
    res = build(args.work, cost)
    md = markdown(res)
    if args.json:
        with open(args.json, "w") as f:
            json.dump(res, f, indent=1)
    if args.md:
        with open(args.md, "w") as f:
            f.write(md)
    else:
        print(md)
    return 0


if __name__ == "__main__":
    sys.exit(main())
