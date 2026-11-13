# 0005 Deterministic ids

ULIDs normally carry random entropy, which would break the byte-for-byte
determinism test. The time part comes from the `Clock`; the entropy comes from
a SHA-256 counter stream seeded by `id_seed`. Same clock, seed and call order
give the same ids. Ids stay unique because the counter never repeats within a
process and the time part advances.
