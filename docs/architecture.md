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

## Client layer (`internal/api`)

Every command talks to the API through this layer — never the generated client
directly. `api.New(ctx, rt, credStore, getenv)` returns a ready
`*vibexp.ClientWithResponses` composed of:

- **Transport (`doer.go`)** — a `Doer` implementing the generated
  `HttpRequestDoer`: a per-request timeout (`--timeout`, default 30s), a
  `User-Agent: vibexp-cli/<version>`, and bounded exponential-backoff+jitter
  retries (3 attempts, cap 5s, `Retry-After` honored) on **429/5xx/transport
  errors for safe methods only** (GET/HEAD). POST/PUT/PATCH/DELETE never retry.
- **Auth editor (`factory.go`)** — a `RequestEditorFn` that sets
  `Authorization: Bearer …` from the credential store (env `VIBEXP_API_KEY`,
  stored API key, or a transparently-refreshed OAuth token). Every credential is
  registered with the log redactor.
- **Error mapper (`errors.go`)** — `api.Check(status, body)` turns any non-2xx
  into a single `*api.Error` carrying the RFC 7807 `detail`, `validation_errors`,
  and `request_id`. It satisfies `exitcode.ExitCoder` (401/403 → 4, else 1), and
  the root logs the `request_id` to the file log on failure.
- **Resolution (`resolve.go`)** — `api.Team(rt)` / `api.Project(rt)` return the
  already-precedence-resolved id/slug (flag > env > context), or a usage error
  (exit 2) naming all three ways to set it.

`GET /health` (`health.go`) is the unauthenticated server version handle;
`vibexp version` appends the server release sha when a context resolves.

## Browser login (`internal/oauth`)

`vibexp auth login` runs RFC 8414 discovery → RFC 7591 dynamic client
registration (public client, loopback `http://127.0.0.1:<port>/callback`) →
authorization code + PKCE S256 → RFC 8707 `resource` indicator. The requested
scopes are negotiated against the server's `scopes_supported` and declared at
registration.

`oauth.Flow.Run` owns the callback server for the whole login, which lets it
retry **once** — and only once — when the authorization callback comes back
`error=invalid_scope`: it re-runs the browser leg with the `scope` parameter
omitted, so the server applies its own default grant. The retry reuses the same
listener, port, redirect URI and `client_id` (the redirect URI is pinned by the
registration) but generates a fresh `state` nonce and PKCE pair, so a late
callback from the first attempt fails closed as a state mismatch. A stderr
notice precedes the second browser open. Any other callback error fails on the
first attempt.

`credentials.json` records the scopes the successful attempt actually carried,
not the ones negotiated — so after a retry the entry stores none. That is what
makes the next login register a fresh client instead of replaying an
authorization request this server is known to refuse.

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
