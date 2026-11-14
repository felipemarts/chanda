# 0080 MCP envelope, error mapping and actor

BASE.md fixes the envelope (`ok`, `data`, `notices`, `audit_id`) and asks for
stable error codes, but not how they map onto MCP results.

- **Envelope everywhere.** Every `tools/call` result carries the envelope as
  `structuredContent` and, as the same JSON, in `content[0]` (text), so clients
  that ignore structured output still parse it. An `error: {code, message}`
  member is added for failures; codes are `core.Code` (see `docs/errors.md`).
- **`isError` mirrors `!ok`.** The MCP spec reports tool execution errors with
  `isError: true` so the model sees them; clients that read the envelope get
  the stable code. Successful checks that report a bad state (`verify` with a
  broken chain, `forget` with a failed probe) are `ok: true`, not errors.
- **Low-level registration.** Tools are registered with `Server.AddTool`, not
  the generic `mcp.AddTool`, because the generic path answers argument
  validation failures with bare text. We validate against the same inferred
  JSON Schema (plus enums and ranges) and answer `invalid_argument` in the
  envelope. Each tool still declares an `outputSchema`: the envelope with that
  tool's `data` type.
- **Profiles are registration, not checks.** Only tools of `mcp.profiles` are
  registered; anything else is an `unknown tool` protocol error.
- **Actor.** The server puts `chanda.Actor{Name: mcp.actor, Human:
  mcp.human_actor}` in the request context (`chanda.WithActor`); the API reads
  it with `chanda.ActorFrom` and records `Name` in the audit log. The actor is
  not an input field, so a client cannot claim to be someone else. `promote`
  is refused with `permission_denied` before reaching the API unless
  `mcp.human_actor` is true; the API must enforce the same rule for Go callers.
- **`audit_id`** is the integer id of `audit_log` (BASE.md does not type it).
  Outputs embed `chanda.Meta{AuditID, Notices}`, which is not serialized in
  `data`; the envelope builder lifts it, also on failure.
- **`context` output** has `retrieval_id`, `text`, `items`, `reason` and
  `tokens_used`, as the Hermes adapter expects (`docs/research/hermes.md`).
