# Error codes

Every Chanda tool answers in the same envelope, over MCP, the CLI and the Go
package (`pkg/chanda.Envelope`):

```json
{"ok": false, "data": null, "notices": [], "audit_id": 42,
 "error": {"code": "not_found", "message": "record fct_01J... not found"}}
```

- `ok` is `true` on success; then `data` holds the tool output and `error` is absent.
- `notices` is always an array (possibly empty). Notices are remarks, never instructions.
- `audit_id` is present when the call wrote an audit-log entry, also on some failures.
- `error.code` is one of the stable codes below. Codes are part of the public
  contract: they are never renamed or reused with another meaning. Match on
  `code`, never on `message`.

In Go, the same codes are `chanda.Code` constants (`chanda.CodeNotFound`, ...)
and `chanda.CodeOf(err)` extracts the code of any error returned by an
`chanda.API` method.

## Codes

| Code | Meaning | Typical causes | Retry? |
| --- | --- | --- | --- |
| `invalid_argument` | The call is malformed | Missing required field, wrong type, unknown field, value out of range (`outcome.value` outside −1..1), unparseable date, unknown forget scope kind | No; fix the arguments |
| `not_found` | The referenced object does not exist in this namespace | Unknown record id, retrieval id, subject or document | No |
| `rejected` | An admission rule refused the content | Empty, too short or over the size limit (BASE.md write step 3) | No; change the content |
| `permission_denied` | The caller may not do this | `promote` from a non-human actor; namespace mismatch; external-zone caller sending `outcome` | No |
| `conflict` | The request conflicts with the current state | Promote to a zone not higher than the current one; a sleep run already in progress | Maybe, after the state changes |
| `immutable` | Attempt to edit the raw log or the audit log | Any path that would `UPDATE` event content or an audit row | No |
| `budget_exceeded` | A configured ceiling was reached | Context budget above the ceiling; sleep budget exhausted before any progress | No; lower the request |
| `rate_limited` | Too many calls | `outcome` above `ranking.outcome_rate_per_minute` | Yes, later |
| `unavailable` | An optional component is not configured or not wired | Phase 2 sleep without an LLM provider; reranker not installed; the core not wired into this binary | No, until configured |
| `integrity` | A consistency check failed while serving the call | Audit chain broken; provenance checker found an orphan at the end of a write | No; run `verify` and investigate |
| `internal` | Unexpected failure | Database error, a bug, a panic in a handler | Maybe; report it |

Checks whose purpose is to report a state are not errors: `verify` with a
broken chain answers `ok: true` with `data.ok: false`; `forget` whose probes
fail answers `ok: true` with `data.verification.passed: false`; `context` with
nothing relevant answers `ok: true` with empty `data.text` and
`data.reason: "no_relevant_memory"`.

## MCP mapping

- Every `tools/call` result carries the envelope twice: as `structuredContent`
  and as JSON text in `content[0]`. Each tool declares an `outputSchema`
  describing the envelope with its own `data` type.
- `isError` is `true` exactly when `ok` is `false`. Clients that only look at
  `isError` still see failures; clients that parse the envelope get the code.
- Argument validation failures (missing fields, wrong types, unknown fields)
  also come back as an envelope with `invalid_argument`, not as bare text.
- A tool outside the configured profiles is not registered: it does not appear
  in `tools/list` and calling it is a JSON-RPC protocol error
  (`unknown tool`), not an envelope. The profile is a server setting, so no
  call can reach a tool of another profile.
- `promote` is registered with the admin profile but answers
  `permission_denied` unless `mcp.human_actor` is true in `chanda.yaml`.

See `docs/decisions/0080-mcp-envelope-and-errors.md` for the reasoning.

## CLI mapping

CLI subcommands print the envelope as JSON on stdout and exit with status 0
when `ok` is true, 1 when it is false and 2 on a usage error.
