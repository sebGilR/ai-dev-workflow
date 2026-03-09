#!/usr/bin/env bash

# ai-dev-workflow managed Claude daily token watcher.
# Reads transcript events from ~/.claude/projects and estimates daily token usage.

set -u

LIMIT="${CLAUDE_TOKEN_LIMIT:-200000}"
INTERVAL=20
PROJECTS_DIR="${HOME}/.claude/projects"
SNAPSHOT_SCRIPT="${HOME}/.claude/save-wip-snapshot.sh"
USAGE_CACHE_FILE="${CLAUDE_USAGE_CACHE:-$HOME/.claude/usage-status.json}"
FETCH_SCRIPT="${HOME}/.claude/claude-fetch-usage.sh"

warn75_sent=0
warn85_sent=0
warn90_sent=0
warn_day=""   # track date to reset flags at midnight
EMERGENCY_FLAG="${HOME}/.claude/.wip-emergency-save"

tokens_today() {
  if [[ ! -d "$PROJECTS_DIR" ]]; then
    echo 0
    return
  fi

  local result
  result="$(find "$PROJECTS_DIR" -type f -name '*.jsonl' 2>/dev/null | while IFS= read -r f; do
    awk -v d="$(date '+%Y-%m-%d')" 'index($0, d) > 0' "$f" 2>/dev/null
  done | jq -Rs '
    split("\n")
    | map(select(length > 0) | (fromjson? // {}))
    | map(
        (.input_tokens // 0)
      + (.output_tokens // 0)
      + (.cache_creation_input_tokens // 0)
      + (.cache_read_input_tokens // 0)
      )
    | add // 0
  ' 2>/dev/null)"
  
  # Validate result is a valid integer; if jq output is corrupted/partial, return 0.
  if [[ "$result" =~ ^[0-9]+$ ]]; then
    echo "$result"
  else
    echo 0
  fi
}

human_time() {
  local seconds="$1"
  if (( seconds <= 0 )); then
    echo "0m"
    return
  fi
  awk -v s="$seconds" 'BEGIN {
    h = int(s / 3600);
    m = int((s % 3600) / 60);
    if (h > 0) { printf "%dh %dm", h, m; }
    else { printf "%dm", m; }
  }'
}

write_usage_cache() {
  local used="$1"
  local remaining="$2"
  local usage_pct="$3"
  local eta="$4"
  local updated_at="$5"

  # Validate numeric inputs before passing to jq --argjson
  local validated_used="$used"
  local validated_remaining="$remaining"

  if ! [[ "$validated_used" =~ ^[0-9]+$ ]]; then
    validated_used="0"
  fi
  if ! [[ "$validated_remaining" =~ ^[0-9]+$ ]]; then
    validated_remaining="0"
  fi

  local tmp_file="${USAGE_CACHE_FILE}.tmp"
  jq -n \
    --arg source "transcript-estimate" \
    --argjson used_tokens "$validated_used" \
    --argjson limit_tokens "$LIMIT" \
    --argjson remaining_tokens "$validated_remaining" \
    --arg usage_percentage "$usage_pct" \
    --arg eta "$eta" \
    --arg updated_at "$updated_at" \
    --arg daily_reset_at "" \
    --arg weekly_reset_at "" \
    '{
      source: $source,
      used_tokens: $used_tokens,
      limit_tokens: $limit_tokens,
      remaining_tokens: $remaining_tokens,
      usage_percentage: $usage_percentage,
      eta: $eta,
      updated_at: $updated_at,
      daily_reset_at: $daily_reset_at,
      weekly_reset_at: $weekly_reset_at
    }' > "$tmp_file" 2>/dev/null \
    && mv "$tmp_file" "$USAGE_CACHE_FILE" 2>/dev/null \
    || { rm -f "$tmp_file" 2>/dev/null || true; }
}

# Returns 0 if the cache file exists and was written within CLAUDE_USAGE_TTL seconds.
cache_fresh() {
  local cache="$1" ttl="${CLAUDE_USAGE_TTL:-300}"
  [[ -f "$cache" ]] || return 1
  local updated_at cache_epoch now_epoch
  updated_at="$(jq -r '.updated_at // empty' "$cache" 2>/dev/null)"
  [[ -n "$updated_at" ]] || return 1
  # Strip fractional seconds (e.g. 2026-03-08T22:18:39.123Z → 2026-03-08T22:18:39Z)
  cache_epoch="$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "${updated_at%%.*}Z" "+%s" 2>/dev/null)" || return 1
  now_epoch="$(date +%s)"
  (( (now_epoch - cache_epoch) < ttl ))
}

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for claude-watch.sh"
  exit 1
fi

mkdir -p "$(dirname "$USAGE_CACHE_FILE")" 2>/dev/null || true

while true; do
  # Reset per-day threshold flags when the calendar date changes.
  today="$(date '+%Y-%m-%d')"
  if [[ "$today" != "$warn_day" ]]; then
    warn75_sent=0
    warn85_sent=0
    warn90_sent=0
    warn_day="$today"
  fi

  oauth_used=0

  if [[ -x "$FETCH_SCRIPT" ]]; then
    # Only call the OAuth fetcher when the cache is stale (respects CLAUDE_USAGE_TTL, default 300s).
    # This prevents hammering the API on every 20s watcher tick.
    if ! cache_fresh "$USAGE_CACHE_FILE"; then
      "$FETCH_SCRIPT" --once 2>/dev/null || true
    fi
    # Use cache if it contains valid OAuth data (fetcher may have just refreshed it or it may
    # still be fresh from a previous poll).
    if jq -e '.source == "oauth"' "$USAGE_CACHE_FILE" >/dev/null 2>&1; then
      usage_pct="$(jq -r '.usage_percentage // empty' "$USAGE_CACHE_FILE" 2>/dev/null)"
      [[ "$usage_pct" =~ ^[0-9.]+$ ]] || usage_pct="0"
      if [[ -t 1 ]]; then
        clear 2>/dev/null || true
        echo "Claude usage monitor (OAuth)"
        echo "Usage %: ${usage_pct}%"
        echo "Updated: $(date '+%Y-%m-%d %H:%M:%S')"
      fi
      oauth_used=1
    fi
  fi

  if (( oauth_used == 0 )); then
    # Fallback: transcript-based estimate
    used="$(tokens_today)"
    [[ "$used" =~ ^[0-9]+$ ]] || used=0

    remaining=$(( LIMIT - used ))
    if (( remaining < 0 )); then remaining=0; fi

    usage_pct="$(awk -v u="$used" -v l="$LIMIT" 'BEGIN {
      if (l <= 0) { print 0; exit }
      p = (u / l) * 100
      if (p > 100) p = 100
      if (p < 0) p = 0
      printf "%.1f", p
    }')"

    now_epoch="$(date +%s)"
    midnight_epoch="$(date -j -f '%Y-%m-%d %H:%M:%S' "$(date '+%Y-%m-%d') 00:00:00" '+%s' 2>/dev/null || date -d "$(date '+%Y-%m-%d') 00:00:00" '+%s' 2>/dev/null || echo "$now_epoch")"
    elapsed=$(( now_epoch - midnight_epoch ))
    if (( elapsed < 1 )); then elapsed=1; fi

    eta="unknown"
    if (( used > 0 )) && (( remaining > 0 )); then
      sec_left="$(awk -v u="$used" -v r="$remaining" -v e="$elapsed" 'BEGIN {
        rate = u / e
        if (rate <= 0) { print 0; exit }
        printf "%d", (r / rate)
      }')"
      eta="$(human_time "$sec_left")"
    elif (( remaining == 0 )); then
      eta="0m"
    fi

    if [[ -t 1 ]]; then
      clear 2>/dev/null || true
      echo "Claude usage monitor"
      echo "Limit: ${LIMIT}"
      echo "Used: ${used}"
      echo "Remaining: ${remaining}"
      echo "Usage %: ${usage_pct}%"
      echo "Estimated remaining: ${eta}"
    fi
    updated_at="$(date '+%Y-%m-%d %H:%M:%S')"
    [[ -t 1 ]] && echo "Updated: ${updated_at}"

    write_usage_cache "$used" "$remaining" "$usage_pct" "$eta" "$updated_at"
  fi

  pct_int="$(awk -v p="$usage_pct" 'BEGIN { printf "%d", p }')"

  if (( pct_int >= 75 )) && (( warn75_sent == 0 )); then
    echo "WARNING: Usage has crossed 75%."
    warn75_sent=1
  fi

  if (( pct_int >= 85 )) && (( warn85_sent == 0 )); then
    echo "WARNING: Usage has crossed 85%. Triggering warn snapshot."
    if [[ -x "$SNAPSHOT_SCRIPT" ]]; then
      "$SNAPSHOT_SCRIPT" warn >/dev/null 2>&1 || true
    fi
    warn85_sent=1
  fi

  if (( pct_int >= 90 )) && (( warn90_sent == 0 )); then
    echo "CRITICAL: Usage has crossed 90%. Writing emergency save flag and triggering snapshot."
    printf 'emergency' > "$EMERGENCY_FLAG" 2>/dev/null || true
    if [[ -x "$SNAPSHOT_SCRIPT" ]]; then
      "$SNAPSHOT_SCRIPT" critical >/dev/null 2>&1 || true
    fi
    warn90_sent=1
  fi

  sleep "$INTERVAL"
done
