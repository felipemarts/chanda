"""Reader: answer each sampled question from a condition's context.

Input: a context JSONL (one line per question; condition A needs none), the
LongMemEval S file and eval/sample.json. Every condition uses the same
official reader template (prompts.READER_HISTORY_COT by default) with the
context in the History Chats slot; condition A has an empty context.
Calls go through lmeproxy with tag "<cond>-reader", temperature 0.

Output: <out-dir>/<cond>.answers.jsonl, one line per question, appended and
fsynced as each answer arrives; already answered questions are skipped, so
a rerun after a crash or a refusal never pays twice.

Context JSONL line (same shape as the Go context generator and mem0_cond.py):
  {"question_id", "condition", "context", "context_tokens",
   "ingest_seconds", "retrieval_latency_ms", ...}
Extra keys are kept under "context_meta".

Example:
  python answer.py --condition B --contexts ctx/B.jsonl \
      --dataset data/longmemeval_s_cleaned.json --sample ../../sample.json \
      --proxy http://127.0.0.1:8787 --out-dir ../../reports/2026-11-15/raw --limit 10
"""

from __future__ import annotations

import argparse
import concurrent.futures as cf
import hashlib
import os
import sys
import threading

import common
import prompts

BUDGET = 2000


def build_jobs(args) -> list[dict]:
    data = common.load_dataset(args.dataset)
    ids = common.select_ids(common.load_sample_ids(args.sample), args.limit, args.questions)
    ctx = {}
    if args.condition != "A" or args.contexts:
        if not args.contexts:
            sys.exit(f"condition {args.condition} needs --contexts")
        for r in common.read_jsonl(args.contexts):
            if r.get("condition", args.condition) != args.condition:
                sys.exit(f"{args.contexts}: line for condition {r.get('condition')}, expected {args.condition}")
            ctx[r["question_id"]] = r
    jobs = []
    for qid in ids:
        if qid not in data:
            sys.exit(f"question {qid} is not in {args.dataset}")
        q = data[qid]
        if args.condition == "A" and not args.contexts:
            c = {"question_id": qid, "condition": "A", "context": "", "context_tokens": 0}
        elif qid not in ctx:
            sys.exit(f"{args.contexts} has no context for {qid}; refusing to answer a partial set")
        else:
            c = ctx[qid]
        text = c.get("context") or ""
        tokens = common.est_tokens(text)
        if tokens > args.budget and not args.allow_over_budget:
            sys.exit(f"{qid}: context is {tokens} tokens (ceil(chars/4)) > budget {args.budget}")
        if args.condition == "A" and text:
            sys.exit("condition A must have an empty context")
        use_noret = args.a_template == "official-no-retrieval" and not text
        prompt = prompts.reader_prompt(
            text, q["question_date"], q["question"], cot=args.reading == "con", no_retrieval_template=use_noret
        )
        meta = {k: v for k, v in c.items() if k not in ("question_id", "condition", "context", "context_tokens")}
        jobs.append(
            {
                "question_id": qid,
                "condition": args.condition,
                "question_type": q["question_type"],
                "abstention": common.is_abstention(qid),
                "question": q["question"],
                "question_date": q["question_date"],
                "gold_answer": common.gold_text(q["answer"]),
                "context": text,
                "context_tokens": tokens,
                "context_tokens_reported": c.get("context_tokens"),
                "ingest_seconds": c.get("ingest_seconds"),
                "retrieval_latency_ms": c.get("retrieval_latency_ms"),
                "context_meta": meta,
                "reader_prompt": prompt,
            }
        )
    return jobs


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--condition", required=True, choices=common.CONDITIONS)
    ap.add_argument("--contexts", help="context JSONL (not needed for A)")
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--sample", required=True)
    ap.add_argument("--limit", type=int, help="first N questions of the sample (smoke run: 10)")
    ap.add_argument("--questions", nargs="*", help="restrict to these question ids")
    ap.add_argument("--proxy", default="http://127.0.0.1:8787")
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--out-dir", required=True)
    ap.add_argument("--reading", choices=["con", "direct"], default="con",
                    help="official reading method: con = step by step (README recommendation), direct")
    ap.add_argument("--max-tokens", type=int, help="default: official gen_length (800 con, 500 direct)")
    ap.add_argument("--a-template", choices=["same", "official-no-retrieval"], default="same",
                    help="empty context: same history template (default, owner rule 3) or the official no-retrieval one")
    ap.add_argument("--budget", type=int, default=BUDGET)
    ap.add_argument("--allow-over-budget", action="store_true")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--tag", help="proxy tag (default <condition>-reader); the prompt is unchanged")
    args = ap.parse_args(argv)

    max_tokens = args.max_tokens or (prompts.GEN_LENGTH_COT if args.reading == "con" else prompts.GEN_LENGTH_DIRECT)
    out = os.path.join(args.out_dir, f"{args.condition}.answers.jsonl")
    jobs = build_jobs(args)
    done = common.done_ids(out)
    todo = [j for j in jobs if j["question_id"] not in done]
    tag = args.tag or f"{args.condition}-reader"
    print(f"{len(jobs)} questions, {len(done & {j['question_id'] for j in jobs})} already answered, {len(todo)} to do; tag {tag}", file=sys.stderr)
    lock = threading.Lock()
    stop = threading.Event()

    def work(j: dict) -> None:
        if stop.is_set():
            return
        try:
            r = common.chat(args.proxy, tag, [{"role": "user", "content": j["reader_prompt"]}],
                            model=args.model, max_tokens=max_tokens)
        except common.ProxyRefused:
            stop.set()  # before any other worker picks up a job
            raise
        rec = dict(j)
        rec["reader_prompt_sha256"] = hashlib.sha256(j["reader_prompt"].encode()).hexdigest()
        del rec["reader_prompt"]
        rec["answer"] = r["content"].strip()  # run_generation.py: content.strip()
        rec["reader"] = {
            "tag": tag,
            "model": args.model,
            "reading": args.reading,
            "max_tokens": max_tokens,
            "a_template": args.a_template if not j["context"] else "same",
            "prompts_commit": prompts.LONGMEMEVAL_COMMIT,
            "tokens": common.usage_tokens(r["usage"]),
            "latency_ms": r["latency_ms"],
            "finish_reason": r["finish_reason"],
            "cost_usd": r["cost_usd"],
        }
        with lock:
            common.append_jsonl(out, rec)

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
            except Exception as e:  # noqa: BLE001 - reported, run continues
                print(f"error on {futs[f]}: {e}", file=sys.stderr)
                failed += 1
    print(f"wrote {out}; failures {failed}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
