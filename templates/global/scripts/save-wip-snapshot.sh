#!/usr/bin/env bash

# ai-dev-workflow managed WIP snapshot helper.
# Usage: save-wip-snapshot.sh [warn|critical|manual|stop|precompact|sessionend]

set -u

level="${1:-manual}"
case "$level" in
  warn|critical|manual|stop|precompact|sessionend) ;;
  *) level="manual" ;;
esac

timestamp="$(date '+%Y-%m-%d %H:%M:%S %z' 2>/dev/null || echo "unknown-time")"

repo_root=""
branch=""

if command -v git >/dev/null 2>&1; then
  # Get repo root from the most reliable source: rev-parse --show-toplevel
  repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  # Only use rev-parse --abbrev-ref if we have a valid repo_root
  if [[ -n "$repo_root" ]]; then
    branch="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  fi
fi

if [[ -z "$repo_root" ]]; then
  # Fall back to last known active repo written by start-claude-watch.sh.
  # Verify it is still a valid git repo before using it.
  _fallback="$(cat "${HOME}/.claude/.active-repo" 2>/dev/null || true)"
  if [[ -n "$_fallback" ]] && git -C "$_fallback" rev-parse --show-toplevel >/dev/null 2>&1; then
    repo_root="$_fallback"
    branch="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    echo "warn: save-wip-snapshot using .active-repo fallback ($repo_root) — concurrent multi-repo sessions may target the wrong context.md" >&2
  fi
fi

if [[ -z "$repo_root" ]]; then
  repo_root="$PWD"
fi

# Keep the active-repo file current whenever we successfully resolve a git repo.
if [[ -n "$repo_root" ]] && git -C "$repo_root" rev-parse --show-toplevel >/dev/null 2>&1; then
  printf '%s\n' "$repo_root" > "${HOME}/.claude/.active-repo" 2>/dev/null || true
fi

if [[ -z "$branch" || "$branch" == "HEAD" ]]; then
  branch="detached-head"
fi

# Sanitize branch name to match aidw.py's safe_slug(): replace non-[A-Za-z0-9_.-]
# chars with '-', strip leading/trailing '-', append 8-char sha256 suffix when slug
# differs from original (prevents collisions, e.g. "feature/foo" vs "feature-foo").
_branch_slug() {
  local name="$1"
  local slug
  slug="$(printf '%s' "$name" | sed 's/[^A-Za-z0-9_.-]/-/g; s/^-*//; s/-*$//')"
  [[ -n "$slug" ]] || slug="unknown-branch"
  if [[ "$slug" != "$name" ]]; then
    local hash=""
    if command -v sha256sum >/dev/null 2>&1; then
      hash="$(printf '%s' "$name" | sha256sum | cut -c1-8 2>/dev/null || true)"
    elif command -v shasum >/dev/null 2>&1; then
      hash="$(printf '%s' "$name" | shasum -a 256 | cut -c1-8 2>/dev/null || true)"
    fi
    [[ -n "$hash" ]] && slug="${slug}-${hash}"
  fi
  printf '%s' "$slug"
}
branch_slug="$(_branch_slug "$branch")"

wip_dir="$repo_root/.wip/$branch_slug"
legacy_wip_dir="$repo_root/wip/$branch_slug"
handoff_file="$wip_dir/handoff.md"
progress_file="$wip_dir/progress.log"
research_file="$wip_dir/research.md"
plan_file="$wip_dir/plan.md"

mkdir -p "$wip_dir" 2>/dev/null || true

# Migrate legacy snapshots created under wip/<branch>/ to .wip/<branch>/.
if [[ -d "$legacy_wip_dir" ]]; then
  for f in handoff.md progress.log research.md plan.md; do
    if [[ -f "$legacy_wip_dir/$f" && ! -f "$wip_dir/$f" ]]; then
      cp "$legacy_wip_dir/$f" "$wip_dir/$f" 2>/dev/null || true
    fi
  done
fi

[[ -f "$research_file" ]] || : > "$research_file" 2>/dev/null || true
[[ -f "$plan_file" ]] || : > "$plan_file" 2>/dev/null || true

git_status="(git unavailable)"
last_commits="(git unavailable)"
changed_files="(git unavailable)"
diff_summary="(git unavailable)"

