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

# Always emit ANSI color codes — this script is exclusively used for terminal
# statusline display. Claude Code captures stdout (not a TTY) but renders the
# ANSI sequences in the status bar, so the TTY check would strip all colors.
c_reset='\033[0m'
c_dim='\033[2m'
c_green='\033[92m'
c_yellow='\033[93m'
c_orange='\033[33m'
c_red='\033[91m'

color_pct() {
  local pct="$1" text="$2" int_pct
  int_pct="$(awk -v p="$pct" 'BEGIN { printf "%d", p+0 }')"
  if   (( int_pct >= 90 )); then printf "${c_red}%s${c_reset}"    "$text"
  elif (( int_pct >= 75 )); then printf "${c_orange}%s${c_reset}"  "$text"
  elif (( int_pct >= 50 )); then printf "${c_yellow}%s${c_reset}"  "$text"
  else                            printf "${c_green}%s${c_reset}"  "$text"
  fi
}

dim() { printf "${c_dim}%s${c_reset}" "$1"; }

format_reset_time() {
  local raw="$1"
  [[ -n "$raw" ]] || return
  # Strip fractional seconds (.085098) and normalize timezone colon (+00:00 → +0000)
  local normalized
  normalized="$(printf '%s' "$raw" | sed -E 's/\.[0-9]+([+-])/\1/; s/([+-][0-9]{2}):([0-9]{2})$/\1\2/')"
  date -j -f "%Y-%m-%dT%H:%M:%S%z" "$normalized" "+%a %H:%M" 2>/dev/null || printf '%s' "$raw"
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
seven_day_pct="$(usage_cache_get '.seven_day_pct')"
usage_pct_display="$(format_usage_pct "$usage_pct")"
seven_day_pct_display="$(format_usage_pct "$seven_day_pct")"

usage_line="$(dim "Claude usage:") unavailable"
if (( cache_corrupted )); then
  usage_line="$(dim "Claude usage:") ${c_red}cache error${c_reset}"
elif [[ -n "$usage_pct_display" ]]; then
  if [[ -n "$used_tokens" && "$used_tokens" != "0" ]]; then
    # Transcript-estimate path: show token counts
    usage_line="$(dim "Claude usage:") $(color_pct "$usage_pct" "$(human_tokens "$used_tokens")")"
    if [[ -n "$limit_tokens" && "$limit_tokens" != "0" ]]; then
      usage_line+="$(dim " / $(human_tokens "$limit_tokens") today")"
    fi
    usage_line+=" $(color_pct "$usage_pct" "${usage_pct_display}%")"
  else
    # OAuth path: percentage only
    usage_line="$(dim "Claude usage:") $(color_pct "$usage_pct" "${usage_pct_display}%")"
  fi

  if [[ -n "$seven_day_pct_display" ]]; then
    usage_line+=" $(dim "|") $(dim "7d") $(color_pct "$seven_day_pct" "${seven_day_pct_display}%")"
  fi

  if [[ -n "$daily_reset_at" ]]; then
    usage_line+=" $(dim "| resets $(format_reset_time "$daily_reset_at")")"
  fi

  if [[ -n "$weekly_reset_at" && "$weekly_reset_at" != "$daily_reset_at" ]]; then
    usage_line+=" $(dim "| 7d resets $(format_reset_time "$weekly_reset_at")")"
  fi

  if [[ -n "$source_label" && "$source_label" != "oauth" ]]; then
    usage_line+=" $(dim "| $source_label")"
  fi
fi

printf "%b\n" "$usage_line"
printf "%b\n" "$(dim "Session:") $(color_pct "$used_pct" "${used_pct}%")"
