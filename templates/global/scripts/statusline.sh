#!/usr/bin/env bash

# ai-dev-workflow managed Claude status line renderer.
# Reads a JSON payload on stdin and prints a compact context summary.

set -u

CLAUDE_USAGE_CACHE="${CLAUDE_USAGE_CACHE:-$HOME/.claude/usage-status.json}"

if ! command -v jq >/dev/null 2>&1; then
  echo "Claude usage: unavailable"
  echo "Session: 0% | last 0/0 | total 0/0"
  exit 0
fi

json_payload="$(cat 2>/dev/null || true)"

json_get() {
  local key="$1"
  jq -r "$key // empty" 2>/dev/null <<<"$json_payload"
}

used_pct_raw="$(json_get '.context_window.used_percentage')"
last_in="$(json_get '.context_window.current_usage.input_tokens')"
last_out="$(json_get '.context_window.current_usage.output_tokens')"
total_in="$(json_get '.context_window.total_input_tokens')"
total_out="$(json_get '.context_window.total_output_tokens')"

[[ -n "$last_in" ]] || last_in="0"
[[ -n "$last_out" ]] || last_out="0"
[[ -n "$total_in" ]] || total_in="0"
[[ -n "$total_out" ]] || total_out="0"

format_session_pct() {
  local raw="$1"
  awk -v raw="$raw" 'BEGIN {
    value = raw
    if (value == "" || value == "null") {
      print 0
      exit
    }

    # Accept both decimal (0.5) and integer (50) format. If value is a fraction (<=1), convert to percent.
    if ((raw ~ /^[0-9]+$/ || raw ~ /^[0-9]*\.[0-9]+$/) && value <= 1) {
      value = value * 100
    }

    if (value < 0) value = 0
    if (value > 100) value = 100
    printf "%d", value
  }'
}

human_tokens() {
  local raw="$1"
  awk -v value="$raw" 'BEGIN {
    if (value == "" || value == "null" || value < 0) {
      print "0"
      exit
    }
    if (value >= 1000000) {
      formatted = sprintf("%.1f", value / 1000000)
      sub(/\.0$/, "", formatted)
      printf "%sm", formatted
      exit
    }
    if (value >= 1000) {
      formatted = sprintf("%.1f", value / 1000)
      sub(/\.0$/, "", formatted)
      printf "%sk", formatted
      exit
    }
    printf "%d", value
  }'
}

format_usage_pct() {
  local raw="$1"
  awk -v value="$raw" 'BEGIN {
    if (value == "" || value == "null") {
      print ""
      exit
    }
    formatted = sprintf("%.1f", value)
    sub(/\.0$/, "", formatted)
    print formatted
  }'
}

usage_cache_get() {
  local key="$1"
  if [[ ! -f "$CLAUDE_USAGE_CACHE" ]]; then
    return 1
  fi
  # Validate JSON before extracting values. If parse fails, return 1 (error).
  if ! jq empty "$CLAUDE_USAGE_CACHE" 2>/dev/null; then
    return 1
  fi
  jq -r "$key // empty" 2>/dev/null "$CLAUDE_USAGE_CACHE"
}

used_pct="$(format_session_pct "$used_pct_raw")"

# Check if cache file exists but is corrupted
cache_corrupted=0
if [[ -f "$CLAUDE_USAGE_CACHE" ]] && ! jq empty "$CLAUDE_USAGE_CACHE" 2>/dev/null; then
  cache_corrupted=1
fi

usage_pct="$(usage_cache_get '.usage_percentage')"
used_tokens="$(usage_cache_get '.used_tokens')"
limit_tokens="$(usage_cache_get '.limit_tokens')"
source_label="$(usage_cache_get '.source')"
daily_reset_at="$(usage_cache_get '.daily_reset_at')"
weekly_reset_at="$(usage_cache_get '.weekly_reset_at')"
usage_pct_display="$(format_usage_pct "$usage_pct")"

usage_line="Claude usage: unavailable"
if (( cache_corrupted )); then
  usage_line="Claude usage: cache error"
elif [[ -n "$usage_pct_display" && -n "$used_tokens" ]]; then
  usage_line="Claude usage: $(human_tokens "$used_tokens")"
  if [[ -n "$limit_tokens" && "$limit_tokens" != "0" ]]; then
    usage_line+=" / $(human_tokens "$limit_tokens") today"
  fi
  usage_line+=" | ${usage_pct_display}%"

  if [[ -n "$daily_reset_at" ]]; then
    usage_line+=" | daily reset ${daily_reset_at}"
  fi

  if [[ -n "$weekly_reset_at" ]]; then
    usage_line+=" | weekly reset ${weekly_reset_at}"
  fi

  if [[ -n "$source_label" ]]; then
    usage_line+=" | source ${source_label}"
  fi
fi

echo "$usage_line"
echo "Session: ${used_pct}% | last ${last_in}/${last_out} | total ${total_in}/${total_out}"
