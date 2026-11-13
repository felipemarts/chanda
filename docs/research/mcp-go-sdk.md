# MCP Go SDK cheat sheet (v1.8.0)

Verified empirically on 2026-11-13 (go1.26.2 darwin/arm64, `CGO_ENABLED=0`). All code below was run; the
scratch project lives in the session scratchpad (`.../scratchpad/mcpsdk`), not in this repo.

## Versions and imports

- Module `github.com/modelcontextprotocol/go-sdk@v1.8.0` (BASE.md says v1.7.0; v1.8.0 is current). `go.mod` requires `go 1.25.0`.
- Imports: `github.com/modelcontextprotocol/go-sdk/mcp` (everything we need),
  `github.com/google/jsonschema-go/jsonschema` (schema inference/override, transitive dep v0.4.3),
  `.../go-sdk/jsonrpc` (error codes), `.../go-sdk/auth` (OAuth, not needed now).
- Builds and tests pass with `CGO_ENABLED=0`; no cgo in the dep graph.
- Protocol revisions (`mcp.SupportedProtocolVersions()`, newest first):
  `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`.
  Narrow with `ServerOptions.SupportedProtocolVersions` (can only narrow, never widen).
- Observed negotiation (client = SDK client, latest):
  stdio and in-memory -> `2026-07-28`; Streamable HTTP with `Stateless:false` -> `2025-11-25`
  (SDK client tries `server/discover`, falls back to `initialize`); HTTP with `Stateless:true` -> `2026-07-28`.
  Docs (`docs/protocol.md`): `2026-07-28` over HTTP is accepted ONLY by a `Stateless: true` handler.
  Most real clients today speak <= 2025-11-25, so stateful default is fine.

## Core API (exact signatures)

```go
func NewServer(impl *Implementation, opts *ServerOptions) *Server        // impl must be non-nil
type Implementation struct{ Name, Title, Description, Version, WebsiteURL string; Icons []Icon }
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])   // generic, validates + infers schemas
func (s *Server) AddTool(t *Tool, h ToolHandler)                           // low-level, raw JSON, no validation
type ToolHandlerFor[In, Out any] func(ctx context.Context, req *CallToolRequest, in In) (*CallToolResult, Out, error)
type ToolHandler func(context.Context, *CallToolRequest) (*CallToolResult, error)
func (s *Server) Run(ctx context.Context, t Transport) error               // single session, blocks until client closes/ctx done
func (s *Server) Connect(ctx context.Context, t Transport, o *ServerSessionOptions) (*ServerSession, error)
func (s *Server) RemoveTools(names ...string)
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, o *StreamableHTTPOptions) *StreamableHTTPHandler // http.Handler
type StdioTransport struct{ MaxLineLength int }                            // newline-delimited JSON on stdin/stdout
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)      // (clientSide, serverSide)
// client side
func NewClient(impl *Implementation, o *ClientOptions) *Client
func (c *Client) Connect(ctx context.Context, t Transport, o *ClientSessionOptions) (*ClientSession, error)
func (cs *ClientSession) ListTools(ctx, *ListToolsParams) (*ListToolsResult, error)
func (cs *ClientSession) CallTool(ctx, *CallToolParams{Name string; Arguments any}) (*CallToolResult, error)
func (cs *ClientSession) InitializeResult() *InitializeResult             // .ProtocolVersion, .ServerInfo, .Capabilities
type StreamableClientTransport struct{ Endpoint string; HTTPClient *http.Client; ... }
type CommandTransport struct{ Command *exec.Cmd; TerminateDuration time.Duration } // spawn stdio server
type LoggingTransport struct{ Transport Transport; Writer io.Writer }     // wrap any transport to dump JSON-RPC
```

`ServerOptions` useful fields: `Instructions`, `Logger *slog.Logger`, `PageSize`, `KeepAlive`, `Capabilities`,
`SupportedProtocolVersions`, `GetSessionID`. Middleware: `Server.AddReceivingMiddleware(...)` (logging, auth, audit hooks).
`StreamableHTTPOptions`: `Stateless`, `JSONResponse` (plain JSON instead of SSE), `Logger`, `EventStore`,
`SessionTimeout`, `DisableLocalhostProtection`, `MaxRequestBodyBytes` (default 4 MiB), `CrossOriginProtection` (deprecated;
wrap handler with `http.NewCrossOriginProtection().Handler(h)`).

## Schema inference (via google/jsonschema-go)

- `In`/`Out` struct -> JSON Schema 2020-12 object with `additionalProperties:false` (unknown args are REJECTED).
- Field name from the `json` tag. **Required = no `omitempty`/`omitzero`**; optional = `omitempty` (or `omitzero`).
- `jsonschema:"text"` tag = the field description only. It is plain text; no `key=value`, no `enum=`/`minimum=`.
  (A tag value starting with `WORD=` is rejected by schema inference.)
