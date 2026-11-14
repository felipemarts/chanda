# 0041 Near-duplicate signature

BASE.md: near-duplicates need cosine above the threshold and identical
numbers, dates, entities and negation markers. `internal/dedup` defines them:

- **Numbers and dates** keep their order of appearance (normalized as in
  `internal/facts/norm`); "from 10 to 12" differs from "from 12 to 10".
- **Entities** are a sorted set: capitalized words that do not start a
  sentence, all-caps words of two or more letters, and known aliases passed
  by the caller. Sentence-initial words are ignored unless they are known
  aliases, so "The price" and "the price" match. Possessive "'s" is dropped.
- **Negations** are a sorted multiset of not, no, never, cannot, -n't, não,
  nunca, nem, sem, jamais. Portuguese "no" (em + o) also counts, which can
  only cause a missed duplicate, never a wrong one.
- "Exceeds the threshold" is strict: cosine equal to the threshold is not a
  duplicate.

The bias is deliberate: discarding an update as a copy (target 0%) is worse
than leaving a real duplicate uncollapsed (target up to 5%).
