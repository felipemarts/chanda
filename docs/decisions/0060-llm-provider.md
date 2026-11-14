# 0060 LLM provider: response format, validation, no retry

`internal/llm.OpenAICompatible` posts to `{endpoint}/chat/completions` with
temperature 0 and the request's fixed schema as
`response_format: {type: json_schema, strict: true}`.

- **Capability fallback.** With `llm.response_format: auto` (default), a
  400/422 whose body names `response_format`/`json_schema` switches the
  provider to `json_object` for the rest of the process, and that one request
  is resent with the schema in the system prompt. This is a protocol
  negotiation before any answer exists, not a retry of an invalid answer.
  `json_schema` and `json_object` pin the mode and never resend.
- **Validation.** Every answer is checked against the schema by an in-package
  validator for the subset sleep uses (type, properties, required,
  additionalProperties false, items, enum, min/max items/length, minimum,
  maximum). Unsupported keywords are a schema error, so a schema never reads
  stricter than it validates. No dependency was added.
- **Invalid answer.** Not JSON, schema mismatch, refusal, empty or truncated
  (`finish_reason: length`) answers return an error wrapping
  `llm.ErrInvalidOutput` and the token usage; the caller discards the answer
  and nothing is retried. One surrounding ```json fence is stripped; nothing
  else is repaired.
- Transport and HTTP errors return `core.CodeUnavailable`.
