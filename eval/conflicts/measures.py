"""Measures of the conflict-resolution study (eval/conflicts/PROTOCOL.md),
shared by precheck.py, tune.py, report.py and suite/score.py. No LLM, no
network.

Inputs are the harness's own files:
- ask lines (`lme study ask`): evidence.turns[] holds every has_answer turn
  of the question with its key "s<session>t<turn>", event_id and whether it
  reached the block (the selection study's 60%-prefix rule, applied in Go);
  dropped_superseded lists the candidates the read path dropped with reason
  "superseded" (decision 0148);
- study.json: turns[] maps each turn key to its event id;
- outputs of `lme study conflicts` (scan, list, stats), the tool's data as
  returned. The list shape is not fixed by decision 0148, so pairs and
  supersessions are found by their keys wherever they sit in the JSON.
"""

from __future__ import annotations

import json
import math
import os
import statistics
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import kuturns  # noqa: E402


def key(st) -> str:
    """Turn key of a (session, turn) pair, as study.json and ask lines name it."""
    return f"s{st[0]}t{st[1]}"


def load_json(path: str):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def load_jsonl(path: str) -> list[dict]:
    with open(path, encoding="utf-8") as f:
        return [json.loads(x) for x in f if x.strip()]


def load_sets(path: str = os.path.join(HERE, "sets.json")) -> dict:
    return load_json(path)


def set_ids(sets: dict, which: str) -> list[str]:
    """Question ids of a set in sets order: knowledge-update first, then the others."""
    s = sets[which]
    return list(s["knowledge_update"]) + list(s["other"])


# ---------------------------------------------------------------- turns


def turn_events(meta: dict) -> dict[str, str]:
    """study.json: turn key -> event id ("" when the turn was rejected)."""
    return {t["key"]: t.get("event_id") or "" for t in meta.get("turns") or []}


def has_answer_keys(q: dict) -> list[str]:
    return [key((s, t)) for s, sess in enumerate(q["haystack_sessions"]) for t, tr in enumerate(sess) if tr.get("has_answer")]


def needed_keys(q: dict) -> list[str]:
    """Needed evidence turns: kuturns.needed for qualifying knowledge-update
    questions, every has_answer turn for the others."""
    n = kuturns.needed(q)
    return [key(x) for x in n] if n is not None else has_answer_keys(q)


def ku_keys(q: dict):
    """(outdated key, current key) or None."""
    k = kuturns.ku_turns(q)
    return None if k is None else (key(k[0]), key(k[1]))


# ---------------------------------------------------------------- blocks


def evidence_turns(line: dict) -> dict[str, dict]:
    return {t["key"]: t for t in (line.get("evidence") or {}).get("turns") or []}


def reached(line: dict, k: str) -> bool:
    t = evidence_turns(line).get(k)
    return bool(t and t.get("reached"))


def stale(q: dict, line: dict) -> bool | None:
    """Outdated information delivered: the block holds the outdated turn.
    None when the question is not in the stale measure (kuturns.stale_measure)."""
    if not kuturns.stale_measure(q):
        return None
    old, _ = ku_keys(q)
    return reached(line, old)


def stale_without_current(q: dict, line: dict) -> bool | None:
    if not kuturns.stale_measure(q):
        return None
    old, cur = ku_keys(q)
    return reached(line, old) and not reached(line, cur)


def hidden(q: dict, c0: dict, c1: dict, events: dict[str, str] | None = None) -> list[dict]:
    """Hidden evidence: needed turns in C0's block, missing from C1's because
    C1 dropped their event as superseded (drop reason in C1's retrieval log)."""
    dropped = set(c1.get("dropped_superseded") or [])
    t1 = evidence_turns(c1)
    out = []
    for k in needed_keys(q):
        if not reached(c0, k) or reached(c1, k):
            continue
        ev = (t1.get(k) or {}).get("event_id") or (events or {}).get(k) or ""
        if ev and ev in dropped:
            out.append({"key": k, "event_id": ev})
    return out


# ---------------------------------------------------------------- tool outputs


