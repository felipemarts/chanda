#!/usr/bin/env python3
"""Part 0 of the usage-guided sleep study: diagnosis of the 2026-11-14
battery's sleep (condition E) from recorded data only, no LLM call.

Inputs (all local):
  eval/reports/2026-11-14/raw/{D,E}.jsonl   contexts, answers, verdicts
  eval/lme/data/longmemeval_s_cleaned.json  haystacks with has_answer turns
  eval/lme/llmstore/                        recorded E-sleep requests and
                                            responses (50 of the 60 questions;
                                            the smoke run's 10 predate the store)

Output: JSON on stdout (eval/sleep/diagnosis.json), read by DIAGNOSIS.md.

Evidence matching is the selection study's rule (eval/lme/lme/evidence.go):
whitespace removed, a turn reaches the block when the longest prefix of it
found contiguously in the block is at least 60% of its runes. Only the
block text is used here ("by": "text"); the battery did not keep the
turn-to-event map.
"""
import glob
import gzip
import json
import math
import re
import sys
from collections import Counter, defaultdict

RAW = "eval/reports/2026-11-14/raw"
DATA = "eval/lme/data/longmemeval_s_cleaned.json"
STORE = "eval/lme/llmstore"
MIN_COV = 0.6


def squash(s):
    return re.sub(r"\s+", "", s)


def unquote(block):
    return "\n".join(l[2:] if l.startswith("> ") else l for l in block.split("\n"))


def coverage(turn, text):
    t = squash(turn)
    if not t:
        return 0.0
    lo, hi = 0, len(t)
    while lo < hi:
        mid = (lo + hi + 1) // 2
        if t[:mid] in text:
            lo = mid
        else:
            hi = mid - 1
    return lo / len(t)


def tokens(s):
    return math.ceil(len(s) / 4)


LABEL = re.compile(r"\n\[(excerpt|fact|page|lesson) (\w+?_\w+) \|")


def segments(block):
    """(kind, id, rendered segment) per item, in block order."""
    ms = list(LABEL.finditer(block))
    end = block.rfind("</chanda-context>")
    end = len(block) if end < 0 else end
    out = []
    for i, m in enumerate(ms):
        stop = ms[i + 1].start() if i + 1 < len(ms) else end
        out.append((m.group(1), m.group(2), block[m.start():stop]))
    return out


def evidence(q, block):
    text = squash(unquote(block))
    out = []
    for s, sess in enumerate(q["haystack_sessions"]):
        for t, tr in enumerate(sess):
            if tr.get("has_answer"):
                c = coverage(tr["content"], text)
                out.append({"session": s, "turn": t, "role": tr["role"], "coverage": round(c, 3), "reached": c >= MIN_COV})
    return out


ULID = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"


def ulid_ms(u):
    v = 0
    for ch in u[:10]:
        v = v * 32 + ULID.index(ch)
    return v


def iso_ms(s):
    from datetime import datetime, timezone
    s = s.rstrip("Z")
    if "." in s:
        a, b = s.split(".")
        s = a + "." + (b + "000000")[:6]
    return int(datetime.fromisoformat(s).replace(tzinfo=timezone.utc).timestamp() * 1000)


EVENT = re.compile(r'<event id="(evt_\w+)"[^>]*>\n(.*?)\n</event>', re.S)


