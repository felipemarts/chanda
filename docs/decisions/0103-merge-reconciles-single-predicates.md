# 0103 An accepted merge reconciles single predicates

Closes the known gap of decision 0093: after a merge, two active values of a
single predicate (one per old entity id) both stayed active.

When sleep accepts a merge proposal (phase-2 verdict `supersede`), in the
same staging transaction and right after `entity_merge`, it takes the
active facts with `is_single = 1` of every entity in the surviving entity's
merge set (recursive over `merged_into`, 16 hops) and, per predicate with
two or more of them, applies the decision table:

1. **Lower zone → quarantined.** A fact whose zone is below the highest zone
   among them goes to `quarantined` (audit `quarantine`, details: the
   remaining active fact, `reason: entity_merge`, the two entity ids, the run).
2. **Later `valid_from` wins**, ties by `recorded_at`, then by id. The others
   form a chain in that order: each is closed by the next one
   (`status = superseded`, `valid_to` = the next one's `valid_from`,
   `superseded_by`, a `supersedes` provenance edge), so `as_of` never sees
   two values at once. Audit `supersede` per closed fact, same details.

Facts keep their subject id (decision 0062). The ids go to the run's
`facts_superseded` and the new `facts_quarantined` counts and report lines,
so a gate revert discards them together with the merge. Multi predicates
coexist; superseded history of the merged entities is not re-threaded.
