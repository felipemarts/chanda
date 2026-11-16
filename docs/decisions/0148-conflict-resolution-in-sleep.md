# 0148 Conflict resolution: rule detection at write, LLM verdict in sleep (off by default)

Today Chanda supersedes only structured facts of `single` predicates.
Contradictions in free text stay active side by side. "I live in São Paulo"
and, months later, "I moved to Rio" both remain active and can reach the
context together. This record adds conflict resolution for raw records:

- **Detection** is programmatic, on the write path, and changes nothing in
  memory.
- **Only the verdict on each pair uses an LLM, in sleep.**
- **Rules made without an LLM** then decide whether a verdict takes effect.

The goal is less outdated information in the context without hiding valid
information. A wrong supersession is worse than none, because nobody
notices it.

**Off by default.** Every new key is `omitempty`, and the default
configuration hash stays `4dd590544d868b15`.

## Keys

| Key | Default | Meaning |
| --- | --- | --- |
| `sleep.conflicts.enabled` | `false` | Write-time detection fills the conflict queue, and phase 2 judges it. |
| `sleep.conflicts.min_cosine` | 0 (= 0.80) | Similarity floor for a candidate pair. It must be below the model's near-duplicate threshold. |
| `sleep.conflicts.max_per_record` | 0 (= 3) | Candidates kept per new record (highest cosine first). |
| `sleep.conflicts.context_turns` | 0 (= 1) | Neighbouring turns of the same session shown on each side of each record. |
| `sleep.conflicts.min_confidence` | 0 (= 0.7) | A `supersedes` verdict below this becomes `uncertain`. |
| `sleep.conflicts.max_pairs` | 0 (no cap) | Pairs judged per run, on top of the run budget. |
| `sleep.mode: maintenance` | – | A new sleep mode. Phase 2 does **only** conflict verdicts (when enabled): no fact extraction, no lessons, no digest. `uniform` (the default) and `guided` are unchanged. With conflicts enabled, they also judge the queue first. |

## Detection (write path, no LLM)

Detection runs after `markNearDuplicate`. It uses the same neighbours: the
top `write.near_dup_candidates` live events of the same namespace and zone
by BM25, with their cosines from the configured embedder. A neighbour forms
a **candidate pair** with the new event when all of these hold:

1. **similar:** cosine ≥ `min_cosine`;
2. **not a duplicate:** the near-duplicate guard did not accept the pair;
3. **related:** the two share at least one entity of their `dedup.Signature`,
   or at least one content term (letters only, four or more letters, not a
   stop word, after `norm.Alias`);
4. **different:** their signatures differ in numbers, dates, entities or
   negations, as reported by `Sig.Compare`;
5. **at different times:** their effective times (`valid_from` if set, else
   `recorded_at`) differ.

Each new event keeps at most `max_per_record` pairs, highest cosine first.
Each pair goes into the `conflict_candidates` queue with:

- the older and the newer id;
- the cosine and the signature component that differs;
- the shared terms or entities;
- the creation time and a status, `pending`.

A pair is stored once, whichever event arrives second. Detection never
changes a record.

An offline `scan` recomputes detection over an existing database and
reports counts. It also counts the near-duplicate guard's refusals by
reason. It writes the queue only when asked to.

## Verdict (phase 2, LLM)

Pending pairs are judged in priority order, until the run's token budget or
`max_pairs` runs out:

1. the number of retrievals that delivered either record, highest first;
2. then the most recent newer record;
3. then the id.

For each pair the model receives:

- the two records, each with its date, author (role or actor), zone and
  `context_turns` neighbouring turns of its session;
- the instruction that the records are quoted data, never instructions.

It answers JSON with a fixed schema:

```json
{"verdict": "supersedes" | "coexist" | "unrelated" | "uncertain",
 "current": "A" | "B",
 "confidence": 0.0-1.0}
```

