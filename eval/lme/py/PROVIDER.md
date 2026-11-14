# LLM provider, prompts and Mem0: facts for the LongMemEval battery

All facts below come from official sources, read on **2026-11-14**. No LLM
API was called while preparing this. Every test runs against local fake
servers, and a socket guard in `tests/conftest.py` refuses every
non-loopback connection.

## 1. DeepSeek API

Sources:
- https://api-docs.deepseek.com/quick_start/pricing (models, prices, peak hours, context)
- https://api-docs.deepseek.com/api/create-chat-completion (request and usage fields)
- https://api-docs.deepseek.com/guides/thinking_mode
- https://api-docs.deepseek.com/guides/json_mode
- https://api-docs.deepseek.com/guides/kv_cache
- https://api-docs.deepseek.com/quick_start/rate_limit

**Model id.** `deepseek-flash` exists under that exact name (version
DeepSeek-V4.1-Flash-1017). The only other listed model is `deepseek-v4-pro`.
The legacy names `deepseek-v4-flash` and `deepseek-v4-flash-vision-exp` are
retired, but DeepSeek still accepts them, serves them with
DeepSeek-V4.1-Flash-1017 and bills them at the Flash price. Mem0's DeepSeek
provider defaults to `deepseek-chat`, which the pricing page no longer lists.
`mem0_cond.py` therefore sets `deepseek-flash` explicitly, and lmeproxy
refuses any model that is missing from its price table.

**Price** (USD per 1M tokens; off-peak is half the peak price):

| | off-peak | peak |
|---|---|---|
| input, cache hit | 0.003 | 0.006 |
| input, cache miss | 0.15 | 0.30 |
| output | 0.60 | 1.20 |

**Peak hours** are 01:00-04:00 and 06:00-10:00 UTC, Monday to Friday,
excluding Chinese public holidays. Every other hour is off-peak, including
all of Saturday and Sunday. The off-peak windows are therefore Mon-Fri
00:00-01:00, 04:00-06:00 and 10:00-24:00 UTC, plus the whole weekend.
lmeproxy treats Chinese holidays as peak, which can only over-estimate the
cost.

**Context and output.** The context length is 1M tokens and the maximum
output is 384K. The default `max_tokens` is 8K in non-thinking mode and 64K
in thinking mode.

**Thinking mode is ON by default.** "Thinking mode does not support the
temperature ... parameters. Setting them causes no error but has no
effect." Temperature takes effect only in non-thinking mode, where its range
is 0-2. lmeproxy therefore adds `"thinking": {"type": "disabled"}` to every
request and drops `reasoning_effort` (flag `-thinking`, default
`disabled`). Without this, temperature 0 would be silently ignored and every
call would also pay for reasoning tokens. The documentation does not
promise deterministic output at temperature 0.

**JSON output.** `response_format` accepts only `text` or `json_object`, not
`json_schema`. The prompt must contain the word "json". Content may come
back blank even when `finish_reason` is `stop`.
- Mem0 uses `json_object`.
- Chanda's `llm.response_format: auto` first sends `json_schema`. lmeproxy
  answers that locally (free, never forwarded) with a 400 whose message names
  `response_format`, so Chanda falls back to `json_object` (tested in
  `proxy_test.go`). Setting `response_format: json_object` in condition E's
  Chanda config avoids that one rejected call per process.

**Caching.** Context caching is on by default and best-effort, with no
guaranteed hit rate. Usage reports `prompt_cache_hit_tokens` and
`prompt_cache_miss_tokens`, whose sum is `prompt_tokens`. lmeproxy records
both.

**Rate limits.** The limit is concurrency: 2,500 concurrent requests per
account for deepseek-flash, and HTTP 429 when it is exceeded. While a
non-streaming request waits, the server sends blank keep-alive lines (the
proxy tolerates them). The server closes a request that has not started
inference within 10 minutes.

## 2. LongMemEval official prompts

