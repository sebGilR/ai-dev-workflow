#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CLAUDE_HOME="${CLAUDE_HOME:-$HOME/.claude}"
COPILOT_HOME="${COPILOT_HOME:-$HOME/.copilot}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

# Parse arguments — positional arg is workspace root.
WORKSPACE_ROOT=""
for _arg in "$@"; do
  case "$_arg" in
    --*) echo "Unknown flag: $_arg" >&2; exit 1 ;;
    *) WORKSPACE_ROOT="$_arg" ;;
  esac
done
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$PWD}"

mkdir -p "$CLAUDE_HOME"
mkdir -p "$COPILOT_HOME"

# Safe, idempotent symlink helper.
# - If dest already points to src, do nothing.
# - If dest exists and is NOT a symlink, fail with a useful message.
# - Otherwise create (or update) the symlink.
safe_link() {
  local src="$1"
  local dest="$2"
  mkdir -p "$(dirname "$dest")"

  if [ -L "$dest" ]; then
    current="$(readlink "$dest")"
    if [ "$current" = "$src" ]; then
      return 0
    fi
    # Stale symlink — update it
    ln -sfn "$src" "$dest"
    return 0
  fi

  if [ -e "$dest" ]; then
    echo "ERROR: $dest exists and is not a symlink managed by ai-dev-workflow." >&2
    echo "       Remove or rename it, then re-run the installer." >&2
    return 1
  fi

  ln -s "$src" "$dest"
}

# Keep a stable link back to this repo so skills can reference scripts reliably.
safe_link "$SCRIPT_DIR" "$CLAUDE_HOME/ai-dev-workflow"

install_managed_script() {
  local rel_src="$1"
  local dest_name="$2"
  local src="$SCRIPT_DIR/$rel_src"
  local dest="$CLAUDE_HOME/$dest_name"

  if [ ! -f "$src" ]; then
    echo "WARNING: managed script template missing: $src" >&2
    return 0
  fi

  chmod +x "$src" 2>/dev/null || true

  if safe_link "$src" "$dest"; then
    chmod +x "$dest" 2>/dev/null || true
    return 0
  fi

  echo "WARNING: could not install managed script at $dest (existing unmanaged file)." >&2
  echo "         Move/remove it and re-run install.sh if you want ai-dev-workflow to manage it." >&2
  return 0
}

install_managed_script "templates/global/scripts/statusline.sh" "statusline.sh"
install_managed_script "templates/global/scripts/claude-watch.sh" "claude-watch.sh"
install_managed_script "templates/global/scripts/save-wip-snapshot.sh" "save-wip-snapshot.sh"
install_managed_script "templates/global/scripts/claude-fetch-usage.sh" "claude-fetch-usage.sh"
install_managed_script "templates/global/scripts/start-claude-watch.sh" "start-claude-watch.sh"

mkdir -p "$CLAUDE_HOME/skills" "$CLAUDE_HOME/agents"
mkdir -p "$COPILOT_HOME/skills"

