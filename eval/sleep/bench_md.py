#!/usr/bin/env python3
"""Write eval/sleep/BENCH.md from the bench outputs (eval/sleep/work/bench,
produced by run_bench.sh and bench_report.py), and copy the raw blocks,
answers and sleep results to eval/sleep/bench/ for the record."""
import json
import os
import re
import shutil
import sys

W = "eval/sleep/work/bench"
OUT = "eval/sleep/bench"
data = {q["question_id"]: q for q in json.load(open("eval/lme/data/longmemeval_s_cleaned.json"))}
b = json.load(open(f"{W}/bench.json"))
md = open(f"{W}/bench.md").read()
fichas_md = md.split("## Fichas", 1)[1]


def ficha_section(qid, mode):
    m = re.search(rf"### {re.escape(qid)} {mode}: .*?\n\n```\n(.*?)```", fichas_md, re.S)
    return m.group(1).strip() if m else "(no ficha)"


def final_answer(a):
    a = a.strip()
    m = re.findall(r"\*\*Answer:?\*\*:?\s*(.+)", a)
    if m:
        return m[-1].strip()
    return a[-300:].replace("\n", " ")


def block_items(qid, mode):
    x = json.loads(open(f"{W}/{qid}/{mode}.ask.jsonl").readline())
    return x


os.makedirs(OUT, exist_ok=True)
L = []
L.append("# Bench: usage-guided sleep on 9 LongMemEval tuning questions (diagnostic)\n")
L.append("> **This bench asks the same question again after the sleep. It measures memorisation, not")
L.append("> improvement on future questions.** The sleep saw one retrieval (that question's) and its outcome")
L.append("> sign, so the group it digests is that question's top 10. The evidence for later answers is the")
L.append("> pre-registered LoCoMo test (`PROTOCOL.md`, `RESULTS.md`).\n")
L.append("Selection rule, fixed before running (task, Part 2): in `eval/split-tune.json` order, the first 6 "
         "questions D2 (decision 0141, run V7) gets wrong among multi-session and temporal ones, then the first "
         "3 it gets right. For each one: ingest; ask with D2; judge; record the outcome (+1/−1, used ids); copy the "
         "database twice; run the guided sleep, extractive (X) on one copy and LLM (L) on the other; ask again. "
         "Reader and judge are the official LongMemEval CoT reader and judge (deepseek-flash), as in D2. "
         "The context budget is 2,000 tokens, and the derived share is 0.30 with the top 2 raw excerpts protected.\n")
L.append(f"Result: correct before {b['correct_before']}/9, after X {b['correct_X']}/9, after L {b['correct_L']}/9. "
         "No question changed verdict. The 3 that were right stayed right, so the stop rule (sleep worsens the 3 "
         "right ones) did not trigger. Cost of the whole bench: see the last table.\n")
L.append(md.split("## Fichas", 1)[0].replace("## Verdicts", "## Summary: verdicts").strip() + "\n")

L.append("## Case by case\n")
L.append("Blocks are listed item by item: `key` is the haystack turn (`s<session>t<turn>`), ★ marks an evidence "
         "turn, and the number is the rendered tokens. The full blocks, answers and sleep results are in "
         "`eval/sleep/bench/<qid>/`.\n")
for q in b["questions"]:
    qid = q["question_id"]
    dq = data[qid]
    ev = {f"s{s}t{t}" for s, ss in enumerate(dq["haystack_sessions"]) for t, tr in enumerate(ss) if tr.get("has_answer")}
    L.append(f"### {qid} ({q['question_type']})\n")
    L.append(f"- Question: {q['question']}")
    L.append(f"- Gold: {q['gold']}")
    L.append(f"- Evidence turns: {', '.join(sorted(ev)) or 'none'}\n")
    blocks = {}
    for mode in ("before", "X", "L"):
        x = block_items(qid, mode)
        blocks[mode] = x
        items = []
        for it in x["items"]:
            key = it.get("key") or it["type"]
            star = "★" if it.get("key") in ev else ""
            items.append(f"{star}{key}:{it['tokens']}" if it["type"] == "event" else f"**{it['type']}**:{it['tokens']}")
        r = q["rounds"][mode]
        evd = r["evidence"]
        L.append(f"- Block {mode}: {r['context_tokens']} tokens, sleep share {float(r['sleep_share']):.0%}, "
                 f"evidence {evd['reached']}/{evd['turns']}: " + " ".join(items))
    before_keys = {it.get("key") for it in blocks["before"]["items"] if it["type"] == "event"}
    for mode in ("X", "L"):
        keys = {it.get("key") for it in blocks[mode]["items"] if it["type"] == "event"}
        lost = sorted(before_keys - keys)
        if lost:
            L.append(f"- Raw excerpts in the block before, not after {mode}: " +
                     ", ".join(("★" if k in ev else "") + k for k in lost))
    L.append("")
    for mode in ("before", "X", "L"):
        r = q["rounds"][mode]
        rt = r["reader_tokens"]
        L.append(f"- Answer {mode} ({'✓' if r['verdict'] else '✗'}; reader {rt['prompt']}+{rt['completion']} tokens): "
                 f"{final_answer(r['answer'])}")
    L.append("")
    for mode in ("X", "L"):
        s = q["sleep"][mode]
        g = s.get("guided") or {}
        c = s["cost"]
        L.append(f"- Sleep {mode}: {c['llm_calls']} LLM calls, {c['input_tokens']}+{c['output_tokens']} tokens, "
                 f"{s['seconds']:.1f} s; groups digested {g.get('groups_digested')}, lines written "
                 f"{g.get('lines_written')}, discarded {g.get('lines_discarded') or {}}")
    L.append("")
    for mode in ("X", "L"):
        L.append(f"Ficha {mode}:\n\n```\n{ficha_section(qid, mode)}\n```\n")
    d = f"{OUT}/{qid}"
    os.makedirs(d, exist_ok=True)
    for f in ("before.ask.jsonl", "X.ask.jsonl", "L.ask.jsonl", "X.sleep.json", "L.sleep.json", "outcomes.jsonl"):
        shutil.copy(f"{W}/{qid}/{f}", f"{d}/{f}")

L.append("## Cost (proxy, by tag)\n")
L.append("| Tag | Calls | Prompt tokens | Completion tokens | USD |")
L.append("| --- | --- | --- | --- | --- |")
tot = [0, 0, 0, 0.0]
for tag, t in sorted(b["proxy_by_tag"].items()):
    L.append(f"| {tag} | {t['calls']} | {t['prompt_tokens']} | {t['completion_tokens']} | {t['cost_usd']:.4f} |")
    for i, k in enumerate(("calls", "prompt_tokens", "completion_tokens", "cost_usd")):
        tot[i] += t[k]
L.append(f"| **total** | {tot[0]} | {tot[1]} | {tot[2]} | **{tot[3]:.4f}** |\n")
for mode in ("before", "X", "L"):
    shutil.copytree(f"{W}/llm-{mode}", f"{OUT}/llm-{mode}", dirs_exist_ok=True)
open("eval/sleep/BENCH.md", "w").write("\n".join(L) + "\n")
print("wrote eval/sleep/BENCH.md")
