# LongMemEval S (cleaned): the dataset of the battery

## Verdict

The dataset and its licence are what the task describes: LongMemEval,
variant S, "cleaned" release, on Hugging Face (`xiaowu0162/longmemeval-cleaned`),
MIT licence on both the dataset card and the official repository. Fields,
question types and the abstention rule match the official README.

Four facts about the data that the README does not say, or says otherwise,
and that matter for the battery:

1. **Haystacks are not always sorted by date.** The README says the session
   lists of `longmemeval_s` are "sorted by timestamp"; 211 of the 500
   haystacks are not. The harness ingests sessions into Chanda in date order
   and renders plain-RAG context sorted by date.
2. **Some sessions are dated after the question.** 76 questions have
   1,475 haystack sessions dated after `question_date`, 75 of them evidence
   sessions, always on the same day (at most 0.99 days later): the dataset
   gives the question a time of day earlier than some of its own sessions.
   A system that filters events strictly by `as_of` would lose those
   evidence sessions. Chanda today does not filter events by date (only
   facts), so this has no effect on the current runs.
3. **Abstention questions have evidence ids.** All 30 abstention questions
   carry `answer_session_ids` (sessions about a related but different fact).
   The official retrieval evaluation skips them anyway (below).
4. **51 questions have no official retrieval target.** All are
   `single-session-assistant`: the evidence is in an assistant turn, and the
   official code only counts sessions with a user turn marked `has_answer`.
   The official retrieval averages skip them; the battery's own recall uses
   `answer_session_ids` and keeps them (both are reported).

The filler sessions come from ShareGPT and UltraChat (official README,
"Creating Custom Chat Histories"); the MIT licence is the dataset authors'
release licence. The text is English and conversational.

## Pinned file

| | |
| --- | --- |
| Repository | https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned |
| Revision (commit) | `98d7416c24c778c2fee6e6f3006e7a073259d48f` (last modified 2025-09-19) |
| File | `longmemeval_s_cleaned.json` |
| URL | https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/98d7416c24c778c2fee6e6f3006e7a073259d48f/longmemeval_s_cleaned.json |
| Size | 277,383,467 bytes (264.5 MiB) |
| SHA-256 | `d6f21ea9d60a0d56f34a05b609c79c88a451d2ae03597821ea3d5a9678c3a442` (equals the Git LFS oid reported by the Hub API) |
| Downloaded | 2026-11-14, into `eval/lme/data/` (git-ignored) |

`lme fetch` downloads it to `eval/lme/data/` and checks size and SHA-256;
every other command refuses a file with another digest. The other files of
the revision (`longmemeval_oracle.json`, `longmemeval_m_cleaned.json`) were
not downloaded.

## Licence

- Dataset card (`README.md` at the revision above): `license: mit`,
  `language: en`. The card says the cleaned release "removes noisy history
  sessions that interfere with the answer correctness" and replaces the
  original dataset.
- Official repository `github.com/xiaowu0162/LongMemEval`, commit
  `9e0b455f4ef0e2ab8f2e582289761153549043fc` (2026-05-11): `LICENSE` is the
  MIT licence, "Copyright (c) 2024 Di Wu". Paper: Wu et al., "LongMemEval:
  Benchmarking Chat Assistants on Long-Term Interactive Memory", ICLR 2025,
  arXiv:2410.10813.

## Fields (official README, verified on the file)

| Field | Content |
| --- | --- |
| `question_id` | unique id; ends with `_abs` for abstention questions |
| `question_type` | `single-session-user`, `single-session-assistant`, `single-session-preference`, `temporal-reasoning`, `knowledge-update`, `multi-session` |
| `question` | the question |
| `answer` | gold answer; a string, a number in some questions (the loader keeps it raw) |
| `question_date` | e.g. `2023/05/30 (Tue) 23:40`; no time zone (the harness reads it as UTC) |
| `haystack_session_ids` | session ids; evidence sessions are named `answer_...` |
| `haystack_dates` | one date per session, same format |
| `haystack_sessions` | sessions as lists of `{"role", "content"}` turns (only `user` and `assistant` occur); evidence turns have `"has_answer": true` |
| `answer_session_ids` | evidence sessions, for session-level recall |

Every question validates: as many ids, dates and sessions; every date
parses; every `answer_session_ids` entry is in its haystack; in all 500
questions `answer_session_ids` equals the set of haystack ids containing
"answer"; no `has_answer` turn sits in a session without "answer" in its id.

## Question types

| Type | Questions | of which abstention |
| --- | --- | --- |
| single-session-user | 70 | 6 |
| single-session-assistant | 56 | 0 |
| single-session-preference | 30 | 0 |
| temporal-reasoning | 133 | 6 |
| knowledge-update | 78 | 6 |
| multi-session | 133 | 12 |
| **total** | **500** | **30** |

## Abstention

- **Identification** (official README, "Dataset Format"): "If `question_id`
  ends with `_abs`, then the question is an `abstention` question." The
  official scripts test the substring `'_abs' in question_id`; on this file
  both rules select the same 30 questions.
