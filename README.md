# vibexp

Cross-platform command-line interface for the [VibeXP](https://vibexp.io) platform.

> Status: early development. This is the repository skeleton (issue #3 of epic #2);
> commands are landing issue by issue.

## Install

| Method | Command | Upgrades via |
| --- | --- | --- |
| **Homebrew** (macOS) | `brew install vibexp/tap/vibexp` | `brew upgrade vibexp` |
| **Binary download** (Linux · macOS · Windows) | archive from [Releases](https://github.com/vibexp/cli/releases/latest) | `vibexp update` |
| **`go install`** | `go install github.com/vibexp/cli/cmd/vibexp@latest` | re-run with `@latest` |

### Homebrew (macOS)

```bash
brew install vibexp/tap/vibexp
vibexp version            # source: brew
```

### Binary download (Linux · macOS · Windows)

Download the archive for your OS/arch from the [latest release](https://github.com/vibexp/cli/releases/latest)
(`vibexp_<version>_<os>_<arch>.tar.gz`, or `.zip` on Windows), verify it against
`checksums.txt`, extract, and put `vibexp` on your `PATH`:

```bash
VERSION=X.Y.Z; OS=linux; ARCH=amd64          # e.g. VERSION=0.1.0
base=https://github.com/vibexp/cli/releases/download/v${VERSION}
curl -sSLO "${base}/vibexp_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -sSLO "${base}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "vibexp_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo install vibexp /usr/local/bin/vibexp
vibexp version            # source: binary — self-updates via `vibexp update`
```

Binaries installed this way self-update in place with `vibexp update`.

### go install

```bash
go install github.com/vibexp/cli/cmd/vibexp@latest    # or @vX.Y.Z
```

Requires no cgo and no build-time codegen; the version is reported from the
module build info (`debug.ReadBuildInfo`). Upgrade by re-running with `@latest`.

Or build from source:

```bash
make build                # -> bin/vibexp
```

Every release archive also bundles shell completions and man pages — see
[Shell completions & man pages](#shell-completions--man-pages).

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

The scope requested on the authorization request is **negotiated** against the
server's advertised `scopes_supported` (from RFC 8414 discovery) — the CLI only
requests scopes the server actually offers, so login works across deployments
rather than only the default embedded authorization server. For a deployment
whose authorization server uses non-standard scope names, override negotiation
with the repeatable `--scope` flag or `VIBEXP_OAUTH_SCOPE` (space/comma
separated; flag > env):

```bash
vibexp auth login --scope openid --scope profile
VIBEXP_OAUTH_SCOPE="openid profile" vibexp auth login
```

Browser login also works against the REST API (`/api/v1`) — **by default** on
platform v0.8.0+ deployments running the embedded authorization server, which
auto-wire REST to accept these tokens. On a **headless/SSH** host (no browser), a
deployment **without an OAuth server**, or the exception of one whose REST layer
isn't wired to accept browser-login tokens (an external IdP, or `api_oauth`
disabled), login fails fast (exit `4`) and points you at `--with-api-key`.

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
vibexp blueprint get rules --project my-proj                 # detail view: +PATH, source repo/commit
vibexp blueprint update rules --project my-proj --body-file rules.md
vibexp blueprint delete rules --project my-proj              # --yes for scripts
```

The single-blueprint detail view (get/create/update) surfaces the v0.8.0
file-fidelity fields — the canonical `path` and, for imported blueprints, the
import provenance (`source.repo` / short `source.commit_sha`); `list` stays
compact.

Any resource `get` (memory · blueprint · prompt · artifact) accepts
`--show-relations`, which prints a compact summary of the v0.8.0 read-time
neighborhood to **stderr** — the typed `related` edges and embedding-`similar`
resources — leaving stdout (and `--format=json`, which always carries the full
arrays) untouched:

```bash
vibexp memory get <id> --show-relations
# stdout: the memory row
# stderr: related (2): outgoing governed-by blueprint "House rules"; …
#         similar (1): memory "Why pgvector" (0.82)
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

### Artifacts

Full CRUD over artifacts — the polished-output store. Artifacts are
project-scoped (addressed by `(project, slug)`), so every single-item verb
resolves a project (`--project`, `VIBEXP_PROJECT`, or the active context; missing
→ exit 2):

```bash
vibexp artifact list --project my-proj                       # +pagination flags
vibexp artifact create report --project my-proj \
  --title "Build report" --body-file report.md               # --body-file <path|->
vibexp artifact get report --project my-proj
vibexp artifact update report --project my-proj --body-file report.md
vibexp artifact delete report --project my-proj              # --yes for scripts
```

### Feeds

Read and post to team feeds — the CLI equivalent of the MCP `post_to_feed` /
`reply_to_feed_item` tools, so CI pipelines and agents can post status:

```bash
vibexp feed list                                             # feeds in the team
vibexp feed items --feed <feed-id>                           # +pagination flags
vibexp feed get-item <item-id>                               # item + its replies
vibexp feed post "deploy #1234 succeeded" --feed <feed-id> --title "Deploy"
echo "see logs" | vibexp feed post --feed <feed-id> --title "Deploy" --body-file -
vibexp feed reply <item-id> "thanks!"                        # or --body-file <path|->
```

Message content comes from a positional argument or `--body-file` (a path, or
`-` for stdin) — supply one, not both. `--author` (default `vibexp-cli`) sets the
AI-assistant name recorded on the item/reply. `get-item` lists replies in
table/text output; `--format=json` prints the raw item.

### Search

Semantic search across the team's prompts, artifacts, blueprints, and memories:

```bash
vibexp search "retry backoff"                                # all types
vibexp search "retry backoff" --type prompts --type memories # narrow by type
vibexp search "auth" --project my-proj --format json         # scope + raw envelope
```

### Attachments

Upload files as attachments, streamed as `multipart/form-data` so memory stays
bounded regardless of file size. Attachments are owned by a resource, so
`upload`/`list` take `--owner-id` (required) and `--owner-type` (default
`artifact`):

```bash
vibexp attachment upload ./build.log --owner-id <artifact-id>   # content type detected
vibexp attachment upload ./data --owner-id <id> --content-type application/json
vibexp attachment list --owner-id <artifact-id>                 # +pagination flags
vibexp attachment delete <attachment-id>                        # --yes for scripts
```

The content type is detected from the file (extension, then a content sniff) and
can be overridden with `--content-type`. A progress indicator is shown only on a
terminal, so piped output stays clean.

### Relations

Typed, directed edges between resources (artifact · memory · prompt · blueprint)
— the knowledge-graph primitive (platform v0.8.0). `list` shows the relations
touching a resource, in both directions:

```bash
vibexp relations list blueprint <blueprint-id>          # both directions; +pagination flags
vibexp relations create \
  --from-type artifact --from-id <id> \
  --to-type blueprint  --to-id <id> \
  --relation-type governed-by --origin human            # idempotent; 200 if the edge exists
vibexp relations confirm <relation-id>                  # promote suggested → confirmed
vibexp relations delete <relation-id>                   # --yes for scripts
vibexp relations seed                                   # AI-propose edges from similarity (background)
```

Relation types are `governed-by` · `built-from` · `explained-by` · `supersedes`;
origin is `human` or `ai`; status is `suggested` or `confirmed`.

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

## Updating

`vibexp` checks for a newer release in the background — at most once every 24h,
cached in `~/.vibexp/state.json`, never blocking or failing your command. When a
newer version exists it prints a single notice to stderr (stdout is never
touched, so pipelines are unaffected).

```bash
vibexp update           # download, checksum-verify, and atomically self-replace
vibexp update --check   # report whether an update is available, without installing
```

`vibexp update` only self-replaces binaries installed from GitHub Releases;
Homebrew and `go install` builds refuse to self-update and print the correct
command instead (`brew upgrade vibexp` / `go install …@latest`). The download is
verified against the release `checksums.txt` before the swap — a mismatch aborts
and leaves the current binary untouched.

**Privacy & opt-out:** the check contacts only `api.github.com` and sends no
identifying data beyond the HTTP request. It is automatically disabled in CI
(`CI` is set) and can be turned off anywhere with `VIBEXP_NO_UPDATE_CHECK=1`.

## Logging

Every invocation appends structured JSON-lines logs to `~/.vibexp/logs/cli.log`
(5 MB × 3 rotation). Credentials are redacted at the logger and can never be
written. `--debug` mirrors logs to stderr.

## Shell completions & man pages

`vibexp` generates its own completion scripts for **bash, zsh, fish, and
powershell**:

```bash
vibexp completion bash > /etc/bash_completion.d/vibexp         # bash
vibexp completion zsh  > "${fpath[1]}/_vibexp"                 # zsh
vibexp completion fish > ~/.config/fish/completions/vibexp.fish
```

Run `vibexp completion --help` for the exact per-shell instructions. The
**Homebrew** cask installs completions and a man page automatically. The
**binary-download** archives bundle `completions/` (all four shells) and
`manpages/` (`man vibexp`, plus one page per subcommand) for manual
installation.

## Development

```bash
make lint     # golangci-lint (v2)
make test     # go test -race ./...
make build    # ldflags-stamped binary
```

Releases are automated with [goreleaser](https://goreleaser.com); see
[docs/releasing.md](docs/releasing.md) for the release process, and
[docs/architecture.md](docs/architecture.md) for the canonical package layout
every command follows.