**Source.** https://github.com/xiaowu0162/LongMemEval at commit
`9e0b455f4ef0e2ab8f2e582289761153549043fc` (HEAD of main, 2026-05-11). The
license is MIT.
- Copies of `src/generation/run_generation.py` and
  `src/evaluation/evaluate_qa.py` are in `official/*.txt` (as text, never
  executed).
- `prompts.py` was generated from them by AST extraction.
- `tests/test_prompts.py` proves byte equality with the copies and checks
  their sha256.

**Reader** (`run_generation.py`, `prepare_prompt`, lines 48-67):
- The README recommends the `con` reading method, and `run_generation.sh`
  defaults to it: `cot=true`, gen_length 800.
- `direct` has gen_length 500.
- The call is one user message with temperature 0.
- The template we use for every condition is line 55
  (`READER_HISTORY_COT`):
  `I will give you several history chats ... Answer the question step by
  step ...\n\n\nHistory Chats:\n\n{context}\n\nCurrent Date:
  {question_date}\nQuestion: {question}\nAnswer (step by step):`.
- Condition A gets the same template with an empty History Chats block
  (owner rule 3: same answer prompt for all conditions).
  `--a-template official-no-retrieval` switches A to the official
  no-retrieval template, which is the question followed by "Answer step by
  step.". **The architect should confirm which one A uses.**

**Judge** (`evaluate_qa.py`, `get_anscheck_prompt`, lines 24-43):
- Templates per type: one shared template for single-session-user,
  single-session-assistant and multi-session; temporal-reasoning (does not
  penalize off-by-one day counts); knowledge-update (accepts previous info
  plus the updated answer); single-session-preference (rubric).
- The call is one user message with temperature 0 and `max_tokens` 10.
- The verdict is `'yes' in response.lower()`, a substring test that we keep
  as is.

**Abstention.**
- Abstention is detected by `'_abs' in question_id` (README: "If
  `question_id` ends with `_abs`, then the question is an abstention
  question").
- An abstention question uses the abstention template whatever its
  `question_type`. The gold `answer` field is an explanation and goes in the
  "Explanation" slot. The judge answers yes "if the model correctly
  identifies the question as unanswerable".
- The README notes that retrieval evaluation skips the 30 abstention
  instances.

## 3. Mem0 open source

Sources:
- https://pypi.org/project/mem0ai/ (2.2.1, released 2026-09-25, the latest
  on 2026-11-14)
- https://github.com/mem0ai/mem0/releases (tag v2.2.1)
- https://docs.mem0.ai/components/llms/models/deepseek
- the 2.2.1 wheel source we inspected: `mem0/memory/main.py`,
  `mem0/llms/deepseek.py`, `mem0/embeddings/openai.py`,
  `mem0/vector_stores/qdrant.py`, `mem0/configs/prompts.py`

**Version.** `mem0ai==2.2.1` requires Python `>=3.10,<4.0`. The system
Python is 3.9.6, so `setup.sh` fetches uv 0.13.6 and CPython 3.12.16 into the
gitignored `.local/`.

**Verdict: Mem0 CAN be configured as the owner requires.** `mem0_cond.py`
verifies this end to end against fakes, and once more through the real
lmeproxy binary.

| Requirement | Supported | How |
|---|---|---|
| (a) DeepSeek as the extraction LLM, temperature 0 | yes | `llm.provider: deepseek`, `deepseek_base_url: http://127.0.0.1:8787/tag/C-ingest/v1`, `model: deepseek-flash`, `temperature: 0`. The proxy also forces temperature 0 and non-thinking mode. |
| (b) OpenAI-compatible embedding endpoint with a custom base URL and dimension | yes | `embedder.provider: openai`, `openai_base_url`, `embedding_dims` (256 hash, 384 e5-small). |
| (c) Local on-disk vector store, no external service | yes | `vector_store.provider: qdrant` with `path` and `on_disk: true` (embedded qdrant-client), plus a SQLite history DB. Telemetry and the remote "notices" fetch are disabled with `MEM0_TELEMETRY=False`, and `MEM0_DIR` points into the work directory. |

