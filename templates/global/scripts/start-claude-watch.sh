#!/usr/bin/env bash

# ai-dev-workflow managed Claude session start hook.
# Starts claude-watch.sh in the background if not already running,
# and increments a session reference count so the last session to end
# can cleanly stop the watcher.

set -uo pipefail

WATCH_SCRIPT="${HOME}/.claude/claude-watch.sh"
PID_FILE="${HOME}/.claude/.claude-watch.pid"
SESSION_COUNT_FILE="${HOME}/.claude/.claude-watch-sessions"
ACTIVE_REPO_FILE="${HOME}/.claude/.active-repo"

# Record active repo path for emergency save fallback (best-effort).
active_repo="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -n "$active_repo" ]]; then
  printf '%s\n' "$active_repo" > "$ACTIVE_REPO_FILE" 2>/dev/null || true
fi

# Guard: check watcher exists before touching the session count.
# This prevents the count from being incremented for missing-script cases,
# which would cause sessionend to decrement and potentially kill a legitimate watcher.
[[ -x "$WATCH_SCRIPT" ]] || exit 0

# Reconcile stale count: if the watcher process is dead, reset the count to 0.
# This heals crash cycles where SessionEnd never fired.
if [[ -f "$PID_FILE" ]]; then
  pid="$(cat "$PID_FILE" 2>/dev/null)"
  if [[ -z "$pid" ]] || ! kill -0 "$pid" 2>/dev/null; then
    printf '0\n' > "$SESSION_COUNT_FILE" 2>/dev/null || true
    rm -f "$PID_FILE" 2>/dev/null || true
  fi
else
  printf '0\n' > "$SESSION_COUNT_FILE" 2>/dev/null || true
fi

# Increment session count
count="$(cat "$SESSION_COUNT_FILE" 2>/dev/null || echo 0)"
printf '%d\n' $(( count + 1 )) > "$SESSION_COUNT_FILE" 2>/dev/null || true

# Start watcher if not already running (PID file removed above if dead)
if [[ ! -f "$PID_FILE" ]]; then
  nohup "$WATCH_SCRIPT" >/dev/null 2>&1 &
  printf '%d\n' $! > "$PID_FILE" 2>/dev/null || true
  disown
fi

exit 0