for skill_dir in "$SCRIPT_DIR"/claude/skills/*; do
  [ -d "$skill_dir" ] || continue
  skill_name="$(basename "$skill_dir")"
  safe_link "$skill_dir" "$CLAUDE_HOME/skills/$skill_name"
  safe_link "$skill_dir" "$COPILOT_HOME/skills/$skill_name"
done

for agent_file in "$SCRIPT_DIR"/claude/agents/*; do
  [ -f "$agent_file" ] || continue
  agent_name="$(basename "$agent_file")"
  safe_link "$agent_file" "$CLAUDE_HOME/agents/$agent_name"
done

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/merge_claude_md.py" \
  --claude-md "$CLAUDE_HOME/CLAUDE.md" \
  --snippet "$SCRIPT_DIR/templates/global/claude_managed_block.md"

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/merge_settings.py" \
  --settings "$CLAUDE_HOME/settings.json" \
  --template "$SCRIPT_DIR/templates/global/settings.template.json"

normalize_managed_settings_paths() {
  "$PYTHON_BIN" - "$CLAUDE_HOME/settings.json" "$CLAUDE_HOME" <<'PY'
import json
import sys
from pathlib import Path

settings_path = Path(sys.argv[1])
claude_home = Path(sys.argv[2])

if not settings_path.exists():
  raise SystemExit(0)

data = json.loads(settings_path.read_text(encoding="utf-8"))

status_cmd = str(claude_home / "statusline.sh")
snapshot_script = str(claude_home / "save-wip-snapshot.sh")

# Verify managed scripts are actually symlinks (installed successfully).
# Only write paths if the scripts are managed symlinks; skip if unmanaged local files exist.
if not Path(status_cmd).is_symlink() or not Path(snapshot_script).is_symlink():
  raise SystemExit(0)

# Normalize statusLine only when missing or already pointing at a statusline helper.
status_line = data.get("statusLine")
if not isinstance(status_line, dict):
  data["statusLine"] = {"type": "command", "command": status_cmd}
else:
  current_cmd = str(status_line.get("command", ""))
  if (not current_cmd) or current_cmd.endswith("statusline.sh"):
    status_line["type"] = "command"
    status_line["command"] = status_cmd

hooks = data.get("hooks")
if not isinstance(hooks, dict):
  hooks = {}
  data["hooks"] = hooks

managed_kinds = {
  "Stop": "stop",
  "PreCompact": "precompact",
  "SessionEnd": "sessionend",
}

for hook_name, arg in managed_kinds.items():
  canonical = {
    "matcher": "*",
    "hooks": [
      {
        "type": "command",
        "command": f"{snapshot_script} {arg}",
      }
    ],
  }

  existing = hooks.get(hook_name, [])
  if not isinstance(existing, list):
    existing = []

  normalized = []
  found_managed = False

  for item in existing:
    if not isinstance(item, dict):
      normalized.append(item)
      continue

    item_hooks = item.get("hooks", [])
    if not isinstance(item_hooks, list):
      normalized.append(item)
      continue

    is_managed_variant = False
    non_managed_entries = []
    for entry in item_hooks:
      if not isinstance(entry, dict):
        non_managed_entries.append(entry)
        continue
      cmd = str(entry.get("command", "")).strip()
      # Check if this is a managed variant: save-wip-snapshot.sh with the matching argument
      # Use split() for robust parsing instead of endswith() to handle spacing/comments
      parts = cmd.split()
      if len(parts) >= 2 and Path(parts[0]).name == "save-wip-snapshot.sh" and parts[1] == arg:
        is_managed_variant = True
      else:
        non_managed_entries.append(entry)

    if is_managed_variant:
      if not found_managed:
        normalized.append(canonical)
        found_managed = True
      # Preserve any non-managed hooks that were co-located with the managed entry
      if non_managed_entries:
        new_item = dict(item)
        new_item["hooks"] = non_managed_entries
        normalized.append(new_item)
    else:
      normalized.append(item)

  if not found_managed:
    normalized.append(canonical)

  hooks[hook_name] = normalized

# Normalize SessionStart → start-claude-watch.sh (different script, same pattern)
start_script = str(claude_home / "start-claude-watch.sh")
start_canonical = {
  "matcher": "*",
  "hooks": [{"type": "command", "command": start_script}],
}
existing_start = hooks.get("SessionStart", [])
if not isinstance(existing_start, list):
  existing_start = []
normalized_start = []
found_managed_start = False
for item in existing_start:
  if not isinstance(item, dict):
    normalized_start.append(item)
    continue
  is_managed = False
  for e in item.get("hooks", []):
    if not isinstance(e, dict):
      continue
    parts = str(e.get("command", "")).strip().split()
    if parts and Path(parts[0]).name == "start-claude-watch.sh":
      is_managed = True
      break
  if is_managed:
    if not found_managed_start:
      normalized_start.append(start_canonical)
      found_managed_start = True
  else:
    normalized_start.append(item)
if not found_managed_start:
  normalized_start.append(start_canonical)
hooks["SessionStart"] = normalized_start

settings_path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY
}

normalize_managed_settings_paths

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/update_global_gitignore.py"

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/aidw.py" bootstrap-workspace "$WORKSPACE_ROOT"

# ---------------------------------------------------------------------------
# MCP server configuration (Serena, Context7)
# ---------------------------------------------------------------------------

_mcp_missing=()
command -v uvx &>/dev/null || _mcp_missing+=("uvx (required for Serena; install via: curl -LsSf https://astral.sh/uv/install.sh | sh)")
command -v npx &>/dev/null || _mcp_missing+=("npx (required for Context7; install Node.js from https://nodejs.org)")

if [ "${#_mcp_missing[@]}" -eq 0 ]; then
    echo "→ Configuring MCP servers (Serena, Context7)..."
    "$PYTHON_BIN" "$SCRIPT_DIR/scripts/merge_mcp_json.py" || {
        echo ""
        echo "⚠  MCP server configuration failed. Check ~/.claude/mcp.json for issues."
        echo "   Re-run the installer once resolved."
    }
else
    echo ""
    echo "⚠  MCP servers were not configured. Missing dependencies:"
    for _dep in "${_mcp_missing[@]}"; do echo "   - $_dep"; done
    echo "   Re-run the installer once resolved."
fi

# ---------------------------------------------------------------------------
# Shell profile and env file setup
# ---------------------------------------------------------------------------

write_env_file() {
  local env_file="$CLAUDE_HOME/ai-dev-workflow/aidw.env.sh"
  if [ ! -f "$env_file" ]; then
    cat > "$env_file" << 'ENVEOF'
# ai-dev-workflow configuration
# Edit this file to customise your settings. It is NOT committed to git.
# Sourced automatically by your shell profile after install.

# Gemini adversarial review (optional, requires `npm install -g @google/gemini-cli` + auth)
# Set AIDW_GEMINI_REVIEW=1 to enable; requires GEMINI_API_KEY or prior `gemini auth login`
export AIDW_GEMINI_REVIEW="0"
export AIDW_GEMINI_MODEL="gemini-2.5-pro"
export AIDW_GEMINI_TIMEOUT="120"
ENVEOF
    echo "Created env file:  $env_file"
  else
    echo "Env file already exists: $env_file (appending missing vars if needed)"
    local _added=0
    for _var_line in \
      'export AIDW_GEMINI_REVIEW="0"' \
      'export AIDW_GEMINI_MODEL="gemini-2.5-pro"' \
      'export AIDW_GEMINI_TIMEOUT="120"'
    do
      local _var_name
      _var_name=$(echo "$_var_line" | sed 's/export \([^=]*\)=.*/\1/')
      if ! grep -q "^export ${_var_name}=" "$env_file" 2>/dev/null; then
        echo "$_var_line" >> "$env_file"
        _added=$((_added + 1))
      fi
    done
    if [ "$_added" -gt 0 ]; then
      echo "  Added $_added missing var(s) to $env_file"
    fi
  fi
}

