# 0137 Context selection knobs (defaults unchanged pending the tuning study)

The LongMemEval battery of 2026-11-14 (`eval/reports/2026-11-14/REPORT.md`
section 4, `error-analysis-D.md`) found that search ranks the evidence
turns as well as plain RAG, but the 2,000-token context block loses them:
items are whole turns with no per-item cap, so a block holds about seven
long items (about 280 tokens each, mostly assistant replies) and the short
user turn with the fact is cut; and the label does not say who spoke (the
reader once took an assistant estimate for a user statement).

This record adds the knobs the pre-registered selection study
(`eval/selection/PROTOCOL.md`) tunes. **Every default keeps the context
block byte for byte as it was.** The values will be chosen on the tuning
set and fixed by a later decision with the tuning numbers. Search mode is
unchanged by all of them. None changes ranking, scores, the relevance cut,
the evidence floors or utility: items are still taken in final-score order,
and the knobs only decide how much of each item is shown and which items
are skipped.

## Keys (`read.*`, context mode only)

| Key | Default | Meaning |
| --- | --- | --- |
| `context_item_max_tokens` | `0` (off) | An item whose rendered block (label included, `estimateTokens` = ceil(runes/4)) costs more than this is shortened to fit it. Must be 0 or at least `context_min_item_tokens`. |
| `context_truncate` | `head` (empty means head) | How an item is shortened, for the per-item cap and for the last item that only partly fits the budget: `head` or `best_window`. |
| `context_max_per_document` | `0` (= `read.max_per_document`) | Per-document item limit in context mode; `-1` lifts the limit. |
| `context_max_per_session` | `0` (off) | At most this many raw excerpts of one session (`events.session_id`) per block. |
| `context_show_role` | `false` | The label of a session turn shows its speaker: `[excerpt evt_… \| role: user \| zone: internal \| date: … \| source: s001]`. |

The configuration hash is unchanged for a file that does not set these
keys (`omitempty`; an explicit `head` hashes as the default), so committed
evaluation reports keep their hash (`TestContextKnobsDefaultsAndHash`).
`Validate` rejects an unknown `context_truncate`, a negative cap, a cap
below `context_min_item_tokens`, `context_max_per_document` below -1 and a
negative `context_max_per_session`.

### `head`

The existing `truncateToFit`: keep the start of the text, cut on a rune,
append "…". Unchanged.

### `best_window`

Keep the contiguous window of the text that best matches the query:

- The text is split into sentences (a cut after whitespace that follows
  `.`, `!`, `?` or `…`, optionally with closing quotes or brackets, or
  after whitespace containing a line break). A sentence that cannot fit on
  its own is split into words; a word that cannot fit on its own into
  fixed-size rune pieces (an eighth of the room, at least one rune). The
  pieces join back into the text.
- Score of a window: the number of distinct query terms it contains, then
  the number of term occurrences. The terms are the read path's own
  (`queryTerms`, the tokens and stopwords used for FTS and lexical
  coverage); no new tokenizer.
- From each start the longest window that fits is taken (two pointers);
  the best score wins, ties to the earliest window.
- "…" marks each end where text was cut (none at the start when the window
  starts the text, none at the end when it ends it), and the item is
  marked `truncated` in the label and in the item.
- The cost is `estimateTokens` of the rendered block (label, sanitising,
  "> " quotation prefixes included), so the result never exceeds the
  target. Below `context_min_item_tokens` it refuses, like `head`.
- Shortening always starts from the whole text: an item cut by the cap and
  then too long for the remaining budget is cut again from the original,
  not from the first cut.

## Per-document and per-session limits

`read.max_per_document` counts items per *document*: the diversity key is
the smallest document id among the item's origin events (`resolveOrigin`).
**`log_session` turns have no document** (`events.doc_id` is NULL; their
source is the session, `sources.kind = session`, `uri` = the session id),
so their key is empty and neither `max_per_document` nor
`context_max_per_document` ever limited them. In the battery a block could
therefore hold any number of turns of one session. The per-session limit
is a separate key, `context_max_per_session`, keyed by `events.session_id`
of raw excerpts (facts and pages are not counted). A turn dropped by it is
logged as `max_per_session`.

## The role was not stored

`log_session` wrote user, assistant and system turns from one session
source as events of kind `turn`; the role was used only to choose the
source and the event kind and was then lost (tool turns were
distinguishable by kind `tool_result` and zone). Migration 0137 adds a
nullable `events.role`, written by `log_session` with the turn's role
(`user`, `assistant`, `system`, `tool`) and included in the
append-only trigger. Other events and turns written before the migration
have no role, and their label is unchanged. The role is not part of the
public `Item` (search and context JSON are unchanged); it is used only by
the label. The audit entry of a write is unchanged.

## Tests

`internal/read/selection_test.go`: a golden of the context block of a
conversation fixture at budgets 800 and 300, captured before the knobs
existed (`TestContextDefaultsUnchangedGolden`); the cap with both modes;
best window for the last item; the window that holds the query's fact,
ties to the earliest, accented and unspaced text, never above the target
(internal, external and page items, targets 1 to 200); sentence and word
splits that join back; the per-document override in context only; the
per-session limit; the role label; search unaffected; determinism.
`internal/config`: defaults, hash, validation. `internal/write`: the role
is stored and immutable. `eval/scenarios` results are unchanged.
