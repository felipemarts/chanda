# 0040 Fact reconciliation details

BASE.md "Decision table for facts" leaves some cases open. `internal/facts` decides:

- **Equal `valid_from`, different value, `single`.** Treated as a correction made
  later: the active fact is superseded with `valid_to = valid_from` (an empty
  period), so `as_of` never returns it.
- **History insert trims an overlap.** When a history fact lands inside the
  period of an older closed fact, that fact's `valid_to` becomes the new
  fact's `valid_from` and its `superseded_by` the new fact (audited as
  `fact_adjust`). Without this, `as_of` could return two values of a `single`
  predicate. If the covering fact already has the same value, the new one
  only confirms it.
- **Quarantine applies to `single` predicates only**, and whatever the date:
  a lower-zone value of a `multi` predicate supersedes nothing, so it
  coexists, marked with its zone (BASE.md "Trust zones": external facts can
  be active). A repeat of a quarantined value confirms the quarantined fact.
- **Confirmation from a lower zone** adds the `confirmed_by` edge but does not
  raise `confidence`. The fact's zone does not change: `confirmed_by` is
  supporting evidence; `derived_from` edges define the zone.
- **Same origin again** (re-running an extractor or sleep) adds nothing and
  writes no audit row (`reason = already_recorded`).
- **`recorded_at` fallback for `valid_from`** is the latest `recorded_at` of
  the origin events, not the time of the assert, so sleep extraction days
  later still dates the fact by when it was said. The document date is the
  one passed in, else the latest `documents.doc_date` of the origins.
- **Retired origins are refused**, so a fact is never born from a retired chunk.
  `stale_source` counts both `derived_from` and `confirmed_by` edges as live
  origins; a stale active fact does not reopen its predecessor.
- **Value normalization** keeps accents (only alias and predicate names strip
  them). Numbers: a single separator followed by exactly three digits is a
  thousands separator ("1.000" = "1,000" = 1000), otherwise a decimal point;
  with both separators the last is the decimal point. Dates become ISO only
  when unambiguous (D/M vs M/D needs a component above 12). See
  `internal/facts/norm`.
- **Unknown predicates** enter as `multi` unless listed in
  `facts.single_predicates`; the list seeds the catalog on the first touch of
  a namespace. A catalog row is never changed by a later config change.
- `supersedes` edges point new fact → old fact. Graph walks that collect
  derived records from a source (forget) must follow only `derived_from`,
  `confirmed_by` and `cites`, or forgetting an old fact would take its
  successor with it.
