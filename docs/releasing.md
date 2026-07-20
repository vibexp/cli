# Releasing

Releases are fully automated with [goreleaser](https://goreleaser.com) and the
`.github/workflows/release.yml` workflow. Cutting a release is a single tag push.

## Cut a release

```bash
# from an up-to-date main
git tag v0.1.0            # semver, v-prefixed; the v0.x line is pre-1.0
git push origin v0.1.0
```

The `Release` workflow then, on the `v*` tag:

1. builds the six target binaries (`linux/darwin/windows × amd64/arm64`,
   `CGO_ENABLED=0`) with version/commit/date/`InstallSource` stamped via ldflags,
2. generates shell completions + man pages (`go run ./cmd/docgen`),
3. archives them (`vibexp_<version>_<os>_<arch>.tar.gz`, `.zip` on Windows) with
   completions, man pages, and the README bundled in,
4. writes `checksums.txt`,
5. builds a changelog grouped from the conventional-commit history,
6. publishes the **GitHub Release**, and
7. updates the **Homebrew cask** in [`vibexp/homebrew-tap`](https://github.com/vibexp/homebrew-tap).

No manual steps. To preview locally without publishing:

```bash
goreleaser release --snapshot --clean     # writes ./dist, uploads nothing
goreleaser check                          # validate .goreleaser.yaml
```

The `release.yml` workflow also runs `goreleaser check` + a snapshot build on any
pull request that touches the release config, so breakage is caught before merge.

## Archive / install-source contract

Two build sets ship every release, on purpose:

- **canonical** — `vibexp_<version>_<os>_<arch>.{tar.gz,zip}`, built with
  `InstallSource=binary`. These are what `vibexp update` self-replaces and what
  the self-updater (`internal/update`) matches by name.
- **homebrew** — `vibexp_homebrew_<version>_<os>-<arch>.tar.gz` (darwin only),
  built with `InstallSource=brew` so `vibexp version` reports `source: brew` and
  upgrades route to `brew upgrade`. A single build can't vary ldflags per
  distribution, hence the second build. The distinct name (note `_<os>-` rather
  than `_<os>_`) keeps the self-updater from ever selecting it — locked by
  `internal/update/contract_test.go`.

Changing either the archive `name_template` in `.goreleaser.yaml` or the matcher
in `internal/update/apply.go` **must** keep that contract test green.

## Homebrew tap credentials (one-time maintainer setup)

goreleaser pushes the cask to the separate `vibexp/homebrew-tap` repo. The
release job authenticates that push with a **short-lived GitHub App installation
token** (best practice over a long-lived PAT), while the GitHub Release itself is
created with the workflow's built-in `GITHUB_TOKEN`.

Required once:

- A GitHub App with **Repository → Contents: Read and write**, **installed on
  `vibexp/homebrew-tap`**.
- Two repository secrets on `vibexp/cli`:
  - `VIBEXP_BOT_CLIENT_ID` — the App's App ID or Client ID
  - `VIBEXP_BOT_PRIVATE_KEY` — the App's private key (`.pem`)

`release.yml` mints the token with `actions/create-github-app-token`, scoped to
`homebrew-tap` alone. If these secrets are absent the release still publishes the
GitHub artifacts; only the cask push fails.

## Verify a release

```bash
brew install vibexp/tap/vibexp && vibexp version          # source: brew
go install github.com/vibexp/cli/cmd/vibexp@v0.1.0 && vibexp version
vibexp update --check                                     # binary-download builds
```
