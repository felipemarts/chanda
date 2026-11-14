# 0123 Training split with paraphrased and unanswerable questions

Decision 0121 calibrated the evidence floors on the training split of
synthetic-pt and found the reason they failed on paraphrases: its 99
training questions were all answerable and plainly worded, so no
calibration on it could place a floor between "paraphrase of the evidence"
and "unrelated but same names". This record extends the training split; the
evaluated and gate questions do not change.

## What the generator adds

`genHardTraining` (`internal/eval/synth_train.go`) runs after every other
generator and appends 50 training questions, t100..t149:

| kind | n | built from |
| --- | --- | --- |
| paraphrase: who is responsible for a task | 21 | three responsibilities per department, new wordings (`trainRespQ`) |
| paraphrase: which branch serves a sector | 3 | `trainSpecialtyQ`, branches drawn from the new stream |
| paraphrase: a product described by its features | 5 | `trainProductQ`, products drawn from the new stream |
| paraphrase: a procedure rule | 4 | `trainRuleQ`, rules drawn from the new stream |
| unanswerable: person who does not exist (real first or last name) | 4 | names drawn from the new stream, checked absent from the corpus |
| unanswerable: attribute never stated about a real person | 3 | RG, birthplace, salary |
| unanswerable: product, branch city, topic the corpus lacks | 10 | fixed list (Plano Bronze, CRM integration, Natal, Vitória, …) |

The training split is now 149 questions: 22.1% paraphrase and 11.4%
unanswerable, against 21.6% and 11.5% in the evaluated split (305). The
paraphrases share even less vocabulary with their gold spans than the
evaluated ones (share of the question's content terms found in the gold
span: 0.004 for the new training paraphrases, 0.031 for the evaluated
paraphrases, 0.301 for the other training questions;
`TestTrainingSplitResemblesEvaluated`). No wording repeats an evaluated or
gate question (the generator panics on a repeat; the test checks every
question text is distinct). The paraphrased targets are of the same kinds
as the evaluated paraphrase tag, and for responsibilities and branches the
same spans (every responsibility and branch already has an evaluated
paraphrase); that is the point of the extension (training questions that
resemble the evaluated ones) and it is how the existing training split was
built ("other wordings of the same tasks", t001..t099). Unanswerable
training questions follow the evaluated near-miss patterns with other
names, attributes, products and cities.

Validation no longer requires gold on training questions: an unanswerable
training question carries the abstention tag like any other. C5's
simulated task sequence (decision 0092) runs over the whole training split,
so it now also sees paraphrased questions and unanswerable ones (outcome
−1: no delivered record is used).

## The evaluated and gate questions are unchanged

The new questions are drawn from their own stream,
`hashRand{seed + "/hard-training"}`, and appended after every other
question, so the main stream, the corpus and every earlier question are
produced exactly as before. SHA-256 of the canonical JSONL lines, `main`
(81d5ff1, before) and this branch (after):

| part | lines | before | after |
| --- | --- | --- | --- |
| corpus.jsonl | 197 | `5da06006…bdc8ea70b` | `5da06006…bdc8ea70b` |
| evaluated questions | 305 | `57270c20…77706c3bfa` | `57270c20…77706c3bfa` |
| gate questions | 33 | `b6a213e1…ccbe67e2e` | `b6a213e1…ccbe67e2e` |
| evaluated + gate, in file order | 338 | `089674ce…a6a56483` | `089674ce…a6a56483` |
| training t001..t099 | 99 | `66e3ba1a…436d5671` | `66e3ba1a…436d5671` |
| training (all) | 99 → 149 | `66e3ba1a…436d5671` | `13c23a75…e1483f95` |

Full digests: corpus
`5da060066917c6adb4399e9512292d070c101b52d6e0f92434cc5505fe9ea70b`;
evaluated + gate
`089674cefa340b0a4e14b28261ce023fd53e5f0738abfe33e2c87108a6a56483`;
evaluated `57270c200b7b093940e4099bc09c35616ba8993f1a3a9c7bd3ae9577706c3bfa`;
gate `b6a213e143eab810d24092730203407148a3d0cee91a529f4135683ccbe67e2e`.
The first 437 lines of `questions.jsonl` are byte-identical to the
committed file before this change; the 50 new lines follow them.
`TestHardTrainingLeavesEvaluatedAndGateUnchanged` recomputes the corpus,
evaluated + gate and t001..t099 digests from the generator on every test
run. The dataset hash in the reports (`Dataset.Hash`, over corpus and all
questions) changes because the training split is part of it.

The corpus did not change: the new questions only use what it already
holds (and absences from it).
