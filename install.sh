#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_HOME="${CLAUDE_HOME:-$HOME/.claude}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

# Parse arguments — positional arg is workspace root; flags are --option style.
WORKSPACE_ROOT=""
PULL_OLLAMA_MODELS=0
for _arg in "$@"; do
  case "$_arg" in
    --pull-ollama-models) PULL_OLLAMA_MODELS=1 ;;
    --*) echo "Unknown flag: $_arg" >&2; exit 1 ;;
    *) WORKSPACE_ROOT="$_arg" ;;
  esac
done
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$PWD}"

mkdir -p "$CLAUDE_HOME"

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

mkdir -p "$CLAUDE_HOME/skills" "$CLAUDE_HOME/agents"

for skill_dir in "$SCRIPT_DIR"/claude/skills/*; do
  [ -d "$skill_dir" ] || continue
  skill_name="$(basename "$skill_dir")"
  safe_link "$skill_dir" "$CLAUDE_HOME/skills/$skill_name"
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
# Ollama environment setup
# ---------------------------------------------------------------------------

write_ollama_env() {
  local env_file="$CLAUDE_HOME/ai-dev-workflow/aidw.env.sh"
  if [ -f "$env_file" ]; then
    echo "Ollama env file already exists: $env_file (skipping)"
    return 0
  fi
  cat > "$env_file" << 'ENVEOF'
# ai-dev-workflow managed Ollama model configuration
# Edit this file to customise your models. It is NOT committed to git.
# Sourced automatically by your shell profile after install.
#
# Task routing (16 GB Mac defaults):
#   fast  (docs-needed, summaries, synthesis)         -> phi3:mini
#   review (bug-risk, missing-tests, regression-risk) -> qwen2.5-coder:7b
#   generate (generate-code, debug-patch, patch-draft) -> deepseek-coder:6.7b
export AIDW_OLLAMA_MODEL_FAST="phi3:mini"
export AIDW_OLLAMA_MODEL_REVIEW="qwen2.5-coder:7b"
export AIDW_OLLAMA_MODEL_GENERATE="deepseek-coder:6.7b"
export AIDW_OLLAMA_MODEL="${AIDW_OLLAMA_MODEL_REVIEW}"
export AIDW_OLLAMA_ENDPOINT="http://localhost:11434"
ENVEOF
  echo "Created Ollama env file:  $env_file"
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
    echo "To load Ollama model settings automatically, add this line to your shell profile:"
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
    printf '# Ollama model configuration -- managed by ai-dev-workflow install.sh\n'
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
  echo "Added Ollama env source line to: $profile"
  echo "Reload with:  source $profile"
}

write_ollama_env
patch_shell_profile

# ---------------------------------------------------------------------------
# Optional: pull recommended Ollama models
# ---------------------------------------------------------------------------

if [ "$PULL_OLLAMA_MODELS" = "1" ]; then
  echo ""
  echo "Pulling recommended Ollama models..."
  if command -v ollama >/dev/null 2>&1; then
    ollama pull phi3:mini
    ollama pull qwen2.5-coder:7b
    ollama pull deepseek-coder:6.7b
    echo "Done pulling models."
  else
    echo "Ollama binary not found. Install Ollama first:"
    echo "  macOS: brew install ollama"
    echo "  Linux: curl -fsSL https://ollama.com/install.sh | sh"
    echo "Then re-run: ./install.sh --pull-ollama-models"
  fi
elif command -v ollama >/dev/null 2>&1; then
  echo ""
  echo "Ollama detected. Checking recommended model status..."
  _missing=()
  for _model in "phi3:mini" "qwen2.5-coder:7b" "deepseek-coder:6.7b"; do
    if ! ollama list 2>/dev/null | grep -qF "$_model"; then
      _missing+=("$_model")
    fi
  done
  if [ "${#_missing[@]}" -gt 0 ]; then
    echo "Missing recommended model(s):"
    for _m in "${_missing[@]}"; do echo "  - $_m"; done
    echo "Pull individually, or re-run:  ./install.sh --pull-ollama-models"
  else
    echo "All recommended Ollama models are available."
  fi
fi

echo
echo "ai-dev-workflow installed."
echo "Workspace bootstrapped: $WORKSPACE_ROOT"
echo "Claude home: $CLAUDE_HOME"
echo
echo "Ollama env file: $CLAUDE_HOME/ai-dev-workflow/aidw.env.sh"
echo "Ollama config:   aidw ollama-config"
echo "Ollama check:    aidw ollama-check"
echo
echo "Suggested next step inside a repo: /wip-start"
echo ""
echo "Repository intelligence tools:"
echo "  Serena   → semantic code navigation (symbol lookup, call chains, file discovery)"
echo "  Context7 → up-to-date library documentation (API usage, framework examples)"
