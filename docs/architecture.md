# Architecture

This document records the canonical package layout and conventions established
by the foundation (issue #3). Every later command plugs into this skeleton.

## Grammar

`vibexp <noun> <verb>` (gh/kubectl-style). Curated commands per noun plus a raw
`vibexp api <METHOD> <path>` passthrough (later issues).

## Package layout

```
cmd/vibexp/main.go          entrypoint; runs the root command, maps errors -> exit codes
internal/cli/               cobra command tree
  root.go                   root command, global flags, persistent pre-run
  context.go                runtime/logger stashed on context.Context
  configcmd/                config context subcommands
  versioncmd/               vibexp version
internal/config/            named-context store (koanf) + precedence resolution
internal/logging/           always-on JSON-lines file logger + rotation + redaction
internal/exitcode/          exit-code constants + typed CodedError
internal/version/           ldflags-injected build metadata
```

Later issues add: `internal/cred/`, `internal/oauth/`, `internal/api/`,
`internal/output/`, `internal/update/`, and more command packages under
`internal/cli/`.

## Global flags & precedence

Root owns the persistent flags `--context`, `--team`, `--project`, `--format`,
`--debug`, `--timeout`. Resolution precedence for every setting is:

```
flag  >  environment variable  >  active context
```

Environment variables: `VIBEXP_CONTEXT`, `VIBEXP_BASE_URL`, `VIBEXP_TEAM`,
`VIBEXP_PROJECT`. The resolved values are captured in a `config.Runtime` and
handed to commands via `context.Context` (`cli.RuntimeFrom(ctx)`).

## Config store

Named contexts live in `~/.vibexp/config.yaml` (dir `0700`, file `0600`, atomic
temp-file + rename writes). A context is `{name, base_url, team, project}`.
Credentials are **never** stored here — a separate `credentials.json` (issue #4)
holds those.

## Logging & redaction

`internal/logging` wraps `log/slog`'s JSON handler over a size-rotating file
(`~/.vibexp/logs/cli.log`, 5 MB × 3 backups). It is always on at info level;
`--debug` raises it to debug and mirrors to stderr.

Redaction happens at the handler:

- **By key** — attributes whose key matches
  `(?i)(api[_-]?key|token|secret|authorization|password|credential|bearer)` are
  replaced with `***REDACTED***`.
- **By value** — every credential the CLI handles is registered via
  `logging.RegisterSecret`, and any registered value is scrubbed wherever it
  appears (including inside free-form messages).

stdout is reserved for command data; all status/diagnostic output goes to
stderr or the log file.

## Exit codes

Defined once in `internal/exitcode` and mapped centrally in `main.go`:

| Code | Constant | Meaning |
| --- | --- | --- |
| 0 | `OK` | success |
| 1 | `RuntimeErr` | API / runtime error |
| 2 | `UsageErr` | invalid usage |
| 4 | `AuthErr` | auth failure |

Commands return `*exitcode.CodedError` (directly or wrapped) to select a code;
anything unclassified maps to `1`. Cobra flag-parse errors map to `2` via the
root's `FlagErrorFunc`.

## Build metadata

`internal/version` exposes `Version`, `Commit`, `Date`, `InstallSource`, set via
`-ldflags -X` (see the `Makefile`). `InstallSource` gates self-update behavior
in later issues.
