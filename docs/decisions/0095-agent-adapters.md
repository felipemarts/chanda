# 0095 Agent adapters: shared client and failure behaviour

BASE.md asks for a Hermes adapter and a generic example that call `context`
before the first turn and `log_session`/`outcome` on finishing; it leaves the
mechanics open.

- **One Python client.** `adapters/generic/chanda_adapter/client.py` holds
  `ChandaClient` (a blocking facade over the async `mcp` SDK, on a private
  event loop thread, because agent code and Hermes provider methods are
  synchronous and threaded), the tool bridge, usage tracking and transcript
  helpers. It depends only on `mcp>=2.3.0,<3` (Hermes v0.23.1 pins
  `mcp==2.3.0`; tested with 2.3.0 against go-sdk v1.8.0, which negotiates
  protocol 2025-11-25). The Hermes plugin ships a byte-identical copy (0097).
- **Read and standard profiles only.** A spawned server always gets
  `--profiles read,standard`: the adapter needs `log_session` and `outcome`,
  never the admin tools, whatever `chanda.yaml` says. With `--http` the
  profiles are the server's own setting and the README says to start it the
  same way.
- **Envelope, not isError.** The client reads the envelope from
  `structuredContent` (else `content[0]` text) and raises `ChandaError` with
  the stable code when `ok` is false. Failures without an envelope (spawn
  failure, handshake or call timeout, protocol errors such as a tool outside
  the profiles) get the adapter-only code `transport`; it is never sent to or
  returned by Chanda.
- **No hidden retries; memory is optional.** Each call is sent once. If the
  server cannot start or `context` fails (for example `unavailable` from an
  unwired core) the adapter logs a warning, injects nothing and runs the
  agent; the finishing calls are still attempted once each when connected.
- **Pre-context placement.** The `data.text` block is already delimited and
  labelled by Chanda, so the adapter does not wrap it again: the first prompt
  is `block + "\n\n" + request`. An empty block (`no_relevant_memory`) injects
  nothing (principle 8).
- **What is logged.** `log_session` receives the user and assistant turns with
  text; system prompts, tool calls and tool results are left out, and the
  injected block is removed from user turns so memory does not re-ingest its
  own retrieval as session content.
- **The model cannot pick the namespace.** The bridge drops a `namespace`
  argument coming from the model; the namespace is adapter configuration.