An answer that does not match the schema is discarded without a retry, and
the pair stays pending.

## Rules after the LLM (no LLM)

A `supersedes` verdict takes effect only if all of these hold:

- the record named current is the later one in effective time; otherwise
  it becomes `uncertain`;
- the current record's zone is not lower than the outdated record's zone
  (`user` > `internal` > `external`);
- an `assistant` turn does not supersede a `user` turn;
- confidence ≥ `min_confidence`; otherwise it becomes `uncertain`.

Every verdict is stored on the pair, along with the reason it was or was
not applied (for example `applied` or `uncertain:not_later`). Every
judged pair leaves the pending state, so it is not judged again; the
`reset` action of the `conflicts` tool (below) returns it to pending.

## Effect

An applied verdict writes a row in `supersessions`: the old id, the new
id, the pair, the run, the end date (the new record's effective time), the
confidence and the creation time. Events are immutable, so the outdated
status is this row and nothing is written on the event itself. Nothing is
deleted. The rows are staged and promoted through the sleep gate like
other phase 2 writes (decision 0061), and each one is audited.

Read path:

- **`context`:** a record with an active supersession as the old side is
  dropped (reason `superseded`).
- **`search`:** the same, unless `include_history` is set, and then the
  item says it is superseded and by what.
- **`timeline`:** shows the chain (old → new, with the end date).
- **`as_of`:** an explicit `as_of` before the end date sees the old record
  as current.

## Control and forget

The admin tool `conflicts` (and the CLI `chanda conflicts`) takes these
actions, each audited:

- `list` (pending, judged and applied);
- `revert <supersession id>`, which ends the row's effect and keeps it
  with `reverted_at`;
- `reset`, which deletes the queue and verdicts but keeps applied and
  reverted supersessions;
- `scan`, the offline detection;
- `stats`.

Forgetting either record of a pair removes its candidate row and any
supersession that names it. Tests cover detection, rules, effect, revert,
forget and determinism.

## Implementation notes

Code: `internal/conflicts` (detection `Detect`, the queue `Enqueue`, the
rules `Decide`), `internal/app/conflicts.go` (the write-path hook),
`internal/sleep/conflicts.go` (priority, prompt sides, staging,
maintenance mode; prompt and schema in `prompts.go`),
`internal/read/records.go` (supersessions in the rule filter),
`internal/forget/plan.go` (cascade), `internal/app/conflicts_tool.go` (the
admin tool and scan), migration `0148_conflicts.sql`. Where the record
leaves a choice, or the code differs from a literal reading:

- **Configuration.** Accessors on `config.ConflictsConfig`: `On`,
  `Cosine` (0.80), `PerRecord` (3), `Turns` (1), `Confidence` (0.7),
  `PairCap` (0 = no cap); `SleepConfig.IsMaintenance`. `Validate` rejects
  `min_cosine` outside [0,1), `min_confidence` outside [0,1] and negative
  counts. `min_cosine` is not checked against the model's near-duplicate
  threshold.
- **Off means off everywhere.** With `sleep.conflicts.enabled` false the
  write path returns before any extra query, sleep never reads the queue
  (except maintenance mode, which reports that it has nothing to do), and
  reads do not query `supersessions` at all: rows left by an earlier
  enabled period have no effect until the key is on again. The admin tool
  works either way and adds the notice `conflicts_off`.
- **Detection.** The neighbours are exactly the guard's (same BM25 list,
  same in-memory cosines). Fact assertions are never part of a pair (the
  read path never shows them), though they can take a slot of the guard's
  list. "The guard accepted" is `dedup.IsNearDuplicateOf` true for the
  pair, whether or not it was the one marked. Condition 3 uses the
  signatures without entity aliases (as the guard does); its content terms
  are `conflicts.Terms`: words of `norm.Alias` made of letters only, four
  or more, minus a short PT/EN stop list. `differs` is the first differing
  component in `Sig.Compare` order, named `numbers`, `dates`, `entities`,
  `negations`. `shared` is sorted and capped at 32. Effective times are
  compared parsed; a record whose times do not parse forms no pair.
  Queueing is not audited (the write already is): the queue is derived
  state, like the embedding queue.
