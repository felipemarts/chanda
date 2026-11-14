# 0130 Entity scope: the events that name the entity

Fixes eval/deviations.md entry 1 (phase 5 scenario F3). Decision 0050 says
the entity scope "resolves to the events that mention the entity", but it
read that set only from `mentions` provenance edges, and nothing outside
tests writes them. `forget {kind: entity}` therefore removed the entity's
facts and their assertion events, and left every free-text statement that
names the entity in `events`, FTS, the FTS5 vocabulary, `embeddings` and the
file. Forget's own verification passed, because it only probed the records
it had removed.

## Decision: match the text at forget time

The entity scope finds mentions by matching the entity's names against the
text itself, in every event of the namespace, whenever the entity was
created. Explicit `mentions` edges are still honoured when present.

**Mentions edges are not written at write time.** Forget cannot depend on
them: an entity is often created after the text that names it (an
`assert_fact` or an extraction a week later), so an edge written at write
time would be missing for exactly the older text a person wants gone. Edges
written later (by a backfill on entity creation) would be a second copy of
the rule below that can drift from it. One rule, run at forget time over
the FTS index, is simpler and cannot be stale. `internal/write` is
unchanged.

## The mention rule (`internal/forget/entity.go`)

1. **Entities.** The reference (entity id, canonical name or alias) is
   resolved, followed up its `merged_into` chain to the entity that
   absorbed it, and then down to every entity merged into that one,
   directly or through a chain (16 hops at most, as `internal/facts`). An
   accepted merge says they are the same real-world entity, and reads
   already resolve names that way.
2. **Names.** The canonical name and every alias of every entity in the set.
3. **Folding.** A name is folded as FTS5 `unicode61 remove_diacritics 2`
   folds text: NFD, combining marks dropped, lowercase, and every rune that
   is not a letter or a number is a separator.
4. **Distinctive names only.** A name is used when it has two or more tokens
   with at least 6 letters and digits in all, or one token of at least 8
   runes. Shorter names ("Owner", "Ana", "Brasil", "TI", "Ana Li") match
   nothing and are reported as skipped. The entity's facts and explicit
   edges still apply to it.
5. **Match.** A used name matches as an FTS5 phrase: the same tokens,
   consecutive, in the same order, on token boundaries. Case and diacritics
   do not matter, and neither does the punctuation or whitespace between
   tokens.

Boundaries, each covered by `TestEntityScopeRemovesEventsThatNameTheEntity`:

| Text | Matches "Ysolde Karvenni" |
| --- | --- |
| `Ysolde Karvenni prefere ...` | yes |
| `YSOLDE KARVENNI aprovou ...` (case) | yes |
| `Ysoldé Kárvenni ligou ...` (diacritics) | yes |
| `ysolde-karvenni@exemplo.com` (punctuation between tokens) | yes |
| `Ysolde chegou ...`, `A Karvenni Logística ...` (partial name) | no |
| `Ysolde Karvenniana ...` (name inside another word) | no |
| `Karvenni, Ysolde: ...` (another order) | no |
| `Ysolde e Karvenni ...` (not consecutive) | no |
| the same name in another namespace | no |

## What the scope removes

- Events that name the entity, or that carry a `mentions` edge to it.
- Facts whose subject is in the entity set, that carry a `mentions` edge,
  or whose text names the entity. The events they came from are removed
  too (0050: "the origin events of facts whose subject is the entity or that
  mention it").
- Page items whose text names the entity, and pages whose title names it.
  The events a page item cites are not removed for that reason alone.
- Everything derived from the removed events, in cascade as for any other
  scope (0050 rules for facts with another live origin and for pages that
  keep items).

**Heading paths (0110).** The rule reads `events.content` only, through
`events_fts`. A chunk whose `heading_path` names the entity but whose own
text does not is not a mention and survives, as 0110 decided. A sibling
chunk whose own text names the entity is now removed.

## Dry run and report

`forget.Report` gains `EntityTerms` (every name, with `used` or the skip
reason) and `Mentions` (every event, fact, page and page item matched, with
the folded term that matched). A dry run therefore shows a person every
record a name would sweep before anything is deleted. Both hold entity
names, so they are returned to the caller and never stored. The tombstone
scope keeps the entity ids and the counts `terms_used` and `terms_skipped`.

## Verification: the scope's own claim

The entity scope adds the probe `entity_mentions`. After deletion it runs
the same rule again over what remains in the namespace (events, facts, page
titles and page items). Any hit is a failure that names the record id and
the term's position, never the name. A regression of the F3 kind (a mention
the resolver did not find) is now reported as a failed probe and a
`core.CodeIntegrity` error, not as `passed: true`
(`TestEntityProbeReportsASurvivingMention`). Other scopes do not run it.

## Known limits

- A name of one long common word (for example "Marketing" as a department)
  matches every event that uses the word. The dry run lists each match with
  its term. Read it before running.
- Tokens separated by sentence punctuation (`... Ysolde. Karvenni disse`)
  still form the phrase, as they do for FTS5.
- References without the name (pronouns, a short first name, a nickname
  that is not an alias) are not mentions. Register the nickname as an alias
  first if it must go.
- The two thresholds (6 and 8) are constants in `internal/forget`, not
  `config.Forget` fields. `internal/config` was being edited by another
  area when this landed. Moving them is a mechanical follow-up.
- The MCP and CLI outputs (`chanda.ForgetOutput`) do not yet carry
  `entity_terms` and `mentions`. The Go report does. Exposing them needs
  `internal/app` and `pkg/chanda`.

## Effect on the evaluation

Scenario F3 passes unchanged. The core deletion suite
(`internal/eval/coresys`) runs its entity requests first and then picks
session turns and documents for its other requests. Those turns and
documents can now name the entity, so they were already removed, and the
later request fails with `not_found`. The suite's target selection must
account for the larger entity scope.
