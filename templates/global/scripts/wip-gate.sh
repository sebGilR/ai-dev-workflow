#!/usr/bin/env bash
# ai-dev-workflow managed workflow-gate hook (PreToolUse: Edit|Write|NotebookEdit).
# Thin pass-through: resolves the aidw binary and execs `aidw hook-gate`,
# which reads the hook's stdin JSON and prints the permission decision JSON.
# This script must NEVER fail closed: if aidw can't be found or the call
# otherwise fails, it prints an explicit allow decision itself, rather than
# relying on Claude Code's own missing-output behavior.
set -u

allow_json='{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"wip-gate: aidw binary unavailable; failing open"}}'

aidw_bin="$HOME/.claude/ai-dev-workflow/bin/aidw"
if [[ ! -x "$aidw_bin" ]]; then
  aidw_bin="$(command -v aidw 2>/dev/null || true)"
fi

if [[ -z "$aidw_bin" || ! -x "$aidw_bin" ]]; then
  printf '%s\n' "$allow_json"
  exit 0
fi

if ! "$aidw_bin" hook-gate; then
  printf '%s\n' "$allow_json"
fi
exit 0
