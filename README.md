# vibexp

Cross-platform command-line interface for the [VibeXP](https://vibexp.io) platform.

> Status: early development. This is the repository skeleton (issue #3 of epic #2);
> commands are landing issue by issue.

## Install

```bash
go install github.com/vibexp/cli/cmd/vibexp@latest
```

Or build from source:

```bash
make build      # -> bin/vibexp
```

## Usage

The CLI follows a `vibexp <noun> <verb>` grammar (gh/kubectl-style):

```bash
vibexp --help
vibexp version

# Contexts (kubectl-style; stored in ~/.vibexp/config.yaml)
vibexp config set-context dev --base-url https://your-deployment.example --team my-team
vibexp config use-context dev
vibexp config get-contexts
vibexp config current-context
```

### Authentication

Two methods are supported. **Browser login** (default) is best for interactive
use; **API keys** are best for CI/scripts and work on every deployment.

```bash
# Interactive OAuth 2.1 browser login (opens your browser):
vibexp auth login

# API key — read from a hidden prompt or piped, never a CLI argument:
vibexp auth login --with-api-key
printf '%s' "$MY_KEY" | vibexp auth login --with-api-key

vibexp auth status     # method + identity + credential fingerprint (never the secret)
vibexp auth logout     # remove the active context's stored credential
```

`vibexp auth login` performs the OAuth 2.1 authorization-code + PKCE flow
(RFC 8414 discovery → RFC 7591 dynamic client registration → loopback callback →
RFC 8707 resource-indicated token exchange) and stores rotating tokens. Access
tokens are refreshed transparently on expiry; concurrent invocations are
serialized by a file lock so exactly one refresh happens.

On a **headless/SSH** host (no browser), or a deployment **without an OAuth
server**, or one whose REST API doesn't accept browser-login tokens, login fails
fast (exit `4`) and points you at `--with-api-key`.

Credentials are stored per context in `~/.vibexp/credentials.json` (0600),
separate from config. For CI/scripts, set `VIBEXP_API_KEY` and skip login
entirely — it takes **precedence over stored credentials**:

```bash
export VIBEXP_API_KEY="…"   # env > stored
vibexp auth status
```

An invalid or expired key exits `4` with the server's error detail (and request
id). The full secret never appears in `auth status`, logs, or error messages.

### Global flags

| Flag | Env | Purpose |
| --- | --- | --- |
| `--context` | `VIBEXP_CONTEXT` | Select the active context |
| `--team` | `VIBEXP_TEAM` | Team id or slug |
| `--project` | `VIBEXP_PROJECT` | Project id or slug |
| `--format` | `VIBEXP_FORMAT` | Output format (wired in a later issue) |
| `--debug` | — | Mirror debug logs to stderr |
| `--timeout` | — | Per-request timeout |

Base URL can also be overridden with `VIBEXP_BASE_URL`. Resolution precedence
everywhere is **flag > env > active context**.

## Output & scripting

Output is **TTY-aware**: a terminal gets an aligned, colored table; a pipe gets
tab-separated values (no color, no header) — so `vibexp … | cut -f1` just works.

| `--format` | On a terminal | Piped |
| --- | --- | --- |
| *(default)* | aligned color table | TSV |
| `table` | aligned color table | aligned table (no color) |
| `text` | TSV | TSV |
| `json` | **raw API response body, byte-for-byte** | same |
| `yaml` | faithful YAML of the JSON body | same |

`--format` (or `VIBEXP_FORMAT`) always overrides TTY detection. `NO_COLOR=1`
(or `TERM=dumb`) strips all color.

**`--jq <expr>`** filters output with a built-in [gojq](https://github.com/itchyny/gojq)
engine (no external `jq` needed):

```bash
vibexp … --jq '.items[].id'          # newline-delimited JSON values
vibexp … --jq '.items | length'
vibexp … --format=yaml --jq '.items[0]'   # jq result as YAML
```

`--jq` forces JSON output (or YAML with `--format=yaml`); a bad expression exits
`2`. **stdout carries only data** — all status/progress/errors go to stderr, so
`vibexp … > out.json` is always clean, parseable output.

## Quickstart

```bash
vibexp config set-context dev --base-url https://your-deployment.example
vibexp auth login                      # or: vibexp auth login --with-api-key
vibexp whoami                          # who am I?
vibexp team list                       # teams I belong to
vibexp project list --team acme        # projects in a team (or set a default team on the context)
```

All three honor `--format=json|yaml|table|text`, `--jq`, piped TSV, and the
pagination flags `--limit` / `--page` / `--offset`. Adding a new resource
command is mechanical — see [docs/adding-commands.md](docs/adding-commands.md).

### Memories

Full CRUD over memories (team-scoped; content via `--body-file`):

```bash
vibexp memory list --project my-proj              # filter by project; +pagination flags
echo "a useful note" | vibexp memory create --project my-proj --body-file -
vibexp memory get <id>
vibexp memory update <id> --status archived       # or --body-file to replace content
vibexp memory delete <id>                         # prompts on a TTY; --yes for scripts
```

`delete` refuses to run non-interactively without `--yes` (exit 2). Every verb
honors `--format`/`--jq`.

### Blueprints

Full CRUD over blueprints — the standing-rules primitive. Blueprints are
addressed by `(project, slug)`, so every single-item verb resolves a project
(`--project`, `VIBEXP_PROJECT`, or the active context):

```bash
vibexp blueprint list --project my-proj                      # +pagination flags
vibexp blueprint create rules --project my-proj \
  --title "House rules" --body-file rules.md                 # --body-file <path|->
vibexp blueprint get rules --project my-proj
vibexp blueprint update rules --project my-proj --body-file rules.md
vibexp blueprint delete rules --project my-proj              # --yes for scripts
```

### Prompts

Full CRUD over the reusable-prompt library (team-scoped, addressed by slug),
plus `render`:

```bash
vibexp prompt list --project my-proj                         # filter by project
vibexp prompt create greet --project my-proj \
  --name "Greeting" --body-file greet.tmpl
vibexp prompt get greet
vibexp prompt update greet --name "Greeting v2"
vibexp prompt delete greet --yes
```

`prompt render` substitutes repeatable `--var key=value` pairs and prints the
rendered text raw to stdout — pipe-safe, no decoration — so it drops straight
into a shell pipeline or an AI-tool hook:

```bash
vibexp prompt render greet --var env=prod --var region=eu | your-tool
vibexp prompt render greet --var env=prod --format json      # full API envelope
```

A missing required variable surfaces the API's validation error with field
detail (exit 1); on a duplicate `--var` key the last value wins.

## Escape hatch: `vibexp api`

Reach any of the API's endpoints directly (like `gh api`), with the active
context's base URL, auth, retries, and the output engine applied:

```bash
vibexp api GET /api/v1/{team}/memories            # {team} resolves like curated commands
vibexp api GET '/api/v1/{team}/memories?limit=20' --jq '.items[].id'
vibexp api POST /api/v1/{team}/memories --input body.json
echo '{"text":"hi"}' | vibexp api POST /api/v1/{team}/memories --input -
vibexp api DELETE /api/v1/{team}/memories/123
vibexp api GET /api/v1/{team}/memories --paginate  # merge every page into one JSON array
```

- `--input <file>` or `--input -` (stdin) sends a body (`Content-Type:
  application/json` by default). `--header 'Key: Value'` (repeatable) overrides.
- Paths are server-relative; `{team}` substitutes the resolved team (flag > env >
  context). `--paginate` (GET only) walks `page`/`limit` until a short page and
  emits the union of items.
- Exit codes and RFC 7807 errors (with `request_id`) are identical to curated
  commands.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | API / runtime error |
| `2` | Usage error (bad flags/args) |
| `4` | Authentication error |

## Logging

Every invocation appends structured JSON-lines logs to `~/.vibexp/logs/cli.log`
(5 MB × 3 rotation). Credentials are redacted at the logger and can never be
written. `--debug` mirrors logs to stderr.

## Development

```bash
make lint     # golangci-lint (v2)
make test     # go test -race ./...
make build    # ldflags-stamped binary
```

See [docs/architecture.md](docs/architecture.md) for the canonical package
layout every command follows.
