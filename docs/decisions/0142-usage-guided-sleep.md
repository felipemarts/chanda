# 0142 Usage-guided sleep (off by default; the old sleep is "uniform")

The battery's sleep (condition E) read every unprocessed event in event
order and extracted facts from all of them. About 95% of the events it read
came from sessions with no evidence for the question, and the facts it
placed in the block displaced evidence turns in 3 of the 5 questions it
lost (`eval/sleep/DIAGNOSIS.md`). This record adds a second sleep mode
that digests only what retrieval keeps returning, and that does most of
the work without an LLM. It also adds the block composition rule the
diagnosis made mandatory (decision 0143). The study that measures both is
`eval/sleep/` (bench on the LongMemEval tuning set, then a pre-registered
transfer test on LoCoMo).

**Nothing changes by default.** `sleep.mode` defaults to `uniform`, the
sleep as it was. Every new key is `omitempty` with a zero default that
means "the documented default". The default configuration hash stays
`4dd590544d868b15`.

## Keys

| Key | Default | Meaning |
| --- | --- | --- |
| `sleep.mode` | `uniform` (empty means uniform) | `uniform`: phase 2 as before. `guided`: phase 2 is replaced by the guided digest below. Phase 1 runs in both. |
| `sleep.guided.digest` | `extractive` | `extractive` (no LLM) or `llm`. |
| `sleep.guided.top_k` | 10 | How many of a retrieval's best-ranked event candidates count as "returned" by it. |
| `sleep.guided.weight_demand` | 1 | Weight of the demand signal. |
| `sleep.guided.weight_near_miss` | 2 | Weight of the near-miss signal. |
| `sleep.guided.weight_failure` | 2 | Weight of the failure signal. |
| `sleep.guided.weight_fragmentation` | 1 | Weight of the fragmentation signal. |
| `sleep.guided.demand_half_life_hours` | 168 | Half-life of a retrieval's contribution to demand. |
| `sleep.guided.group_max_events` | 12 | Members kept per group (highest priority first). |
| `sleep.guided.group_merge_jaccard` | 0.5 | Two groups whose member sets overlap at least this much (Jaccard) are merged. |
| `sleep.guided.max_groups` | 0 (no cap) | Groups digested per run, on top of the run budget. |
| `sleep.guided.line_tokens` | 60 | Extractive digest: length of the literal window kept per turn (estimateTokens). |

A zero or empty value means the default. `Validate` rejects an unknown mode
or digest, negative weights, `top_k < 0`, a Jaccard outside [0, 1], a
negative half-life, and `line_tokens` or `group_max_events` below zero.

## What the guided sleep reads

Only the learning layer and the group's own events:

- `retrievals.candidates` (order and type), `retrievals.delivered`,
  `retrievals.ts`;
- `outcomes.value` and `outcomes.used_ids`;
- for each member event: id, content, zone, role, session, ordinal,
  `valid_from` and `recorded_at`.

It never reads `retrievals.query_text` or `params`, nor any event outside
the group being digested. It never receives an answer key or a judge's
reasoning, only the outcome sign the caller recorded. A test plants a
marker in a query text and checks that no prompt and no ficha contains it.

## Priority (no LLM)

The "top k" of a retrieval is the first `top_k` candidates of type event in
the logged order: scored candidates by final score, as `logRetrieval`
writes them. It includes candidates the cut or the budget dropped. For
every live event e:

- **demand**: the sum, over retrievals whose top k holds e, of
  0.5^(age / half-life), where age = now − `retrievals.ts`;
- **near miss**: retrievals whose top k holds e and whose `delivered` does
  not;
- **failure**: retrievals whose top k holds e and that have an outcome
  with `value < 0`;
- **fragmentation**: the number of distinct sessions, other than e's own,
  among the events delivered by the retrievals whose top k holds e.

priority(e) = w_demand·demand + w_near_miss·near_miss + w_failure·failure
+ w_fragmentation·fragmentation. Events with priority 0 are never digested.
`utility` is not read: it is computed from the same retrievals and outcomes
(divergence from the task, which named it as an input; it adds nothing the
raw rows do not hold).

## Groups (no LLM)

Each retrieval's top k (live events only) is a candidate group. Candidate
groups are visited by descending priority (the sum of their members'
priorities; ties by smallest member id). A candidate whose Jaccard
overlap with an accepted group is at least `group_merge_jaccard` is merged
into it; otherwise it becomes a new group. A group keeps at most
`group_max_events` members, the highest priority ones (ties by id). Group
priority is the sum of its members' priorities.

The run digests groups in descending priority until the run budget
(tokens, or events read, `sleep.budget_*`) or `max_groups` is reached.
It skips a group whose members all already carry the `guided_digest` step
in `event_processing`. After a group is digested, its members get that
step.

## Digest: the ficha

A ficha is one page of kind `summary` (title `Digest: <up to 3 group
terms>`). Each line is one page item, written as staged and promoted
through the gate like lesson items (decision 0061). Each item has `cites`
provenance edges to its origin events. The item's zone is the lowest zone
among its origins, and the page's zone is the lowest among its items. The
page is enqueued for embedding like any page. Forgetting an origin event
removes the item, and the page when no item survives (the existing
cascade; a test covers fichas).

Line format, one per item:

    2023-05-22 | user | evt_01… | <text>

The date is the origin event's `valid_from`, else its `recorded_at`, as
YYYY-MM-DD. The author is the event's role (`user`, `assistant`, or `-`
when the event has none). With several origins, the date is the earliest,
the author is the common role (else `mixed`), and the ids are
comma-separated. Lines are sorted by date, then the origin's session and
ordinal, then id.

- **Extractive (no LLM).** One line per member event: the best literal
  window of `line_tokens` of its content, or the whole content if it is
  shorter. The window is scored by the group terms: terms that occur in at
  least two members, plus digits and capitalised words. Ties go to the
  earliest window. This is the same window algorithm as
  `read.context_truncate: best_window`, which moves to a shared package. A
  line whose normalised text equals, or is contained in, an earlier line
  of the ficha is dropped ("no repetition").
- **LLM.** The model sees only the group's turns (id, date, role, content)
  and returns JSON with a fixed schema: `{"lines": [{"kind":
  "line"|"count"|"timeline", "event_ids": [...], "text": "..."}]}`. Code
  sets the date and author from the cited events, never from the model.
  Every line passes the verifier before it is written.

## Verifier (no LLM)

A line written by the LLM is kept only if:

- it cites at least one event, and every cited id is a group member
  (otherwise the reason is `unknown_origin`);
- every number in its text (digit runs, with `.`/`,` inside) occurs in the
  cited events' content or date metadata (`unsupported_number`). For a
  `count` line, a number equal to the number of distinct cited events is
  also accepted (divergence: a count the model computes over the cited
  turns cannot appear in them literally);
- every proper name in its text occurs in the cited events' content,
  ignoring case (`unsupported_name`). A proper name here is a capitalised
  word that is not the first word of a sentence and is not on a short
  stop list (I, The, ...), or an all-caps acronym of two or more letters.
  Month and weekday names may also match the cited events' date metadata;
- its text is not empty (`empty`).

Discarded lines are counted by reason in the run report and never written.

## Report

The run report and `Counts` gain: candidate groups, groups digested and
skipped, lines written, lines discarded by reason, and fichas created
(page ids). Cost is reported as before.
