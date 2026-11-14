# 0101 Streamable HTTP: bearer token and loopback-only without one

The Streamable HTTP transport had no authentication: anyone who could reach
the port could read and write memory with the server's actor and zone
ceiling.

- `mcp.http_token_env` (default `CHANDA_MCP_TOKEN`) names the environment
  variable holding the token. The token itself is never in the
  configuration file (it would end up in backups and in `config.Hash`
  inputs); the variable name is left out of `config.Hash`, like
  `mcp.http_addr`.
- With a token, every HTTP request must carry `Authorization: Bearer
  <token>` (scheme case-insensitive). Anything else is `401` with
  `WWW-Authenticate: Bearer realm="chanda"`. Both sides are SHA-256 hashed
  before `subtle.ConstantTimeCompare`, so neither content nor length leaks
  through timing.
- Without a token, `Server.Serve` refuses a listener that is not loopback
  (`0.0.0.0`, `::`, a LAN address) with `permission_denied` and closes it.
  `CheckListenAddr` exposes the same rule.
- The SDK's DNS-rebinding protection (a request reaching a loopback address
  with a non-localhost `Host` is `403`) stays on, behind the auth check.

One token per server, no scopes: the actor and zone ceiling are still set by
the configuration (`mcp.actor`, `mcp.max_zone`), not by the token. stdio is
unchanged (the client spawns the process; no authentication needed).
