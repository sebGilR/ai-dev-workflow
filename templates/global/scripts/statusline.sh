#!/usr/bin/env bash

# ai-dev-workflow managed Claude status line renderer.
# Reads a JSON payload on stdin and prints a compact context summary.

set -u

if ! command -v jq >/dev/null 2>&1; then
  echo "[unknown-model] unknown-repo | unknown-branch"
  echo "---------- 0% context | last 0/0 | total 0/0"
  exit 0
fi

json_payload="$(cat 2>/dev/null || true)"

json_get() {
  local key="$1"
  jq -r "$key // empty" 2>/dev/null <<<"$json_payload"
}

model="$(json_get '.model.display_name')"
workspace_dir="$(json_get '.workspace.current_dir')"
used_pct_raw="$(json_get '.context_window.used_percentage')"
last_in="$(json_get '.context_window.current_usage.input_tokens')"
last_out="$(json_get '.context_window.current_usage.output_tokens')"
total_in="$(json_get '.context_window.total_input_tokens')"
total_out="$(json_get '.context_window.total_output_tokens')"

[[ -n "$model" ]] || model="unknown-model"
[[ -n "$workspace_dir" ]] || workspace_dir="$PWD"
[[ -n "$last_in" ]] || last_in="0"
[[ -n "$last_out" ]] || last_out="0"
[[ -n "$total_in" ]] || total_in="0"
[[ -n "$total_out" ]] || total_out="0"

repo_name="$(basename "$workspace_dir" 2>/dev/null || echo "unknown-repo")"
branch="unknown-branch"
if command -v git >/dev/null 2>&1; then
  branch="$(git -C "$workspace_dir" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown-branch")"
fi

used_pct="0"
if [[ -n "$used_pct_raw" ]]; then
  used_pct="$(awk -v v="$used_pct_raw" 'BEGIN {
    if (v == "" || v == "null") { print 0; exit }
    if (v <= 1) { v = v * 100 }
    if (v < 0) v = 0
    if (v > 100) v = 100
    printf "%d", v
  }')"
fi

progress_bar() {
  local pct="$1"
  local width=10
  local filled=$(( pct * width / 100 ))
  local empty=$(( width - filled ))
  local out=""
  local i

  for ((i=0; i<filled; i++)); do out+="#"; done
  for ((i=0; i<empty; i++)); do out+="-"; done
  printf "%s" "$out"
}

bar="$(progress_bar "$used_pct")"

echo "[$model] $repo_name | $branch"
echo "$bar ${used_pct}% context | last ${last_in}/${last_out} | total ${total_in}/${total_out}"
