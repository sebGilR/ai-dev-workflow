#!/usr/bin/env bash

# ai-dev-workflow managed Claude status line renderer.
# Reads a JSON payload on stdin and prints a compact context summary.

set -u

if ! command -v jq >/dev/null 2>&1; then
  echo "Session: 0%"
  exit 0
fi

json_payload="$(cat 2>/dev/null || true)"

json_get() {
  local key="$1"
  jq -r "$key // empty" 2>/dev/null <<<"$json_payload"
}

used_pct_raw="$(json_get '.context_window.used_percentage')"
total_in_raw="$(json_get '.context_window.total_input_tokens')"
total_out_raw="$(json_get '.context_window.total_output_tokens')"

format_session_pct() {
  local raw="$1"
  awk -v raw="$raw" 'BEGIN {
    value = raw
    if (value == "" || value == "null") {
      print 0
      exit
    }

    # Only treat value as a fraction when it is explicitly decimal-formatted (e.g. 0.5).
    # Integer 1 means 1%, not 100%, so the float pattern check is intentionally strict.
    if (raw ~ /^[0-9]*\.[0-9]+$/ && value <= 1) {
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

used_pct="$(format_session_pct "$used_pct_raw")"

[[ "$total_in_raw"  =~ ^[0-9]+$ ]] || total_in_raw=0
[[ "$total_out_raw" =~ ^[0-9]+$ ]] || total_out_raw=0
total_tokens=$(( total_in_raw + total_out_raw ))

# Session line: context window % + total tokens consumed this session
session_line="$(dim "Session:") $(color_pct "$used_pct" "${used_pct}%")"
if (( total_tokens > 0 )); then
  session_line+=" $(dim "·") $(color_pct "$used_pct" "$(human_tokens "$total_tokens") tokens")"
fi
printf "%b\n" "$session_line"
