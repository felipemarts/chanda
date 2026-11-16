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