def output_of(res) -> object:
    """The tool's data from a saved step result ({"output": ...}) or the data itself."""
    if isinstance(res, dict) and "output" in res and ("tool" in res or "action" in res):
        return res["output"]
    return res


def _walk(obj, path=()):
    if isinstance(obj, dict):
        for k, v in obj.items():
            yield from _walk(v, path + (str(k),))
    elif isinstance(obj, list):
        if obj and all(isinstance(x, dict) for x in obj):
            yield path, obj
        for x in obj:
            yield from _walk(x, path)


OLD_KEYS = ("old_id", "outdated_id", "superseded_id", "older_id")
NEW_KEYS = ("new_id", "current_id", "superseding_id", "newer_id")


def _first(d: dict, names) -> str:
    for n in names:
        if d.get(n):
            return str(d[n])
    return ""


def scan_lists(res) -> dict:
    """candidates and near_dup_refusals of a scan -details output, plus its
    other scalar fields (the counts)."""
    data = output_of(res)
    if isinstance(data, dict) and isinstance(data.get("scan"), dict):
        data = data["scan"]  # the tool nests the scan result under "scan" (decision 0148)
    out = {"candidates": [], "near_dup_refusals": [], "counts": {}}
    if isinstance(data, dict):
        out["candidates"] = list(data.get("candidates") or [])
        out["near_dup_refusals"] = list(data.get("near_dup_refusals") or [])
        out["counts"] = {k: v for k, v in data.items() if not isinstance(v, (list, dict))}
        for k, v in data.items():
            if isinstance(v, dict) and k not in ("candidates", "near_dup_refusals"):
                out["counts"][k] = v
    return out


def _verdict_fields(p: dict) -> dict:
    v = p.get("verdict")
    conf, cur = p.get("confidence"), p.get("current")
    if isinstance(v, str) and v.strip().startswith("{"):
        try:
            v = json.loads(v)
        except ValueError:
            pass
    if isinstance(v, dict):
        conf = v.get("confidence", conf)
        cur = v.get("current", cur)
        v = v.get("verdict")
    return {"verdict": v or None, "confidence": conf, "current": cur}


def conflict_pairs(res) -> list[dict]:
    """Every candidate pair in a conflicts list (or scan) output, normalised:
    older_id, newer_id, status, verdict, current, confidence, reason,
    applied, plus the raw row. Near-duplicate refusals and supersession rows
    are not pairs."""
    seen: dict[tuple, dict] = {}
    for path, rows in _walk(output_of(res)):
        if any("refusal" in p or "supersession" in p for p in path):
            continue
        for r in rows:
            o, n = r.get("older_id"), r.get("newer_id")
            if not o or not n:
                continue
            vf = _verdict_fields(r)
            status = r.get("status")
            reason = r.get("reason") or r.get("applied_reason") or r.get("outcome")
            applied = bool(r.get("applied")) or status == "applied" or reason == "applied"
            p = {"older_id": o, "newer_id": n, "status": status, "reason": reason, "applied": applied,
                 "cosine": r.get("cosine"), **vf, "raw": r}
            k = (o, n)
            if k not in seen or _richness(p) > _richness(seen[k]):
                seen[k] = p
    return list(seen.values())


def _richness(p: dict) -> int:
    return sum(1 for f in ("status", "verdict", "reason", "confidence") if p.get(f) is not None) + int(p["applied"])


def supersessions(res) -> list[dict]:
    """Supersession rows of a conflicts list output: old_id, new_id,
    confidence, reverted (bool), raw."""
    out, seen = [], set()
    for path, rows in _walk(output_of(res)):
        if not any("supersession" in p for p in path):
            continue
        for r in rows:
            o, n = _first(r, OLD_KEYS), _first(r, NEW_KEYS)
            if not o or not n:
                continue
            k = (r.get("id"), o, n)
            if k in seen:
                continue
            seen.add(k)
            out.append({"id": r.get("id"), "old_id": o, "new_id": n, "confidence": r.get("confidence"),
                        "reverted": bool(r.get("reverted_at")), "raw": r})
    return out


