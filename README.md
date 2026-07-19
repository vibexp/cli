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
