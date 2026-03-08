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
  repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  branch="$(git -C "${repo_root:-.}" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
fi

if [[ -z "$repo_root" ]]; then
  repo_root="$PWD"
fi

if [[ -z "$branch" || "$branch" == "HEAD" ]]; then
  branch="detached-head"
fi

wip_dir="$repo_root/.wip/$branch"
legacy_wip_dir="$repo_root/wip/$branch"
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

exit 0
