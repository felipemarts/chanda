#!/usr/bin/env python3
"""Report of the conflict study's test run (eval/conflicts/PROTOCOL.md):
results/conflicts.{md,json} from run_test.sh's work directory.

Per test question <work>/<qid>/: base/study.json, C0.ask.jsonl (the base),
C1.ask.jsonl (after the maintenance sleep), C1.sleep.json, C1.list.json and
C0.list.json (the conflicts queue), and the judged answers
<work>/llm-C0/D.jsonl, <work>/llm-C1/D.jsonl.

Primary measures (no LLM):
- outdated information delivered, in the stale-measure questions
  (kuturns.stale_measure): blocks holding the outdated turn, C0 and C1,
  exact paired sign test (two-sided);
- hidden evidence over all questions: needed turns (kuturns.needed for the
  qualifying knowledge-update questions, has_answer turns otherwise) in
  C0's block, missing from C1's because C1 dropped them as superseded;
  each case with both records' text and the verdict and confidence.
Secondary: accuracy C0 and C1 (Wilson 95%, exact paired sign test);
candidates, judged pairs and verdicts by question type; sleep calls,
tokens, cost and time per history. The decision rule's three conditions,
and five correct supersessions and five wrong ones (measures.classify_applied).
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import measures as M  # noqa: E402


def _load_project_cost():
    # Under its own name: eval/sleep and eval/learn have a project_cost.py too.
    import importlib.util
    spec = importlib.util.spec_from_file_location("conflicts_project_cost", os.path.join(HERE, "project_cost.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


PC = _load_project_cost()

TOTAL = 107
ALPHA = 0.05
MAX_HIDDEN = 2
NOT_ESTABLISHED = ("The claim \"conflict resolution reduces outdated information in the context without hiding valid "
                   "information\" was not established. sleep.conflicts stays off by default, and no other variant is proposed or built.")


def load_verdicts(path: str) -> dict[str, dict]:
    return {r["question_id"]: r for r in M.load_jsonl(path)} if os.path.exists(path) else {}


def collect(work: str, data: dict, ids: list[str], sleep_cost: dict) -> tuple[list[dict], list[str]]:
    rows, missing = [], []
    for qid in ids:
        d = os.path.join(work, qid)
        need = ["base/study.json", "C0.ask.jsonl", "C1.ask.jsonl", "C1.list.json"]
        if not all(os.path.exists(os.path.join(d, p)) for p in need):
            missing.append(qid)
            continue
        q = data[qid]
        meta = M.load_json(os.path.join(d, "base", "study.json"))
        c0 = M.load_jsonl(os.path.join(d, "C0.ask.jsonl"))[0]
        c1 = M.load_jsonl(os.path.join(d, "C1.ask.jsonl"))[0]
        sp = os.path.join(d, "C1.sleep.json")
        r = M.history_measures(q, meta, c0, c1, M.load_json(os.path.join(d, "C1.list.json")), M.load_json(sp) if os.path.exists(sp) else None)
        c0l = os.path.join(d, "C0.list.json")
        if os.path.exists(c0l):
            r["c0_queue"] = len(M.conflict_pairs(M.load_json(c0l)))
        r["sleep"]["cost_usd"] = (sleep_cost.get(f"C1-sleep-{qid}") or {}).get("usd")
        r["sleep"]["proxy_calls"] = (sleep_cost.get(f"C1-sleep-{qid}") or {}).get("calls")
        r["context_tokens"] = {"C0": c0.get("context_tokens"), "C1": c1.get("context_tokens")}
        rows.append(r)
    return rows, missing


def accuracy(v0: dict, v1: dict, rows: list[dict]) -> dict:
    ids = [r["question_id"] for r in rows if r["question_id"] in v0 and r["question_id"] in v1]
    a0 = {i: bool(v0[i]["verdict"]) for i in ids}
    a1 = {i: bool(v1[i]["verdict"]) for i in ids}
    p = M.paired_flags(a0, a1)
    out = {"n": len(ids), "paired": p}
    for c, a in (("C0", a0), ("C1", a1)):
        k = sum(a.values())
        lo, hi = M.wilson(k, len(ids))
        out[c] = {"correct": k, "accuracy": k / len(ids) if ids else None, "wilson95": [lo, hi]}
    by_type: dict[str, dict] = {}
    for r in rows:
        if r["question_id"] not in a0:
            continue
        t = by_type.setdefault(r["question_type"], {"n": 0, "C0": 0, "C1": 0})
        t["n"] += 1
        t["C0"] += a0[r["question_id"]]
        t["C1"] += a1[r["question_id"]]
    out["by_type"] = dict(sorted(by_type.items()))
    return out


def by_type_table(rows: list[dict]) -> dict:
    out: dict[str, dict] = {}
    for r in rows:
        t = out.setdefault(r["question_type"], {"histories": 0, "candidates": 0, "judged": 0, "verdicts": {}, "applied": 0})
        t["histories"] += 1
        t["candidates"] += r["candidates"]
        t["judged"] += r["judged"]
        t["applied"] += len(r["applied"])
        for k, v in r["verdicts"].items():
            t["verdicts"][k] = t["verdicts"].get(k, 0) + v
    return dict(sorted(out.items()))


def examples(rows: list[dict], cls: str, n: int = 5) -> list[dict]:
    out = []
    for r in rows:
        for s in r["applied"]:
            if s["class"] == cls:
                out.append({"question_id": r["question_id"], "question_type": r["question_type"], "old_id": s["old_id"], "new_id": s["new_id"],
                            "old": s.get("old"), "new": s.get("new"), "verdict": s.get("verdict"),
                            "confidence": s.get("pair_confidence") if s.get("pair_confidence") is not None else s.get("confidence"),
                            "why": s["why"]})
    return out[:n]


def build(work: str, data: dict, ids: list[str], cost: dict, calls_path: str | None) -> dict:
    calls = PC.call_costs(calls_path, PC.rates()) if calls_path else {}
    rows, missing = collect(work, data, ids, calls)
    st = [r for r in rows if r["stale_measure"]]
    stale = M.paired_flags({r["question_id"]: bool(r["stale_c0"]) for r in st}, {r["question_id"]: bool(r["stale_c1"]) for r in st})
    hid = [r for r in rows if r.get("hidden")]
    acc = accuracy(load_verdicts(os.path.join(work, "llm-C0", "D.jsonl")), load_verdicts(os.path.join(work, "llm-C1", "D.jsonl")), rows)
    classes: dict[str, int] = {}
    for r in rows:
        for s in r["applied"]:
            classes[s["class"]] = classes.get(s["class"], 0) + 1
    ku = [r["ku_pair"] for r in rows if r["ku_pair"]]
    sleep = [r["sleep"] for r in rows if r["sleep"]]
    complete = not missing and len(rows) == TOTAL and acc["n"] == len(rows)
    c1 = stale["second_true"] < stale["first_true"] and stale["p"] < ALPHA
    c2 = len(hid) <= MAX_HIDDEN
    p = acc["paired"]
    c3 = not (acc["n"] and acc["C1"]["correct"] < acc["C0"]["correct"] and p["p"] < ALPHA)
    decision = {
        "complete": complete,
        "conditions": [
            {"n": 1, "rule": "stale delivered falls from C0 to C1, exact paired sign test p < 0.05",
             "c0": stale["first_true"], "c1": stale["second_true"], "of": stale["n"], "p": stale["p"], "holds": c1},
            {"n": 2, "rule": f"hidden evidence in at most {MAX_HIDDEN} of the {TOTAL} questions", "questions": len(hid), "holds": c2},
            {"n": 3, "rule": "C1's accuracy is not lower than C0's with p < 0.05",
             "c0": acc["C0"]["correct"], "c1": acc["C1"]["correct"], "of": acc["n"], "p": p["p"], "holds": c3},
        ],
    }
    decision["established"] = complete and c1 and c2 and c3
    tag_cost = cost.get("by_tag") or {}
    return {
        "questions": len(rows), "expected": TOTAL, "missing": missing,
        "primary": {"stale": {**stale, "questions": [r["question_id"] for r in st],
                              "c0_only": [r["question_id"] for r in st if r["stale_c0"] and not r["stale_c1"]],
                              "c1_only": [r["question_id"] for r in st if r["stale_c1"] and not r["stale_c0"]]},
                    "hidden": {"questions": len(hid), "turns": sum(len(r["hidden"]) for r in hid),
                               "cases": [{"question_id": r["question_id"], "question_type": r["question_type"], **h} for r in hid for h in r["hidden"]]}},
        "secondary": {
            "accuracy": acc,
            "by_type": by_type_table(rows),
            "candidates": M.summary([r["candidates"] for r in rows]), "judged": M.summary([r["judged"] for r in rows]),
            "applied_by_class": dict(sorted(classes.items())),
            "ku_pairs": {"qualifying": len(ku), "same_event": sum(bool(k["same_event"]) for k in ku), "candidate": sum(k["candidate"] for k in ku),
                         "judged": sum(k["judged"] for k in ku), "applied": sum(bool(k["applied"]) for k in ku),
                         "reversed": sum(bool(k.get("applied_reversed")) for k in ku),
                         "scored": sum(k["correct"] is not None for k in ku), "correct": sum(k["correct"] is True for k in ku)},
            "sleep": {"llm_calls": M.summary([s["llm_calls"] for s in sleep]), "input_tokens": M.summary([s["input_tokens"] for s in sleep]),
                      "output_tokens": M.summary([s["output_tokens"] for s in sleep]), "seconds": M.summary([s["seconds"] for s in sleep]),
                      "cost_usd": M.summary([s["cost_usd"] for s in sleep if s.get("cost_usd") is not None])},
            "cost": {"spent_usd": cost.get("spent_usd"), "cap_usd": cost.get("cap_usd"),
                     "by_tag": {k: {"calls": v.get("calls"), "cost_usd": v.get("cost_usd"), "replayed": v.get("replayed")}
                                for k, v in sorted(tag_cost.items()) if k.startswith(("C0-", "C1-"))}},
        },
        "decision": decision,
        "examples": {"correct": examples(rows, "correct"), "wrong": examples(rows, "wrong")},
        "rows": rows,
    }


def pct(x) -> str:
    return "-" if x is None else f"{100 * x:.1f}%"


def snippet(rec: dict | None, n: int = 240) -> str:
    if not rec:
        return "(text not found)"
    t = " ".join((rec.get("text") or "").split())
    return f"[{rec.get('date')}, {rec.get('role')}, {rec.get('key')}] " + (t[:n] + ("..." if len(t) > n else ""))


def markdown(res: dict) -> str:
    pr, se, de = res["primary"], res["secondary"], res["decision"]
    s, h, a = pr["stale"], pr["hidden"], se["accuracy"]
    L = ["# Conflict resolution in sleep: test results", "",
         f"{res['questions']} of {res['expected']} test questions" + (f"; **missing: {', '.join(res['missing'])}**" if res["missing"] else "") + ".", "",
         "## Decision rule (PROTOCOL.md)", ""]
    for c in de["conditions"]:
        detail = {1: f"C0 {c.get('c0')}/{c.get('of')}, C1 {c.get('c1')}/{c.get('of')}, p = {c.get('p', 1):.4f}",
                  2: f"{c.get('questions')} questions", 3: f"C0 {c.get('c0')}/{c.get('of')}, C1 {c.get('c1')}/{c.get('of')}, p = {c.get('p', 1):.4f}"}[c["n"]]
        L.append(f"{c['n']}. {c['rule']}: {detail}: **{'holds' if c['holds'] else 'fails'}**")
    L.append("")
    if not de["complete"]:
        L.append("**The run is incomplete: the decision rule is not evaluated.**")
    elif de["established"]:
        L.append("All three conditions hold: conflict resolution reduces outdated information in the context without hiding valid information "
                 "(on these 107 LongMemEval questions, with the frozen settings).")
    else:
        L.append(NOT_ESTABLISHED)
    L += ["", "## Primary measures", "",
          f"- Outdated information delivered ({s['n']} stale-measure questions): C0 {s['first_true']}, C1 {s['second_true']}; "
          f"discordant: only C0 {s['first_only']}, only C1 {s['second_only']}; exact sign test p = {s['p']:.4f}.",
          f"- Hidden evidence: {h['questions']} questions ({h['turns']} turns).", ""]
    for c in h["cases"]:
        L += [f"  - {c['question_id']} ({c['question_type']}) turn {c['key']}, verdict {c.get('verdict')}, confidence {c.get('confidence')}",
              f"    - old (hidden): {snippet(c.get('old'))}", f"    - new: {snippet(c.get('new'))}"]
    L += ["", "## Secondary measures", ""]
    if a["n"]:
        L.append(f"- Accuracy (n = {a['n']}): C0 {a['C0']['correct']} ({pct(a['C0']['accuracy'])}, Wilson {pct(a['C0']['wilson95'][0])}-{pct(a['C0']['wilson95'][1])}), "
                 f"C1 {a['C1']['correct']} ({pct(a['C1']['accuracy'])}, Wilson {pct(a['C1']['wilson95'][0])}-{pct(a['C1']['wilson95'][1])}); "
                 f"only C0 {a['paired']['first_only']}, only C1 {a['paired']['second_only']}, sign test p = {a['paired']['p']:.4f}.")
    else:
        L.append("- Accuracy: no judged answers.")
    k = se["ku_pairs"]
    L += [f"- Knowledge-update (outdated, current) pairs: {k['qualifying']} qualifying; same event {k['same_event']}; candidate {k['candidate']}; "
          f"judged {k['judged']}; applied {k['applied']} (reversed {k['reversed']}); correct {k['correct']} of {k['scored']} scored.",
          f"- Applied supersessions by class: {json.dumps(se['applied_by_class'])}.",
          f"- Candidates per history: mean {se['candidates']['mean']}, max {se['candidates']['max']}; judged: mean {se['judged']['mean']}, max {se['judged']['max']}.",
          "", "| question type | histories | candidates | judged | verdicts | applied | accuracy C0 | accuracy C1 |", "|---|---|---|---|---|---|---|---|"]
    for t, x in se["by_type"].items():
        bt = a["by_type"].get(t, {})
        L.append(f"| {t} | {x['histories']} | {x['candidates']} | {x['judged']} | {json.dumps(x['verdicts'])} | {x['applied']} | "
                 f"{bt.get('C0', '-')}/{bt.get('n', '-')} | {bt.get('C1', '-')}/{bt.get('n', '-')} |")
    sl = se["sleep"]
    L += ["", f"Sleep per history (mean / median / max): calls {sl['llm_calls']['mean']} / {sl['llm_calls']['median']} / {sl['llm_calls']['max']}; "
          f"input tokens {sl['input_tokens']['mean']} / {sl['input_tokens']['median']} / {sl['input_tokens']['max']}; "
          f"output tokens {sl['output_tokens']['mean']} / {sl['output_tokens']['median']} / {sl['output_tokens']['max']}; "
          f"seconds {sl['seconds']['mean']} / {sl['seconds']['median']} / {sl['seconds']['max']}; "
          f"US$ (paid-equivalent) {sl['cost_usd']['mean']} / {sl['cost_usd']['median']} / {sl['cost_usd']['max']}, total {sl['cost_usd']['total']}.",
          f"Spent (cost file): US$ {se['cost']['spent_usd']} of the US$ {se['cost']['cap_usd']} cap.", "",
          "| question | type | candidates | judged | verdicts | applied | stale C0 / C1 | hidden | sleep calls | sleep s | sleep US$ |",
          "|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in res["rows"]:
        sv = r["sleep"]
        stl = f"{'yes' if r['stale_c0'] else 'no'} / {'yes' if r['stale_c1'] else 'no'}" if r["stale_measure"] else "-"
        cu = sv.get("cost_usd")
        L.append(f"| {r['question_id']} | {r['question_type']} | {r['candidates']} | {r['judged']} | {json.dumps(r['verdicts'])} | {len(r['applied'])} | "
                 f"{stl} | {len(r.get('hidden') or [])} | {sv.get('llm_calls', '-')} | {sv.get('seconds', '-')} | {f'{cu:.5f}' if cu is not None else '-'} |")
    for title, key in (("Five correct supersessions", "correct"), ("Five wrong supersessions", "wrong")):
        L += ["", f"## {title}", ""]
        ex = res["examples"][key]
        if not ex:
            L.append("None.")
        for e in ex:
            L += [f"- {e['question_id']} ({e['question_type']}): verdict {e.get('verdict')}, confidence {e.get('confidence')}"
                  + (f"; {'; '.join(e['why'])}" if e["why"] else ""),
                  f"  - old: {snippet(e.get('old'))}", f"  - new: {snippet(e.get('new'))}"]
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True)
    ap.add_argument("--data", required=True)
    ap.add_argument("--ids", help="space-separated (default: the 107 test questions)")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"))
    ap.add_argument("--calls", default=os.path.join(HERE, "llm_calls.jsonl"))
    ap.add_argument("--md")
    ap.add_argument("--json")
    a = ap.parse_args(argv)
    ids = a.ids.split() if a.ids else M.set_ids(M.load_sets(), "test")
    want = set(ids)
    data = {q["question_id"]: q for q in M.load_json(a.data) if q["question_id"] in want}
    cost = M.load_json(a.cost) if a.cost and os.path.exists(a.cost) else {}
    res = build(a.work, data, ids, cost, a.calls)
    if a.json:
        with open(a.json, "w") as f:
            json.dump(res, f, indent=1, ensure_ascii=False)
            f.write("\n")
    md = markdown(res)
    if a.md:
        with open(a.md, "w") as f:
            f.write(md)
    else:
        print(md)
    return 0


if __name__ == "__main__":
    sys.exit(main())
