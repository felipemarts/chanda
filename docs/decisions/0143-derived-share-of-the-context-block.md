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

## Implementation notes

Where the rule above leaves a choice, `selectItems` does this:

- **Drop reason.** A derived item that cannot be shortened gets
  `derived_share` only when the share or the protected room made the
  difference (the room under both limits is smaller than the plain
  budget left). When the plain budget alone would have dropped it, the
  reason stays `budget`, as without the share, so `budget_too_small` is
  still reported for a block left empty by the budget.
- **Item cap and the share.** With the share on, the per-item cap
  applies to any item that has at least `context_item_cap_after` raw
  excerpts admitted ahead of it. A derived item ranked ahead of them is
  not capped (the share bounds it) and does not use up an exemption. A
  derived item is first capped as before, then shortened from its whole
  text to the room left when it still does not fit.
- **Cost kept for a protected excerpt.** The pre-pass gives the k-th
  protected excerpt (k from 0) the cost of its whole block, or the cost
  after shortening to `context_item_max_tokens` when k ≥
  `context_item_cap_after`. That is the cost it has when admitted if the
  protected excerpts before it are admitted. An excerpt that the item
  cap would drop is not protected. The cost stops being kept when the
  walk reaches the excerpt, whether it is admitted or dropped.
- **Filters in the pre-pass.** The per-document and per-session counts
  of the pre-pass count only the protected excerpts before it. A derived
  item that takes a document's last slot ahead of a protected excerpt
  can still make it fail the per-document filter. In that case its room
  is freed when the walk reaches it.
- **Floor.** floor(share × budget) adds 1e-9 before the floor, so
  0.29 × 100 gives 29 and not 28.
- Each candidate's item (`buildItem`, which reads the database) is built
  once and shared by the pre-pass and the walk.
