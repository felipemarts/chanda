# 0153 Source labels

Before this record, a source had only its kind (`document`, `session`,
`tool`, `web`, `user_statement`) and its URI. The context block showed
the URI (`source: https://...` or `doc: /path/file.md`). An agent had no
way to give it a readable name, and an operator had no way to name a whole
family of sources.

A source can now carry a **label**: a short name shown next to it. The kind
list is unchanged and the label never touches the zone. The zone stays
bound to the channel (principle 5, `ZoneForSource` capped by
`mcp.max_zone`). A web page labelled "trusted" is still an external
quotation.

## Two ways to set a label

| Who | How | Precedence |
| --- | --- | --- |
| Operator | `sources.labels`: URI prefix → label. The longest matching prefix wins. | First |
| Agent | `source.label` on `remember`, `assert_fact` and `ingest`. The latest label of a source wins. | When no configured prefix matches |

| Key | Default | Meaning |
| --- | --- | --- |
| `sources.labels` | empty | Prefix → label. Validated: the prefix is non-empty, and the label is one line of at most 60 characters, without `[ ] \| < >`. |
| `sources.agent_labels` | on (unset) | `false` ignores `source.label` from non-human actors, with a notice. A human actor (CLI) can still set labels. |

Both keys are `omitempty`, and `agent_labels: true` is normalised away. The
default configuration hash stays `4dd590544d868b15`.

## Agent labels

- **Cleaned.** Control characters and `[ ] | < >` become spaces, runs of
  spaces collapse, and the label is cut at 60 characters
  (`config.CleanLabel`). These characters could otherwise break the label
  line of the context block. A label with nothing left after cleaning is
  ignored (`source_label_ignored`).
- **Screened.** The label goes through the admitter with the external
  zone. A label that holds a secret or reads as an instruction is refused
  (`source_label_refused`).
- **Stored after the write.** The write runs first. Then the label is
  stored in `source_labels` (migration 0153, one row per source) in its own
  transaction, audited as `source_label` with the source id as target.
  The audit entry never holds the label text. Repeating the same label
  writes nothing. A label that cannot be stored never fails the write; it
  only adds a notice.
- **Which source.** For `remember` and `assert_fact`, the source of the
  written event. For `ingest`, the folder or file source (`source.uri`, by
  default the path). `log_session` has no `source` field and takes no label.

## Where labels appear

- `Origin.source_label` in `context`, `search` and `get` items, for the
  first source URI of the origin.
- The context block label line: `[excerpt evt_… | zone: internal | origin:
  Manual interno | doc: …]`. The URI stays shown after it.

## Forget

Forgetting a source deletes its label row before the source row (foreign
key). The label is not scanned by the content probes, because it is not
text of the removed records.

Tests: `TestSourceLabels` (app), `TestSourceLabelsConfig` (config). The
determinism scenario labels its web write.