patch_shell_profile() {
  local source_line='[ -f "$HOME/.claude/ai-dev-workflow/aidw.env.sh" ] && source "$HOME/.claude/ai-dev-workflow/aidw.env.sh"'
  local profile=""

  if [ "$(uname)" = "Darwin" ] && [ -f "$HOME/.zshrc" ]; then
    profile="$HOME/.zshrc"
  elif [ -f "$HOME/.bashrc" ]; then
    profile="$HOME/.bashrc"
  elif [ -f "$HOME/.bash_profile" ]; then
    profile="$HOME/.bash_profile"
  fi

  if [ -z "$profile" ]; then
    echo ""
    echo "NOTE: Could not detect a shell profile (~/.zshrc, ~/.bashrc, ~/.bash_profile)."
    echo "To load ai-dev-workflow env settings automatically, add this line to your shell profile:"
    echo ""
    echo "  $source_line"
    echo ""
    return 0
  fi

  if grep -qF "aidw.env.sh" "$profile" 2>/dev/null; then
    echo "Shell profile already sources aidw.env.sh: $profile (skipping)"
    return 0
  fi

  {
    printf '\n'
    printf '# BEGIN ai-dev-workflow managed block\n'
    printf '# ai-dev-workflow env configuration -- managed by ai-dev-workflow install.sh\n'
    printf '# Do not manually remove these markers; re-run install.sh to update.\n'
    printf '%s\n' "$source_line"
    printf '# END ai-dev-workflow managed block\n'
  } >> "$profile" || {
    echo ""
    echo "WARNING: Could not write to $profile."
    echo "Add this line to your shell profile manually:"
    echo ""
    echo "  $source_line"
    echo ""
    return 0
  }
  echo "Added aidw env source line to: $profile"
  echo "Reload with:  source $profile"
}

configure_gemini_review() {
  local env_file="$CLAUDE_HOME/ai-dev-workflow/aidw.env.sh"

  # Skip if env file missing or not interactive
  [ -f "$env_file" ] || return 0
  [ -t 0 ] || return 0

  # Skip if already explicitly enabled
  if grep -q '^export AIDW_GEMINI_REVIEW="1"' "$env_file" 2>/dev/null; then
    echo "Gemini adversarial review: already enabled in $env_file"
    return 0
  fi

  echo ""
  if command -v gemini &>/dev/null; then
    echo "Gemini CLI detected: $(command -v gemini)"
  else
    echo "Gemini CLI not found (can be installed later: npm install -g @google/gemini-cli)"
  fi
  printf "Enable Gemini adversarial review? [y/N] "
  read -r _gemini_response </dev/tty || _gemini_response=""
  echo ""

  if [[ "${_gemini_response:-}" =~ ^[Yy]$ ]]; then
    "$PYTHON_BIN" - "$env_file" <<'PY'
import sys, re
from pathlib import Path

env_file = Path(sys.argv[1])
text = env_file.read_text(encoding="utf-8")

# Uncomment or update any existing AIDW_GEMINI_REVIEW line to "1"
updated = re.sub(
    r'^#*\s*export AIDW_GEMINI_REVIEW=.*$',
    'export AIDW_GEMINI_REVIEW="1"',
    text,
    flags=re.MULTILINE,
)
if updated == text:
    # Line not present at all — append it
    updated = text.rstrip("\n") + '\nexport AIDW_GEMINI_REVIEW="1"\n'

env_file.write_text(updated, encoding="utf-8")
print("Gemini adversarial review enabled.")
PY
    if ! command -v gemini &>/dev/null; then
      echo "  Install:       npm install -g @google/gemini-cli"
      echo "  Authenticate:  gemini auth login"
    fi
  else
    echo "Gemini adversarial review skipped. To enable later:"
    echo "  Set AIDW_GEMINI_REVIEW=1 in $env_file"
  fi
}

write_env_file
patch_shell_profile
configure_gemini_review

echo
echo "ai-dev-workflow installed."
echo "Workspace bootstrapped: $WORKSPACE_ROOT"
echo "Claude home: $CLAUDE_HOME"
echo
echo "Suggested next step inside a repo: /wip-start"
echo ""
echo "Repository intelligence tools:"
echo "  Serena   → semantic code navigation (symbol lookup, call chains, file discovery)"
echo "  Context7 → up-to-date library documentation (API usage, framework examples)"
