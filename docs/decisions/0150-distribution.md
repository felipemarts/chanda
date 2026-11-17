# 0150 Distribution: release binaries, MCP bundles, the MCP Registry and the Hermes catalog

Chanda had no release: users built it from a clone. This decision makes it
installable and discoverable without Go, through the channels agents and MCP
clients already search.

## `serve --db`

An MCP bundle is started by a desktop client from a working directory the
bundle does not choose, and the default `db_path` (`chanda.db`) is relative to
it. `chanda serve --db <file>` overrides `db_path` and creates the file's
folder (mode 0700) when missing. With `--config ""` the defaults apply, so a
bundle needs no `chanda.yaml`. Test: `TestServeDBFlag` (the database lands at
`--db`, nothing is written to the working directory).

## Release (`.github/workflows/release.yml`, on a `v*` tag)

1. `make release` builds the six static binaries and `SHA256SUMS`.
2. `scripts/mcpb.py` packs one MCP bundle per platform,
   `chanda-mcp-<os>-<arch>.mcpb` (the registry requires "mcp" in the file
   name when the repository name lacks it), from `packaging/mcpb/manifest.json`:
   `server.type: binary`, the `read` and `standard` profiles only, and two
   user settings, `db_path` (default `${HOME}/.chanda/chanda.db`) and an
   optional `config_path`. It also writes `server.json` with each bundle's
   URL and SHA-256.
3. The binaries, bundles, `SHA256SUMS` and `server.json` are attached to a
   GitHub Release.
4. `mcp-publisher` (pinned, signature verified with cosign) logs in with
   GitHub OIDC and publishes `io.github.felipemarts/chanda` to the MCP
   Registry. No secret is stored.

## Hermes

Hermes does not install plugins from PyPI and asks providers not to be pip
installed into its environment. The supported paths are a Git install of the
plugin folder (`hermes plugins install felipemarts/chanda/adapters/hermes/chanda_hermes`)
and the reviewed plugin catalog (a PR adding `plugin-catalog/chanda.yaml` to
NousResearch/hermes-agent, pinned to a commit SHA). `plugin.yaml` moves to
manifest v2 with `requires_hermes: ">=0.23.1"` and the `on_session_end` hook
declared (the provider tools come from the memory provider, not
`register()`, so `provides_tools` stays empty; `hermes plugins validate
--install-deps` on v0.23.2 passes with no warning), and the `chanda` binary installed at a
pinned version (`go install ...@v0.1.0`) instead of `@latest`, as the
catalog's no-unpinned-download rule requires.

## Not done

No PyPI package (unused by Hermes), no Docker image (only the Docker MCP
Catalog would need one), no Homebrew tap.