def main():
    data = {q["question_id"]: q for q in json.load(open(DATA))}
    D = {x["question_id"]: x for x in map(json.loads, open(f"{RAW}/D.jsonl"))}
    E = {x["question_id"]: x for x in map(json.loads, open(f"{RAW}/E.jsonl"))}
    qids = list(E)
    res = {"n": len(qids)}

    ev = {}
    for qid in qids:
        q = data[qid]
        ev[qid] = {"D": evidence(q, D[qid]["context"]), "E": evidence(q, E[qid]["context"])}

    # Item 1: the questions where D and E disagree.
    div = []
    for qid in qids:
        d, e = D[qid], E[qid]
        if d["verdict"] == e["verdict"]:
            continue
        segE = segments(e["context"])
        segD = segments(d["context"])
        sleep_tok = sum(tokens(s) for k, _, s in segE if k != "excerpt")
        raw_tok = sum(tokens(s) for k, _, s in segE if k == "excerpt")
        idsE = {i for _, i, _ in segE}
        dropped = [i for k, i, _ in segD if k == "excerpt" and i not in idsE]
        ed, ee = ev[qid]["D"], ev[qid]["E"]
        displaced = [f"s{a['session']}t{a['turn']}" for a, b in zip(ed, ee) if a["reached"] and not b["reached"]]
        gained = [f"s{a['session']}t{a['turn']}" for a, b in zip(ed, ee) if b["reached"] and not a["reached"]]
        div.append({
            "question_id": qid, "type": e["question_type"], "abstention": e["abstention"],
            "D_correct": d["verdict"], "E_correct": e["verdict"],
            "sleep_status": e["context_meta"]["sleep"]["status"],
            "E_items": len(segE), "E_fact_items": sum(1 for k, _, _ in segE if k != "excerpt"),
            "E_sleep_tokens": sleep_tok, "E_raw_tokens": raw_tok,
            "E_sleep_share": round(sleep_tok / max(1, sleep_tok + raw_tok), 3),
            "D_excerpts_missing_in_E": len(dropped),
            "evidence_turns": len(ed),
            "D_reached": sum(a["reached"] for a in ed), "E_reached": sum(b["reached"] for b in ee),
            "displaced": displaced, "gained": gained,
            "E_facts_text": [s.strip()[:300] for k, _, s in segE if k != "excerpt"],
        })
    res["item1_divergent"] = div

    # Sleep share of every E block.
    shares = []
    for qid in qids:
        segE = segments(E[qid]["context"])
        st = sum(tokens(s) for k, _, s in segE if k != "excerpt")
        tt = sum(tokens(s) for _, _, s in segE)
        shares.append(st / max(1, tt))
    res["E_sleep_share_mean_all60"] = round(sum(shares) / len(shares), 3)

    # Item 2: full evidence in the block, paired.
    full = {"D": 0, "E": 0, "both": 0, "D_only": 0, "E_only": 0, "neither": 0, "scored": 0}
    mean = {"D": [], "E": []}
    for qid in qids:
        ed, ee = ev[qid]["D"], ev[qid]["E"]
        if not ed:
            continue
        full["scored"] += 1
        fd, fe = all(a["reached"] for a in ed), all(b["reached"] for b in ee)
        full["D"] += fd
        full["E"] += fe
        full["both" if fd and fe else "D_only" if fd else "E_only" if fe else "neither"] += 1
        mean["D"].append(sum(a["reached"] for a in ed) / len(ed))
        mean["E"].append(sum(b["reached"] for b in ee) / len(ee))
    full["mean_turn_recall_D"] = round(100 * sum(mean["D"]) / len(mean["D"]), 1)
    full["mean_turn_recall_E"] = round(100 * sum(mean["E"]) / len(mean["E"]), 1)
    res["item2_full_evidence"] = full

    # Item 3: accuracy by sleep outcome.
    def stop_reason(x):
        rep = x["context_meta"]["sleep"]["report"]
        if x["context_meta"]["sleep"]["status"] == "completed":
            return "completed"
        if "llm provider error" in rep:
            return "partial_provider_error"
        return "partial_event_ceiling"
    groups = defaultdict(lambda: {"n": 0, "D": 0, "E": 0, "E_only": 0, "D_only": 0, "haystack_events": []})
    for qid in qids:
        for g in (stop_reason(E[qid]), "partial_all" if E[qid]["context_meta"]["sleep"]["status"] != "completed" else None):
            if g is None:
                continue
            gr = groups[g]
            gr["n"] += 1
            gr["D"] += D[qid]["verdict"]
            gr["E"] += E[qid]["verdict"]
            gr["E_only"] += E[qid]["verdict"] and not D[qid]["verdict"]
            gr["D_only"] += D[qid]["verdict"] and not E[qid]["verdict"]
            gr["haystack_events"].append(sum(len(s) for s in data[qid]["haystack_sessions"]))
    for g in groups.values():
        h = g.pop("haystack_events")
        g["median_turns"] = sorted(h)[len(h) // 2]
    res["item3_by_sleep_outcome"] = dict(groups)

    # Item 4: origin of the facts the sleep extracted, from the recorded calls.
    windows = []
    for qid in qids:
        sl = E[qid]["context_meta"]["sleep"]
        t0 = ulid_ms(sl["run_id"].split("_")[1])
        windows.append((t0 - 2000, t0 + int(E[qid]["context_meta"]["sleep_ms"]) + 5000, qid))
    turnmap = {}
    for qid in qids:
        q = data[qid]
        gold = set(q["answer_session_ids"])
        m = {}
        for s, sess in enumerate(q["haystack_sessions"]):
            sid = q["haystack_session_ids"][s]
            for t, tr in enumerate(sess):
                m.setdefault(tr["content"].strip(), (s, t, sid in gold, bool(tr.get("has_answer"))))
        turnmap[qid] = m
    per_q = defaultdict(lambda: {"calls": 0, "events_seen": 0, "events_evidence_session": 0, "events_unmatched": 0,
                                  "facts": 0, "facts_evidence_session": 0, "facts_evidence_turn": 0, "facts_unmatched": 0,
                                  "evidence_sessions_reached": set(), "invalid": 0})
    unassigned = 0
    for f in glob.glob(f"{STORE}/*/*.json.gz"):
        d = json.load(gzip.open(f))
        if d["first_tag"] != "E-sleep":
            continue
        t = iso_ms(d["time"])
        qs = [w[2] for w in windows if w[0] <= t <= w[1]]
        user = next(m["content"] for m in d["request"]["messages"] if m["role"] == "user")
        events = EVENT.findall(user)
        if not events:
            continue  # not an extraction call (lesson or page call)
        qs = [qid for qid in qs if sum(txt.strip() in turnmap[qid] for _, txt in events) * 2 > len(events)] or qs
        if len(qs) != 1:
            unassigned += 1
            continue
        qid = qs[0]
        pq = per_q[qid]
        pq["calls"] += 1
        emap = {}
        for eid, txt in events:
            hit = turnmap[qid].get(txt.strip())
            emap[eid] = hit
            pq["events_seen"] += 1
            if hit is None:
                pq["events_unmatched"] += 1
            elif hit[2]:
                pq["events_evidence_session"] += 1
                pq["evidence_sessions_reached"].add(hit[0])
        try:
            content = json.loads(d["response"])["choices"][0]["message"]["content"]
            facts = json.loads(content)["facts"]
        except Exception:
            pq["invalid"] += 1
            continue
        for fa in facts:
            hits = [emap.get(e) for e in fa.get("event_ids", [])]
            hits = [h for h in hits if h]
            pq["facts"] += 1
            if not hits:
                pq["facts_unmatched"] += 1
            elif any(h[2] for h in hits):
                pq["facts_evidence_session"] += 1
                if any(h[3] for h in hits):
                    pq["facts_evidence_turn"] += 1
    tot = Counter()
    rows = []
    for qid, pq in per_q.items():
        q = data[qid]
        gold_idx = {s for s, sid in enumerate(q["haystack_session_ids"]) if sid in set(q["answer_session_ids"])}
        reached = pq.pop("evidence_sessions_reached")
        pq["evidence_sessions"] = len(gold_idx)
        pq["evidence_sessions_reached"] = len(reached & gold_idx)
        pq["sleep_status"] = E[qid]["context_meta"]["sleep"]["status"]
        pq["report_extracted"] = sum(int(n) for n in re.findall(r"(\d+)", re.search(r"Facts from extraction: ([^\n]*)", E[qid]["context_meta"]["sleep"]["report"]).group(1)))
        pq["question_id"] = qid
        rows.append(pq)
        for k, v in pq.items():
            if isinstance(v, int) and not isinstance(v, bool):
                tot[k] += v
    tot["questions"] = len(rows)
    tot["questions_all_evidence_sessions_read"] = sum(r["evidence_sessions_reached"] == r["evidence_sessions"] for r in rows)
    tot["partial_questions"] = sum(r["sleep_status"] != "completed" for r in rows)
    tot["partial_all_evidence_sessions_read"] = sum(r["evidence_sessions_reached"] == r["evidence_sessions"] for r in rows if r["sleep_status"] != "completed")
    tot["unassigned_calls"] = unassigned
    res["item4_fact_origin"] = {"totals": dict(tot), "per_question": sorted(rows, key=lambda r: r["question_id"])}
    json.dump(res, sys.stdout, indent=1, default=list)


if __name__ == "__main__":
    main()
