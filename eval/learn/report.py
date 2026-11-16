"""Results of the learning study (eval/learn/PROTOCOL.md "Decision rules"
and "Measures"), from the work directory run_learn.sh fills:

  <work>/<conv>/out/base.use.{ask,answers,outcomes}.jsonl   use half, U0 + keys written
  <work>/<conv>/out/base.leak.json                          keys created (leak check of base)
  <work>/<conv>/out/<C>.test.{ask,answers}.jsonl            C in B, U0, U1, K
  <work>/<conv>/out/<C>.sleep.json, <C>.leak.json           learning step, leak check (U0, U1, K)
  <work>/<conv>/{base,U0,U1,K,K.nokeys}/chanda.db           database sizes

Writes markdown (--md) and JSON (--json). Accuracy has Wilson 95%
intervals; paired comparisons use the exact two-sided sign test on the
questions only one condition answered correctly (eval/sleep/report.py).
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import math
import os
import sqlite3
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))

import common  # noqa: E402


def _load_sleep_report():
    spec = importlib.util.spec_from_file_location("sleep_report", os.path.join(HERE, "..", "sleep", "report.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


SR = _load_sleep_report()
wilson, sign_test, acc, fmt_acc = SR.wilson, SR.sign_test, SR.acc, SR.fmt_acc

TEST_CONVS = ("conv-26", "conv-41", "conv-42", "conv-43", "conv-44", "conv-48", "conv-50")
CHANDA = ("U0", "U1", "K")
CONDS = ("B",) + CHANDA
CATS = SR.CATS
QA_TAGS = {f"{c}-test-{r}" for c in CONDS for r in ("reader", "judge")} | {"base-use-reader", "base-use-judge"}
CLAIMS = {
    "A": {"first": "U0", "second": "B", "claim": "Chanda is more accurate than plain RAG"},
    "B": {"first": "K", "second": "U0", "claim": "learning from use improves later answers"},
}
SECONDARY = {"U1 vs U0": ("U1", "U0", "utility alone"), "K vs U1": ("K", "U1", "keys alone")}
NOT_B = ("Claim B is not established. No other learning variant is proposed or built; keys and digestion "
         "stay off by default, and sleep remains maintenance only: integrity, utility and deduplication.")


def load(path: str) -> list[dict] | None:
    return common.read_jsonl(path) if os.path.exists(path) else None


def load_json(path: str):
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


def collect(work: str, convs: list[str]) -> dict:
    d = {"answers": {c: {} for c in CONDS}, "asks": {c: {} for c in CONDS}, "use_ask": {}, "use_answers": {}, "outcomes": {},
         "leak": {c: {} for c in ("base",) + CHANDA}, "sleep": {c: {} for c in ("U1", "K")}}
    for conv in convs:
        od = os.path.join(work, conv, "out")
        for c in CONDS:
            for kind, key in (("answers", "answers"), ("ask", "asks")):
                x = load(os.path.join(od, f"{c}.test.{kind}.jsonl"))
                if x is not None:
                    d[key][c][conv] = x
        for name, key in (("base.use.ask.jsonl", "use_ask"), ("base.use.answers.jsonl", "use_answers"),
                          ("base.use.outcomes.jsonl", "outcomes")):
            x = load(os.path.join(od, name))
            if x is not None:
                d[key][conv] = x
        for c in ("base",) + CHANDA:
            x = load_json(os.path.join(od, f"{c}.leak.json"))
            if x is not None:
                d["leak"][c][conv] = x
        for c in ("U1", "K"):
            x = load_json(os.path.join(od, f"{c}.sleep.json"))
            if x is not None:
                d["sleep"][c][conv] = x
    return d


def pooled(by_conv: dict, convs: list[str]) -> list[dict]:
    return [r for conv in convs for r in by_conv.get(conv, [])]


def accuracy_table(answers: dict, convs: list[str]) -> dict:
    out = {}
    for c, by_conv in answers.items():
        recs = pooled(by_conv, convs)
        if not recs:
            continue
        out[c] = {"total": acc(recs), "complete": all(conv in by_conv for conv in convs),
                  "by_category": {n: acc([r for r in recs if r.get("category") == k]) for k, n in CATS.items()},
                  "by_conversation": {conv: acc(by_conv[conv]) for conv in convs if conv in by_conv}}
    return out


def paired(first: list[dict], second: list[dict]) -> dict:
    p = SR.paired(first, second)
    common_ids = {r["question_id"] for r in first} & {r["question_id"] for r in second}
    p["accuracy_first"] = acc([r for r in first if r["question_id"] in common_ids])["accuracy"]
    p["accuracy_second"] = acc([r for r in second if r["question_id"] in common_ids])["accuracy"]
    return p


def claims(answers: dict, convs: list[str]) -> dict:
    out = {}
    for name, c in CLAIMS.items():
        a, b = pooled(answers[c["first"]], convs), pooled(answers[c["second"]], convs)
        if not a or not b:
            continue
        p = paired(a, b)
        p.update(c)
        p["established"] = bool(p["accuracy_first"] > p["accuracy_second"] and p["p"] < 0.05)
        p["decision"] = (f"Claim {name}, \"{c['claim']}\": " +
                         ("**established**" if p["established"] else "not established") +
                         f" ({c['first']} {100 * p['accuracy_first']:.1f}% vs {c['second']} {100 * p['accuracy_second']:.1f}%,"
                         f" {p['first_only']} vs {p['second_only']} discordant, p = {p['p']:.3f}).")
        out[name] = p
    return out


def secondary(answers: dict, convs: list[str]) -> dict:
    out = {}
    for name, (f, s, what) in SECONDARY.items():
        a, b = pooled(answers[f], convs), pooled(answers[s], convs)
        if a and b:
            p = paired(a, b)
            p["what"] = what
            out[name] = p
    return out


def evidence_table(asks: dict, convs: list[str]) -> dict:
    out = {}
    for c, by_conv in asks.items():
        recs = [r for r in pooled(by_conv, convs) if (r.get("evidence") or {}).get("scored")]
        if not recs:
            continue
        rec = [r["evidence"]["reached"] / len(r["evidence"]["turns"]) for r in recs if r["evidence"].get("turns")]
        out[c] = {"scored": len(recs), "full": sum(1 for r in recs if r["evidence"]["full"]),
                  "turns": sum(len(r["evidence"]["turns"]) for r in recs),
                  "turns_reached": sum(r["evidence"]["reached"] for r in recs),
                  "mean_turn_recall": sum(rec) / len(rec) if rec else None}
    return out


def evidence_ids(line: dict) -> set[str]:
    return {t["event_id"] for t in (line.get("evidence") or {}).get("turns") or [] if t.get("event_id")}


def key_brought(k_lines: list[dict], u0_lines: list[dict]) -> dict:
    """Records K delivered with features.key_rank > 0 that U0 did not
    deliver for the same question, and how many were evidence turns."""
    u0 = {r["question_id"]: {it["id"] for it in r.get("items") or []} for r in u0_lines}
    n = qs = recs = ev = ev_q = with_key = 0
    per_q = {}
    for r in k_lines:
        if r["question_id"] not in u0:
            continue
        n += 1
        if r.get("key_candidates", 0) > 0:
            with_key += 1
        new = [it["id"] for it in r.get("items") or [] if it.get("key_rank", 0) > 0 and it["id"] not in u0[r["question_id"]]]
        if not new:
            continue
        evs = evidence_ids(r)
        e = sum(1 for x in new if x in evs)
        qs += 1
        recs += len(new)
        ev += e
        ev_q += e > 0
        per_q[r["question_id"]] = {"records": new, "evidence": e}
    return {"questions": n, "questions_with_key_candidates": with_key, "questions_key_brought": qs,
            "share": qs / n if n else None, "records_key_brought": recs, "evidence_records": ev,
            "questions_with_evidence_brought": ev_q, "per_question": per_q}


def keys_table(d: dict, convs: list[str]) -> dict:
    out = {"per_conversation": {}, "total_created": 0}
    for conv in convs:
        b = d["leak"]["base"].get(conv) or {}
        k = d["leak"]["K"].get(conv) or {}
        oc = d["outcomes"].get(conv) or []
        row = {"keys_created": b.get("keys"), "key_table": b.get("key_table"), "keys_in_K": k.get("keys"),
               "positive_outcomes": sum(1 for o in oc if o.get("value", 0) > 0), "outcomes": len(oc)}
        if conv in d["asks"]["K"] and conv in d["asks"]["U0"]:
            kb = key_brought(d["asks"]["K"][conv], d["asks"]["U0"][conv])
            kb.pop("per_question")
            row["key_brought"] = kb
        out["per_conversation"][conv] = row
        out["total_created"] += row["keys_created"] or 0
    k_all, u_all = pooled(d["asks"]["K"], convs), pooled(d["asks"]["U0"], convs)
    if k_all and u_all:
        out["key_brought"] = key_brought(k_all, u_all)
    return out


def overlap_ceiling(d: dict, convs: list[str]) -> dict:
    """PRECHECK.md section 2 on these conversations: test questions with an
    evidence turn used (outcomes.used_ids) by a use-half question, in a
    positive outcome, or delivered to one; and the tighter ceiling: wrong in
    U0 and missing (in U0's block) an evidence turn used positively."""
    rows = {}
    tot = {k: 0 for k in ("test", "scored", "used_any", "used_positive", "delivered_any", "missing_evidence",
                          "missing_used_positive", "wrong_U0", "tight")}
    tight_ids = []
    for conv in convs:
        oc, ua, u0 = d["outcomes"].get(conv), d["use_ask"].get(conv), d["asks"]["U0"].get(conv)
        if oc is None or u0 is None:
            continue
        used_any, used_pos, delivered = set(), set(), set()
        for o in oc:
            used_any |= set(o.get("used_ids") or [])
            if o.get("value", 0) > 0:
                used_pos |= set(o.get("used_ids") or [])
        for l in ua or []:
            delivered |= {it["id"] for it in l.get("items") or []}
        verdict = {r["question_id"]: bool(r["verdict"]) for r in d["answers"]["U0"].get(conv) or []}
        r = {k: 0 for k in tot}
        for l in u0:
            r["test"] += 1
            turns = [t for t in (l.get("evidence") or {}).get("turns") or [] if t.get("event_id")]
            if not turns:
                continue
            evs = {t["event_id"] for t in turns}
            r["scored"] += 1
            r["used_any"] += bool(evs & used_any)
            r["used_positive"] += bool(evs & used_pos)
            r["delivered_any"] += bool(evs & delivered)
            missing = {t["event_id"] for t in turns if not t.get("reached")}
            r["missing_evidence"] += bool(missing)
            r["missing_used_positive"] += bool(missing & used_pos)
            wrong = l["question_id"] in verdict and not verdict[l["question_id"]]
            r["wrong_U0"] += wrong
            if wrong and missing & used_pos:
                r["tight"] += 1
                tight_ids.append(l["question_id"])
        rows[conv] = r
        for k in tot:
            tot[k] += r[k]
    return {"per_conversation": rows, "total": tot, "tight_question_ids": tight_ids,
            "share_used_positive": tot["used_positive"] / tot["test"] if tot["test"] else None,
            "share_tight": tot["tight"] / tot["test"] if tot["test"] else None}


def latency(asks: dict, convs: list[str]) -> dict:
    out = {}
    for c, by_conv in asks.items():
        xs = sorted(float(r.get("retrieval_ms", 0)) for r in pooled(by_conv, convs))
        if xs:
            out[c] = {"n": len(xs), "mean_ms": sum(xs) / len(xs), "p50_ms": quantile(xs, 0.5), "p95_ms": quantile(xs, 0.95)}
    return out


def quantile(xs: list[float], q: float) -> float:
    """Nearest rank on sorted xs."""
    if not xs:
        return float("nan")
    return xs[min(len(xs) - 1, max(0, math.ceil(q * len(xs)) - 1))]


def file_bytes(db: str) -> int | None:
    if not os.path.exists(db):
        return None
    return sum(os.path.getsize(p) for p in (db, db + "-wal", db + "-shm") if os.path.exists(p))


def open_ro(db: str) -> sqlite3.Connection:
    """A closed Chanda database, read-only. A cleanly closed WAL database has
    no -wal and no -shm file, and a plain mode=ro open cannot create the
    -shm: it is opened immutable. With a -wal file left over, mode=ro."""
    mode = "ro" if os.path.exists(db + "-wal") else "ro&immutable=1"
    return sqlite3.connect(f"file:{db}?mode={mode}", uri=True)


def compact_bytes(db: str) -> int | None:
    """Size of VACUUM INTO a temporary file (the database is opened read-only)."""
    if not os.path.exists(db):
        return None
    with tempfile.TemporaryDirectory() as t:
        dst = os.path.join(t, "v.db")
        c = open_ro(db)
        try:
            c.execute("VACUUM INTO ?", (dst,))
        finally:
            c.close()
        return os.path.getsize(dst)


def sizes(work: str, convs: list[str]) -> dict:
    out = {}
    for conv in convs:
        row = {}
        for c in ("base", "U0", "U1", "K", "K.nokeys"):
            row[c] = file_bytes(os.path.join(work, conv, c, "chanda.db"))
        row["K_compact"] = compact_bytes(os.path.join(work, conv, "K", "chanda.db"))
        row["K_nokeys_compact"] = compact_bytes(os.path.join(work, conv, "K.nokeys", "chanda.db"))
        out[conv] = row
    return out


def calls_check(calls_path: str | None, d: dict, convs: list[str]) -> dict:
    """Learning steps made zero LLM calls: every proxy call carries a
    reader/judge tag of this study, and each sleep result reports 0 calls."""
    res = {"call_log": calls_path, "calls": 0, "other_tags": {}, "peak_calls": {}, "sleep_llm_calls": {}}
    if calls_path and os.path.exists(calls_path):
        for r in common.read_jsonl(calls_path):
            res["calls"] += 1
            tag = r.get("tag") or ""
            if tag not in QA_TAGS:
                res["other_tags"][tag] = res["other_tags"].get(tag, 0) + 1
            if r.get("price_window") == "peak" and r.get("outcome") == "forwarded":
                res["peak_calls"][tag] = res["peak_calls"].get(tag, 0) + 1
    else:
        res["call_log"] = None
    for c, by in d["sleep"].items():
        for conv in convs:
            s = by.get(conv)
            if s is not None:
                k = ((s.get("output") or {}).get("cost") or {})
                res["sleep_llm_calls"][f"{c}/{conv}"] = int(k.get("llm_calls", 0))
    res["learning_steps_llm_free"] = not res["other_tags"] and all(v == 0 for v in res["sleep_llm_calls"].values())
    return res


def cost_table(cost: dict | None) -> dict:
    if not cost:
        return {}
    by = cost.get("by_tag") or {}
    return {"spent_usd": cost.get("spent_usd"), "cap_usd": cost.get("cap_usd"), "replayed": (cost.get("total") or {}).get("replayed"),
            "by_tag": {t: {"calls": v.get("calls", 0), "cost_usd": float(v.get("cost_usd", 0)) + float(v.get("unverified_usd", 0)),
                           "replayed": v.get("replayed", 0)} for t, v in sorted(by.items())}}


def leak_table(d: dict, convs: list[str]) -> dict:
    out = {"per_check": {}, "listed_gold_in_use_question": [], "listed_test_question_inside_use_question": [], "all_ok": True}
    for c in ("base",) + CHANDA:
        for conv in convs:
            r = d["leak"][c].get(conv)
            if r is None:
                continue
            out["per_check"][f"{c}/{conv}"] = {"ok": r.get("ok"), "key_table": r.get("key_table"), "keys": r.get("keys"),
                                              "failures": r.get("failures") or []}
            out["all_ok"] = out["all_ok"] and bool(r.get("ok"))
            if c != "base":
                for h in r.get("gold_in_use_question") or []:
                    out["listed_gold_in_use_question"].append({"check": f"{c}/{conv}", **h})
                for h in r.get("test_question_inside_use_question") or []:
                    out["listed_test_question_inside_use_question"].append({"check": f"{c}/{conv}", **h})
    return out


def build(work: str, convs: list[str], cost: dict | None, calls: str | None) -> dict:
    d = collect(work, convs)
    res = {"work": os.path.abspath(work), "conversations": convs}
    res["test_accuracy"] = accuracy_table(d["answers"], convs)
    res["claims"] = claims(d["answers"], convs)
    res["secondary"] = secondary(d["answers"], convs)
    res["full_evidence_test"] = evidence_table(d["asks"], convs)
    res["keys"] = keys_table(d, convs)
    res["overlap_ceiling"] = overlap_ceiling(d, convs)
    cb = res["claims"].get("B")
    if cb:
        gain = cb["first_only"] - cb["second_only"]
        tight = res["overlap_ceiling"]["total"]["tight"]
        res["too_good"] = {"gain_questions": gain, "tight_ceiling_questions": tight, "flag": gain > tight}
    res["latency"] = latency(d["asks"], convs)
    res["db_bytes"] = sizes(work, convs)
    res["cost"] = cost_table(cost)
    res["llm_calls"] = calls_check(calls, d, convs)
    res["leak_checks"] = leak_table(d, convs)
    if d["use_answers"]:
        res["use_half_U0"] = acc(pooled(d["use_answers"], convs))
    res["unparsed_verdicts"] = sum(1 for by in d["answers"].values() for r in pooled(by, convs) if r.get("verdict_parse") == "unparsed")
    return res


def pct(x) -> str:
    return "-" if x is None else f"{100 * x:.1f}%"


def mb(x) -> str:
    return "-" if x is None else f"{x / 1e6:.2f}"


def markdown(res: dict) -> str:
    convs = res["conversations"]
    L = []
    ta = res["test_accuracy"]
    L += [f"## Accuracy on the test half ({len(convs)} conversations pooled)", "",
          "| Condition | Total | " + " | ".join(f"{n} ({k})" for k, n in CATS.items()) + " |",
          "| --- | --- | " + "--- | " * len(CATS)]
    for c in CONDS:
        if c in ta:
            row = ta[c]
            L.append(f"| {c}{'' if row['complete'] else ' (incomplete)'} | {fmt_acc(row['total'])} | "
                     + " | ".join(fmt_acc(row["by_category"][n]) for n in CATS.values()) + " |")
    L += ["", "Accuracy with Wilson 95% interval (correct/n).", "", "### By conversation", "",
          "| Condition | " + " | ".join(convs) + " |", "| --- | " + "--- | " * len(convs)]
    for c in CONDS:
        if c in ta:
            L.append(f"| {c} | " + " | ".join(fmt_acc(ta[c]["by_conversation"][v]) if v in ta[c]["by_conversation"] else "-" for v in convs) + " |")
    L += ["", "## Decision rules (exact two-sided sign test on the paired outcomes)", "",
          "| Claim | Pair | n | first only | second only | p | Decision (higher accuracy and p < 0.05) |",
          "| --- | --- | --- | --- | --- | --- | --- |"]
    for name, p in res["claims"].items():
        L.append(f"| {name}: {p['claim']} | {p['first']} vs {p['second']} | {p['n']} | {p['first_only']} | {p['second_only']} | "
                 f"{p['p']:.3f} | {'**established**' if p['established'] else 'not established'} |")
    L.append("")
    for p in res["claims"].values():
        L.append(p["decision"])
    if "B" in res["claims"] and not res["claims"]["B"]["established"]:
        L += ["", NOT_B]
    tg = res.get("too_good")
    if tg and tg["flag"]:
        L += ["", f"**Too good:** K gains {tg['gain_questions']} questions over U0, above the tight overlap ceiling of "
              f"{tg['tight_ceiling_questions']}. Check for leaks before this result is reported (PROTOCOL.md, Leak check)."]
    L += ["", "### Secondary comparisons (descriptive only)", "", "| Pair | What | n | first only | second only | p |",
          "| --- | --- | --- | --- | --- | --- |"]
    for name, p in res["secondary"].items():
        L.append(f"| {name} | {p['what']} | {p['n']} | {p['first_only']} | {p['second_only']} | {p['p']:.3f} |")
    L += ["", "No correction for multiple tests is applied (two claims and two secondary comparisons).", ""]
    L += ["## Full evidence in the block (test half, no LLM)", "",
          "| Condition | Scored | Full evidence | Turns reached | Mean turn recall |", "| --- | --- | --- | --- | --- |"]
    for c, e in res["full_evidence_test"].items():
        L.append(f"| {c} | {e['scored']} | {e['full']} ({pct(e['full'] / e['scored'])}) | {e['turns_reached']}/{e['turns']} | {pct(e['mean_turn_recall'])} |")
    k = res["keys"]
    L += ["", "## Learned keys", "", "| Conversation | Outcomes (positive) | Keys created | Key table | "
          "Test questions | With a key-brought record | Records | Evidence records |", "| --- | --- | --- | --- | --- | --- | --- | --- |"]
    for conv, r in k["per_conversation"].items():
        kb = r.get("key_brought") or {}
        L.append(f"| {conv} | {r['outcomes']} ({r['positive_outcomes']}) | {'-' if r['keys_created'] is None else r['keys_created']} | "
                 f"{r['key_table'] or 'none'} | {kb.get('questions', '-')} | {kb.get('questions_key_brought', '-')} | "
                 f"{kb.get('records_key_brought', '-')} | {kb.get('evidence_records', '-')} |")
    if "key_brought" in k:
        kb = k["key_brought"]
        L += ["", f"Pooled: in {kb['questions_key_brought']} of {kb['questions']} test questions ({pct(kb['share'])}) a key brought "
              f"into K's block a record U0 did not deliver (`features.key_rank` > 0); {kb['records_key_brought']} such records, "
              f"{kb['evidence_records']} of them evidence turns, in {kb['questions_with_evidence_brought']} questions. "
              f"Questions with any key candidate: {kb['questions_with_key_candidates']}. Keys created: {k['total_created']}."]
    oc = res["overlap_ceiling"]
    t = oc["total"]
    L += ["", "## Overlap ceiling (as PRECHECK.md, on these conversations)", "",
          "| Conversation | Test | Evidence used by a use question | …in a positive outcome | Evidence delivered to a use question | "
          "Missing evidence in U0 | …used positively | Wrong in U0 | Wrong and missing evidence used positively |",
          "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
    for conv, r in list(oc["per_conversation"].items()) + [("**total**", t)]:
        L.append(f"| {conv} | {r['test']} | {r['used_any']} | {r['used_positive']} | {r['delivered_any']} | {r['missing_evidence']} | "
                 f"{r['missing_used_positive']} | {r['wrong_U0']} | {r['tight']} |")
    L += ["", f"Ceiling for keys (evidence in a positive outcome): {pct(oc['share_used_positive'])}. Tight ceiling on a gain in "
          f"accuracy: {t['tight']} questions ({pct(oc['share_tight'])})."]
    L += ["", "## Read latency (`retrieval_ms`, test half)", "", "| Condition | n | Mean | p50 | p95 |", "| --- | --- | --- | --- | --- |"]
    for c, x in res["latency"].items():
        L.append(f"| {c}{' (plain RAG, in the harness)' if c == 'B' else ''} | {x['n']} | {x['mean_ms']:.1f} | {x['p50_ms']:.1f} | {x['p95_ms']:.1f} |")
    L += ["", "U0 and U1 read without keys, K with them.", "", "## Database size (MB)", "",
          "| Conversation | base | U0 | U1 | K | K, compacted | K without keys (reset), compacted |", "| --- | --- | --- | --- | --- | --- | --- |"]
    for conv, r in res["db_bytes"].items():
        L.append(f"| {conv} | {mb(r['base'])} | {mb(r['U0'])} | {mb(r['U1'])} | {mb(r['K'])} | {mb(r['K_compact'])} | {mb(r['K_nokeys_compact'])} |")
    L += ["", "File sizes include the WAL and SHM files. Every copy holds the keys the use half created (inert unless "
          "`read.learned_keys` is on); the compacted pair measures the keys' own size."]
    lc = res["llm_calls"]
    L += ["", "## LLM calls and cost", ""]
    if lc["call_log"]:
        L.append(f"Proxy log `{lc['call_log']}`: {lc['calls']} calls; calls with a tag other than this study's reader/judge tags: "
                 f"{sum(lc['other_tags'].values())}{' ' + json.dumps(lc['other_tags']) if lc['other_tags'] else ''}.")
    else:
        L.append("No proxy call log found.")
    L.append(f"Learning steps (sleep phase 1, no LLM configured): LLM calls reported per run {json.dumps(lc['sleep_llm_calls'])}. "
             + ("**The learning steps made zero LLM calls.**" if lc["learning_steps_llm_free"] else "**A learning step made LLM calls or an untagged call was logged: check.**"))
    if lc["peak_calls"]:
        L.append(f"Deviation: {sum(lc['peak_calls'].values())} calls were made in peak hours {json.dumps(lc['peak_calls'])}.")
    else:
        L.append("No call was made in peak hours.")
    c = res["cost"]
    if c:
        L += ["", "| Tag | Calls | Replayed | Cost US$ |", "| --- | --- | --- | --- |"]
        for tag, v in c["by_tag"].items():
            L.append(f"| {tag} | {v['calls']} | {v['replayed']} | {v['cost_usd']:.4f} |")
        L += ["", f"Spent US${(c['spent_usd'] or 0):.4f} of the US${c['cap_usd']} cap; replayed calls {c['replayed']}."]
    lk = res["leak_checks"]
    L += ["", "## Leak checks (before each test half)", "",
          f"All checks passed: {'yes' if lk['all_ok'] else '**no**'} ({len(lk['per_check'])} checks)."]
    bad = {k: v for k, v in lk["per_check"].items() if not v["ok"]}
    for k2, v in bad.items():
        L.append(f"- {k2}: {'; '.join(v['failures'])}")
    if lk["listed_gold_in_use_question"]:
        L += ["", "Gold answers found inside a use-half question's text (listed, not a stop):", "",
              "| Check | Table.column | Row | Gold | Use question |", "| --- | --- | --- | --- | --- |"]
        for h in lk["listed_gold_in_use_question"]:
            L.append(f"| {h['check']} | {h['table']}.{h['column']} | {h['row']} | {h['match']} | {h.get('via', '')} |")
    if lk["listed_test_question_inside_use_question"]:
        L += ["", f"Test questions whose text is inside a use-half question's text: "
              f"{len(lk['listed_test_question_inside_use_question'])} (listed in the JSON)."]
    if "use_half_U0" in res:
        L += ["", f"Use half (U0, base database): {fmt_acc(res['use_half_U0'])}."]
    L += [f"Unparsed judge verdicts (scored WRONG): {res['unparsed_verdicts']}."]
    return "\n".join(x.rstrip() for x in L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", required=True, help="eval/learn/work/test")
    ap.add_argument("--convs", help="space-separated conversations (default: the 7 test conversations)")
    ap.add_argument("--cost", default=os.path.join(HERE, "cost.json"))
    ap.add_argument("--calls", default=os.path.join(HERE, "llm_calls.jsonl"))
    ap.add_argument("--md", help="markdown output (default: stdout)")
    ap.add_argument("--json", help="JSON output")
    args = ap.parse_args(argv)
    convs = args.convs.split() if args.convs else list(TEST_CONVS)
    res = build(args.work, convs, load_json(args.cost) if args.cost else None, args.calls)
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
