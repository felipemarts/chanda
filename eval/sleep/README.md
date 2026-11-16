# eval/sleep: harness of the sleep study

Protocol: `PROTOCOL.md` (pre-registered in dfb68b0). Features under test:
decisions 0142 (`sleep.mode: guided`, `sleep.guided.*`) and 0143
(`read.context_derived_max_share`, `read.context_protect_raw`). All steps
run from the repository root.

## Pieces

- `lme study <step>` (Go, `eval/lme/cmd/lme/study.go`, `eval/lme/lme/study*.go`):
  steps on a *study directory* whose `chanda.db` persists between steps.
  Each step starts its own `lme chanda serve` on the directory and closes it,
  so the database can be copied between steps. `study.json` holds the source,
  the embedder and the turn map (dia_id or `s<i>t<j>` → event id), never a
  gold answer or a question.
  - `ingest`: LoCoMo (`-locomo FILE -conv ID`; one `log_session` per session,
    id `<sample_id>-s<n>`, speaker_a → user, speaker_b → assistant, content
    `"<Speaker>: <text>"` plus `" [shares a photo: <caption>]"`, ts = the
    session date) or one LongMemEval haystack (`-data FILE -question QID`,
    `IngestQuestion`, exactly as condition D).
  - `ask`: `context` (budget 2,000) per question, in split order. JSONL with
    question, gold (harness only), category, retrieval id, the block, the
    delivered items (id, type, rendered tokens, truncation, text), evidence
    matching and the sleep-item share (fact + page tokens / block tokens).
  - `outcome`: one `outcome` per retrieval from a judged answers JSONL:
    value +1/−1, `used_ids` by the protocol rule (`lme/usedids.go`). Refuses
    retrievals of another database and retrievals that already have one.
  - `copy`, `sleep` (`-phases`, `-budget-tokens`, `-budget-events`; `-llm-url
    ... -allow-llm` for an LLM; the result JSON has the MCP output, the
    `sleep_runs` row with its counts, stats before/after and every page with
    its items and cited events), `rag` (plain RAG B, one index per
    conversation), `fichas`, `leakcheck`, `check` (validates a configuration
    with Chanda's loader; prints its hash). The learning study adds `keys`
    and `leakcheck -protocol learn` (`eval/learn/README.md`).
  - Every server-starting step takes `-set key=value` (repeatable) and
    `-set-file FILE`, and refuses an unknown key before starting.
- `locomo_qa.py`: LoCoMo reader then judge, prompts verbatim from the
  protocol (`locomo_prompts.py`, checked against `PROTOCOL.md` by the tests),
  deepseek-flash, temperature 0, max_tokens 200 / 5, tags `<tag>-reader` /
  `<tag>-judge`. Resumable. `--fake` for dry runs (no network).
- The bench uses `eval/lme/py/answer.py` and `judge.py` (official
  LongMemEval CoT reader and judge, condition D template) with `--tag`.
- `project_cost.py`: projection printed before paid steps (exit 3 when spent
  + upper bound > cap). `report.py`: LoCoMo tables. `bench_report.py`:
  bench data for BENCH.md.

## Evidence matching

The selection study's rule (`eval/lme/lme/evidence.go`: 60% prefix,
whitespace removed), first in the item with the turn's event id, then in
the text of all raw excerpts. Facts and pages are left out of that fallback
(an extractive ficha copies turn text). Secondary count: a delivered fact or
page whose text names the evidence event id credits the turn ("cited").
LoCoMo evidence ids are parsed per `eval/locomo/SOURCE.md` (`D8:6; D9:17`,
`D:11:26`, `D30:05` and the space-separated lists are split and normalised;
`D` and the dangling `D10:19`, `D4:36` are skipped); a question with no
valid evidence is not scored.

## Running

Prerequisites: `eval/locomo/data/locomo10.json` and
`eval/lme/data/longmemeval_s_cleaned.json`; for e5, one embed server
(`eval/lme/bin/lme embedserve -embedder e5 -e5-dir $CHANDA_HUGOT_MODELS/e5-small -addr 127.0.0.1:8089`);
`.env` with `DEEPSEEK_API_KEY` when the driver starts the proxy.

```sh
eval/sleep/run_bench.sh --dry                       # hash, no LLM: X only, fake verdicts
eval/sleep/run_bench.sh                             # checks + projection, stops
eval/sleep/run_bench.sh --paid --start-proxy        # the bench
eval/sleep/run_locomo.sh --dry --limit 3            # hash, no LLM: S0, S1, B, fake verdicts
eval/sleep/run_locomo.sh                            # checks + projection, stops
eval/sleep/run_locomo.sh --paid --start-proxy       # the study
```

`--start-proxy` builds and starts `lmeproxy -cost eval/sleep/cost.json -log
eval/sleep/llm_calls.jsonl -store eval/lme/llmstore -replay -offpeak-only
-cap-usd 2`, with the key loaded by `set -a; . ./.env; set +a` into the
proxy's environment only. Without it, a proxy must already answer at
`$PROXY_URL` and hold `eval/sleep/cost.json.lock`; its other flags cannot be
checked from the driver.

`run_locomo.sh` refuses to run unless dfb68b0 is an ancestor of HEAD, and in
paid mode unless `eval/sleep/frozen.set` (the bench's frozen overrides,
decision 0144; one `key=value` per line, may be empty) exists and the tag
`sleep-freeze` is an ancestor of HEAD. S3 runs last, with S2's tokens for
that conversation as its token budget and 1,000,000 events, and only if the
spend so far plus its upper projection stays under the cap.

Outputs: `eval/sleep/work/` (git-ignored): `bench/<qid>/` and
`locomo/<conv>/{base,S0,S1,S2,S3}/` databases; `locomo/<conv>/out/`
`<C>.<half>.ask.jsonl`, `.answers.jsonl`, `<C>.sleep.json`, `<C>.leak.json`,
`base.use.outcomes.jsonl`; `bench/llm-{before,X,L}/` reader/judge records,
`bench/bench.{md,json}`. Report tables: `eval/sleep/results/locomo.{md,json}`.