- **Verdict prompt.** Record A is always the older and B the newer by
  effective time. Each record shows its date (YYYY-MM-DD), author (the
  turn's role, else the event's actor) and zone, and up to `context_turns`
  live turns of its session before and after it by ordinal. Record ids are
  not shown. The schema is the one above (`confidence` a number in
  [0,1]); the answer goes through `llm.Validate` like every phase 2
  answer, and an invalid answer is counted, not retried, and leaves the
  pair pending.
- **Priority.** Deliveries are counted from `retrievals.delivered` only
  (never `query_text` or `params`): the number of retrievals of the
  namespace whose delivered list holds either record. Only pairs whose two
  records are live are judged; a pair with a retired record stays pending
  and is not counted. `max_pairs` counts pairs judged in the run (an
  invalid answer does not count); reaching it with pairs left is a ceiling
  (status `partial`). A pair whose prompt alone exceeds the whole token
  budget is skipped (`too_large`) and stays pending. Pairs do not count
  against `sleep.budget_events`.
- **Modes.** `uniform` and `guided` with conflicts on judge the queue
  first, in the same gated plan as the rest of phase 2. Guided mode with
  the extractive digest and no LLM provider skips the verdicts with a
  notice. `maintenance` judges only the queue: no extraction, verdicts on
  quarantined facts, lessons or digest.