if command -v git >/dev/null 2>&1; then
  git_status="$(git -C "$repo_root" status --short --branch 2>&1 || true)"
  last_commits="$(git -C "$repo_root" log --oneline -n 8 2>&1 || true)"
  changed_files="$(git -C "$repo_root" diff --name-only 2>&1 || true)"
  diff_summary="$(git -C "$repo_root" diff --stat 2>&1 || true)"
fi

{
  echo "# Handoff Snapshot"
  echo
  echo "- timestamp: $timestamp"
  echo "- snapshot_level: $level"
  echo "- branch: $branch"
  echo "- repo_path: $repo_root"
  echo
  echo "## Git Status"
  echo '```text'
  printf '%s\n' "$git_status"
  echo '```'
  echo
  echo "## Last 8 Commits"
  echo '```text'
  printf '%s\n' "$last_commits"
  echo '```'
  echo
  echo "## Changed Files"
  echo '```text'
  printf '%s\n' "$changed_files"
  echo '```'
  echo
  echo "## Diff Summary"
  echo '```text'
  printf '%s\n' "$diff_summary"
  echo '```'
} > "$handoff_file" 2>/dev/null || true

printf '%s | level=%s | branch=%s | repo=%s\n' "$timestamp" "$level" "$branch" "$repo_root" >> "$progress_file" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Session end: decrement reference count; stop watcher when last session ends.
# ---------------------------------------------------------------------------
if [[ "$level" == "sessionend" ]]; then
  PID_FILE="${HOME}/.claude/.claude-watch.pid"
  SESSION_COUNT_FILE="${HOME}/.claude/.claude-watch-sessions"

  count="$(cat "$SESSION_COUNT_FILE" 2>/dev/null || echo 1)"
  count=$(( count - 1 ))
  if (( count <= 0 )); then
    count=0
    if [[ -f "$PID_FILE" ]]; then
      pid="$(cat "$PID_FILE" 2>/dev/null)"
      [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
      rm -f "$PID_FILE" 2>/dev/null || true
    fi
  fi
  printf '%d\n' "$count" > "$SESSION_COUNT_FILE" 2>/dev/null || true
fi

# ---------------------------------------------------------------------------
# Emergency save: if the watcher wrote ~/.claude/.wip-emergency-save and this
# is a Stop hook, update context.md and block the next Claude turn so Claude
# is forced to run /wip-sync before consuming more tokens.
# ---------------------------------------------------------------------------
EMERGENCY_FLAG="${HOME}/.claude/.wip-emergency-save"
if [[ "$level" == "stop" && -f "$EMERGENCY_FLAG" ]]; then
  context_file="$wip_dir/context.md"
  wrote_context=0
  if [[ -f "$context_file" ]]; then
    if printf '\n---\n**Emergency save %s — usage at 90%%+.**\nRun /wip-sync to persist in-memory progress before the limit resets.\n' \
        "$timestamp" >> "$context_file" 2>/dev/null; then
      wrote_context=1
    fi
  fi

  if (( wrote_context )); then
    rm -f "$EMERGENCY_FLAG" 2>/dev/null || true  # consume flag only after successful write
    printf '\n⚠ Claude usage is at 90%%+. WIP snapshot saved to:\n  %s/handoff.md\nRun /wip-sync now to persist in-memory progress before the limit resets.\n' \
      "$wip_dir" >&2
  else
    printf '\n⚠ Claude usage is at 90%%+. Git snapshot saved but context.md was not found at:\n  %s\nRun /wip-sync manually to persist in-memory progress.\n' \
      "$wip_dir" >&2
  fi
  exit 2
fi

# ---------------------------------------------------------------------------
# Post-stop usage refresh: update the usage cache after each Claude turn,
# rate-limited by TTL so we never hammer the API on rapid consecutive runs.
# ---------------------------------------------------------------------------
if [[ "$level" == "stop" ]]; then
  FETCH_SCRIPT="${HOME}/.claude/claude-fetch-usage.sh"
  if [[ -x "$FETCH_SCRIPT" ]]; then
    "$FETCH_SCRIPT" --if-stale &>/dev/null &
    disown 2>/dev/null || true
  fi
fi

exit 0
