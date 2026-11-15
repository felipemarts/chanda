#!/usr/bin/env python3
"""Split the 440 LongMemEval S questions outside eval/sample.json into a
tuning set and a held-out set, before any of them is looked at.

Deterministic, standard library only. Reads only question ids, types and the
abstention marker (never answers, sessions or results).

1. Pool: every question of the dataset file not in eval/sample.json.
2. Strata: (question_type, abstention), abstention = "_abs" in the id.
3. Tuning quota per stratum: TUNE * size / pool by largest remainder
   (floor of each exact share, then one seat at a time to the largest
   remainder; ties to the stratum that sorts first).
4. Draw: random.Random(SEED); strata visited in sorted order; each stratum's
   ids sorted, shuffled, and the first k go to tuning, the rest to held-out.
5. Order inside each file: shuffled once more with the same source.

Output files have the shape of eval/sample.json (questions: question_id,
question_type, abstention), so `lme contexts -sample` and the Python scripts
read them unchanged.

    python3 eval/lme/split.py            # writes eval/split-tune.json, eval/split-holdout.json
"""
import hashlib
import json
import os
import random
import sys

SEED = 43
TUNE = 140
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
DATA = os.path.join(HERE, "data", "longmemeval_s_cleaned.json")
SHA256 = "d6f21ea9d60a0d56f34a05b609c79c88a451d2ae03597821ea3d5a9678c3a442"


def main() -> int:
    h = hashlib.sha256()
    with open(DATA, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    if h.hexdigest() != SHA256:
        sys.exit(f"{DATA}: sha256 {h.hexdigest()}, want {SHA256}")
    with open(DATA) as f:
        ds = json.load(f)
    meta = [(q["question_id"], q["question_type"]) for q in ds]
    del ds
    with open(os.path.join(ROOT, "eval", "sample.json")) as f:
        sample = json.load(f)
    taken = {q["question_id"] for q in sample["questions"]}
    pool = [(qid, t, "_abs" in qid) for qid, t in meta if qid not in taken]
    if len(pool) != len(meta) - len(taken):
        sys.exit("sample ids missing from the dataset")

    strata: dict[tuple[str, bool], list[str]] = {}
    for qid, t, ab in pool:
        strata.setdefault((t, ab), []).append(qid)
    keys = sorted(strata)
    exact = {k: TUNE * len(strata[k]) / len(pool) for k in keys}
    quota = {k: int(exact[k]) for k in keys}
    for k in sorted(keys, key=lambda k: (-(exact[k] - quota[k]), k))[: TUNE - sum(quota.values())]:
        quota[k] += 1

    rng = random.Random(SEED)
    tune, hold = [], []
    for k in keys:
        ids = sorted(strata[k])
        rng.shuffle(ids)
        for i, qid in enumerate(ids):
            (tune if i < quota[k] else hold).append({"question_id": qid, "question_type": k[0], "abstention": k[1]})
    rng.shuffle(tune)
    rng.shuffle(hold)

    common = {
        "dataset": sample["dataset"], "file": sample["file"], "revision": sample["revision"], "sha256": SHA256,
        "seed": SEED, "excludes": "eval/sample.json (the 60 questions of the battery of 2026-11-14)",
        "algorithm": __doc__.strip(), "generator": "eval/lme/split.py",
    }
    for name, qs, role in (("split-tune.json", tune, "tuning: may be looked at and used to choose settings"),
                           ("split-holdout.json", hold, "held out: run once, after every setting is frozen")):
        by_type: dict[str, int] = {}
        for q in qs:
            by_type[q["question_type"]] = by_type.get(q["question_type"], 0) + 1
        out = dict(common, role=role, n=len(qs), abstention=sum(q["abstention"] for q in qs),
                   by_type=dict(sorted(by_type.items())), questions=qs)
        with open(os.path.join(ROOT, "eval", name), "w") as f:
            json.dump(out, f, indent=1)
            f.write("\n")
        print(name, len(qs), "abstention", out["abstention"], out["by_type"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
