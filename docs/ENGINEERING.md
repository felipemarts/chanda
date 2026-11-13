# Engineering rules

Read this and `BASE.md` before writing code. BASE.md is the specification; this
file is how we build it. If the two disagree, BASE.md wins unless a record in
`docs/decisions/` says otherwise.

## Build

- Go 1.26, module `github.com/felipemarts/chanda-mcp`, always `CGO_ENABLED=0`.
- `go vet ./...`, `gofmt -l .` empty and `go test ./...` must pass before every commit.
- No network at read or write time. Tests never touch the network.

## Shared foundation (do not change without the architect)

| Package | What it gives you |
| --- | --- |
| `internal/core` | Zones (`MinZone`, `ZoneForSource`), record types and id prefixes, fact statuses, provenance relations, `TimeLayout`/`FormatTime`/`ParseTime`, error codes (`core.Errf`, `core.CodeOf`), interfaces `Embedder`, `VectorIndex`, `Reranker`, `LLMProvider`, `Clock` |
| `internal/store` | `Open`, `Tx`, `Checkpoint`, embedded numbered migrations, `Queryer` (works with `*sql.DB` and `*sql.Tx`), `IsImmutableViolation` |
| `internal/store/migrations/0001_init.sql` | The whole v1 schema. Read it before writing any query |
| `internal/audit` | `Log.Append(ctx, q, Entry)` inside your transaction; `Verify`; action names |
| `internal/ids` | `Generator.For(core.RecEvent)` → `evt_<ULID>`, deterministic |
| `internal/clock` | `System`, `Fake` |
| `internal/config` | Every threshold. Add a field + default + test when you need a new one |
| `internal/env` | `Env{Cfg, Store, Clock, IDs, Audit}`; `env.NewTest(t)` for tests |

## Rules

1. **One connection.** The store has `MaxOpenConns(1)`. Never run a query while a
   `*sql.Rows` is open: scan into a slice, `Close`, then continue. Inside a
   transaction use only the `*sql.Tx` (passing `Store.DB()` there deadlocks).
2. **Determinism.** Never call `time.Now()`; use `env.Clock`. No `math/rand`.
   Never let map iteration order reach output, SQL or ids: sort first. Every
   SQL that returns a list has a total `ORDER BY` (end with the id).
3. **Every mutation is audited** in the same transaction (`env.Audit.Append`).
   Audit details hold ids and counts, never record content.
4. **Every derived record gets provenance edges** (`provenance` table) to the
   events it came from, in the same transaction.
5. **Zones only go down.** A derived record's zone is `core.MinZone` of its origins.
6. **Schema changes** go in a new migration file in your reserved range
   (see the header of `0001_init.sql`). Never edit an applied migration.
7. **Errors** crossing a package boundary carry a `core.Code`.
8. **Config, not constants.** Thresholds come from `env.Cfg`.
9. **Tests.** Table tests for each rule of BASE.md you implement, named after
   the rule. Use `env.NewTest(t)`.
10. **Determinism test.** If your feature mutates the database, add its
    operations to `Scenario` in `internal/determinism/determinism_test.go`.
11. **Divergence** from BASE.md becomes a short record in `docs/decisions/NNNN-title.md`.

## Commits

Small and incremental: one logical step per commit, tests included, each
commit builds and passes. Message: `area: imperative summary` (e.g.
`write: mask secrets during admission`). End every commit message with:

    Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