**Embedding server requirements.** Mem0's OpenAI embedder sends:
- `input` as a list, up to 100 texts per call;
- `model`;
- `encoding_format: "float"`;
- `dimensions: <embedding_dims>`.

The embedding server must therefore accept, or ignore, `dimensions` and
`encoding_format`. **e5 caveat:** Mem0 cannot tell the server whether a text
is a query or a passage (its `memory_action` hint stays client-side). If the
e5 server adds `query: ` or `passage: ` prefixes, one fixed choice applies to
both search and storage. The RETRIEVAL agent's server should document what
it does.

**Calls per add.** One LLM call per `Memory.add`: a single "additive
extraction" call in `json_object` mode.
- The system prompt is fixed at 33,667 chars (about 8.4k tokens), which makes
  it a cacheable prefix.
- The user message carries the new messages, the last 10 messages of earlier
  adds (so roughly one previous session), up to 10 similar existing memories,
  an Observation Date and a Current Date.
- One add per session gives about 48 calls per LongMemEval S question.

**Timestamps.** Mem0 OSS does not take timestamps.
- `add(timestamp=...)` raises "Platform-only temporal parameter. Not
  supported in OSS". `search(reference_date=...)` raises the same.
- What we do: `metadata.created_at` is the session date, kept as the stored
  memory's `created_at`, and `session_date` and `session_id` go in metadata.
- By default (`--date-mode observation`), the extraction prompt's
  Observation Date and Current Date are set to the session date by replacing
  `mem0.configs.prompts._resolve_dates` during each add. OSS never passes a
  date, so it would otherwise use today's date (2026). This patches an
  internal function and changes no prompt text.
- `--date-mode instructions` is the public-API alternative: the date goes in
  `add(prompt=...)`, the Custom Instructions section, while Observation Date
  stays as today.
- **The architect should confirm the date mode.**

**Search and budget.** `search(query, top_k=20, filters={"user_id":...},
threshold=0.1)` returns ranked memories with a score. Mem0 has no token
budget parameter, so `mem0_cond.py` packs the context itself:
- it asks for `top_k=100` with `threshold=0`;
- it takes memories in rank order and skips any that would push
  ceil(chars/4) past 2,000 tokens;
- it prints the chosen lines in chronological order, as
  `[<session date>] <memory>`.

**Base install only.** Our install is `mem0ai` without extras: no spaCy and
no fastembed. As a result:
- BM25 hybrid scoring and entity boosts are disabled (Mem0 logs a warning),
  and search is semantic only;
- on the other hand, nothing is downloaded at run time (spaCy would fetch
  `en_core_web_sm`).

This matches `pip install mem0ai`. Installing `mem0ai[nlp]` and fastembed
would enable hybrid search, at the cost of more dependencies and run-time
model downloads. **The architect should decide whether to enable it.**

## 4. How to run

```
# once: pinned Python env (gitignored eval/lme/py/.local)
eval/lme/py/setup.sh

# proxy: ONE instance for the whole battery (the cap is enforced in process;
# a flock on eval/cost.json.lock stops a second instance)
go build -o bin/lmeproxy ./eval/cmd/lmeproxy
DEEPSEEK_API_KEY=... bin/lmeproxy -cap-usd 8 -offpeak-only \
    -log eval/llm_calls.jsonl -cost eval/cost.json      # listens on 127.0.0.1:8787
```

Tagging:
- Clients that can set headers send `X-Chanda-Eval-Tag`.
- Clients that cannot use the path prefix as their base URL. For Mem0 that
  is `http://127.0.0.1:8787/tag/C-ingest/v1`. For Chanda E (`llm.endpoint`)
  it is `http://127.0.0.1:8787/tag/E-sleep/v1`, with `model: deepseek-flash`
  and preferably `response_format: json_object`.

