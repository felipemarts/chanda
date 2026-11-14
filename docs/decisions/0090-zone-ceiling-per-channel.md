# 0090 Zone ceiling per channel

Problem: `remember`, `assert_fact` and `ingest` accept a `source` kind from
the caller. If a client could send `user_statement` freely, it would pick the
highest zone itself, which breaks principle 5 ("origin is fixed at write time
and never rises on its own") and the trust-zone rule "no text can declare its
own zone".

Decision: the channel, not the call, bounds the zone. Each MCP server
instance has `mcp.max_zone` (default `internal`). The zone of a write is
`MinZone(ZoneForSource(kind), max_zone)`; when the request is clamped, the
response carries a notice. Only a channel configured as human
(`mcp.human_actor: true`, e.g. the CLI or an adapter relaying the user's own
message) may set `max_zone: user`. Tool results and web content are always
`external`, whatever the ceiling.
