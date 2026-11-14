# 0062 Sleep: rules BASE.md leaves open

- **Burst.** A run (also on demand) is refused with `conflict` when the last
  write is newer than `sleep.burst_seconds` (default 10). Automatic runs also
  need `idle_minutes` of idleness. A namespace that never ran meets the
  interval condition once it has new events.
- **Snapshot timing.** Taken right before the first phase-2 change, and only
  when phase 2 has something to apply (no snapshot for phase-1-only runs).
  The recorded path is relative to the database file.
- **Merge proposals.** Similarity = max(1 − edit distance / longer length,
  token Jaccard) over normalized canonical names and aliases, on pairs that
  share a token or a 3-letter prefix; plus cosine of entity vectors when an
  `EntityVectors` is wired (up to `sleep.merge_max_entities`). Different
  non-empty types are never proposed; a pair with any earlier proposal is
  never proposed again.
- **stale_source.** Active or quarantined facts with event origins
  (`derived_from`, `confirmed_by`, `cites`) none of which is live.
- **Page rebuild runs in phase 1.** Rebuilding `needs_rebuild` pages from the
  remaining items needs no LLM, so it also happens without a provider (it
  removes forgotten content from page bodies sooner). Items whose cited
  events are all gone are retired. The zone never rises on rebuild.
- **Verdicts by rule.** `supersede` on a quarantined fact is refused while an
  active fact of a higher zone exists (zone table: a lower zone never
  supersedes a higher one; only `promote` can). `coexist` on a single
  predicate is refused. Refused verdicts leave the fact quarantined. A merge
  `supersede` merges the newer entity id into the older (`merged_into`,
  aliases moved); facts keep their subject id, so readers must resolve
  `entities.merged_into`. `coexist`/`reject` on a merge mark it rejected.
- **Lessons.** Similar = Jaccard of key terms (normalized words, ≥ 3
  letters, no stopwords) ≥ `sleep.lesson_similarity` (0.5); groups are
  connected components with ≥ `lesson_min_events` events, capped at
  `batch_events`. `instruction_like` events never feed lessons. The model
  does not choose citations: every event it was shown is cited (`cites`) by
  the item and the page, and the zone is their `MinZone`, so a model cannot
  launder an external origin by citing only internal events. It picks the
  target page from a schema enum of existing lesson pages, or `new`.
- **Extraction.** A candidate citing an event outside its batch is dropped.
  If an event of a batch was deleted (forget) between the LLM call and the
  apply, the whole batch is dropped. An event too large for the whole token
  budget is marked processed with no facts so it cannot block every run.
- **Budget.** Token estimate = (chars of system + prompt) / 4 + `llm.max_tokens`,
  checked before each call; actual usage is what counts. Events budget counts
  events sent to the LLM (extraction and lesson groups). On a ceiling or a
  provider error the run stops LLM work, applies what it has through the gate
  and ends `partial`. Invalid answers are discarded and the run continues.
- **Report** (`sleep_runs.report`) lists ids, counts and cost, never record
  content.
- **Audit actions** added by sleep, as strings in `internal/sleep` until
  integration moves them to `internal/audit`: `merge_propose`,
  `merge_verdict`, `fact_verdict`, `events_processed`.