- **Retrieval evaluation** (README, "Memory Retrieval"): "for evaluating the
  retrieval, we always skip the 30 abstention instances ... these instances
  generally refer to non-existing events and do not have a ground truth
  answer location." `src/retrieval/run_retrieval.py` skips them, and also
  skips questions without a user turn marked `has_answer`
  ("ignored due to no target turns from the user side");
  `src/evaluation/print_retrieval_metrics.py` filters `'_abs'` the same way.
  The battery does the same: abstention questions are run (their latency
  and size count) but never enter a recall, MRR or nDCG average.
- **QA evaluation** (`src/evaluation/evaluate_qa.py`): abstention questions
  get a different judge prompt (`get_anscheck_prompt(..., abstention=True)`),
  which credits a response that says the question cannot be answered.

## Official retrieval metrics

Read in `github.com/xiaowu0162/LongMemEval` at commit
`9e0b455f4ef0e2ab8f2e582289761153549043fc`:

- `src/retrieval/eval_utils.py`, `evaluate_retrieval(rankings, correct_docs, corpus_ids, k)`:
  - `recall_any@k` = 1 if any correct document is among the first k, else 0;
  - `recall_all@k` = 1 if every correct document is among the first k;
  - `ndcg_any@k` = DCG of the binary relevances of the first k over the ideal
    DCG (all correct documents first), with
    `dcg(r) = r[0] + sum_{i>=1} r[i] / log2(i+1)` (0-based i).
  - `evaluate_retrieval_turn2session`: with turn granularity, turn ids are
    mapped to their session and k is widened until k distinct sessions are
    covered.
- `src/retrieval/run_retrieval.py`: k in {1, 3, 5, 10, 30, 50}; at session
  granularity the corpus document of a session is the **user turns only**,
  joined by spaces; the correct documents are the haystack sessions whose id
  contains "answer" **and** that have a user turn with `has_answer` (other
  "answer" sessions are renamed "noans"); the query is the question text;
  averages skip abstention and no-target questions.
- `src/evaluation/print_retrieval_metrics.py` prints `recall_all@5`,
  `ndcg_any@5`, `recall_all@10`, `ndcg_any@10` at session and turn level.

What the battery reports (eval/lme/lme/metrics.go):

- its own recall@5, recall@10 (share of `answer_session_ids` among the first
  k distinct sessions ranked) and MRR (1/rank of the first evidence session),
  over every non-abstention question;
- the official `recall_any@k`, `recall_all@k`, `ndcg_any@k` (k = 5, 10) with
  the official targets and the official skips.

One deviation from the official setup, on purpose: every condition indexes
both user and assistant turns, because that is what a memory system
receives (Chanda's `log_session` gets the whole session). The official
corpus is user turns only, so the numbers are not directly comparable to
the paper's tables.

## Size (measured with `lme stats`)

| Per question | min | mean | median | p95 | max |
| --- | --- | --- | --- | --- | --- |
| sessions | 38 | 47.7 | 48 | 53 | 62 |
| turns | 396 | 493.5 | 491 | 545 | 616 |
| characters (turn contents) | 454,980 | 489,298 | 489,763 | 503,397 | 513,953 |
| evidence sessions | 1 | 1.9 | 2 | 4 | 6 |

Across the 500 questions (haystacks overlap):

| | total | unique | unique characters | unique est. tokens (ceil(runes/4)) |
| --- | --- | --- | --- | --- |
| sessions (by id) | 23,867 | 19,195 ids | | |
| sessions (by text) | 23,867 | 18,464 | 189,984,885 | 47.5 M |
| turn texts (what Chanda embeds) | 246,750 | 189,515 | 187,122,004 | 46.9 M |
| plain-RAG chunks (1,000 runes) | | 247,893 | 188,548,669 | 47.2 M |
| questions | 500 | 500 | 42,503 | 10.8 k |

Roles: 122,416 user turns, 124,334 assistant turns. The overlap is modest:
the embedding cache saves about 23% of the turn embeddings over the 500
questions and almost nothing inside the 60-question sample (29,395 turns,
28,424 unique).

### Embedding cost projection (e5-small)

From decision 0036 (darwin/arm64, measured on a busy machine, so an upper
bound; idle is 25 to 40% lower): simd 16 ms for ~20 tokens and 63 ms for a
~200-token paragraph, stock 41 ms and 350 ms, linear in tokens; texts
clipped at e5's 512-token window. Applied to each unique text
(`E5Seconds` in stats.go):

| | unique texts | e5 simd | e5 stock |
| --- | --- | --- | --- |
| Chanda turns, 500 questions | 189,515 | 12,769 s (3.5 h) | 71,822 s (20 h) |
| RAG chunks, 500 questions | 247,893 | 14,999 s (4.2 h) | 82,744 s (23 h) |
| questions (×2 roles possible) | 500 | 8 s | 22 s |
| Chanda turns, 60-question sample | 28,424 | 1,926 s (32 min) | 10,844 s (3.0 h) |
| RAG chunks, 60-question sample | 37,480 | 2,271 s (38 min) | 12,530 s (3.5 h) |

hugot serialises calls (one text per call, under a mutex), so one e5
process is a single queue: these are wall-clock times. Only the simd build
(`GOEXPERIMENT=simd`) is practical.
