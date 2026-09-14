#!/usr/bin/env bash
# ai-dev-workflow managed workflow-gate hook (PreToolUse: Edit|Write|NotebookEdit).
# Thin pass-through: resolves the aidw binary and execs `aidw hook-gate`,
# which reads the hook's stdin JSON and prints the permission decision JSON
# (deny only — see hookgate.RenderOutput's doc comment for why an allow
# decision is never printed as JSON).
#
# This script must NEVER fail closed: if aidw can't be found or the call
# otherwise fails, it produces NO output and exits 0. Empty stdout is Claude
# Code's documented "no opinion, defer to the normal permission flow"
# signal (https://code.claude.com/docs/en/hooks) — the same signal a
# successful allow decision from `aidw hook-gate` itself produces. It is NOT
# safe to print an explicit "allow" JSON here (or anywhere in this hook):
# that would be an affirmative decision that silently overrides the user's
# own permissions.ask/permissions.deny rules for Edit/Write/NotebookEdit,
# not merely "this hook doesn't object." Matches save-wip-snapshot.sh's
# existing convention of a bare `exit 0` on every no-op/failure path.
set -u

aidw_bin="$HOME/.claude/ai-dev-workflow/bin/aidw"
if [[ ! -x "$aidw_bin" ]]; then
  aidw_bin="$(command -v aidw 2>/dev/null || true)"
fi

if [[ -z "$aidw_bin" || ! -x "$aidw_bin" ]]; then
  exit 0
fi

exec "$aidw_bin" hook-gate
