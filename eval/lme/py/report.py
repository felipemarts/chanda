"""Accuracy per condition (total and per type) with Wilson 95% intervals,
context/latency statistics, cost per condition, and eval/review.csv.

Reads <raw-dir>/<cond>.jsonl (judge.py output) for each condition present,
and optionally the proxy's cost.json for the cost per condition (tags are
"<cond>-<role>", e.g. C-ingest, B-reader, E-sleep).

review.csv is WIDE: one row per question (sample order), with columns
question_id, question_type, abstention, question, gold_answer, then for
each condition X: answer_X, verdict_X (yes/no/empty). Wide was chosen so a
person reviewing by hand sees one question and all conditions on one line.

Per-type rows group by question_type as the official print_qa_metrics does
(abstention questions stay in their type); an extra "abstention" row
groups the questions whose id contains "_abs".
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import sys

import common

Z95 = 1.959963984540054


def wilson(k: int, n: int, z: float = Z95) -> tuple[float, float]:
    """Wilson score interval for k successes in n trials."""
    if n <= 0:
        return (0.0, 1.0)
    p = k / n
    den = 1 + z * z / n
    centre = (p + z * z / (2 * n)) / den
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    lo = 0.0 if k == 0 else max(0.0, centre - half)
    hi = 1.0 if k == n else min(1.0, centre + half)
    return (lo, hi)


def percentile(xs: list[float], q: float) -> float | None:
    """Nearest-rank percentile (q in [0, 100])."""
    xs = sorted(x for x in xs if x is not None)
    if not xs:
        return None
    k = max(1, math.ceil(q / 100 * len(xs)))
    return xs[k - 1]


def acc_row(records: list[dict]) -> dict:
    n = len(records)
    k = sum(1 for r in records if r["verdict"])
    lo, hi = wilson(k, n)
    return {"n": n, "correct": k, "accuracy": (k / n) if n else None, "wilson95": [lo, hi]}


def summarize(by_cond: dict[str, list[dict]], cost: dict | None = None) -> dict:
    out = {"estimator": "context tokens = ceil(chars/4)", "conditions": {}}
    for cond, recs in sorted(by_cond.items()):
        types = sorted({r["question_type"] for r in recs})
        per_type = {t: acc_row([r for r in recs if r["question_type"] == t]) for t in types}
        per_type["abstention"] = acc_row([r for r in recs if common.is_abstention(r["question_id"])])
        ctx = [r.get("context_tokens", 0) for r in recs]
        lat = [r.get("retrieval_latency_ms") for r in recs]
        ing = [r.get("ingest_seconds") for r in recs]
        tok = {"reader": {"prompt": 0, "cache_hit": 0, "completion": 0}, "judge": {"prompt": 0, "cache_hit": 0, "completion": 0}}
        for r in recs:
            for role in ("reader", "judge"):
                for k, v in ((r.get(role) or {}).get("tokens") or {}).items():
                    tok[role][k] += v
        c = {
            "total": acc_row(recs),
            "by_type": per_type,
            "context_tokens": {"mean": sum(ctx) / len(ctx) if ctx else None, "max": max(ctx) if ctx else None},
            "retrieval_latency_ms": {"p50": percentile(lat, 50), "p95": percentile(lat, 95)},
            "ingest_seconds": {"p50": percentile(ing, 50), "p95": percentile(ing, 95),
                               "total": sum(x for x in ing if x is not None) if any(x is not None for x in ing) else None},
            "tokens": tok,
        }
        if cost:
            tags = {t: v for t, v in (cost.get("by_tag") or {}).items() if t.split("-", 1)[0] == cond}
            c["cost_usd_by_tag"] = {t: v.get("cost_usd", 0) + v.get("unverified_usd", 0) for t, v in sorted(tags.items())}
            c["cost_usd"] = sum(c["cost_usd_by_tag"].values())
        out["conditions"][cond] = c
    if cost:
        out["cost_total_usd"] = cost.get("spent_usd")
        out["cap_usd"] = cost.get("cap_usd")
    return out


def fmt_pct(row: dict) -> str:
    if not row["n"]:
        return "-"
    lo, hi = row["wilson95"]
    return f"{100 * row['accuracy']:.1f}% [{100 * lo:.1f}, {100 * hi:.1f}] ({row['correct']}/{row['n']})"


def markdown(summary: dict) -> str:
    conds = list(summary["conditions"])
    lines = ["| type | " + " | ".join(conds) + " |", "|---|" + "---|" * len(conds)]
    lines.append("| **total** | " + " | ".join(fmt_pct(summary["conditions"][c]["total"]) for c in conds) + " |")
    types = sorted({t for c in conds for t in summary["conditions"][c]["by_type"]})
    for t in types:
        cells = [fmt_pct(summary["conditions"][c]["by_type"].get(t, {"n": 0})) for c in conds]
        lines.append(f"| {t} | " + " | ".join(cells) + " |")
    lines.append("")
    lines.append("Accuracy with Wilson 95% interval (correct/n).")
    return "\n".join(lines)


def write_review(path: str, by_cond: dict[str, list[dict]], order: list[str] | None = None) -> None:
    conds = sorted(by_cond)
    rows: dict[str, dict] = {}
    for cond in conds:
        for r in by_cond[cond]:
            row = rows.setdefault(r["question_id"], {
                "question_id": r["question_id"],
                "question_type": r["question_type"],
                "abstention": "yes" if common.is_abstention(r["question_id"]) else "no",
                "question": r["question"],
                "gold_answer": r["gold_answer"],
            })
            row[f"answer_{cond}"] = r["answer"]
            row[f"verdict_{cond}"] = "yes" if r["verdict"] else "no"
    ids = [i for i in (order or []) if i in rows] + sorted(i for i in rows if i not in set(order or []))
    fields = ["question_id", "question_type", "abstention", "question", "gold_answer"]
    for cond in conds:
        fields += [f"answer_{cond}", f"verdict_{cond}"]
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=fields, quoting=csv.QUOTE_ALL)
        w.writeheader()
        for i in ids:
            w.writerow({k: rows[i].get(k, "") for k in fields})


def load_raw(raw_dir: str) -> dict[str, list[dict]]:
    by_cond = {}
    for cond in common.CONDITIONS:
        p = os.path.join(raw_dir, f"{cond}.jsonl")
        if os.path.exists(p):
            recs = common.read_jsonl(p)
            ids = [r["question_id"] for r in recs]
            if len(ids) != len(set(ids)):
                raise ValueError(f"{p}: duplicate question ids")
            by_cond[cond] = recs
    return by_cond


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--raw-dir", required=True)
    ap.add_argument("--cost", help="lmeproxy cost.json")
    ap.add_argument("--sample", help="eval/sample.json, for review.csv row order")
    ap.add_argument("--review", help="review.csv path (eval/review.csv)")
    ap.add_argument("--summary", help="write the summary JSON here")
    args = ap.parse_args(argv)
    by_cond = load_raw(args.raw_dir)
    if not by_cond:
        sys.exit(f"no <cond>.jsonl files in {args.raw_dir}")
    cost = None
    if args.cost and os.path.exists(args.cost):
        with open(args.cost) as f:
            cost = json.load(f)
    s = summarize(by_cond, cost)
    if args.summary:
        with open(args.summary, "w") as f:
            json.dump(s, f, indent=2)
    if args.review:
        order = common.load_sample_ids(args.sample) if args.sample else None
        write_review(args.review, by_cond, order)
    print(markdown(s))
    return 0


if __name__ == "__main__":
    sys.exit(main())
