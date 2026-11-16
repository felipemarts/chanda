#!/usr/bin/env python3
"""Pre-registered split of the learning study's test set
(eval/learn/PROTOCOL.md). Deterministic; run with python3 -I.

The test set is the 7 LoCoMo conversations not used by the sleep study
(conv-30, conv-47 and conv-49 are the development set). The rule is the
sleep study's (eval/sleep/split_locomo.py), with seed 45 and no draw of
conversations:

rng = random.Random(45)
for each test conversation in sorted sample_id order, for each category
1..4 in ascending order: the indexes of that category's QA items in file
order, rng.shuffle, the first n//2 to "use", the rest to "test".
Category 5 is excluded. A question whose normalised text repeats in its
conversation goes to the half of its first occurrence.

Only sample ids, categories and question texts (for the duplicate rule)
are read; the script prints counts, nothing else."""
import hashlib, json, random, re, sys

path = sys.argv[1] if len(sys.argv) > 1 else "eval/locomo/data/locomo10.json"
raw = open(path, "rb").read()
data = json.loads(raw)
DEV = {"conv-30", "conv-47", "conv-49"}
rng = random.Random(45)
test_convs = sorted(s["sample_id"] for s in data if s["sample_id"] not in DEV)
by_id = {s["sample_id"]: s for s in data}
out = {"dataset": "LoCoMo locomo10.json", "revision": "3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376",
       "sha256": hashlib.sha256(raw).hexdigest(), "seed": 45, "categories": [1, 2, 3, 4],
       "category_names": {"1": "multi-hop", "2": "temporal", "3": "open-domain", "4": "single-hop"},
       "development": sorted(DEV), "conversations": []}
norm = lambda q: re.sub(r"[^a-z0-9]", "", q.lower())
for sid in test_convs:
    qa = by_id[sid]["qa"]
    half = {}
    for cat in (1, 2, 3, 4):
        idx = [i for i, q in enumerate(qa) if q.get("category") == cat]
        rng.shuffle(idx)
        k = len(idx) // 2
        for i in idx[:k]:
            half[i] = "use"
        for i in idx[k:]:
            half[i] = "test"
    first, moved = {}, 0
    for i in sorted(half):
        n = norm(qa[i]["question"])
        if n in first and half[i] != half[first[n]]:
            half[i] = half[first[n]]
            moved += 1
        first.setdefault(n, i)
    qs = [{"index": i, "category": qa[i]["category"], "half": half[i]} for i in sorted(half)]
    cnt = lambda h, c=None: sum(1 for q in qs if q["half"] == h and (c is None or q["category"] == c))
    out["conversations"].append({"sample_id": sid, "questions": qs, "moved_duplicates": moved,
        "use": cnt("use"), "test": cnt("test"),
        "by_category": {str(c): {"use": cnt("use", c), "test": cnt("test", c)} for c in (1, 2, 3, 4)}})
json.dump(out, sys.stdout, indent=1)
print()