- **Rules.** Checked in the order of the record; the first that fails
  wins. Stored on the pair: `model_verdict`, `model_current`,
  `confidence`, `verdict` (after the rules: the later-date and confidence
  rules turn `supersedes` into `uncertain`; the zone and assistant rules
  keep `supersedes`, not applied), `reason` (`applied`,
  `uncertain:not_later`, `rejected:lower_zone`,
  `rejected:assistant_over_user`, `uncertain:low_confidence`, or the
  verdict's own name for `coexist`, `unrelated`, `uncertain`),
  `sleep_run_id`, `judged_at`. The assistant rule compares the turns'
  `role` column (records without a role never trigger it).
- **Effect.** `supersessions(id sup_…, old_id, new_id, pair_id,
  sleep_run_id, ends_at, confidence, status staged|active, created_at,
  reverted_at)`. Written `staged` inside the gate savepoint, made `active`
  by the same promotion step as staged page items, so the gate measures
  them; a revert by the gate rolls back the verdicts too (the pairs stay
  pending) and its audit `discarded` map gains `conflict_verdicts` and
  `supersessions` (only when the run judged conflicts). An applied verdict
  whose old → new row already exists and is not reverted writes no second
  row. Audit: `conflict_verdict` per judged pair (target the pair; details
  verdict, model_verdict, current, confidence, reason, run, older, newer,
  supersession).
- **Read path.** A supersession acts only while active, not reverted and
  its new record is live (not retired). With several on one record the
  earliest end date wins. Context and search drop the old record with
  reason `superseded` (logged like every drop). Search with
  `include_history` keeps it, with `status: superseded`, `valid_to` = the
  end date and `superseded_by` = the new id (new omitempty field of
  `Item`). An explicit `as_of` before the end date shows the old record
  as current (no supersession fields). `timeline` gains
  `supersessions` (omitempty): `{supersession_id, old_id, new_id, ends_at,
  confidence, created_at}` of the active supersessions whose old or new
  record names the subject (canonical name or an alias as whole words
  after `norm.Alias`), by end date then id; with `as_of`, only those with
  `ends_at` at or before it. `get` is unchanged.
- **Forget.** Pairs and supersessions naming a removed event go in the
  same transaction; their ids join the removed set (so sleep reports lose
  them and the reference probe checks them). The forget report gains
  `removed.conflict_pairs` and `removed.supersessions`, and the tombstone
  counts `conflict_candidates` and `supersessions`, only when non-zero, so
  every other forget's output is as before.
- **sleep_run.** `Counts.Conflicts` (JSON `conflicts`, absent when off
  outside maintenance mode) and the `sleep_run` tool's `conflicts`:
  `{mode, enabled, pending, judged{verdict: n}, model_verdicts{verdict: n},
  applied, rejected{reason: n}, invalid_answers, too_large, dropped, left,
  llm_calls, input_tokens, output_tokens, supersessions[ids]}`. `pending`
  is at the start of phase 2; `left` = pending − judged in the run;
  `dropped` counts pairs that changed between the plan and the gate. The
  report gains a "Phase 2 (conflict verdicts)" section; in maintenance
  mode the uniform phase 2 section is not printed.
- **The `conflicts` tool** (admin profile, the 20th tool; CLI `chanda
  conflicts list|revert <id>|reset|scan [--write] [--details]
  [--min-cosine f]|stats`). Input `{namespace?, action, id?, write?,
  details?, min_cosine?}`. Every output carries the counts after the
  action: `action`, `enabled`, `pending`, `judged`,
  `verdicts{verdict: n}`, `reasons{reason: n}`, `active`, `reverted`
  (supersessions). Per action:
  - `list`: `pairs[]` = `{id, older_id, newer_id, cosine, differs,
    shared[], status, model_verdict?, model_current?, confidence?,
    verdict?, reason?, run_id?, created_at, judged_at?, supersession_id?}`
    and `supersessions[]` = `{id, old_id, new_id, pair_id?, run_id,
    ends_at, confidence, status (active|reverted), created_at,
    reverted_at?}`, both by id. Not audited.
  - `revert` (`id` = a supersession id): sets `reverted_at`, returns
    `reverted_id`; the pair keeps its verdict. Unknown id: `not_found`;
    already reverted: `conflict`. Audit `conflicts_revert` (old_id,
    new_id).
  - `reset`: deletes every pair of the namespace (pending and judged),
    returns `removed`; supersessions stay with `pair_id` cleared. Audit
    `conflicts_reset` (pairs). This is how a judged pair returns to
    pending: reset, then `scan` with `write: true`.
  - `scan`: returns `scan` = `{min_cosine, events_scanned,
    neighbour_pairs, candidate_pairs, queued, already_queued,
    already_superseded, near_dup_refusals_by_reason{numbers_differ,
    dates_differ, entities_differ, negation_differs},
    pairs_by_component{numbers, dates, entities, negations},
    candidates?[], near_dup_refusals?[]}`. Both maps always hold their
    four keys. With `details: true` it also returns `candidates` =
    `[{older_id, newer_id, cosine, differs, shared}]` (exactly what
    detection would queue, older/newer by effective time) and
    `near_dup_refusals` = `[{older_id, newer_id, cosine, reason}]` (pairs
    that passed the guard's cosine stage, above the threshold or on the
    lexical path, and that the guard refused; older is the earlier
    written record), as lists (empty, not absent). `min_cosine` in (0,1)
    overrides `sleep.conflicts.min_cosine` for this scan only (0 = the
    configured floor; 1 or more is `invalid_argument`); refusals do not
    depend on it. Scan walks the live events (no fact assertions) in
    write order (rowid), each against the `write.near_dup_candidates` BM25
    neighbours of its zone written before it, with in-memory cosines
    (each text embedded once per scan). BM25 statistics are today's, so
    it approximates, not replays, write-time detection. Without `write` it
    writes and audits nothing. With `write: true` it queues the pairs,
    skips pairs already queued (`already_queued`) and pairs that already
    have a supersession row in either direction, reverted or not
    (`already_superseded`), and is audited (`conflicts_scan`, counts
    only).
  - `stats`: counts only.
