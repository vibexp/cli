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

API keys are the primary, always-available auth method (works on every
self-hosted deployment). The key is read from a hidden prompt or stdin — never
as a command-line argument (which would leak into shell history):

```bash
# Interactive (hidden prompt) or piped:
vibexp auth login --with-api-key
printf '%s' "$MY_KEY" | vibexp auth login --with-api-key

vibexp auth status     # method + identity + key fingerprint (never the secret)
vibexp auth logout     # remove the active context's stored credential
```

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
