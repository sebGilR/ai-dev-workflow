#!/usr/bin/env bash
# PostToolUse hook — logs every mcp__serena__* call to ~/.claude/serena.log
#
# Install: add to ~/.claude/settings.json under hooks > PostToolUse
# with matcher "mcp__serena__.*"
#
# Input (stdin): JSON with fields tool_name, tool_input (and others).

set -euo pipefail

LOG_FILE="${SERENA_LOG:-$HOME/.claude/serena.log}"

input="$(cat)"

_parsed="$(printf '%s' "$input" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); print(d.get('tool_name','?')); print(json.dumps(d.get('tool_input',{})))" \
  2>/dev/null)" || _parsed="?"$'\n'"{}"
tool="${_parsed%%$'\n'*}"
params="${_parsed#*$'\n'}"

mkdir -p "$(dirname "$LOG_FILE")"
printf '[%s] [mcp]    %s %s\n' \
  "$(date '+%Y-%m-%d %H:%M:%S')" \
  "$tool" \
  "$params" \
  >> "$LOG_FILE"

exit 0
