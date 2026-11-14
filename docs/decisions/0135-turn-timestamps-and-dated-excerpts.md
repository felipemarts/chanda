# 0135 Turn timestamps, dated excerpts and as_of on raw events

Fixes `eval/deviations.md` 4 (`log_session` dropped the turn timestamp) and
5 (the context block showed no date for raw excerpts), and defines how
`as_of` applies to raw events.

## Write path: the date given in the call reaches `events.valid_from`

BASE.md: "`valid_from` uses, in this order: the date given in the call, the
document date, `recorded_at`." `recorded_at` stays the write time on every
path (bitemporal: when Chanda learned it).

| Path | `events.valid_from` |
| --- | --- |
| `log_session` | the turn's `ts` (RFC 3339 or `YYYY-MM-DD`, `core.ParseTime`) |
| `remember` | `valid_from` of the call (already honoured; now tested) |
| `ingest` | the document date (call `DocDate`, else front matter `date:`) on every **new** chunk |
| `assert_fact` | the fact's `valid_from` (unchanged) |

- An unparsable `ts` fails the whole `log_session` call with
  `invalid_argument` ("turn N: invalid ts ...") before anything is
  written. It is never dropped silently.
- A turn without `ts` has no `valid_from`; the read path falls back to
  `recorded_at`.
- Re-logging a turn already stored (same session, index and content) is
  still a duplicate and keeps the first event, whatever `ts` the new call
  gives: the raw log is immutable.
- A chunk kept from an earlier document version keeps the date of the
  version it first appeared in (immutable, and it did hold since then).

## Facts take their date from the event

- Extractors on the write path already passed the event's `valid_from` as
  the date given in the call, so a fact extracted from a dated turn takes
  the turn's date. Sleep phase 2 already used the origin event's
  `valid_from` (then document date, then `recorded_at`) when the LLM gives
  no date.
- `facts.Assert` now falls back to the origins' `events.valid_from` (the
  latest among the origins) before their document date and `recorded_at`,
  when the call gives neither a date nor a document date. Any caller that
  asserts with event origins and no date gets the event's date.
- `assert_fact` without `valid_from` but with a `session_id` whose turns
  carried `ts` takes the **latest** turn date of that session; otherwise,
  as before, the time of the call. `remember` does not infer a date (its
  `valid_from` lives in the raw log, which only records what the call
  said).

So sessions ingested out of date order reconcile by date: a later-dated
session supersedes an earlier one by `valid_from`, and an earlier-dated
session ingested last is inserted as history (decision table rows 2 and 3),
whatever the ingest order. Tested end to end in
`internal/app/dates_test.go`.

## as_of

- **A date-only `as_of` means the end of that day in UTC**
  (`YYYY-MM-DDT23:59:59.999999999Z`): "what held on 2023-05-20" includes
  everything dated that day. An RFC 3339 `as_of` is exact. This applies to
  `context`, `search` (`filters.as_of`) and `timeline`; the conversion is in
  `internal/app` (`asOfTime`), so the read package keeps taking a
  `time.Time`.
- **A date-only `valid_from` is 00:00 UTC of its day** (`core.ParseTime`
  stores it so), so it holds on its own day for any `as_of` on that day.
- **Raw events:** with an explicit `as_of`, an event whose date is after
  `as_of` is dropped (`after_as_of` in the retrieval log), in **both**
  `context` and `search`: it did not exist yet on that date. The event's
  date is its `valid_from` when set, else its `recorded_at`. Without
  `as_of` nothing changes (an event with a future `valid_from` is still
  delivered: the default "now" is not a time-travel request).
- Facts keep their rule (valid at `as_of`, decision 0021). Pages are not
  filtered by `as_of`: their only date is when sleep wrote them, which says
  nothing about the period they cover.
- The rule lives in `ruleFilter` (`internal/read/records.go`); ranking, the
  relevance cut and the floors are untouched.

## The context block shows dates

The label of each item now carries a date, in the same position as a
fact's validity:

    [excerpt evt_... | zone: internal | date: 2023-05-20 14:03 | source: s026]
    [excerpt evt_... | zone: user | recorded: 2026-01-01 09:30]
    [page pg_... | zone: internal | updated: 2026-02-03 04:05 | summary]
    [external quotation, event evt_... | zone: external | date: 2024-07-01 | source: ...]

- `date:` is the event's `valid_from` (when it happened, as the caller
  said); `recorded:` is shown only when there is none (when Chanda learned
  it), so a reader never mistakes an ingest time for an event date.
  `updated:` is a page's `updated_at`.
- Compact UTC form: the bare date at midnight (a date-only input), else
  date and minutes. About 6 tokens per item, counted in the budget.
- Fact labels are unchanged (`valid from <full timestamp>`), as are the
  delimiters, the not-instructions notice and external quotations.
- `search`/`context` items for events now carry `valid_from` (the
  `pkg/chanda.Item` field already documented "events with a declared
  date").

## LongMemEval smoke (hash, 20 sample questions, no LLM)

`lme retrieve -embedder hash -sample ../sample.json -limit 20 -conds chanda`;
a sanity check that dates flow, nothing tuned on it.

| | recall@5 | recall@10 | MRR |
| --- | --- | --- | --- |
| before (e6da1ae) | 91.2 | 95.6 | 0.858 |
| after | 82.4 | 83.8 | 0.770 |

With `-keep` on `gpt4_468eb063`, all 498 events have `valid_from`, and
for each of the 44 sessions it equals the dataset's session date.

The drop is entirely temporal-reasoning (91.7/100/0.792 to
66.7/66.7/0.542; other types unchanged) and comes from the new `as_of`
rule meeting the dataset: the harness passes the question's full timestamp
as `as_of`, and in LongMemEval_S **41 of 470** non-abstention questions
have an evidence session timestamped *after* the question (6 of 54 in the
60-question sample, 2 of 17 in this smoke: `gpt4_2312f94c`,
`gpt4_385a5000`), always on the same day: **0 of 470** are after the end
of the question's day. Those sessions are now (correctly, by the rule)
"not yet existing". Whether the harness should pass the question date at
day granularity (a date-only `as_of`, which loses no evidence by the
numbers above) is a harness decision, recorded in the report, not made
here.