- Pointers and slices/maps become `["null", T]` in the schema (`*string` -> `["null","string"]`).
  A nil slice marshals to `null` (valid for the schema), but prefer `[]T{}` for clean output.
- `time.Time` -> string; `struct{}` input -> `{"type":"object","additionalProperties":false}`; `any` Out -> no output schema.
- Enums, min/max, patterns, defaults: build the schema yourself and pass it as `Tool.InputSchema`
  (override is honored, still validated by AddTool):
  ```go
  schema, _ := jsonschema.For[SearchIn](nil)
  schema.Properties["mode"].Enum = []any{"fast", "deep"}
  schema.Properties["limit"].Minimum = jsonschema.Ptr(1.0)
  mcp.AddTool(s, &mcp.Tool{Name: "search", InputSchema: schema}, handler)
  ```
- Inferred schemas are generated at `AddTool` time (reflection). Reuse a `ServerOptions.SchemaCache` only if you build
  a `Server` per request.

Inferred for our `SearchIn` (query required, rest optional) and `Envelope`:

```
in : {"additionalProperties":false,"properties":{"as_of":{"description":"RFC3339 time","type":["null","string"]},"limit":{"description":"max results (1-50)","type":"integer"},...,"query":{"description":"free-text query","type":"string"}},"required":["query"],"type":"object"}
out: {"additionalProperties":false,"properties":{"audit_id":{"type":"string"},"data":{"items":{...},"type":["null","array"]},"notices":{...},"ok":{"type":"boolean"}},"required":["ok","data","audit_id"],"type":"object"}
```

## Results and errors (observed behavior)

| Situation | What the client sees |
| --- | --- |
| Handler returns `(nil, out, nil)` | `IsError=false`, `StructuredContent` = out (client side: `map[string]any`), `Content[0]` = `*TextContent` with the same JSON (auto-filled if Content unset) |
| Handler returns `(nil, zero, err)` | **tool error**: `CallTool` err == nil, `res.IsError == true`, `Content[0].Text == err.Error()`, no StructuredContent |
| Handler sets `r.SetError(err)` on a returned result | same as above (tool error), lets you also set custom Content/Meta |
| Input fails validation (missing required, wrong type, extra property, enum/min/max violation) | **tool error** (not protocol error) in v1.8.0: text like `validating "arguments": validating root: required: missing properties: ["query"]` |
| Low-level `Server.AddTool` handler returns `error` | **protocol error**: `CallTool` returns `err` (`calling "tools/call": <msg>`), `res == nil` |
| Unknown tool name | protocol error: `calling "tools/call": unknown tool "nope"` |
| No arguments sent to a no-input tool | OK |

- For Chanda's envelope (`ok/data/notices/audit_id` + stable error codes): make `Out` the envelope type. For domain
  failures prefer returning an envelope with `ok:false` + an error code in the body (structured, schema-valid) OR a tool
  error whose text starts with the stable code (e.g. `E_NOT_FOUND: ...`). Note a returned `err` loses structure: only
  `err.Error()` text reaches the client, and `StructuredContent` is empty.
- Panics in handlers are not part of the contract; recover inside the handler.
- A `*jsonrpc.Error` returned from a low-level handler also arrives as a protocol error (`calling "tools/call": <message>`); whether the numeric code survives on the client was not verified.

## Transports

- **stdio**: `srv.Run(ctx, &mcp.StdioTransport{})`. stdout is the protocol channel: never print/log to stdout
  (`log.SetOutput(os.Stderr)`). Run returns when the client closes stdin or ctx is cancelled (use `signal.NotifyContext`).
- **Streamable HTTP**: `http.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, opts))`.
  Same `*Server` may be returned for every request; the SDK creates a session per client (`Mcp-Session-Id`).
  Default DNS-rebinding protection: requests via loopback with a non-localhost `Host` get 403 (disable only
  with `DisableLocalhostProtection`). Client: `&mcp.StreamableClientTransport{Endpoint: url}`.
  The handler does no authentication; wrap it in your own middleware (bearer token) for non-local use.
