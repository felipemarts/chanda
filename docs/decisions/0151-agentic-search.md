# 0151 Agentic search (on by default, active only with an LLM)

The user asked for an agentic search mode: before a read, an LLM reformulates
the query into better search terms, and the read runs on them. The user
chose it to be **on by default**, with a key to turn it off.

## Divergence from BASE.md

This departs from two inviolable principles, on the user's decision:

- **Principle 2 (no LLM on the read path).** With an LLM configured,
  `context` and `search` make one LLM call before reading.
- **Principle 11 (same input, same output).** With that call, a read
  depends on the LLM's answer, so two reads of the same query on the same
  state can differ.

The departure is bounded:

- **No LLM, no change.** Without `llm.endpoint`, nothing calls an LLM and
  both principles hold exactly as before. The default configuration has
  no endpoint, so a default install is unchanged.
- **The read package stays free of LLMs.** The call is made in
  `internal/app` (`agenticQuery`) before `read.Reader` runs. Ranking,
  floors, the cut, the budget and rendering are untouched.
- **The caller's words stay first.** The read path searches the query
  followed by the LLM's terms (`query + "\n" + terms`), never a
  replacement. A term the LLM drops is still searched, and
  `read.max_query_terms` keeps the caller's terms before the added ones.
- **Any failure falls back.** A transport error, a timeout
  (`read.agentic_timeout_ms`) or an answer off the schema leaves the
  query as given, with the notice `agentic_search_failed`.
- **The gate set and the evaluation stay deterministic.** Gate scoring
  calls `read.Reader.Score` directly and never goes through this step.
  The LongMemEval harness (`eval/lme`) writes
  `read.agentic_search: false`, so the frozen studies keep measuring the
  deterministic read path even when condition E sets an LLM endpoint.

## Keys (configuration)

| Key | Default | Meaning |
| --- | --- | --- |
| `read.agentic_search` | on (unset) | `false` turns it off. Active only with `llm.endpoint`. |
| `read.agentic_timeout_ms` | 0 (= 4000) | Upper bound on the LLM call. |

Both keys are `omitempty`, and `agentic_search: true` is normalised away in
the hash. The default configuration hash stays `4dd590544d868b15`.

## The call

One request per read, with system prompt `agenticSystem`, the caller's query
as the prompt, at most 200 output tokens and the fixed schema
`{"terms": string ≤ 400}`. The LLM is asked for the words the answering
memories would contain (names, places, dates, numbers, the topic, a few
synonyms), in the message's language, with no answer and no invented facts.
Empty terms, or terms equal to the query, leave the query as given.
Otherwise the notice `agentic_search` is set.

The retrieval log records the expanded query (`retrievals.query_text`),
because that is what was searched. Learned keys (decision 0145) and the
guided digest (decision 0142) therefore see the expanded text.

## Not measured

The effect on answer accuracy has not been measured. Unlike decisions
0141 to 0149, this is a product decision taken without an evaluation. To
measure it, run `lme contexts` with `-chanda-set read.agentic_search=true`
against the same frozen split.

Tests: `TestAgenticSearch` (app), `TestAgenticSearchDefaultsAndHash` (config).
