#!/usr/bin/env bash

# ai-dev-workflow managed WIP snapshot helper.
# Usage: save-wip-snapshot.sh [warn|critical|manual|stop|precompact|sessionend]
#
# This hook is a NO-OP outside a git repo and a NO-OP when the current
# branch has no active WIP directory. It never creates a .wip directory and
# never seeds or writes any .wip file for a branch that hasn't run
# `aidw start` / /wip-start.
#
# It is NOT, however, write-free in general: it also makes one best-effort
# `aidw work checkpoint --from-hook` call (see below), which can touch the
# work-model store under $AIDW_STATE_DIR (work/<id>/work.json,
# sessions/<id>.json) for a repo that has an associated work record but no
# .wip directory at all. That call never writes inside the repo.
#
# It always exits 0 so the host is never blocked by a hook failure.

set -u

level="${1:-manual}"
case "$level" in
  warn|critical|manual|stop|precompact|sessionend) ;;
  *) level="manual" ;;
esac

timestamp="$(date '+%Y-%m-%d %H:%M:%S %z' 2>/dev/null || echo "unknown-time")"

aidw_bin="$HOME/.claude/ai-dev-workflow/bin/aidw"
if [[ ! -x "$aidw_bin" ]]; then
  aidw_bin="$(command -v aidw 2>/dev/null || true)"
fi

# Read the hook's JSON payload from stdin, if any (Claude Code passes hook
# events as JSON on stdin; a manual/direct invocation has no stdin to read).
# cwd/session_id are best-effort: prefer jq, fall back to a conservative
# grep/sed extraction so a missing jq never turns this into a hard failure.
hook_input=""
if [[ ! -t 0 ]]; then
  hook_input="$(cat 2>/dev/null || true)"
fi

hook_cwd=""
session_id=""
if [[ -n "$hook_input" ]]; then
  if command -v jq >/dev/null 2>&1; then
    hook_cwd="$(printf '%s' "$hook_input" | jq -r '.cwd // empty' 2>/dev/null || true)"
    session_id="$(printf '%s' "$hook_input" | jq -r '.session_id // empty' 2>/dev/null || true)"
  else
    hook_cwd="$(printf '%s' "$hook_input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
    session_id="$(printf '%s' "$hook_input" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  fi
fi

work_dir="${hook_cwd:-$PWD}"

repo_root=""
branch=""

if command -v git >/dev/null 2>&1; then
  repo_root="$(git -C "$work_dir" rev-parse --show-toplevel 2>/dev/null || true)"
  if [[ -n "$repo_root" ]]; then
    branch="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  fi
fi

# Not a git repo (or git unavailable) — nothing to snapshot. Exit quietly.
if [[ -z "$repo_root" ]]; then
  exit 0
fi

if [[ -z "$branch" || "$branch" == "HEAD" ]]; then
  branch="detached-head"
fi

# Work-model path (additive, best-effort): a single resolver/writer call
# that reads cwd/session_id from the same stdin JSON payload already
# drained above. If the binary is missing or too old to have `work
# checkpoint`, or the command errors for any reason (no active work,
# ambiguous, or a real error), this simply no-ops — no shell-side
# reimplementation of a work-model resolution fallback is attempted.
if [[ -n "$aidw_bin" && -x "$aidw_bin" ]]; then
  printf '%s' "$hook_input" | "$aidw_bin" work checkpoint --from-hook >/dev/null 2>&1 || true
fi

wip_dir=""
resolved_via_binary=0

if [[ -n "$aidw_bin" ]]; then
  resolve_out="$("$aidw_bin" resolve-wip "$repo_root" 2>/dev/null || true)"
  resolved_exists="$(printf '%s\n' "$resolve_out" | sed -n 's/^exists=//p' | head -1)"
  resolved_dir="$(printf '%s\n' "$resolve_out" | sed -n 's/^wip_dir=//p' | head -1)"
  # Only trust this as a real answer from a binary that understands
  # resolve-wip: it must have printed a recognized exists= value. An older
  # aidw with no resolve-wip subcommand exits non-zero and (on this repo's
  # cobra setup) writes its "unknown command" error to stderr, which is
  # already discarded above — but even if some future/wrapped `aidw` were
  # to leak something to stdout on failure, requiring an unambiguous
  # exists=true/false keeps us from misreading that as "no active work"
  # and silently skipping the shell fallback below.
  if [[ "$resolved_exists" == "true" || "$resolved_exists" == "false" ]]; then
    resolved_via_binary=1
    if [[ "$resolved_exists" == "true" && -n "$resolved_dir" ]]; then
      wip_dir="$resolved_dir"
    fi
  fi
fi

if [[ "$resolved_via_binary" -eq 0 ]]; then
  # Shell fallback resolver (aidw binary missing). Must match the Go
  # resolver's branch-dir lookup exactly: same slugification, same dated
  # dir pattern, same legacy fallback, and — critically — no creation.
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
  # Escape ERE metacharacters in the slug before interpolating into
  # `grep -E` — an unescaped dotted slug (e.g. "release.1") can otherwise
  # match an unrelated dir like "releaseX1".
  branch_slug_re="$(printf '%s' "$branch_slug" | sed 's/[][\.^$*+?(){}|]/\\&/g')"

  # Phase 1: find existing dated dir (YYYYMMDD or YYYYMMDDHHMMSS prefix);
  # pick the newest. Only dirs whose date prefix is a validated date-like
  # 8 or 14 digit string count — matches the Go side's time.Parse checks
  # closely enough for shell-fallback purposes (no bogus digit strings).
  candidate="$(find "$repo_root/.wip/" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
    | grep -E '/[0-9]{8}([0-9]{6})?-'"$branch_slug_re"'$' \
    | sort | tail -1)"
  if [[ -n "$candidate" ]]; then
    wip_dir="$candidate"
  elif [[ -d "$repo_root/.wip/$branch_slug" ]]; then
    # Phase 2: legacy unprefixed dir.
    wip_dir="$repo_root/.wip/$branch_slug"
  fi
  # No phase 3: never create a new dir from the shell fallback.
fi

# No active WIP directory for this branch — nothing to snapshot, and we
# never spontaneously create one. Exit quietly.
if [[ -z "$wip_dir" || ! -d "$wip_dir" ]]; then
  exit 0
fi
if [[ ! -f "$wip_dir/status.json" ]]; then
  exit 0
fi

handoff_file="$wip_dir/handoff.md"
progress_file="$wip_dir/progress.log"

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
  [[ -n "$session_id" ]] && echo "- session_id: $session_id"
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

printf '%s | level=%s | branch=%s | repo=%s | session=%s\n' "$timestamp" "$level" "$branch" "$repo_root" "${session_id:-none}" >> "$progress_file" 2>/dev/null || true

# Best-effort context-summary refresh on recovery-relevant checkpoints. This
# never creates a WIP dir (we already confirmed one exists above with
# status.json present) and never blocks the hook on failure.
case "$level" in
  precompact|stop|sessionend)
    if [[ -n "$aidw_bin" && -x "$aidw_bin" ]]; then
      "$aidw_bin" summarize-context "$repo_root" >/dev/null 2>&1 || true
    fi
    ;;
esac

exit 0
