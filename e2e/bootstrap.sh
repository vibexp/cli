#!/usr/bin/env bash
# Bootstrap a throwaway API key from a freshly-booted ephemeral VibeXP stack
# (see e2e/docker-compose.yml). Requires curl + jq.
#
#   key=$(bash e2e/bootstrap.sh [base-url])
#
# Prints ONLY the key to stdout (callers mask it before exporting); all status
# goes to stderr. The flow is dev-login (enabled because the stack runs in
# local-development mode) → wait for the event-driven default team → create an
# API key, whose plaintext full_key is returned exactly once.
set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
COOKIES="$(mktemp)"
trap 'rm -f "$COOKIES"' EXIT

log() { echo "bootstrap: $*" >&2; }

# 1) Dev login → session cookie. The email is arbitrary; the user is created
#    on first login.
curl -fsS -c "$COOKIES" -H 'Content-Type: application/json' \
  -d '{"email":"cli-e2e@vibexp.test","name":"CLI E2E"}' \
  "$BASE_URL/api/v1/auth/dev/login" >/dev/null
log "dev login ok"

# 2) The default team (and project) are created by the user.created event
#    listener — asynchronous, so poll until the team is visible.
teams=0
for i in $(seq 1 30); do
  teams=$(curl -fsS -b "$COOKIES" "$BASE_URL/api/v1/teams" |
    jq -r 'if type == "array" then length else (.teams // .items // .data // []) | length end')
  [ "$teams" -ge 1 ] && break
  sleep 1
done
if [ "$teams" -lt 1 ]; then
  log "default team never appeared after 30s"
  exit 1
fi
log "default team present"

# 3) Create the API key. full_key is the plaintext credential — printed to
#    stdout and nowhere else.
key=$(curl -fsS -b "$COOKIES" -H 'Content-Type: application/json' \
  -d '{"name":"cli-e2e","integration_codes":["cli"]}' \
  "$BASE_URL/api/v1/api-keys" | jq -r '.full_key // empty')
if [ -z "$key" ]; then
  log "API key creation returned no full_key"
  exit 1
fi
log "api key created"
printf '%s\n' "$key"
