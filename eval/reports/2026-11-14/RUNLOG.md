# LongMemEval battery: run log (2026-11-14)

What ran, in order, what failed and why, and every choice made along the way.
Times are UTC. Defects and their fixes are also in `eval/deviations.md`;
this log adds the operational detail (attempts, machine limits, costs).

## Machine

MacBook Air, darwin/arm64, 8 GB RAM, 8 CPUs, about 20 GB free disk. One
e5-small process at a time (about 1.2 to 1.6 GB). Go 1.27 with
`GOEXPERIMENT=simd` for everything that loads hugot.

## Phase 0: build and tests (pass)

`eval/reports/2026-11-14/phase0.md`. Code frozen as tag `eval-lme-freeze`
(7e57d12); phase 0 commit 281de41. Every later change to Chanda is a
deviation entry.

## Phase 1 with the hash embedder, 500 questions (done)

05:44 to 06:08, 23m47s, 4 workers. `phase1-hash.md` / `.json` (commit
2915345). Recall@10 over 470 scored questions: BM25 95.6, plain RAG 82.7,
Chanda 93.4. With hash the plain-RAG baseline is weak (hash vectors carry no
meaning), so this run is not the comparison that counts.

## Phase 1 with e5: three failures before it ran clean

1. **06:08, killed after 28 questions.** `lme embedserve` (Chanda's hugot
   embedder behind an HTTP endpoint) received SIGKILL. The system log kept
   no reason. A first check (RSS over 240 distinct lengths) showed no growth
   and was wrong: macOS compresses memory, so RSS hid it.
2. **06:49 to 11:45, supervised retries.** A runner restarted embedserve and
   the run after each kill (the embedding cache is client-side, so progress
   carried over). The 60-question sample finished (attempt 2, 29m36s); the
   500-question run was killed five more times, about every 40 minutes.
   `footprint` then showed **11 GB** for embedserve.
   - Cause: GoMLX's Go backend keeps every call's buffers in pools that never
     shrink, about 0.45 MB per call (heap profile:
     `gobackend.(*Backend).GetBuffer`).
   - Fix: decision 0136, reload the hugot session every 1,000 calls
     (3591184). Vectors are bit-identical across reloads (tested).
3. **12:01, the first fix had a defect.** The reload created the session with
   the context of the call that triggered it; hugot derives the session's
   context from it, so after the first reload every call failed with
   "context canceled". It stopped phase 1 again and the smoke run's
   condition C on its 10th question.
   - Fix: `context.WithoutCancel` (6e27225), with a regression test that
     fails on 3591184 and passes on the fix.
   - No result was produced on the broken build.

e5 on the 60-question sample (54 scored), recall@10 / MRR: BM25 96.2 /
0.910, plain RAG 99.1 / 0.939, Chanda 98.0 / 0.941. The differences are
within noise at this n. The e5 run on all 500 questions was restarted on
the final build (6e27225 + 8b631cf).

## Embedder of the battery: e5-small

The same model for B, C, D and E: `e5-small@hugot0.8.6+simd`. With hash,
plain RAG and Mem0 would be handicapped by an embedder with no semantics, so
hash would flatter Chanda. Decided after phase 1 on the sample, before any
reader or judge call of the full run.

## Phase 3: smoke run, first 10 questions of the sample

- Proxy: `bin/lmeproxy -cap-usd 8 -offpeak-only`, key from `.env`.
  Saturday is off-peak all day.
- Model: `deepseek-flash` (DeepSeek-V4.1-Flash-1017). It is the cheapest
  model the API lists; the only other one is `deepseek-v4-pro`. Checked
  against `GET /models` with the battery's key.
- Python env: `eval/lme/py/setup.sh`. uv 0.13.6, CPython 3.12.16,
  mem0ai 2.2.1, hash-pinned.
- Mem0 runs on the base install (`pip install mem0ai`): no spaCy, no
  fastembed, so Mem0's own BM25 hybrid scoring and entity boosts are off.
  This was decided before any LLM call (PROVIDER.md, section 3). It is a
  limitation of condition C, to be stated in the report.
- First pass, 12:03 to 12:21:
  - A, B and D ran.
  - C stopped on question 10 (defect 3 above).
  - E stopped on question 1. `sleep_run` returned "write burst in
    progress". This was a harness defect: it called sleep right after
    writing, and Chanda (correctly) refuses during a write burst. The
    harness now waits 11 s (8b631cf, deviation 9).
- Partial accuracy, n = 10 (no conclusion at this size): A 2/10, B 10/10,
  D 8/10.
- Cost after the first pass: US$0.199. Projection for 60 questions,
  conditions A to D: US$1.19 off-peak, of which Mem0 extraction is
  US$1.13. E still to be measured.
- Second pass (C question 10, E, then reader and judge for C and E): running.

## LLM call store

The proxy logs each call's tokens and the request hash, not its bodies.
**The bodies of the smoke-run calls up to the second pass are not
recoverable.** Their results are kept, though:
- contexts: `eval/lme/py/work/ctx/*.jsonl`;
- answers and verdicts: `eval/reports/2026-11-14/raw/`.

A record/replay store in lmeproxy is being added, so later runs reuse
identical calls at no cost. At temperature 0 an identical forwarded request
is answered from the store.

## Phase 4: full run, 60 questions (done)

- The proxy was restarted with `-store eval/lme/llmstore`, recording only, no
  replay. The 50 questions after the smoke run's 10 then ran:
  - Mem0 in three shards, 13:01 to 15:40;
  - B and D contexts;
  - readers and judges for A to D;
  - E last (15:41 to 16:35), after a check that the remaining budget covered
    it twice over.
- The first launch of the runner failed at once on a script error (`lme
  contexts` refuses `-sample` together with `-ids`). It was relaunched
  without restarting the Mem0 shards, which were already running.
- Seven DeepSeek calls ended with "connection reset by peer". The clients
  retried them, and every Mem0 question reports 0 extraction errors.
- Total spend: US$4.278, in 4,951 calls. Results are in `REPORT.md`.

## Phase 1 with e5, 500 questions (done)

12:25 to 11:52 the next day (UTC). There was a single attempt on the final
build, with no kill and no error; the embedding cache carried over the
earlier attempts. Recall@10 / MRR: BM25 95.6 / 0.928, plain RAG 97.9 /
0.946, Chanda 98.3 / 0.946. Results are in `phase1-e5.md`, and REPORT.md
was updated.

## Reusable dataset for later tests (local, git-ignored)

| What | Where | Size | What it saves |
| --- | --- | --- | --- |
| LLM calls of the full run (request and response) | `eval/lme/llmstore/` | 4080 entries, 61 MB | paid calls: `lmeproxy -store eval/lme/llmstore -replay` answers identical requests at no cost (Mem0 extraction, condition A, the judge for identical answers, Chanda sleep for an identical database) |
| e5 embeddings: every turn, chunk and question of LongMemEval S | `eval/lme/work/embcache.sqlite` | 2.1 GB | about 8 h of e5 on this machine; keyed by (model_id, role, sha256(text)) |
| Contexts per condition and question | `eval/lme/py/work/ctx/*.jsonl` (also inside `raw/`) | 4 MB | re-running the readers or the judge needs no retrieval |
| Answers and verdicts | `eval/reports/2026-11-14/raw/` (committed) | 5 MB | comparison baseline for later runs |

Not stored: the request and response bodies of the smoke run's first pass,
from before the store existed (their results are in `raw/`).
