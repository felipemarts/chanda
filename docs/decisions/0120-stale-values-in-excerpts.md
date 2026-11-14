# 0120 Stale values in raw excerpts (temporal suite)

The temporal suite (BASE.md "Up to 1%" of answers with an already
superseded value) measured 35% for C3 (`eval/reports/latest.md` at
3c0b2fe: 39 of 111 questions). Decision 0093 drops an excerpt whose
*extracted* fact was replaced; this record closes the cases it missed.

## What carried the stale values

The temporal suite now logs, for a C system, every delivered record whose
text holds a `must_not_contain` value: question, record id and type, event
kind, source kind, session, document and the facts the record states with
their status (`coresys.TemporalSuite`, stderr through `Logf`; ids, never
content). On C3 every offending record was a raw excerpt (no fact was ever
wrong). Two groups (counts from a diagnostic C3 run that flagged 40):

1. **Explicit `as_of`, excerpt of a fact that began later** (6 questions).
   "What was Zeca's extension in April?": the chunk of the June version
   states `Ramal: 5014`, the origin of an *active* fact. 0093 only looked
   at superseded facts, so an active one never made its excerpt outdated,
   even when `as_of` fell before its validity. Rule 1 alone: 40 → 33.
2. **Prose that states a value without being its fact's origin** (the
   other 33).
   Later announcements ("A partir de julho, o Plano Empresarial passa a sair
   por R$ 263,40"), the `Regras` paragraphs under a document's header (the
   header extractor reads the key/value lines; the paragraph that repeats
   the value has no edge), FAQs, and session turns whose statement the agent
   asserted with `assert_fact` (the fact derives from the `fact_assertion`
   event, not from the turn). Session statements did close the earlier
   values (the facts were superseded as they should); only the turn's prose
   still carried the old one.

## Rules (read path, context mode only)

1. **Derived facts at an explicit `as_of`** (`derivedFact.outdatedAt`): an
   event is outdated when a fact it states (`derived_from` or
   `confirmed_by`), active or superseded, is not valid at the explicit
   `as_of`. Without `as_of` the 0093 rule is unchanged (a history insert is
   kept).
2. **Stale values** (`read/stale.go`, drop reason `stale_value`). A *chain*
   is the active and superseded single-valued facts of one (subject,
   predicate), the subject resolved through accepted merges, when at least
   one is superseded. A value of the chain *holds* at `as_of` when a fact
   with that value is valid then (without `as_of`: is active). A raw
   excerpt is dropped when, for a chain it is about, it contains a value of
   the chain that does not hold. The excerpt is about a chain when:
   - it names the subject: an alias of the entity (`entity_aliases`, the
     names the facts were written with) occurs as whole words in
     `norm.Alias(heading_path + text)`; or
   - it is a turn of the session in which a fact of the chain was asserted
     (`fact_assertion` events of the same `session_id`); or
   - it names the attribute: the predicate's label ("diária", "centro de
     custo") occurs as whole words, and then only values that hold for
     **no** chain of that predicate count, so a price that is current for
     one product never removes an excerpt through another product's
     history.

   Values are compared on `norm.Value` of the excerpt (numbers and dates
   canonical: "R$ 263,40" is `r$ 263.4`), as whole words (`120` is not in
   `1200` nor in `120.5`). Values and names shorter than 3 characters are
   ignored. The value that holds is delivered as a fact, which comes first
   in the block, so the excerpt is only surrounding prose. Labelling it as
   historical was rejected for the reason given in 0093. `search` keeps
   every live event.

Everything is deterministic: the chain query is ordered, aliases and values
are sorted, and the check is string matching on the fact normalizers.

## Measurement (hash embedder, synthetic-pt)

| C3 | before | after |
| --- | --- | --- |
| temporal `stale_answer_rate` | 0.3514 (39/111) | **0.009 (1/111), pass** |
| forbidden in context (all) | 0.3559 | 0.0339 |
| answer in context | 0.637 | 0.637 |
| recall@10 / MRR / nDCG@10 | 0.6074 / 0.5081 / 0.5329 | unchanged |
| context tokens per query (mean) | 95.35 | 91.02 |

C4 and C5 move the same way. C1 (no facts, so no chains) is unchanged at
0.53: the rule needs structured facts by construction.

The remaining question (q221) is an FAQ paragraph that names neither the
subject ("Abertura de chamados de TI") nor the attribute ("atendimento") of
the old value ("no máximo 4 horas úteis"). Matching the value alone would
remove excerpts that mention a common value for unrelated reasons, so it
stays.

## Cost and limits

The chain query reads the superseded single-valued facts through
`sleep_facts_ns_status` and their siblings through `facts_sp`, then the
aliases of those subjects; it runs once per context query, on the excerpts
that passed the cut. On the 100k measurement corpus (no supersessions) it is
one empty index range. Its cost grows with the number of superseded facts
of the namespace; a per-namespace cache is the next step if that shows in
the latency measurement.

Known false positives: an excerpt that names a subject and mentions one of
its old values for another reason ("the price used to be R$ 120") is
dropped, as 0093 already does for derived excerpts.
