# 0096 Adapter outcome calls and used_ids

`outcome` takes one `retrieval_id`, a value in [-1, 1] and `used_ids`, "ids of
delivered records that were actually used". BASE.md leaves open how an
adapter knows what was used ("Reliability of used_ids", phase 8).

- **Used** means the agent opened the record with `get` (through the adapter's
  tool bridge) or its id appears verbatim in an assistant turn (a citation).
  Both are observable without reading the answer's meaning, so the result is
  deterministic for a given transcript.
- **Per retrieval.** The `used_ids` sent for a retrieval are always a subset
  of what that retrieval delivered, sorted. A record opened by `get` but
  delivered by no retrieval of the session credits nothing.
- **Which retrievals.** The `context` retrieval always gets an outcome when
  the task has a value (delivered and not used is itself a signal). Each
  `search` retrieval gets one only when at least one of its records was used;
  unused searches are not reported, to keep calls under
  `ranking.outcome_rate_per_minute`. Order: context first, then searches in
  call order.
- **No value, no outcome.** The generic `run_task` sends outcomes only when the
  agent returns a value; the Hermes value policy is in 0097. Values outside
  [-1, 1] are refused in the adapter before anything is sent.
- The Go example keeps it smaller: one outcome, for the `context` retrieval.
