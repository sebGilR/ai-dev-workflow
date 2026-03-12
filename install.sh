#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CLAUDE_HOME="${CLAUDE_HOME:-$HOME/.claude}"
COPILOT_HOME="${COPILOT_HOME:-$HOME/.copilot}"

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
  if ! safe_link "$skill_dir" "$COPILOT_HOME/skills/$skill_name"; then
    echo "WARNING: Skipping Copilot skill '$skill_name' because '$COPILOT_HOME/skills/$skill_name' already exists and is not a managed symlink." >&2
  fi
done

for agent_file in "$SCRIPT_DIR"/claude/agents/*; do
  [ -f "$agent_file" ] || continue
  agent_name="$(basename "$agent_file")"
  safe_link "$agent_file" "$CLAUDE_HOME/agents/$agent_name"
done

# Build the Go binary if needed (required by merge-* commands below).
if [ ! -x "$SCRIPT_DIR/bin/aidw" ] || ! "$SCRIPT_DIR/bin/aidw" --version &>/dev/null 2>&1; then
  echo "→ Building aidw Go binary..."
  if command -v go &>/dev/null; then
    (cd "$SCRIPT_DIR" && go build -o "bin/aidw-$(go env GOOS)-$(go env GOARCH)" ./cmd/aidw) || {
      echo "ERROR: 'go build' failed. Install Go from https://go.dev/dl/ and retry." >&2
      exit 1
    }
  else
    echo "ERROR: Go toolchain not found. Install from https://go.dev/dl/ and retry." >&2
    exit 1
  fi
fi

"$SCRIPT_DIR/bin/aidw" merge-claude-md \
  --claude-md "$CLAUDE_HOME/CLAUDE.md" \
  --snippet "$SCRIPT_DIR/templates/global/claude_managed_block.md"

"$SCRIPT_DIR/bin/aidw" merge-settings \
  --settings "$CLAUDE_HOME/settings.json" \
  --template "$SCRIPT_DIR/templates/global/settings.template.json"


"$SCRIPT_DIR/bin/aidw" update-global-gitignore

# Bootstrap the workspace if WORKSPACE_ROOT is set.
if [ -n "$WORKSPACE_ROOT" ]; then
  "$SCRIPT_DIR/bin/aidw" bootstrap-workspace "$WORKSPACE_ROOT"
fi

# ---------------------------------------------------------------------------
# MCP server configuration (Serena, Context7)
# ---------------------------------------------------------------------------

_mcp_missing=()
command -v uvx &>/dev/null || _mcp_missing+=("uvx (required for Serena; install via: curl -LsSf https://astral.sh/uv/install.sh | sh)")
command -v npx &>/dev/null || _mcp_missing+=("npx (required for Context7; install Node.js from https://nodejs.org)")

if [ "${#_mcp_missing[@]}" -eq 0 ]; then
    echo "→ Configuring MCP servers (Serena, Context7)..."
    "$SCRIPT_DIR/bin/aidw" merge-mcp-json || {
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
    # Uncomment or update AIDW_GEMINI_REVIEW in env_file
    if grep -qE '^#*[[:space:]]*export AIDW_GEMINI_REVIEW=' "$env_file" 2>/dev/null; then
      awk '{if (/^#*[[:space:]]*export AIDW_GEMINI_REVIEW=/) {print "export AIDW_GEMINI_REVIEW=\"1\""} else {print}}' \
        "$env_file" > "$env_file.tmp" && mv "$env_file.tmp" "$env_file"
    else
      printf '\nexport AIDW_GEMINI_REVIEW="1"\n' >> "$env_file"
    fi
    echo "Gemini adversarial review enabled."
    if ! command -v gemini &>/dev/null; then
      echo "  Install:       npm install -g @google/gemini-cli"
      echo "  Authenticate:  gemini auth login"
    fi
  else
    echo "Gemini adversarial review skipped. To enable later:"
    echo "  Set AIDW_GEMINI_REVIEW=1 in $env_file"
  fi
}

configure_repo_gitignore() {
  # Skip if workspace root is not a git repo or not running interactively
  [ -d "$WORKSPACE_ROOT/.git" ] || return 0
  [ -t 0 ] || return 0

  local exclude_file="$WORKSPACE_ROOT/.git/info/exclude"
  local _global_gi _gi_choice _p
  local _GI_PATHS=(
    ".github/copilot-instructions.md"
    ".github/skills/"
    ".github/agents/"
    ".claude/repo-docs/"
    ".claude/settings.local.json"
  )

  # Idempotency: skip if all seeded entries are already present in either location
  local _all_local=1 _all_global=1
  for _p in "${_GI_PATHS[@]}"; do
    grep -qF "$_p" "$exclude_file" 2>/dev/null || _all_local=0
  done
  if [ "$_all_local" -eq 1 ]; then return 0; fi

  _global_gi=$(git config --global core.excludesfile 2>/dev/null || true)
  if [ -n "$_global_gi" ]; then
    for _p in "${_GI_PATHS[@]}"; do
      grep -qF "$_p" "$_global_gi" 2>/dev/null || _all_global=0
    done
    if [ "$_all_global" -eq 1 ]; then return 0; fi
  fi

  echo ""
  echo "Bootstrap seeded these files into $WORKSPACE_ROOT:"
  echo "  .github/copilot-instructions.md"
  echo "  .github/skills/   (skill definitions)"
  echo "  .github/agents/   (agent definitions)"
  echo "  .claude/repo-docs/            (local repo context docs)"
  echo "  .claude/settings.local.json   (local Claude settings override)"
  echo ""
  echo "How should these be gitignored?"
  echo "  1) Locally   — add to .git/info/exclude  (this repo only, no committed changes)"
  echo "  2) Globally  — add to global gitignore   (applies across all your repos)"
  echo "  3) Not at all — skip                     (commit them, e.g. for GitHub Copilot)"
  printf "Choose [1/2/3] (default: 1): "
  read -r _gi_choice </dev/tty || _gi_choice=""
  echo ""

  case "${_gi_choice:-1}" in
    1)
      for _p in "${_GI_PATHS[@]}"; do
        if ! grep -qF "$_p" "$exclude_file" 2>/dev/null; then
          printf '%s\n' "$_p" >> "$exclude_file"
        fi
      done
      echo "Added to $exclude_file (local, no .gitignore file changes)."
      ;;
    2)
      local _gi_args=()
      for _p in "${_GI_PATHS[@]}"; do _gi_args+=("--add" "$_p"); done
      if ! "$SCRIPT_DIR/bin/aidw" update-global-gitignore "${_gi_args[@]}"; then
        echo "Warning: failed to update global gitignore; please add entries manually." >&2
      fi
      ;;
    3)
      local _hint_global="${_global_gi:-~/.gitignore_global}"
      echo "Skipped. To configure later, re-run install.sh and choose 1 or 2, or add manually:"
      for _p in "${_GI_PATHS[@]}"; do
        echo "  echo '$_p' >> $WORKSPACE_ROOT/.git/info/exclude"
      done
      echo "  # or: bin/aidw update-global-gitignore --add ${_GI_PATHS[*]}"
      ;;
    *)
      echo "Unrecognized choice; skipping gitignore configuration."
      ;;
  esac
}

write_env_file
patch_shell_profile
configure_gemini_review
configure_repo_gitignore

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
