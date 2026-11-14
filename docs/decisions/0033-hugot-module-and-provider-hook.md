# 0033 hugot ships as a nested module, wired through pkg/chanda/embedreg

Follows 0006. The real embedder needs Go 1.27 and a patched go-huggingface,
which the main module must not inherit.

- `embedders/hugot` is its own module,
  `github.com/felipemarts/chanda-mcp/embedders/hugot` (go 1.27.0), with
  `replace github.com/felipemarts/chanda-mcp => ../..` and
  `replace github.com/gomlx/go-huggingface => ./third_party/go-huggingface-patched`
  (upstream v0.4.32 trimmed to the packages hugot imports, Apache-2.0 licence
  kept, change described in its `PATCH.md`). The nested `go.mod` keeps it out
  of the main module's `./...`: `go build`, `go vet` and `go test` at the root
  are unchanged and CI stays on Go 1.26. Only `gofmt -l .` walks into it, and
  the vendored code is gofmt-clean.
- Hook: `pkg/chanda/embedreg` (`Register(name, Factory)`, `Lookup`), with its
  own `Embedder` interface (the method set of `core.Embedder`) and `Config`
  (the fields of `config.EmbedConfig`), so it imports nothing internal and
  any module can register. `internal/embed.NewFromConfig` consults it first
  for any provider except `hash` (which stays built in); a factory error
  keeps its `core.Code`, else becomes `internal`. A binary calls
  `hugot.Register()` from `main`.
- Go's `internal` rule is checked on import paths in module mode, so this
  nested module (its path is under the main module's) may import
  `internal/core`, `internal/config` and `internal/embed`; it does, for coded
  errors and for an end-to-end test of `embed.NewFromConfig`. A module with
  an unrelated path could only use `embedreg`.
- `chanda-hugot` cannot be "the chanda CLI plus a registration" yet: the CLI
  is `package main` in `cmd/chanda` and cannot be imported. The binary has the
  model commands (`models pull|adopt|verify|list`, `embed`, `rerank`,
  `version`) and routes every other command to a `chandaMain` variable that
  reports the gap. Needed change, for integration: move `run` and the command
  functions of `cmd/chanda/main.go` into an importable package (for example
  `internal/cli` with `Run(ctx, args, stdout, stderr) int`; importable from
  this module by the rule above), keep `cmd/chanda` a three-line `main`, and
  set `chandaMain = cli.Run` in `embedders/hugot/cmd/chanda-hugot`.
- Not wired: `embed.reranker_model_path` has no consumer in the main module
  yet; `hugot.OpenReranker(ctx, dir)` returns a `core.Reranker` when one exists.
