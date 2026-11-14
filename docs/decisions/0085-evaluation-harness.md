# 0085 Evaluation harness choices

BASE.md "Evaluation" fixes what is compared and reported, not how. `internal/eval` decides:

- **Relevance is matched by span, not by chunk id.** A gold label is an item id
  plus an optional span. A retrieved excerpt hits it when it comes from that item
  and contains the span (lowercased, accents removed, whitespace collapsed).
  The metrics then do not depend on any system's chunking or record ids.
  Gold entries that share `unit` are alternatives (a paraphrase counts as the
  same evidence). nDCG@k counts one gain per newly hit unit, with the ideal at
  one unit per rank, clamped to 1.
- **One token estimator for every system**: `ceil(characters / 4)`. The harness
  recounts each delivered excerpt and drops the ones that do not fit, counting
  them as budget violations. Every system gets the same budget
  (`read.context_default_tokens`, 800).
- **Raw history for B1 to B3**: every distinct chunk of every item and version,
  never retired. An unchanged paragraph of a new version is not indexed twice,
  as in write step 4. The baselines chunk with a local structural chunker and do
  not import `internal/write`.
- **B3 at an 800-token budget does not fit the corpus**, so it falls back to the
  most recent chunks that fit. The report states how many queries were truncated.
- **B2 uses the configured embedder** (`internal/embed`: the hash embedder
  by default, hugot in chanda-hugot), the same instance C1..C5 use (decision
  0086). The stand-in `eval-localhash-v1-256` remains for tests of the
  package. The report declares the `model_id`.
- **Abstention precision** = empty results on unanswerable questions ÷ all empty
  results. It is undefined (and the suite fails) for a system that never returns
  an empty result. The suite target (95% precision) can be met by a system that
  rarely abstains, so the report also publishes recall.
- **"Forbidden in context"** is an LLM-free proxy for the temporal and injection
  risks. It is the share of questions whose context contains a superseded value
  or an injected instruction, given in each question's `must_not_contain`.
- **Determinism**: everything except the latency section is a function of the
  dataset, systems, budget, configuration, clock date, version and hardware.
  Latency is measured with a real clock and sits in its own JSON key and
  Markdown section. Per-question detail goes in `<name>.questions.jsonl`.
- **Gate set**: `gate_questions.expected` is the JSON object
  `{"record_ids": [...], "answers": [...], "as_of": "..."}`, with every field
  optional. With no ids and no answers, the expected result is empty.
  `GateScore` returns 0 to 100, so the sleep gate tolerance of 2 points applies
  directly.
