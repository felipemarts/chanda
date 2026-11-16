"""Reader then judge for the LoCoMo sleep study (eval/sleep/PROTOCOL.md).

Input: an ask JSONL from `lme study ask` or `lme study rag` (question,
gold, speakers, the context block as delivered). For each line: the reader
prompt (locomo_prompts.READER, max_tokens 200), then the judge prompt
(locomo_prompts.JUDGE, max_tokens 5), both deepseek-flash at temperature 0
through lmeproxy with tags "<tag>-reader" and "<tag>-judge" (e.g.
S1-test-reader). Output: one JSONL line per question with the answer, the
verdict (boolean), the judge's raw reply and how it was parsed, tokens,
latency and cost of both calls. Appended and fsynced per question; a rerun
skips answered questions, so a refusal or a crash never pays twice.

--fake answers "Not mentioned" and judges WRONG without any network call:
for dry runs of the drivers only (its output says so on every line).

Example:
  python3 eval/sleep/locomo_qa.py --ask W/out/S1.test.ask.jsonl \
      --out W/out/S1.test.answers.jsonl --tag S1-test --proxy http://127.0.0.1:8787
"""

from __future__ import annotations

import argparse
import concurrent.futures as cf
import hashlib
import os
import sys
import threading

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lme", "py"))
sys.path.insert(0, HERE)

import common  # noqa: E402  (eval/lme/py/common.py: chat, JSONL, estimator)
import locomo_prompts as P  # noqa: E402

BUDGET = 2000


def build_jobs(lines: list[dict], budget: int) -> list[dict]:
    jobs = []
    seen = set()
    for r in lines:
        qid = r["question_id"]
        if qid in seen:
            sys.exit(f"question {qid} appears twice in the ask file")
        seen.add(qid)
        ctx = r.get("context") or ""
        if common.est_tokens(ctx) > budget:
            sys.exit(f"{qid}: context is {common.est_tokens(ctx)} tokens > budget {budget}")
        if not r.get("speaker_a") or not r.get("speaker_b"):
            sys.exit(f"{qid}: no speaker names (not a LoCoMo ask line?)")
        ev = r.get("evidence") or {}
        jobs.append({
            "question_id": qid,
            "condition": r.get("condition"),
            "half": r.get("half"),
            "sample_id": r.get("sample_id"),
            "index": r.get("index"),
            "category": r.get("category"),
            "category_name": r.get("category_name"),
            "question": r["question"],
            "gold": r["gold"],
            "retrieval_id": r.get("retrieval_id") or "",
            "context_tokens": common.est_tokens(ctx),
            "items": len(r.get("items") or []),
            "sleep_items": r.get("sleep_items", 0),
            "sleep_tokens": r.get("sleep_tokens", 0),
            "sleep_share": r.get("sleep_share", 0.0),
            "evidence": {k: ev.get(k) for k in ("scored", "reached", "full", "reached_or_cited", "full_or_cited")}
                        | {"turns": len(ev.get("turns") or [])},
            "reader_prompt": P.reader_prompt(r["speaker_a"], r["speaker_b"], ctx, r["question"]),
        })
    return jobs


def call_meta(r: dict, tag: str, model: str, max_tokens: int) -> dict:
    return {"tag": tag, "model": model, "max_tokens": max_tokens, "tokens": common.usage_tokens(r["usage"]),
            "latency_ms": r["latency_ms"], "finish_reason": r["finish_reason"], "cost_usd": r["cost_usd"]}


def run_one(j: dict, args) -> dict:
    rtag, jtag = f"{args.tag}-reader", f"{args.tag}-judge"
    rec = {k: v for k, v in j.items() if k != "reader_prompt"}
    rec["reader_prompt_sha256"] = hashlib.sha256(j["reader_prompt"].encode()).hexdigest()
    if args.fake:
        answer, raw = "Not mentioned", "WRONG"
        rec["fake"] = True
        rec["reader"] = {"tag": None, "fake": True}
        rec["judge"] = {"tag": None, "fake": True}
    else:
        r = common.chat(args.proxy, rtag, [{"role": "user", "content": j["reader_prompt"]}],
                        model=args.model, max_tokens=P.READER_MAX_TOKENS)
        answer = r["content"].strip()
        rec["reader"] = call_meta(r, rtag, args.model, P.READER_MAX_TOKENS)
        jp = P.judge_prompt(j["question"], j["gold"], answer)
        g = common.chat(args.proxy, jtag, [{"role": "user", "content": jp}],
                        model=args.model, max_tokens=P.JUDGE_MAX_TOKENS)
        raw = g["content"].strip()
        rec["judge"] = call_meta(g, jtag, args.model, P.JUDGE_MAX_TOKENS)
    verdict, how = P.parse_verdict(raw)
    rec["answer"] = answer
    rec["verdict"] = verdict
    rec["judge_raw"] = raw
    rec["verdict_parse"] = how
    return rec


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--ask", required=True, help="ask JSONL (lme study ask / rag)")
    ap.add_argument("--out", required=True, help="answers JSONL (appended; resumable)")
    ap.add_argument("--tag", required=True, help="tag prefix, e.g. S1-test (calls are tagged <tag>-reader, <tag>-judge)")
    ap.add_argument("--proxy", default="http://127.0.0.1:8787")
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--limit", type=int, help="first N lines only")
    ap.add_argument("--budget", type=int, default=BUDGET)
    ap.add_argument("--fake", action="store_true", help="no network: answer 'Not mentioned', judge WRONG (dry runs)")
    args = ap.parse_args(argv)

    lines = common.read_jsonl(args.ask)
    if args.limit is not None:
        lines = lines[: args.limit]
    jobs = build_jobs(lines, args.budget)
    done = common.done_ids(args.out)
    todo = [j for j in jobs if j["question_id"] not in done]
    print(f"{len(jobs)} questions, {len(jobs) - len(todo)} already answered, {len(todo)} to do; tag {args.tag}"
          + (" (FAKE: no LLM)" if args.fake else ""), file=sys.stderr)
    lock = threading.Lock()
    stop = threading.Event()

    def work(j: dict) -> None:
        if stop.is_set():
            return
        try:
            rec = run_one(j, args)
        except common.ProxyRefused:
            stop.set()
            raise
        with lock:
            common.append_jsonl(args.out, rec)

    failed = 0
    with cf.ThreadPoolExecutor(max_workers=max(1, args.workers)) as ex:
        futs = {ex.submit(work, j): j["question_id"] for j in todo}
        for f in cf.as_completed(futs):
            try:
                f.result()
            except common.ProxyRefused as e:
                stop.set()
                print(f"STOP: proxy refused ({futs[f]}): {e}", file=sys.stderr)
                failed += 1
            except Exception as e:  # noqa: BLE001 - reported; the run continues
                print(f"error on {futs[f]}: {e}", file=sys.stderr)
                failed += 1
    recs = common.read_jsonl(args.out) if os.path.exists(args.out) else []
    ids = {j["question_id"] for j in jobs}
    mine = [r for r in recs if r["question_id"] in ids]
    ok = sum(1 for r in mine if r["verdict"])
    unparsed = sum(1 for r in mine if r.get("verdict_parse") == "unparsed")
    print(f"wrote {args.out}: {len(mine)}/{len(jobs)} answered, {ok} correct, {unparsed} unparsed verdicts; failures {failed}",
          file=sys.stderr)
    return 1 if failed or len(mine) != len(jobs) else 0


if __name__ == "__main__":
    sys.exit(main())
