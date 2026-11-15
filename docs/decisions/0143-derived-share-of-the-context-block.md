# 0143 Derived items take at most a share of the context block

In the battery, sleep facts were placed ahead of raw excerpts and took the
budget that the evidence turns needed: in 08f4fc43 they displaced the
block's two best-ranked raw items, both evidence (`eval/sleep/DIAGNOSIS.md`
item 1). This record caps the block tokens that derived items (facts and
pages) may take. It also makes sure the best-ranked raw excerpts always
keep their place.

**Off by default**: with `context_derived_max_share: 0` the block is byte
for byte as before and the configuration hash does not change.

## Keys (`read.*`, context mode only)

| Key | Default | Meaning |
| --- | --- | --- |
| `context_derived_max_share` | `0` (off) | Derived items (types `fact` and `page`) may take at most floor(share × budget) rendered tokens of the block, all together. Range [0, 1]. |
| `context_protect_raw` | `0` (= 2 when the share is on) | The N best-ranked raw excerpts that pass the other filters are never displaced by a derived item; `-1` protects none. Used only when the share is on. |

## Rule

With the share on, `selectItems` first finds the protected excerpts: the
first N event candidates, in final order, that pass the instruction, per-
document and per-session filters, with the cost they would have when
admitted. The walk then goes in final order as before:

- a raw excerpt is admitted as before;
- a derived item is admitted only if, after it, (a) the derived tokens stay
  within the share and (b) the protected excerpts not yet admitted still
  fit in the remaining budget. If it does not fit, it is shortened (by
  `context_truncate`) to the room left under both limits, when at least
  `context_min_item_tokens` remain; otherwise it is dropped with reason
  `derived_share`;
- `context_item_cap_after` counts raw excerpts only, so a derived item
  ranked first does not take a raw excerpt's exemption from the item cap.

The order of the rendered block (facts, pages, then excerpts) does not
change.

"Items from sleep" in the task are implemented as all facts and pages. In
this system only sleep writes pages. Facts can also come from
`assert_fact`, and those are capped too. In the evaluation data every fact
and page comes from sleep.

Not covered: derived candidates can still take candidate slots before the
cut (the vector top k, the rerank window), as in 1d4e3b97. That is left
for later.
