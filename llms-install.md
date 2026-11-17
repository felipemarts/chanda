# Installing the Chanda MCP server (for AI agents)

Chanda is a local memory server: one static binary, one SQLite file, no API
keys, no network. These steps install it and register it with an MCP client.

## 1. Get the binary

Download the binary for the platform from the latest release,
`https://github.com/felipemarts/chanda/releases/latest`:

| Platform | Asset |
| --- | --- |
| Linux x86-64 | `chanda-<version>-linux-amd64` |
| Linux ARM64 | `chanda-<version>-linux-arm64` |
| macOS Intel | `chanda-<version>-darwin-amd64` |
| macOS Apple silicon | `chanda-<version>-darwin-arm64` |
| Windows x86-64 | `chanda-<version>-windows-amd64.exe` |
| Windows ARM64 | `chanda-<version>-windows-arm64.exe` |

Check it against `SHA256SUMS` from the same release, then put it on a stable
path and make it executable:

- Linux and macOS: `~/.local/bin/chanda` (`chmod +x ~/.local/bin/chanda`; on
  macOS also `xattr -d com.apple.quarantine ~/.local/bin/chanda` if Gatekeeper
  blocks it).
- Windows: `%LOCALAPPDATA%\Programs\chanda\chanda.exe`.

With Go 1.26 or newer installed, this is equivalent:
`CGO_ENABLED=0 go install github.com/felipemarts/chanda/cmd/chanda@latest`
(the binary lands in `$(go env GOPATH)/bin`).

Verify: `<path-to-chanda> version` prints `chanda v...`.

## 2. Register it with the MCP client

Use the absolute path of the binary and of the database file. The database
folder is created on first start; no configuration file is needed.

```json
{
  "mcpServers": {
    "chanda": {
      "command": "/home/USER/.local/bin/chanda",
      "args": ["serve", "--db", "/home/USER/.chanda/chanda.db", "--profiles", "read,standard"],
      "disabled": false,
      "autoApprove": []
    }
  }
}
```

On Windows: `"command": "C:\\Users\\USER\\AppData\\Local\\Programs\\chanda\\chanda.exe"` and
`"--db", "C:\\Users\\USER\\.chanda\\chanda.db"`.

## 3. Check it

The server should list 13 tools: `context`, `search`, `get`, `timeline`,
`explain`, `stats`, `remember`, `assert_fact`, `ingest`, `log_session`,
`outcome`, `flag`, `flush`. A quick test: call `remember` with
`{"content": "Deploys happen on Tuesdays."}`, then `search` with
`{"query": "when are deploys"}`.

## Optional

- A `chanda.yaml` (see the README's "chanda.yaml" section) adds settings such
  as folders the `ingest` tool may read (`write.ingest_roots`). Pass it with
  `"--config", "/path/to/chanda.yaml"`.
- The admin tools (`forget`, `promote`, ...) are left out on purpose; add the
  `admin` profile only for a client a person drives.
