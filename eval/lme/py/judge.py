"""Judge: grade each answer with the official LongMemEval judge prompt.

Reads <raw-dir>/<cond>.answers.jsonl (from answer.py), builds the judge
prompt of the question's type (prompts.judge_prompt, a port of
get_anscheck_prompt; abstention when '_abs' is in the question id), calls
lmeproxy with tag "<cond>-judge" at temperature 0 and max_tokens 10, and
parses the verdict as the official script does ('yes' in response.lower()).

Output: <raw-dir>/<cond>.jsonl, the raw record of integrity rule 5: one line
per question with question, gold answer, question_type, condition, context,
answer, verdict, judge output and the tokens of every call. Resumable like
answer.py.
"""

from __future__ import annotations

import argparse
import concurrent.futures as cf
import os
import sys
import threading

import common
import prompts


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--condition", required=True, choices=common.CONDITIONS)
    ap.add_argument("--raw-dir", required=True, help="eval/reports/<date>/raw")
    ap.add_argument("--proxy", default="http://127.0.0.1:8787")
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--workers", type=int, default=4)
    args = ap.parse_args(argv)

    src = os.path.join(args.raw_dir, f"{args.condition}.answers.jsonl")
    out = os.path.join(args.raw_dir, f"{args.condition}.jsonl")
    answers = common.read_jsonl(src)
    done = common.done_ids(out)
    todo = [a for a in answers if a["question_id"] not in done]
    tag = f"{args.condition}-judge"
    print(f"{len(answers)} answers, {len(todo)} to judge; tag {tag}", file=sys.stderr)
    lock = threading.Lock()
    stop = threading.Event()

    def work(a: dict) -> None:
        if stop.is_set():
            return
        abst = common.is_abstention(a["question_id"])
        p = prompts.judge_prompt(a["question_type"], a["question"], a["gold_answer"], a["answer"], abstention=abst)
        try:
            r = common.chat(args.proxy, tag, [{"role": "user", "content": p}],
                            model=args.model, max_tokens=prompts.JUDGE_MAX_TOKENS)
        except common.ProxyRefused:
            stop.set()  # before any other worker picks up a job
            raise
        raw = r["content"].strip()
        rec = dict(a)
        rec["verdict"] = prompts.parse_verdict(raw)
        rec["judge"] = {
            "tag": tag,
            "model": args.model,
            "template": "abstention" if abst else a["question_type"],
            "raw": raw,
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
        futs = {ex.submit(work, a): a["question_id"] for a in todo}
        for f in cf.as_completed(futs):
            try:
                f.result()
            except common.ProxyRefused as e:
                stop.set()
                print(f"STOP: proxy refused ({futs[f]}): {e}", file=sys.stderr)
                failed += 1
            except Exception as e:  # noqa: BLE001
                print(f"error on {futs[f]}: {e}", file=sys.stderr)
                failed += 1
    print(f"wrote {out}; failures {failed}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