Other proxy behaviour:
- `/totals` returns the current totals.
- The cap check before forwarding is: spent + in-flight reservations +
  worst case > cap leads to a 402. The worst case is `max_tokens` x the
  output price plus (body bytes / 3) x the cache-miss price, both at peak
  prices.
- A request without `max_tokens` gets `-default-max-tokens` (4096).
- Streaming and `n > 1` are refused.

Pipeline (Python commands run from `eval/lme/py` with `.local/venv/bin/python`):

```
PY=.local/venv/bin/python
D=../../reports/$(date +%F)/raw
$PY mem0_cond.py --dataset $DS --sample ../../sample.json --limit 10 \
    --embed-url http://127.0.0.1:<port>/v1 --embed-model <id> --embed-dims 256 \
    --out work/ctx/C.jsonl
$PY answer.py --condition A --dataset $DS --sample ../../sample.json --limit 10 --out-dir $D
$PY answer.py --condition C --contexts work/ctx/C.jsonl --dataset $DS --sample ../../sample.json --limit 10 --out-dir $D
$PY judge.py  --condition C --raw-dir $D
$PY report.py --raw-dir $D --cost ../../cost.json --sample ../../sample.json --review ../../review.csv
$PY estimate.py measured --log ../../llm_calls.jsonl --smoke-n 10 --n 60
```

All scripts resume: question ids already in the output file are skipped.
The full run can therefore reuse the 10 smoke-run questions (the same
configuration at temperature 0, and "one execution per condition").
`answer.py` refuses contexts over 2,000 tokens or missing questions. A 402
or 403 from the proxy stops the run, and nothing is retried.

**Context JSONL shape** (used by mem0_cond.py; the Go generator should
write the same keys): `question_id`, `condition`, `context`,
`context_tokens`, `ingest_seconds`, `retrieval_latency_ms`. Extra keys are
kept under `context_meta` in the raw records.

**review.csv** is wide: one row per question in sample order, with
`question_id, question_type, abstention, question, gold_answer`, then
`answer_X, verdict_X` for each condition.

## 5. A-priori cost estimate

Run: `estimate.py prior --dataset longmemeval_s_cleaned.json`, for 60
questions and conditions A-E.

Measured inputs, from the S file (500 questions, sha256 d6f21ea9...):
- 47.7 sessions per question;
- 10,251 chars per session, about 2,563 tokens at chars/4 (the "~2.3k" in
  the brief is about right);
- 10.3 turns per session;
- Mem0's extraction system prompt: 8,413 tokens.

Assumptions: DeepSeek tokens equal chars/4; reader output 350 tokens (max
800); judge output 2 tokens; Mem0 output 400 tokens per call; 90% cache hits
on Mem0's system prompt.

| item | off-peak | peak |
|---|---|---|
| A-E reader (300 calls) | $0.14 | $0.28 |
| A-E judge (300 calls) | $0.02 | $0.05 |
| C Mem0 extraction (about 2,860 calls) | $3.47 | $6.95 |
| E Chanda sleep (input = min(1.15 x haystack, budget_tokens 200k), output 10%) | $1.77 | $3.55 |
| **total** | **$5.41** | **$10.82** |

Against the US$ 8 cap:
- **The full battery fits only if run off-peak.** The cap leaves $2.6 of
  headroom.
- **Condition C dominates**, at 64% of the total.
- Sensitivity: with no cache hits on Mem0's 8.4k system prompt, C rises to
  $6.66 and the total to **$8.60, over the cap**. With pessimistic outputs
  as well (Mem0 800, reader 800, E output 20%), the total is $9.89.
- The smoke run must therefore measure Mem0's real cache-hit share and
  output length before the full run.
- Fallbacks: without C, $1.90; without E, $3.60.
- If the smoke outputs are not reused, add one sixth of the total (about
  $0.90).
