# 0152 Namespace permissions per MCP channel

Before this record, `namespace` was only a default. The MCP server filled
in an empty namespace with the configured one, but a call that named
another namespace could read or write it. Two agents served by different
`chanda serve` processes over the same database were isolated only by
convention.

## Keys (configuration)

| Key | Default | Meaning |
| --- | --- | --- |
| `mcp.read_namespaces` | empty (any) | Namespaces the read-only tools may name: `context`, `search`, `get`, `timeline`, `explain`, `stats`, `verify`. |
| `mcp.write_namespaces` | empty (any) | Namespaces every other tool may name: `remember`, `assert_fact`, `ingest`, `log_session`, `outcome`, `flag`, `flush` and the admin tools. |

Rules:

- **Empty means any namespace.** A list that contains `"*"` also allows any
  namespace. Existing configurations keep working unchanged.
- **A list must include the default namespace.** Otherwise a call that
  omits `namespace` would always be refused. `Validate` rejects such a
  list, and also any entry that is empty or has surrounding spaces.
- **The lists are independent.** A writable namespace is not readable
  unless it is in `read_namespaces` too (or that list is empty). To allow
  no writes at all, leave out the `standard` and `admin` profiles.
- **Checked before the API.** The server checks the namespace after it
  applies the default, and compares it trimmed, the way the API reads it.
  A refused call returns `permission_denied` and never reaches the API, so
  it writes no audit entry.

The keys are `omitempty`, so the default configuration hash stays
`4dd590544d868b15`. The checks apply only to the MCP channel. An
in-process Go caller (`pkg/chanda/local`) or the CLI is already trusted
with the whole database file.

## Example: two agents, one shared namespace

```yaml
# sales agent
namespace: sales
mcp:
  actor: sales-agent
  read_namespaces: [sales, company]
  write_namespaces: [sales]
```

The sales agent reads its own memory and the shared `company` namespace. It
can write only to its own.

Tests: `TestNamespacePermissions` (mcp).
