#!/usr/bin/env bash

# ai-dev-workflow managed Claude OAuth usage fetcher.
# Fetches real utilization % from the Anthropic OAuth API and writes
# ~/.claude/usage-status.json. macOS only (requires Keychain via security(1)).
#
# Usage:
#   claude-fetch-usage.sh            # poll loop (TTL-based, default 300s)
#   claude-fetch-usage.sh --once     # single fetch and exit
#   claude-fetch-usage.sh --if-stale # fetch only if cache is older than TTL

set -uo pipefail

USAGE_CACHE_FILE="${CLAUDE_USAGE_CACHE:-$HOME/.claude/usage-status.json}"
TTL="${CLAUDE_USAGE_TTL:-300}"
API_URL="https://api.anthropic.com/api/oauth/usage"

# ---------------------------------------------------------------------------
# Guards
# ---------------------------------------------------------------------------

if [[ "$(uname)" != "Darwin" ]]; then
  echo "claude-fetch-usage.sh: macOS only (requires Keychain). Exiting." >&2
  exit 1
fi

for _cmd in jq curl security; do
  if ! command -v "$_cmd" >/dev/null 2>&1; then
    echo "claude-fetch-usage.sh: required command not found: $_cmd" >&2
    exit 1
  fi
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

get_token() {
  local blob
  blob="$(security find-generic-password -s "Claude Code-credentials" -w 2>/dev/null)" || return 1
  local token
  token="$(jq -r '.claudeAiOauth.accessToken // empty' 2>/dev/null <<<"$blob")" || return 1
  [[ -n "$token" ]] || return 1
  printf '%s' "$token"
}

fetch_usage() {
  local token="$1"
  local response http_code body
  # curl outputs body then http_code on the last line via -w
  response="$(curl -sf --max-time 10 \
    -w "\n%{http_code}" \
    -H "Authorization: Bearer ${token}" \
    -H "anthropic-beta: oauth-2025-04-20" \
    -H "anthropic-version: 2023-06-01" \
    "$API_URL" 2>/dev/null)" || return 1

  http_code="${response##*$'\n'}"
  body="${response%$'\n'*}"

  if [[ "$http_code" != "200" ]]; then
    echo "claude-fetch-usage.sh: API returned HTTP ${http_code}" >&2
    return 1
  fi

  printf '%s' "$body"
}

write_cache() {
  local response_json="$1"
  local tmp_file="${USAGE_CACHE_FILE}.tmp"

  local five_pct five_resets seven_pct seven_resets updated_at
  five_pct="$(jq -r '.five_hour.utilization // empty' 2>/dev/null <<<"$response_json")"
  five_resets="$(jq -r '.five_hour.resets_at // empty' 2>/dev/null <<<"$response_json")"
  seven_pct="$(jq -r '.seven_day.utilization // empty' 2>/dev/null <<<"$response_json")"
  seven_resets="$(jq -r '.seven_day.resets_at // empty' 2>/dev/null <<<"$response_json")"

  # Require at least the 5h utilization; if missing the response shape changed
  [[ -n "$five_pct" ]] || { echo "claude-fetch-usage.sh: unexpected API response shape" >&2; return 1; }

  updated_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

  mkdir -p "$(dirname "$USAGE_CACHE_FILE")" 2>/dev/null || true

  jq -n \
    --arg source "oauth" \
    --arg usage_percentage "${five_pct}" \
    --argjson used_tokens 0 \
    --argjson limit_tokens 0 \
    --argjson remaining_tokens 0 \
    --arg eta "" \
    --arg daily_reset_at "${five_resets}" \
    --arg weekly_reset_at "${seven_resets:-}" \
    --arg five_hour_pct "${five_pct}" \
    --arg seven_day_pct "${seven_pct:-}" \
    --arg updated_at "$updated_at" \
    '{
      source: $source,
      usage_percentage: $usage_percentage,
      used_tokens: $used_tokens,
      limit_tokens: $limit_tokens,
      remaining_tokens: $remaining_tokens,
      eta: $eta,
      daily_reset_at: $daily_reset_at,
      weekly_reset_at: $weekly_reset_at,
      five_hour_pct: $five_hour_pct,
      seven_day_pct: $seven_day_pct,
      updated_at: $updated_at
    }' > "$tmp_file" 2>/dev/null || return 1

  mv "$tmp_file" "$USAGE_CACHE_FILE" 2>/dev/null || return 1
}

run_once() {
  local token response
  token="$(get_token)" || { echo "claude-fetch-usage.sh: could not read OAuth token from Keychain" >&2; return 1; }
  response="$(fetch_usage "$token")" || return 1
  write_cache "$response" || return 1
}

cache_is_fresh() {
  [[ -f "$USAGE_CACHE_FILE" ]] || return 1
  local updated_at cache_epoch now_epoch
  updated_at="$(jq -r '.updated_at // empty' "$USAGE_CACHE_FILE" 2>/dev/null)"
  [[ -n "$updated_at" ]] || return 1
  cache_epoch="$(date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$updated_at" '+%s' 2>/dev/null)" || return 1
  now_epoch="$(date -u '+%s')"
  (( now_epoch - cache_epoch < TTL ))
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if [[ "${1:-}" == "--once" ]]; then
  run_once
  exit $?
fi

if [[ "${1:-}" == "--if-stale" ]]; then
  cache_is_fresh && exit 0
  run_once
  exit $?
fi

while true; do
  run_once || true
  sleep "$TTL"
done
