# VibeXP CLI (`vibexp`)

Cross-platform Go CLI for the VibeXP platform (open-source, self-hosted deployments).
Scripting-first, gh/gcloud-grade UX. This file front-loads the full design context —
read it before picking up any issue.

## Where everything is defined

v1 has shipped (epic #2 is closed). **The code and `docs/` are the source of truth** —
this file front-loads the architecture-level facts an agent needs before touching
anything; go to the code for detail, not to issue bodies.

- `docs/architecture.md` — package layout, client layer, flag precedence, exit codes.
- `docs/adding-commands.md` — how to add a curated resource command.
- `docs/e2e.md` — the e2e suite and its stack.
- `docs/releasing.md` — goreleaser + the release workflow.
- `README.md` is **user-facing** (install, usage, scripting recipes). This file is
  agent-facing. Keep them disjoint: don't restate install instructions here, and don't
  put internal conventions there.
- Related repos: `github.com/vibexp/vibexp` (the platform + OpenAPI spec at
  `backend/openapi.yaml`), `github.com/vibexp/api-client-go` (generated Go client this
  CLI consumes — oapi-codegen v2, committed `*.gen.go`, semver v0.x tags).

## Settled architecture (do not re-litigate; change via the epic)

- **Grammar:** `vibexp <noun> <verb>` (e.g. `vibexp memory list`). Curated commands for
  memories, blueprints, prompts (incl. `prompt render`), artifacts, feeds, search,
  attachments, metadata (`vibexp metadata keys|values` — discovery backing the
  `--metadata key=value` list filter on memories/artifacts/blueprints; memory list
  also has `--tags`), relations (`vibexp relations` — note the plural, the only one),
  whoami/teams/projects — plus `vibexp api <METHOD> <path>` raw passthrough for
  everything else. `vibexp --help` is the authoritative surface.
- **Contexts:** multi-context (kubectl-style) in `~/.vibexp/config.yaml`; credentials
  separately in `~/.vibexp/credentials.json` (0600, atomic writes). Precedence everywhere:
  flag > env > active context.
- **Auth:** API key primary (`VIBEXP_API_KEY`; server reserves `acli-`/`vxk_` bearer
  prefixes) + interactive OAuth 2.1 PKCE (`vibexp auth login`): RFC 8414 discovery →
  RFC 7591 dynamic client registration (public client, loopback
  `http://127.0.0.1:<port>/callback`) → auth-code + PKCE S256 → RFC 8707 `resource`
  indicator. Rotating refresh tokens (access ~15m, refresh ~30d); refresh serialized via
  file lock. **No device-code flow exists server-side.** As of platform v0.8.0,
  deployments running the embedded Authorization Server auto-wire `api_oauth.issuer`
  (pinning the audience to the MCP resource), so OAuth JWTs work on REST `/api/v1` by
  default; API-key fallback is the exception (an external IdP not wired for REST, or
  `api_oauth` disabled). The CLI still probes real acceptance after login and guides
  to API keys only when a deployment actually rejects the token.
- **Output:** TTY-aware (tables on terminal, TSV piped); `--format=json|yaml|table|text`
  overrides; built-in `--jq` (gojq). **JSON contract = raw API response body** — no CLI
  mapping layer. stdout carries data only; all status/messaging goes to stderr.
- **Errors:** API errors are RFC 7807 problem-details (`ErrorResponse` with `request_id`,
  `validation_errors`) — map to one uniform CLI error, always surface `request_id`.
  Exit codes: `0` ok · `1` API/runtime · `2` usage · `4` auth.
- **Logging:** always-on JSON-lines file log `~/.vibexp/logs/cli.log` (5MB × 3 rotation);
  `--debug` mirrors to stderr; secrets are redacted at the logger — register every
  credential with the redactor.
- **Updates:** cached (24h) GitHub Releases check → one stderr notice; explicit
  `vibexp update` self-replaces (checksum-verified); brew/`go install` provenance refuses
  self-update and prints the right command. `VIBEXP_NO_UPDATE_CHECK=1` and `CI` suppress.
- **Stack:** Go 1.24, cobra, koanf, gojq, goreleaser. `CGO_ENABLED=0` (keeps `go install`
  clean). No telemetry. Targets: linux/darwin/windows × amd64/arm64.
- **Env vars:** `VIBEXP_API_KEY`, `VIBEXP_BASE_URL`, `VIBEXP_CONTEXT`, `VIBEXP_TEAM`,
  `VIBEXP_PROJECT`, `VIBEXP_FORMAT`, `VIBEXP_OAUTH_SCOPE`, `VIBEXP_NO_UPDATE_CHECK`.

## Canonical layout

```
cmd/vibexp/main.go        entrypoint; unwraps typed errors → exit codes
cmd/docgen/               BUILD-TIME ONLY: generates shell completions + man pages
                          for release archives. Never linked into the binary, which
                          keeps `go install .../cmd/vibexp` free of cobra/doc deps.
internal/cli/             cobra commands: root.go + one package per noun
  authcmd/ configcmd/ apicmd/ resource/ (shared list/pagination/confirm helpers)
  usercmd/ teamcmd/ projectcmd/ memorycmd/ metadatacmd/ blueprintcmd/ promptcmd/
  artifactcmd/ feedcmd/ searchcmd/ attachmentcmd/ relationcmd/ updatecmd/ versioncmd/
internal/clictx/          carries the resolved runtime + logger on context.Context so
                          command packages never import the root cli package (cycle)
internal/config/          context store (koanf, ~/.vibexp/config.yaml)
internal/cred/            credential store (0600 credentials.json, fingerprints)
internal/oauth/           PKCE flow: discovery, DCR, callback server, refresh, flock
internal/api/             client factory over api-client-go: Doer (30s timeout,
                          retry 429/5xx idempotent-only, UA), RFC 7807 mapper,
                          team/project resolution, multipart streamer
internal/output/          renderer: table/TSV/json/yaml, --jq, TTY detection
internal/update/          version check, provenance, self-update
internal/logging/         slog JSON + lumberjack rotation + redaction
internal/version/         ldflags: Version, Commit, Date, InstallSource
internal/exitcode/        0/1/2/4 constants + CodedError
e2e/                      //go:build e2e — drives the built binary against staging
```

## API client facts (github.com/vibexp/api-client-go)

- One flat generated client: `vibexp.NewClientWithResponses(baseURL, opts...)`;
  auth via `WithRequestEditorFn` (set `Authorization: Bearer …`), transport via
  `WithHTTPClient`. **No built-in timeout, retry, or token refresh** — all live in
  `internal/api`. Never call the generated client directly from commands; go through
  the factory.
- Responses are `<Op>HTTPResponse` wrappers with per-status typed fields
  (`JSON200`, `ApplicationproblemJSON4xx…`); HTTP errors are NOT Go errors — always run
  responses through the shared `api.Check` mapper.
- Team scoping is in the URL path (`/api/v1/{team_id}/…`); `team_id` accepts UUID **or**
  slug. There is no server-side "current team" — the CLI resolves it locally.
- Pagination: `page`/`limit`/`offset` query params, `page`/`per_page` response metadata.
- Uploads: only raw `…WithBody` variants — build streamed multipart bodies yourself
  (`internal/api/multipart.go`).
- `GET /health` → `{"status":…,"sha":…}` is the server version/compat handle.

## Conventions

- Every command takes `ctx context.Context`; wire `--timeout` through it.
- Gate on team `permissions` arrays, never on `role`.
- Destructive verbs: TTY confirmation prompt; non-interactive requires `--yes` (else exit 2).
- Secrets are never accepted as plain CLI arguments (shell history) — hidden prompt or stdin.
- Table fixtures/test data are always fabricated — never captured from a real deployment.
- New resource commands follow `docs/adding-commands.md`: endpoint + columns only;
  everything else comes from `internal/cli/resource` + `internal/api`.

## Staging verification (required for every issue)

The development environment automatically provides `VIBEXP_CLI_TEST_URL` (staging base
URL) and `VIBEXP_CLI_TEST_API_KEY` (API key). Before closing any issue, exercise the
delivered functionality against staging using them **by reference only** (e.g.
`VIBEXP_API_KEY="$VIBEXP_CLI_TEST_API_KEY"`). Never echo, log, or commit their values.

**This repository is public.** No staging URLs, keys, tokens, or deployment details may
ever appear in code, fixtures, docs, tests, logs, or CI output. E2E resources are
namespaced `cli-e2e-<run-id>` and always cleaned up.

## Workflow

- One issue = one PR. Check the issue's native "blocked by" panel before starting.
- Board: org project "VibeXP" (`https://github.com/orgs/vibexp/projects/3`) — move the
  card In progress when starting, In review at PR time.
- CI must stay green on lint + tests + the 6-target cross-compile matrix.
- `make lint` · `make test` · `make build` (CGO is scoped per recipe: `test`
  runs with cgo for `-race`; `build`/`install` stay `CGO_ENABLED=0`).
- E2E is separate: `make e2e` (build tag `e2e`, `-count=1`, skips cleanly when the
  staging vars are absent), with `make e2e-stack-up` / `e2e-stack-down` for a local
  platform container.
- Standing contracts that outlived their issues — change these in lockstep:
  release asset naming ↔ the `InstallSource` ldflag (`internal/version`), which
  `vibexp update` reads to refuse self-updating a brew/`go install` build;
  the `credentials.json` schema (`internal/cred`) carries both the API key and the
  OAuth token set in one entry per context (keyed by context name);
  `resource.ConfirmDeletion` is the single confirmation path for every resource
  `delete` command — all six of them. (`auth logout` drops local credentials without
  a prompt; it is not routed through this.)

**Maintenance:** this file is hot-loaded into every agent session, so update it in the
same PR whenever an architecture-level fact changes — a new package, a new noun, a
changed convention. Detail belongs in `docs/`; keep this a map.
