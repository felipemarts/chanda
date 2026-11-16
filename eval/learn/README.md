# eval/learn: harness of the learning study

Protocol: `PROTOCOL.md` (pre-registered in 0fa457e); development checks:
`PRECHECK.md`. Features under test: decisions 0146 (`ranking.utility_lambda`)
and 0145 (learned query keys). The harness reuses the sleep study's
(`eval/sleep/README.md`): `lme study` steps, `locomo_qa.py` (prompts
unchanged), `lib.sh` (with `STUDY_DIR=eval/learn`, so the cost file, call log
and proxy log are this study's own). All steps run from the repository root.

## Pieces

- `lme study keys -db DIR -action rebuild|stats|reset|enable|disable [-out F]`:
  the admin tool `learned_keys` over MCP (decision 0145). Until the tool is in
  the build, it stops with "this Chanda build has no admin tool learned_keys".
- `lme study leakcheck -protocol learn -db DIR -split FILE [-key-table T] [-out F]`:
  the protocol's leak check on a closed database. Every table and text column
  is enumerated from `sqlite_master`; FTS virtual tables and their shadow
  tables (copies of other tables) and the ingested turns (`events.kind =
  'turn'`) are skipped; JSON values are checked by their string leaves.
  Matching is on whole words after lowercasing and dropping punctuation.
  Stops: a test question in any value (unless only inside a use-half question
  the value holds: listed), a key text or retrieval query that is not a
  use-half question, an outcome not ±1 or with used ids outside the
  retrieval's delivered ids, judge output (`CORRECT`/`WRONG`, "gold answer",
  "generated answer"), a gold answer (4 characters or more) in the keys,
  retrievals or outcomes unless inside a use-half question the value holds
  (listed). The key table is detected (`learned_keys`, then any table with
  `text`, `record_id`, `retrieval_id`); with none the report says "no key
  table".
- `lme study ask` records `items[].key_rank` (`features.key_rank` of the
  retrieval log) and `key_candidates`; both are absent without keys.
- `tune.sh` + `tune_summary.py`: the development grid (no LLM).
- `run_learn.sh`: the test run. `project_cost.py`: its projection (both price
  windows, US$4 cap). `report.py` → `results/learn.{md,json}`. `review.py` →
  `review.md`.

## Tuning (development conversations, no LLM)

Needs the keys tool in the build, the sleep study's base databases
(`eval/sleep/work/locomo/<conv>/base`) and one e5 embed server:

```sh
eval/lme/bin/lme embedserve -embedder e5 -e5-dir $CHANDA_HUGOT_MODELS/e5-small -addr 127.0.0.1:8089 &
eval/learn/tune.sh            # grid -> eval/learn/tune/grid.{md,json}
```

`grid.md` ends with a suggested `frozen.set`. Freezing (decision 0147): write
`eval/learn/frozen.set` with all four keys (`ranking.utility_lambda`,
`ranking.min_exposures`, `read.learned_keys_weight`,
`read.learned_keys_top_k`), commit it and tag the commit `learn-freeze`.

## Test run

```sh
eval/learn/run_learn.sh                          # checks + projection, stops (no LLM call)
eval/learn/run_learn.sh --paid --start-proxy     # the study
SPLIT=eval/sleep/split-locomo.json eval/learn/run_learn.sh --dry --convs conv-30 --limit 3 [--skip-keys]
                                                 # harness check on a dev conversation (hash, fake answers)
```

It refuses to run unless 0fa457e and `learn-freeze` are ancestors of HEAD and
`frozen.set` holds the four keys (nothing else). Conditions: base = U0 with
`write.learned_keys=true`; U0 `ranking.utility_lambda=0`, keys off; U1 the
frozen `ranking.*`; K = U1 plus `read.learned_keys=true` and the frozen
weight and top k. `--start-proxy` starts `lmeproxy -cost eval/learn/cost.json
-log eval/learn/llm_calls.jsonl -store eval/lme/llmstore -replay
-offpeak-only -cap-usd 4` (`OFFPEAK_ONLY=0` drops `-offpeak-only`; the report
lists peak calls as a deviation). Every output goes through a `.part` file
and a count check; a rerun resumes. Outputs: `eval/learn/work/test/<conv>/`
(git-ignored).
