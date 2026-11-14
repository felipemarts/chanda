# Phase 0: build and tests

- Chanda commit: 7e57d12bd0ee5f72b20476d55608c6812ba576b9
- Date (UTC): 2026-11-14T05:43:41Z
- Go: go version go1.26.2 darwin/arm64

```
go build -ldflags "-X github.com/felipemarts/chanda-mcp/internal/cli.Version=7e57d12" -o bin/chanda ./cmd/chanda
chanda 7e57d12
go vet ./...
lint: ok
go test -count=1 ./...
ok  	github.com/felipemarts/chanda-mcp/adapters/generic/go	0.272s
ok  	github.com/felipemarts/chanda-mcp/eval/cmd/lmeproxy	1.680s
ok  	github.com/felipemarts/chanda-mcp/eval/scenarios	3.385s
ok  	github.com/felipemarts/chanda-mcp/internal/app	1.878s
ok  	github.com/felipemarts/chanda-mcp/internal/audit	1.061s
ok  	github.com/felipemarts/chanda-mcp/internal/cli	4.123s
ok  	github.com/felipemarts/chanda-mcp/internal/config	1.329s
ok  	github.com/felipemarts/chanda-mcp/internal/dedup	1.551s
ok  	github.com/felipemarts/chanda-mcp/internal/determinism	1.932s
ok  	github.com/felipemarts/chanda-mcp/internal/e2e	1.455s
ok  	github.com/felipemarts/chanda-mcp/internal/embed	1.486s
ok  	github.com/felipemarts/chanda-mcp/internal/eval	2.807s
ok  	github.com/felipemarts/chanda-mcp/internal/eval/coresys	1.798s
ok  	github.com/felipemarts/chanda-mcp/internal/facts	2.056s
ok  	github.com/felipemarts/chanda-mcp/internal/facts/norm	1.639s
ok  	github.com/felipemarts/chanda-mcp/internal/forget	2.639s
ok  	github.com/felipemarts/chanda-mcp/internal/ids	1.597s
ok  	github.com/felipemarts/chanda-mcp/internal/llm	1.598s
ok  	github.com/felipemarts/chanda-mcp/internal/mcp	1.607s
ok  	github.com/felipemarts/chanda-mcp/internal/prov	1.892s
ok  	github.com/felipemarts/chanda-mcp/internal/rank	2.655s
ok  	github.com/felipemarts/chanda-mcp/internal/read	5.895s
ok  	github.com/felipemarts/chanda-mcp/internal/sleep	2.284s
ok  	github.com/felipemarts/chanda-mcp/internal/store	1.774s
ok  	github.com/felipemarts/chanda-mcp/internal/write	2.129s
ok  	github.com/felipemarts/chanda-mcp/internal/write/normalize	1.472s
ok  	github.com/felipemarts/chanda-mcp/pkg/chanda	1.599s
ok  	github.com/felipemarts/chanda-mcp/pkg/chanda/embedreg	1.317s
ok  	github.com/felipemarts/chanda-mcp/pkg/chanda/local	0.554s
result: all packages ok, no FAIL
```
