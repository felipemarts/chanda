# 0154 chanda init and chanda config

The main switches (agentic search, sleep, learned ranking, agent source
labels, conflict detection) were YAML keys spread across sections. The user
asked for them to be "very easy to select". Two commands make them so,
without changing any default.

## Commands

```sh
chanda init   [--config chanda.yaml] [--force]
chanda config [show] [--config f]
chanda config set <name|dotted.key> <value> [--config f]
```

- **`init`** writes a commented `chanda.yaml`. Its header lists the
  switches, and the switches are spelled out with their default values,
  so the file loads to exactly the defaults (same configuration hash).
  It refuses to overwrite an existing file without `--force`.
- **`config`** prints, as an envelope, each switch with `on` and `active`
  (`active` is false when the switch is on but needs `llm.endpoint` and
  none is set). It also prints the namespace, the read and write namespace
  lists (decision 0152), the profiles and the actor.
- **`config set`** takes a switch name with `on` or `off` (also
  true/false, yes/no, 1/0), or any dotted key with a YAML value, for
  example `config set mcp.read_namespaces "[default, company]"`. It edits
  the file through the YAML node tree, so the other keys and the comments
  stay. A missing file is created. The result is written to a temp file
  and loaded with `config.Load`, which knows every key and validates it.
  Only a result that loads replaces the file, atomically. An unknown key
  or an invalid value leaves the file unchanged.

## Switches

| Name | Key | Default |
| --- | --- | --- |
| `agentic-search` | `read.agentic_search` | on (decision 0151; active with `llm.endpoint`) |
| `sleep` | `sleep.scheduler` | on |
| `learning` | `ranking.enabled` | on |
| `agent-labels` | `sources.agent_labels` | on (decision 0153) |
| `conflicts` | `sleep.conflicts.enabled` | off (decision 0149: the claim was not established) |

`learning` is the configuration-wide switch. `chanda ranking on|off`
remains the runtime switch of one namespace, and both must be on for
learned ranking to act.

These commands touch only the file. A running `serve` reads its
configuration at start, so restart it after a change.

Tests: `TestInitAndConfig` (cli).