- Profiles (Read/Standard/Admin): build a different `*Server` per profile (register only that profile's tools) and
  choose it in `getServer(r)` (e.g. by path or token), or at startup for stdio.
- Spawn a stdio server from a test/adapter: `mcp.CommandTransport{Command: exec.Command(bin)}` (proven).

## Complete minimal example (ran: `CGO_ENABLED=0 go test -v ./...` PASS)

`server.go`:

```go
package mcpsdk

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchIn struct {
	Query     string   `json:"query" jsonschema:"free-text query"`                 // required
	Namespace string   `json:"namespace,omitempty" jsonschema:"memory namespace"` // optional
	Limit     int      `json:"limit,omitempty" jsonschema:"max results (1-50)"`   // optional
	Tags      []string `json:"tags,omitempty" jsonschema:"filter by tags"`        // optional
	AsOf      *string  `json:"as_of,omitempty" jsonschema:"RFC3339 time"`         // optional
}
type Hit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}
type Envelope struct {
	OK      bool     `json:"ok"`
	Data    []Hit    `json:"data"`
	Notices []string `json:"notices,omitempty"`
	AuditID string   `json:"audit_id"`
}

func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "chanda", Version: "v0.0.1"},
		&mcp.ServerOptions{Instructions: "Chanda memory server"})

	mcp.AddTool(s, &mcp.Tool{
		Name: "search", Description: "Ranked search",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, Envelope, error) {
		if in.Query == "" {
			return nil, Envelope{}, errors.New("query must not be empty") // -> tool error (isError=true)
		}
		return nil, Envelope{OK: true, Data: []Hit{{ID: "r1", Score: 0.9}}, AuditID: "a1"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{Name: "stats"}, // no input: empty struct
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Envelope, error) {
			return nil, Envelope{OK: true, AuditID: "a2"}, nil
		})

	// low-level handler: returned error => JSON-RPC protocol error
	s.AddTool(&mcp.Tool{Name: "raw", InputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, errors.New("raw handler failure")
		})
	return s
}
```

`server_test.go` (in-memory client/server, plus HTTP):

```go
func connectMem(t *testing.T, ctx context.Context) *mcp.ClientSession {
	ct, st := mcp.NewInMemoryTransports()
	ss, err := NewServer().Connect(ctx, st, nil) // server FIRST, then client
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	cs := connectMem(t, ctx)
	lt, _ := cs.ListTools(ctx, nil)               // lt.Tools[i].InputSchema / OutputSchema are map[string]any
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "hi", "limit": 3}})
	if err != nil || res.IsError { t.Fatalf("err=%v res=%+v", err, res) }
	var env Envelope                              // decode structured output into our own type
	b, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(b, &env)                   // env.Data[0].ID == "r1"
	_ = lt
}

func TestHTTP(t *testing.T) {
	srv := NewServer()
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	defer ts.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil { t.Fatal(err) }
	defer cs.Close()
}
```

`cmd/srv/main.go` (stdio by default, `-http addr [-stateless]` for HTTP; also ran it via `CommandTransport`):

```go
srv := mcpsdk.NewServer()
if *addr == "" {
	log.SetOutput(os.Stderr)                       // never stdout
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil { log.Fatal(err) }
	return
}
mux := http.NewServeMux()
mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
	&mcp.StreamableHTTPOptions{Stateless: *stateless}))
log.Fatal(http.ListenAndServe(*addr, mux))
```

## Gotchas

1. `jsonschema:"..."` is description-only; constraints (enum/min/max) need a schema override (see above).
2. Required-ness is driven by `omitempty`. Forgetting it on an optional field makes it required and clients that omit it fail
   validation. Conversely a required field with `omitempty` becomes optional.
3. `additionalProperties:false` on inputs: clients sending unknown keys (e.g. a future `namespace` on every tool) get a tool error.
   Put `namespace` explicitly in every input struct (BASE.md: every tool receives `namespace`).
4. Validation failures are tool errors (`IsError=true`, `err==nil` on the client), so tests must check `res.IsError`.
5. A handler `error` is flattened to text; to keep stable machine codes, put the code in the text prefix or return `ok:false` envelopes.
6. Typed `Out` makes `outputSchema` mandatory-valid: the returned value is validated against it, so non-nil/consistent
   fields matter (e.g. required `data` serializes `null` for nil slices, which the inferred `["null","array"]` accepts).
7. In-memory: connect the server before the client; the client `Connect` performs initialization.
8. Server default capabilities include `logging`; `tools` capability is added automatically once a tool is registered
   (register tools before `Run`/`Connect`, or tool-list notifications go out to connected peers).
9. stdio: any stray write to stdout corrupts the stream. Inbound frame cap 16 MiB (`StdioTransport.MaxLineLength`).
10. HTTP: `2026-07-28` sessions require `Stateless:true`; stateful mode serves <= `2025-11-25`. SSE standalone GET stream
    is opened by the SDK client unless `DisableStandaloneSSE`; set `JSONResponse:true` for simple request/response curl-friendly replies.
11. Go toolchain: module needs `go >= 1.25`. Deprecated in 2026-07-28 (still working): roots, sampling, logging capability.
12. `Tool.Name` must follow SEP-986 (letters, digits, `_`, `-`, `.`); invalid names only log an error, they do not panic.
