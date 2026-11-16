#!/usr/bin/env python3
"""Part 0 of the conflict study (development set, no LLM): summarise the
work directory precheck.sh fills.

Per development question (work/<qid>/): base/study.json (turn -> event),
scan-<min_cosine>.json (conflicts scan -details on the base) and
D2.ask.jsonl (the question asked once on a copy of the base).

For each knowledge-update question that qualifies (kuturns.py), at each
min_cosine: the near-duplicate refusals (total and by reason) and whether
one links the outdated turn's event to the current one; the number of
detection candidates and whether the (outdated, current) pair is one.
For every history: candidates per history (mean, median, max), for the
knowledge-update and the other questions. From the D2 blocks (item 1 of
the precheck): in the stale-measure questions, blocks holding the
outdated turn, and holding it without the current one.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import kuturns  # noqa: E402
import measures as M  # noqa: E402


def links(rows: list[dict], a: str, b: str) -> bool:
    return bool(a and b and a != b) and any({r.get("older_id"), r.get("newer_id")} == {a, b} for r in rows)


def by_reason(rows: list[dict]) -> dict:
    out: dict[str, int] = {}
    for r in rows:
        k = str(r.get("reason") or "unknown")
        out[k] = out.get(k, 0) + 1
    return dict(sorted(out.items()))


def question_row(q: dict, wdir: str, mcs: list[str]) -> dict:
    qid = q["question_id"]
    meta = M.load_json(os.path.join(wdir, "base", "study.json"))
    ev = M.turn_events(meta)
    kk = M.ku_keys(q)
    row = {"question_id": qid, "question_type": q["question_type"], "knowledge_update": q["question_type"] == "knowledge-update",
           "qualifies": kk is not None, "stale_measure": kuturns.stale_measure(q), "same_day": kuturns.same_day(q),
           "turns": len(meta.get("turns") or []), "by_min_cosine": {}}
    eo = ec = ""
    if kk:
        eo, ec = ev.get(kk[0], ""), ev.get(kk[1], "")
        row.update(outdated_key=kk[0], current_key=kk[1], outdated_event=eo, current_event=ec,
                   same_event=bool(eo) and eo == ec, needed=M.needed_keys(q))
    for mc in mcs:
        p = os.path.join(wdir, f"scan-{mc}.json")
        if not os.path.exists(p):
            continue
        s = M.scan_lists(M.load_json(p))
        cands, refs = s["candidates"], s["near_dup_refusals"]
        r = {"candidates": len(cands), "refusals": len(refs), "refusals_by_reason": by_reason(refs), "counts": s["counts"]}
        if kk:
            r["pair_candidate"] = links(cands, eo, ec)
            r["refusal_links_pair"] = links(refs, eo, ec)
            r["refusal_link_reasons"] = sorted({str(x.get("reason")) for x in refs if {x.get("older_id"), x.get("newer_id")} == {eo, ec}})
        row["by_min_cosine"][mc] = r
    ap = os.path.join(wdir, "D2.ask.jsonl")
    if os.path.exists(ap):
        line = M.load_jsonl(ap)[0]
        row["d2"] = {"context_tokens": line.get("context_tokens"), "evidence_full": (line.get("evidence") or {}).get("full")}
        if kk:
            row["d2"].update(outdated_delivered=M.reached(line, kk[0]), current_delivered=M.reached(line, kk[1]))
        if row["stale_measure"]:
            row["d2"].update(stale=M.stale(q, line), stale_without_current=M.stale_without_current(q, line))
    return row


def build(work: str, data: dict, ids: list[str], mcs: list[str]) -> dict:
    rows = []
    missing = []
    for qid in ids:
        wdir = os.path.join(work, qid)
        if not os.path.exists(os.path.join(wdir, "base", "study.json")):
            missing.append(qid)
            continue
        rows.append(question_row(data[qid], wdir, mcs))
    groups = {"knowledge_update": [r for r in rows if r["knowledge_update"]], "other": [r for r in rows if not r["knowledge_update"]]}
    agg = {}
    for mc in mcs:
        a = {}
        for g, rs in groups.items():
            rr = [r["by_min_cosine"][mc] for r in rs if mc in r["by_min_cosine"]]
            reasons: dict[str, int] = {}
            for x in rr:
                for k, v in x["refusals_by_reason"].items():
                    reasons[k] = reasons.get(k, 0) + v
            a[g] = {"histories": len(rr), "candidates": M.summary([x["candidates"] for x in rr]),
                    "refusals": M.summary([x["refusals"] for x in rr]), "refusals_by_reason": dict(sorted(reasons.items()))}
        q = [r for r in groups["knowledge_update"] if r["qualifies"] and mc in r["by_min_cosine"]]
        a["qualifying_ku"] = {"n": len(q), "pair_candidate": sum(r["by_min_cosine"][mc]["pair_candidate"] for r in q),
                              "refusal_links_pair": sum(r["by_min_cosine"][mc]["refusal_links_pair"] for r in q),
                              "same_event": sum(bool(r.get("same_event")) for r in q)}
        agg[mc] = a
    st = [r for r in rows if r["stale_measure"] and "d2" in r]
    d2 = {"stale_measure_questions": len(st), "outdated_delivered": sum(bool(r["d2"]["stale"]) for r in st),
          "outdated_without_current": sum(bool(r["d2"]["stale_without_current"]) for r in st),
          "current_delivered": sum(bool(r["d2"].get("current_delivered")) for r in st)}
    return {"work": work, "min_cosines": mcs, "questions": len(rows), "missing": missing, "aggregate": agg, "d2": d2, "rows": rows}


def fmt_s(s: dict) -> str:
    if not s["n"]:
        return "-"
    return f"{s['mean']:.1f} / {s['median']:g} / {s['max']:g}"


def markdown(res: dict) -> str:
    L = ["# Conflict study, Part 0 (development set, no LLM)", "",
         f"{res['questions']} development histories (missing: {', '.join(res['missing']) or 'none'}). "
         "Detection at write with `sleep.conflicts.enabled=true`; `conflicts scan -details` (no write) at each min_cosine.", "",
         "## Candidates and near-duplicate refusals per history (mean / median / max)", "",
         "| min_cosine | KU candidates | other candidates | KU refusals | other refusals | qualifying KU: pair is a candidate | refusal links the pair | same event |",
         "|---|---|---|---|---|---|---|---|"]
    for mc in res["min_cosines"]:
        a = res["aggregate"][mc]
        k, o, q = a["knowledge_update"], a["other"], a["qualifying_ku"]
        L.append(f"| {mc} | {fmt_s(k['candidates'])} | {fmt_s(o['candidates'])} | {fmt_s(k['refusals'])} | {fmt_s(o['refusals'])} | "
                 f"{q['pair_candidate']}/{q['n']} | {q['refusal_links_pair']}/{q['n']} | {q['same_event']}/{q['n']} |")
    L += ["", "Refusals by reason (totals):", ""]
    for mc in res["min_cosines"]:
        a = res["aggregate"][mc]
        L.append(f"- {mc}: KU {json.dumps(a['knowledge_update']['refusals_by_reason'])}; other {json.dumps(a['other']['refusals_by_reason'])}")
    d = res["d2"]
    L += ["", "## D2 blocks on the base (item 1)", "",
          f"Stale-measure questions: {d['stale_measure_questions']}. Blocks holding the outdated turn: {d['outdated_delivered']}; "
          f"holding it without the current one: {d['outdated_without_current']}; holding the current one: {d['current_delivered']}.", "",
          "## Knowledge-update questions", "",
          "| question | qualifies | stale measure | same day | same event | " + " | ".join(f"{mc}: cand / pair / refusals (link)" for mc in res["min_cosines"]) + " | D2 outdated / current |",
          "|---|---|---|---|---|" + "---|" * len(res["min_cosines"]) + "---|"]
    for r in res["rows"]:
        if not r["knowledge_update"]:
            continue
        cells = []
        for mc in res["min_cosines"]:
            x = r["by_min_cosine"].get(mc)
            if not x:
                cells.append("-")
                continue
            pc = "yes" if x.get("pair_candidate") else ("no" if r["qualifies"] else "-")
            lk = " (link: " + ",".join(x["refusal_link_reasons"]) + ")" if x.get("refusal_links_pair") else ""
            cells.append(f"{x['candidates']} / {pc} / {x['refusals']}{lk}")
        d2 = r.get("d2") or {}
        dd = f"{'yes' if d2.get('outdated_delivered') else 'no'} / {'yes' if d2.get('current_delivered') else 'no'}" if r["qualifies"] and d2 else "-"
        L.append(f"| {r['question_id']} | {'yes' if r['qualifies'] else 'no'} | {'yes' if r['stale_measure'] else 'no'} | "
                 f"{'yes' if r['same_day'] else 'no'} | {'yes' if r.get('same_event') else 'no'} | " + " | ".join(cells) + f" | {dd} |")
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True)
    ap.add_argument("--data", required=True, help="longmemeval_s_cleaned.json")
    ap.add_argument("--min-cosines", default="0.70 0.75 0.80 0.85")
    ap.add_argument("--ids", help="space-separated (default: the development set)")
    ap.add_argument("--md")
    ap.add_argument("--json")
    a = ap.parse_args(argv)
    ids = a.ids.split() if a.ids else M.set_ids(M.load_sets(), "development")
    data = {q["question_id"]: q for q in M.load_json(a.data) if q["question_id"] in set(ids)}
    res = build(a.work, data, ids, a.min_cosines.split())
    if a.json:
        with open(a.json, "w") as f:
            json.dump(res, f, indent=1)
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
