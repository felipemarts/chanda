#!/usr/bin/env python3
"""Question sets of the conflict-resolution study (eval/conflicts/PROTOCOL.md).

development = every knowledge-update question of eval/split-tune.json
              + 30 other-type questions of the same split,
test        = every knowledge-update question of eval/split-holdout.json
              + 60 other-type questions of the same split.
The other-type questions are drawn with random.Random(46).sample over the
sorted question ids (one generator per set, seeded 46). The test questions
were already answered in the selection study (eval/selection/); no setting
of this study is chosen on them."""
import json, random, sys

def pick(split, n_other):
    qs = json.load(open(split))["questions"]
    ku = sorted(q["question_id"] for q in qs if q["question_type"] == "knowledge-update")
    other = sorted(q["question_id"] for q in qs if q["question_type"] != "knowledge-update")
    types = {q["question_id"]: q["question_type"] for q in qs}
    drawn = sorted(random.Random(46).sample(other, n_other))
    return {"knowledge_update": ku, "other": drawn, "types": {i: types[i] for i in ku + drawn}}

out = {"seed": 46, "development": pick("eval/split-tune.json", 30), "test": pick("eval/split-holdout.json", 60)}
json.dump(out, sys.stdout, indent=1)
print()
