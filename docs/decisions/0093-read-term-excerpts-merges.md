# 0093 Read path: the learned term, outdated excerpts, merged entities

## Step 7 weights the promoted model, not stage-1 utility

Decision 0091 left a gap: the read path computed
`(1−λ)·relevance + λ·utility` while decision 0070 defines the term as the
promoted model's score. Now `read.Deps.Term` gives the term (wired to
`rank.Ranker.Terms`: the promoted model's prediction, whose features include
stage-1 utility) and

    final = relevance                       when λ = 0 (bit for bit)
    final = (1 − λ)·relevance + λ·term      otherwise

Without a `Term` λ is 0. Utility is still looked up and logged as a feature;
the term is logged in `features.term` when λ > 0. A runtime toggle
(`App.RankingToggle`, human only) sets λ to 0; a test promotes a trained
model, sees the scores change, turns learning off and gets the pre-learning
order and scores exactly, equal to an App wired with `DisableLearning` (C4).

## Raw excerpts whose fact was replaced

0091's known gap: a live chunk or statement whose extracted fact was later
superseded still delivered the old value in `context`. **Choice: drop, in
context mode only.** An event is dropped (`outdated_excerpt`) when **any**
fact it stated (`derived_from` or `confirmed_by`) is

- `rejected` or `flagged` (judged wrong), or
- `superseded` by a fact recorded after it,

unless an explicit `as_of` falls inside that fact's validity (it held then).
A value inserted as history (superseded by a fact recorded *before* it) is
not outdated: the event stated the past as past.

"Any" rather than "all": the facts that are still current are delivered as
facts themselves (facts come first in the block), so dropping the excerpt
loses only surrounding prose, while keeping it would put the old value in
front of the agent. Labelling it as historical was rejected: an agent reads
the value, not the label. `search` keeps every live event (it is the free
read, used to find where something was said).

## Accepted merges

Facts keep the subject id they were written with (decision 0062).
`facts.MergeSet` returns an entity and everything merged into it (recursive
over `merged_into`, bounded at 16 hops). Timeline, as_of, current reads and
the reconciliation of a new assertion (`chainFacts`) read the whole set, so a
new value for the surviving entity supersedes a value written on the merged
id. The read path reports a fact's subject as the surviving entity, and `get`
names it.

Known gap: when a merge is applied, two active values of a single-valued
predicate (one per old id) are not reconciled; both stay active until the
next assertion of that predicate or a sleep verdict.
