#!/usr/bin/env bash

# ai-dev-workflow managed SessionStart helper (matchers: compact, resume).
#
# Prints a short pointer to the active branch's context-summary.md (and its
# staleness) to stdout, for the host to auto-inject into context on resume
# or after a compaction. This is read-only: it never creates a .wip
# directory, a summary file, or any other state, and always exits 0 so a
# missing binary or a repo with no active work never blocks session start.

set -u

aidw_bin="$HOME/.claude/ai-dev-workflow/bin/aidw"
if [[ ! -x "$aidw_bin" ]]; then
  aidw_bin="$(command -v aidw 2>/dev/null || true)"
fi
[[ -n "$aidw_bin" ]] || exit 0

hook_input=""
if [[ ! -t 0 ]]; then
  hook_input="$(cat 2>/dev/null || true)"
fi

hook_cwd=""
if [[ -n "$hook_input" ]]; then
  if command -v jq >/dev/null 2>&1; then
    hook_cwd="$(printf '%s' "$hook_input" | jq -r '.cwd // empty' 2>/dev/null || true)"
  else
    hook_cwd="$(printf '%s' "$hook_input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  fi
fi
work_dir="${hook_cwd:-$PWD}"

repo_root=""
if command -v git >/dev/null 2>&1; then
  repo_root="$(git -C "$work_dir" rev-parse --show-toplevel 2>/dev/null || true)"
fi
[[ -n "$repo_root" ]] || exit 0

resolve_out="$("$aidw_bin" resolve-wip "$repo_root" 2>/dev/null || true)"
resolved_exists="$(printf '%s\n' "$resolve_out" | sed -n 's/^exists=//p' | head -1)"
resolved_dir="$(printf '%s\n' "$resolve_out" | sed -n 's/^wip_dir=//p' | head -1)"
if [[ "$resolved_exists" != "true" || -z "$resolved_dir" ]]; then
  exit 0
fi

# Always report that active work exists once we get this far, even if no
# context-summary.md has been generated yet (e.g. right after `aidw start`,
# before the first /wip-sync or set-stage transition) — the host should
# still know there's a branch in progress.
echo "aidw: active work in $resolved_dir"

summary_json="$("$aidw_bin" context-summary "$repo_root" --json 2>/dev/null || true)"
[[ -n "$summary_json" ]] || exit 0

if command -v jq >/dev/null 2>&1; then
  stale="$(printf '%s' "$summary_json" | jq -r '.stale' 2>/dev/null || true)"
  summary_path="$(printf '%s' "$summary_json" | jq -r '.summary_path // empty' 2>/dev/null || true)"
else
  # Two separate BRE passes instead of \(true\|false\) — BSD/POSIX sed
  # (macOS default) doesn't support \| alternation, so a single-pattern
  # version silently returns empty here and this would read as "current"
  # even when stale, inverting exactly what A3b exists to report.
  stale="$(printf '%s' "$summary_json" | sed -n 's/.*"stale"[[:space:]]*:[[:space:]]*true.*/true/p' | head -1)"
  if [[ -z "$stale" ]]; then
    stale="$(printf '%s' "$summary_json" | sed -n 's/.*"stale"[[:space:]]*:[[:space:]]*false.*/false/p' | head -1)"
  fi
  summary_path="$(printf '%s' "$summary_json" | sed -n 's/.*"summary_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
fi

if [[ -n "$summary_path" ]]; then
  if [[ "$stale" == "true" ]]; then
    echo "aidw: context-summary.md is STALE — read $summary_path plus sources directly (see /wip-resume)"
  else
    echo "aidw: context-summary.md is current — $summary_path"
  fi
fi

exit 0