def applied_supersessions(res) -> list[dict]:
    """Active supersessions; when the output lists none, pairs marked applied."""
    sup = [s for s in supersessions(res) if not s["reverted"]]
    if sup:
        return sup
    return [{"id": None, "old_id": p["older_id"], "new_id": p["newer_id"], "confidence": p.get("confidence"),
             "reverted": False, "raw": p["raw"]} for p in conflict_pairs(res) if p["applied"]]


def judged(p: dict) -> bool:
    return p.get("verdict") is not None or (p.get("status") not in (None, "pending"))


def verdict_tally(pairs: list[dict]) -> dict:
    t: dict[str, int] = {}
    for p in pairs:
        if judged(p):
            v = p.get("verdict") or "invalid"
            t[v] = t.get(v, 0) + 1
    return dict(sorted(t.items()))


def find_pair(pairs: list[dict], a: str, b: str) -> dict | None:
    for p in pairs:
        if {p["older_id"], p["newer_id"]} == {a, b}:
            return p
    return None


def ku_pair_verdict(q: dict, events: dict[str, str], pairs: list[dict], applied: list[dict]) -> dict | None:
    """The (outdated, current) pair of a qualifying knowledge-update question:
    whether it was a candidate, its verdict, and whether the verdict is right
    (supersedes with the current turn's record as current, i.e. an applied
    supersession old=outdated, new=current). Same-day pairs are not scored
    (their order is not trusted). None when the question does not qualify."""
    kk = ku_keys(q)
    if kk is None:
        return None
    eo, ec = events.get(kk[0], ""), events.get(kk[1], "")
    r = {"outdated_key": kk[0], "current_key": kk[1], "outdated_event": eo, "current_event": ec,
         "same_event": bool(eo) and eo == ec, "same_day": kuturns.same_day(q),
         "candidate": False, "judged": False, "verdict": None, "confidence": None, "applied": False, "correct": None}
    if not eo or not ec or eo == ec:
        return r
    p = find_pair(pairs, eo, ec)
    if p:
        r.update(candidate=True, judged=judged(p), verdict=p.get("verdict"), confidence=p.get("confidence"),
                 status=p.get("status"), reason=p.get("reason"))
    r["applied"] = any(s["old_id"] == eo and s["new_id"] == ec for s in applied)
    r["applied_reversed"] = any(s["old_id"] == ec and s["new_id"] == eo for s in applied)
    if r["judged"] and not r["same_day"]:
        r["correct"] = bool(r["applied"])
    return r


# ---------------------------------------------------------------- stats


def summary(xs: list[float]) -> dict:
    if not xs:
        return {"n": 0, "mean": None, "median": None, "max": None, "total": 0}
    return {"n": len(xs), "mean": round(sum(xs) / len(xs), 3), "median": statistics.median(xs), "max": max(xs), "total": sum(xs)}


Z95 = 1.959963984540054


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
    return min(1.0, 2 * sum(math.comb(n, i) for i in range(k + 1)) / 2 ** n)


def paired_flags(first: dict[str, bool], second: dict[str, bool]) -> dict:
    """Paired counts over the ids both have: first_only (true in first, false
    in second), second_only, and the exact two-sided sign test."""
    ids = sorted(set(first) & set(second))
    a = sum(1 for i in ids if first[i] and not second[i])
    b = sum(1 for i in ids if second[i] and not first[i])
    return {"n": len(ids), "first_true": sum(first[i] for i in ids), "second_true": sum(second[i] for i in ids),
            "first_only": a, "second_only": b, "p": sign_test(a, b)}


# ---------------------------------------------------------------- one history


def event_text(q: dict, meta: dict) -> dict[str, dict]:
    """event id -> {key, role, date, text} of the turn it was first returned for."""
    out = {}
    for t in meta.get("turns") or []:
        e = t.get("event_id")
        if not e or e in out:
            continue
        try:
            tr = q["haystack_sessions"][t["session"]][t["turn"]]
        except (IndexError, KeyError, TypeError):
            continue
        out[e] = {"key": t["key"], "role": tr.get("role"), "date": q["haystack_dates"][t["session"]], "text": tr.get("content", "")}
    return out


