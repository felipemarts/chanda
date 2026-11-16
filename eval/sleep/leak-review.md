# Leak review of the LoCoMo sleep databases

`lme study leakcheck` runs on every condition's database after its sleep
and before its test half. It checks four things:

- every outcome carries only a value of ±1 and used ids;
- no retrieval holds a test question;
- no judge word appears;
- every gold answer string found in a derived text (fact, page, page item)
  also occurs literally in that record's origin turns.

The first three never failed. The fourth flagged gold strings in S2's
fichas and S3's facts that the origin turns do not contain literally:

| Conversation | S2 hits | S3 hits |
| --- | --- | --- |
| conv-30 | 12 | 6 |
| conv-47 | 15 | 6 |
| conv-49 | 46 | 22 |

Each flagged record was read by hand with its origin turns, using
`study.json` and the database. Every one is a rewrite of its origin turn,
not information from outside it:

- **First person turned into third person.** "to clear my mind" became
  "to clear his mind", and "the perfect spot for your store" became "the
  perfect spot for her store". LoCoMo writes its gold answers in the third
  person, so a faithful rewrite reproduces them.
- **Abbreviations expanded.** "my biz" became "his business".
- **Values the fact extractor normalised** (S3). "We just did a
  contemporary piece called 'Finding Freedom.'" became `dance_piece:
  contemporary piece called "Finding Freedom"`.
- **Words from another cited turn of the same record.** For example, a
  "painting" fact whose first origin is "I want to give it a go" and whose
  other origins name the painting.

The sleep's inputs are what the database holds: turns, retrievals,
deliveries, used ids and outcome signs (`PROTOCOL.md`, decision 0142). The
gold answers, the judge's text and the test questions never entered a
database, which the first three checks and the harness's own test
(`TestStudyEndToEndLocomo`: a gold string absent from every turn is absent from
the database bytes) confirm. No result was good enough to suggest a leak:
both guided modes scored below S0.

During the run, the driver stopped on the first flag (conv-30, S2) before
that condition's test half. After this review, the driver was changed so
that flags of the fourth kind are logged and reviewed by hand instead of
stopping the run (RESULTS.md, deviation 3). The first three remain hard
stops.
