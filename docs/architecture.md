# Architecture

This document records the canonical package layout and conventions established
by the foundation (issue #3). Every later command plugs into this skeleton.

## Grammar

`vibexp <noun> <verb>` (gh/kubectl-style). Curated commands per noun plus a raw
`vibexp api <METHOD> <path>` passthrough (later issues).

## Package layout

```
cmd/vibexp/                 entrypoint; runs the root command, maps errors -> exit codes
cmd/docgen/                 BUILD-TIME ONLY: generates the shell completions and man
                            pages bundled into release archives. Never linked into the
                            binary, which is what keeps `go install .../cmd/vibexp`
                            free of the cobra/doc + go-md2man dependencies.

internal/cli/               cobra command tree
  root.go                   root command, global flags, persistent pre-run. The only
                            non-test .go file at this level.
  resource/                 shared list/pagination/confirm helpers every curated noun
                            builds on (see docs/adding-commands.md)
  apicmd/                   vibexp api <METHOD> <path> — raw passthrough for everything
                            the curated nouns do not cover
  artifactcmd/              vibexp artifact list|create|get|update|delete
  attachmentcmd/            vibexp attachment list|upload|delete
  authcmd/                  vibexp auth login|logout|status (API key + OAuth PKCE)
  blueprintcmd/             vibexp blueprint list|create|get|update|delete
  configcmd/                vibexp config set-context|use-context|get-contexts|
                            current-context
  feedcmd/                  vibexp feed list|post|items|get-item|reply
  memorycmd/                vibexp memory list|create|get|update|delete
  metadatacmd/              vibexp metadata keys|values — discovery backing the
                            --metadata key=value list filter
  projectcmd/               vibexp project list
  promptcmd/                vibexp prompt list|create|get|update|delete|render
  relationcmd/              vibexp relations list|create|confirm|delete|seed — the
                            plural noun, added with platform v0.8.0 relations
  searchcmd/                vibexp search <query>
  teamcmd/                  vibexp team list
  updatecmd/                vibexp update (self-replace)
  usercmd/                  vibexp whoami
  versioncmd/               vibexp version

internal/api/               client factory over api-client-go: Doer, RFC 7807 mapper,
                            team/project resolution, multipart streamer (see below)
internal/clictx/            carries the resolved runtime + logger on a context.Context
                            so command packages never import the root cli package —
                            that import would be a cycle
internal/config/            named-context store (koanf) + precedence resolution
internal/cred/              credential store (0600 credentials.json, atomic writes)
internal/exitcode/          exit-code constants + typed CodedError
internal/logging/           always-on JSON-lines file logger + rotation + redaction
internal/oauth/             PKCE flow: discovery, DCR, callback server, refresh, flock
internal/output/            renderer: table/TSV/json/yaml, --jq, TTY detection
internal/update/            version check, install-source provenance, self-update
internal/version/           ldflags-injected build metadata
```

Keep this block matched to the tree — `ls -d cmd/*/ internal/*/ internal/cli/*/`
should turn up nothing that is not listed here, and nothing here that is not on
disk. A PR that adds a package updates this block in the same PR.

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
