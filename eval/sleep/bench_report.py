"""Data for BENCH.md (the diagnostic bench on 9 LongMemEval tuning
questions, eval/sleep/PROTOCOL.md), from the work directory run_bench.sh
fills:

  <work>/<qid>/{before,X,L}.ask.jsonl   blocks before sleep and after each sleep
  <work>/<qid>/{X,L}.sleep.json         sleep results (counts, cost, fichas)
  <work>/<qid>/outcomes.jsonl           what went into the database
  <work>/llm-{before,X,L}/D.jsonl       official reader and judge (judge.py output)

Writes JSON (--json) and a markdown digest (--md): per question the
verdicts, answers, block composition and evidence; per sleep the counts,
discards and the ficha text. The same question is asked before and after,
so this measures memorisation, not transfer (the protocol says so).
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))

import common  # noqa: E402

ROUNDS = ("before", "X", "L")


def load_json(p: str):
    if not os.path.exists(p):
        return None
    with open(p) as f:
        return json.load(f)


def one(p: str) -> dict | None:
    if not os.path.exists(p):
        return None
    rows = common.read_jsonl(p)
    return rows[0] if rows else None


def build(work: str, ids: list[str], cost: dict | None) -> dict:
    judged = {}
    for r in ROUNDS:
        p = os.path.join(work, f"llm-{r}", "D.jsonl")
        judged[r] = {x["question_id"]: x for x in common.read_jsonl(p)} if os.path.exists(p) else {}
    qs = []
    for qid in ids:
        q = {"question_id": qid, "rounds": {}, "sleep": {}}
        for r in ROUNDS:
            a = one(os.path.join(work, qid, f"{r}.ask.jsonl"))
            j = judged[r].get(qid)
            if a is None:
                continue
            q["question"], q["question_type"], q["gold"] = a["question"], a.get("question_type"), a["gold"]
            q["rounds"][r] = {
                "verdict": None if j is None else bool(j["verdict"]),
                "answer": None if j is None else j["answer"],
                "reader_tokens": None if j is None else (j.get("reader") or {}).get("tokens"),
                "judge_tokens": None if j is None else (j.get("judge") or {}).get("tokens"),
                "context_tokens": a["context_tokens"], "items": len(a["items"]),
                "types": {t: sum(1 for i in a["items"] if i["type"] == t) for t in sorted({i["type"] for i in a["items"]})},
                "sleep_tokens": a["sleep_tokens"], "sleep_share": a["sleep_share"],
                "evidence": {k: a["evidence"][k] for k in ("reached", "full", "reached_or_cited", "full_or_cited")}
                            | {"turns": len(a["evidence"]["turns"])},
                "block_file": os.path.join(qid, f"{r}.ask.jsonl"),
            }
        outs = os.path.join(work, qid, "outcomes.jsonl")
        if os.path.exists(outs):
            q["outcomes"] = common.read_jsonl(outs)
        for m in ("X", "L"):
            s = load_json(os.path.join(work, qid, f"{m}.sleep.json"))
            if s is None:
                continue
            o = s.get("output") or {}
            g = (s.get("counts") or {}).get("guided") or {}
            fichas = [f for f in s.get("fichas") or [] if f["id"] in set(g.get("fichas") or [])]
            tag = (cost or {}).get("by_tag", {}).get(f"BENCH-L-sleep") if m == "L" else None
            q["sleep"][m] = {"status": o.get("status"), "cost": o.get("cost"), "seconds": (s.get("wall_ms") or 0) / 1000,
                             "guided": g, "fichas": [{"title": f["title"], "lines": [i["text"] for i in f["items"]]} for f in fichas],
                             "set": s.get("chanda_set")}
            if tag:
                q["sleep"][m]["all_questions_proxy_cost_usd"] = tag.get("cost_usd")
        qs.append(q)
    res = {"work": os.path.abspath(work), "questions": qs}
    for r in ROUNDS:
        v = [q["rounds"][r]["verdict"] for q in qs if r in q["rounds"] and q["rounds"][r]["verdict"] is not None]
        res[f"correct_{r}"] = f"{sum(v)}/{len(v)}"
    if cost:
        res["proxy_by_tag"] = {t: v for t, v in (cost.get("by_tag") or {}).items() if t.startswith("BENCH-")}
    return res


def markdown(res: dict) -> str:
    L = ["## Verdicts (same question before and after sleep: memorisation, not transfer)", "",
         f"Correct: before {res['correct_before']}, after X (extractive) {res['correct_X']}, after L (LLM) {res['correct_L']}.", "",
         "| Question | Type | Before | X | L | Sleep share X / L | Evidence full before / X / L |", "| --- | --- | --- | --- | --- | --- | --- |"]
    mark = {True: "✓", False: "✗", None: "-"}
    for q in res["questions"]:
        rd = q["rounds"]
        g = lambda r, k: rd[r][k] if r in rd else None  # noqa: E731
        share = " / ".join("-" if g(r, "sleep_share") is None else f"{100 * g(r, 'sleep_share'):.0f}%" for r in ("X", "L"))
        ev = " / ".join("-" if r not in rd else ("yes" if rd[r]["evidence"]["full"] else f"{rd[r]['evidence']['reached']}/{rd[r]['evidence']['turns']}") for r in ("before", "X", "L"))
        L.append(f"| {q['question_id']} | {q.get('question_type')} | {mark[g('before', 'verdict')]} | {mark[g('X', 'verdict')]} | {mark[g('L', 'verdict')]} | {share} | {ev} |")
    L += ["", "## Sleep", "", "| Question | Mode | Status | LLM calls | Tokens in/out | Time s | Groups digested | Lines written | Discarded |", "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
    for q in res["questions"]:
        for m, s in q["sleep"].items():
            c = s.get("cost") or {}
            g = s["guided"]
            L.append(f"| {q['question_id']} | {m} | {s['status']} | {c.get('llm_calls')} | {c.get('input_tokens')}/{c.get('output_tokens')} | "
                     f"{s['seconds']:.1f} | {g.get('groups_digested')} | {g.get('lines_written')} | {json.dumps(g.get('lines_discarded') or {}, sort_keys=True)} |")
    L += ["", "## Fichas", ""]
    for q in res["questions"]:
        for m, s in q["sleep"].items():
            for f in s["fichas"]:
                L += [f"### {q['question_id']} {m}: {f['title']}", "", "```"] + f["lines"] + ["```", ""]
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True)
    ap.add_argument("--ids", required=True, help="comma-separated question ids, in order")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"))
    ap.add_argument("--md")
    ap.add_argument("--json")
    args = ap.parse_args(argv)
    res = build(args.work, args.ids.split(","), load_json(args.cost) if args.cost else None)
    md = markdown(res)
    if args.json:
        with open(args.json, "w") as f:
            json.dump(res, f, indent=1, ensure_ascii=False)
    if args.md:
        with open(args.md, "w") as f:
            f.write(md)
    else:
        print(md)
    return 0


if __name__ == "__main__":
    sys.exit(main())