def sleep_stats(res: dict | None) -> dict:
    """Calls, tokens and time of a saved sleep step (lme study sleep -out)."""
    if not res:
        return {}
    out = res.get("output") or {}
    c = out.get("cost") or {}
    return {"status": out.get("status"), "llm_calls": int(c.get("llm_calls") or 0), "input_tokens": int(c.get("input_tokens") or 0),
            "output_tokens": int(c.get("output_tokens") or 0), "seconds": round((res.get("wall_ms") or 0) / 1000, 3),
            "counts": res.get("counts")}


def classify_applied(q: dict, events: dict[str, str], applied: list[dict]) -> list[dict]:
    """Each applied supersession with a class:
    correct    old = the outdated turn's event, new = the current one's
               (qualifying knowledge-update question, sessions on different days);
    wrong      its old side is needed evidence (needed_keys), or one side is
               an evidence turn of a qualifying knowledge-update question and
               the pair is not (outdated -> current) (the reverse included);
    unverified no ground truth either way."""
    need = {events.get(k) for k in needed_keys(q)} - {"", None}
    kk = ku_keys(q)
    eo, ec = (events.get(kk[0], ""), events.get(kk[1], "")) if kk else ("", "")
    out = []
    for s in applied:
        why = []
        if s["old_id"] in need:
            why.append("old side is needed evidence")
        is_pair = kk is not None and s["old_id"] == eo and s["new_id"] == ec and eo != ec
        if kk is not None and not is_pair and (s["old_id"] in {eo, ec} or s["new_id"] in {eo, ec}):
            why.append("not the (outdated, current) update" + (" (reversed)" if s["old_id"] == ec and s["new_id"] == eo else ""))
        if why:
            cls = "wrong"
        elif is_pair and not kuturns.same_day(q):
            cls = "correct"
        else:
            cls = "unverified"
        out.append({**{k: v for k, v in s.items() if k != "raw"}, "class": cls, "why": why})
    return out


def history_measures(q: dict, meta: dict, c0: dict | None, c1: dict | None, listing=None, sleep=None) -> dict:
    """Every per-history measure of the study: stale delivered (C0, C1),
    hidden evidence, the queue's verdicts, the KU pair's verdict, applied
    supersessions classified, sleep calls/tokens/time."""
    events = turn_events(meta)
    pairs = conflict_pairs(listing) if listing is not None else []
    applied = applied_supersessions(listing) if listing is not None else []
    r = {"question_id": q["question_id"], "question_type": q["question_type"], "knowledge_update": q["question_type"] == "knowledge-update",
         "qualifies": kuturns.ku_turns(q) is not None, "stale_measure": kuturns.stale_measure(q), "same_day": kuturns.same_day(q),
         "needed": needed_keys(q)}
    if c0 is not None:
        r["stale_c0"] = stale(q, c0)
    if c1 is not None:
        r["stale_c1"] = stale(q, c1)
        r["dropped_superseded"] = len(c1.get("dropped_superseded") or [])
    if c0 is not None and c1 is not None:
        r["hidden"] = hidden(q, c0, c1, events)
    r["candidates"] = len(pairs)
    r["judged"] = sum(1 for p in pairs if judged(p))
    r["verdicts"] = verdict_tally(pairs)
    r["applied"] = classify_applied(q, events, applied)
    r["ku_pair"] = ku_pair_verdict(q, events, pairs, applied)
    r["sleep"] = sleep_stats(sleep)
    texts = event_text(q, meta)
    for s in r["applied"]:
        p = find_pair(pairs, s["old_id"], s["new_id"]) or {}
        s["verdict"], s["pair_confidence"], s["reason"] = p.get("verdict"), p.get("confidence"), p.get("reason")
        s["old"], s["new"] = texts.get(s["old_id"]), texts.get(s["new_id"])
    for h in r.get("hidden") or []:
        sup = next((s for s in r["applied"] if s["old_id"] == h["event_id"]), None)
        h["old"] = texts.get(h["event_id"])
        if sup:
            h.update(new_id=sup["new_id"], new=sup["new"], verdict=sup.get("verdict"),
                     confidence=sup.get("pair_confidence") if sup.get("pair_confidence") is not None else sup.get("confidence"))
    return r
