# Running the e2e suite

The e2e suite (`e2e/`, build tag `e2e`) drives the **compiled `vibexp` binary**
against a live VibeXP deployment. It is the automated form of the project's
staging-verification policy: env-var auth, `whoami`/`team list`, one full
memory CRUD lifecycle, `vibexp api` GET + `--paginate`, output contracts
(`--format=json`, TSV piping, `--jq`), and exit-code assertions (0/2/4).

The target deployment is addressed **only** through two environment variables,
consumed by reference — never print or commit their values:

- `VIBEXP_CLI_TEST_URL` — base URL of the deployment
- `VIBEXP_CLI_TEST_API_KEY` — an API key for it

With both set, `make e2e` runs the suite; with either absent, the suite
**skips cleanly** (0 failures), so checkouts without a deployment stay green.

```sh
make e2e
```

Every resource the suite creates is namespaced `cli-e2e-<run-id>` and deleted
again — per-test cleanup plus a final sweep that also removes `cli-e2e-*`
leftovers older than an hour from previous crashed runs.

## Against an ephemeral local stack (what CI does)

No deployment handy? Boot the published platform release locally — this is
exactly what the CI `e2e` job does (no repository secrets involved):

```sh
make e2e-stack-up                                # Postgres + ghcr.io/vibexp/vibexp
key=$(bash e2e/bootstrap.sh)                     # dev login → throwaway API key
VIBEXP_CLI_TEST_URL=http://localhost:8080 \
VIBEXP_CLI_TEST_API_KEY="$key" make e2e
make e2e-stack-down
```

The Postgres pin (`pgvector/pgvector:pg17` in `e2e/docker-compose.yml`) tracks
the platform's own `docker-compose.yml` and must move with it. This stack is the
compatibility harness the platform's `release.yml` dispatches against every
published image, so it only certifies anything while it runs the database
version self-hosters actually deploy.

`VIBEXP_E2E_IMAGE` overrides the platform image (default
`ghcr.io/vibexp/vibexp:latest`; CI pins the latest release tag, and the
workflow's `platform_image_tag` dispatch input overrides it — the escape hatch
when CLI `main` needs a platform build newer than the latest release).

`e2e/bootstrap.sh` needs `curl` and `jq`. It works because the stack runs in
local-development mode (localhost `FRONTEND_BASE_URL`), which enables
`/api/v1/auth/dev/login`; the default team/project are created asynchronously
on first login, and the script waits for them before minting the key.

## Suite expectations

- The key's user must have at least one team with at least one project; the
  suite uses the first of each (the ephemeral stack's bootstrap user gets both
  automatically).
- Tests isolate `$HOME`, so your real `~/.vibexp` config is never read.
- Failure output is truncated and the credential redacted; keep it that way
  when adding tests (`requireCode`/`redact` helpers).
