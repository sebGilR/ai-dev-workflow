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

# Install serena log hook (serena-query lives in bin/ and is reachable via the
# ~/.claude/ai-dev-workflow symlink once PATH is configured in aidw.env.sh).
mkdir -p "$CLAUDE_HOME/hooks"
_serena_hook_installed=false
if [ -f "$SCRIPT_DIR/templates/global/hooks/log-serena.sh" ]; then
  chmod +x "$SCRIPT_DIR/templates/global/hooks/log-serena.sh"
  if safe_link "$SCRIPT_DIR/templates/global/hooks/log-serena.sh" "$CLAUDE_HOME/hooks/log-serena.sh"; then
    chmod +x "$CLAUDE_HOME/hooks/log-serena.sh" 2>/dev/null || true
    _serena_hook_installed=true
  fi
fi

# Register the PostToolUse hook for Serena logging in ~/.claude/settings.json.
_register_serena_hook() {
  local settings="$CLAUDE_HOME/settings.json"
  local hook_cmd="$CLAUDE_HOME/hooks/log-serena.sh"

  # Ensure settings.json exists with a base object.
  if [ ! -f "$settings" ]; then
    echo '{}' > "$settings"
  fi

  # Idempotency: skip if hook is already registered.
  if python3 - "$settings" "$hook_cmd" <<'IDEMPOTENCY_CHECK' 2>/dev/null
import json, sys
settings_path, hook_cmd = sys.argv[1], sys.argv[2]
d = json.load(open(settings_path))
for h in d.get('hooks', {}).get('PostToolUse', []):
    for entry in h.get('hooks', []):
        if hook_cmd in entry.get('command', ''):
            sys.exit(0)
sys.exit(1)
IDEMPOTENCY_CHECK
  then
    return 0
  fi

  python3 - "$settings" "$hook_cmd" << 'PYEOF'
import json, sys, shutil
settings_path, hook_cmd = sys.argv[1], sys.argv[2]
try:
    with open(settings_path) as f:
        d = json.load(f)
except json.JSONDecodeError:
    backup = settings_path + '.bak'
    shutil.copy2(settings_path, backup)
    print(f"WARNING: {settings_path} contained invalid JSON. Backed up to {backup} and reset.", file=sys.stderr)
    d = {}
d.setdefault('hooks', {}).setdefault('PostToolUse', []).append({
    "matcher": "mcp__serena__.*",
    "hooks": [{"type": "command", "command": hook_cmd}]
})
with open(settings_path, 'w') as f:
    json.dump(d, f, indent=2)
    f.write('\n')
print(f"Registered Serena PostToolUse hook in {settings_path}")
PYEOF
}
if [ "$_serena_hook_installed" = true ]; then
  echo ""
  echo "NOTE: Registering a PostToolUse hook for Serena MCP calls."
  echo "      The hook logs tool name and input parameters to ~/.claude/serena.log."
  echo "      This log is local — nothing is sent remotely."
  echo "      To opt out: remove the mcp__serena\\.\\* entry from ~/.claude/settings.json"
  echo "      and delete ~/.claude/hooks/log-serena.sh"
  echo ""
  _register_serena_hook
fi

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

# ---------------------------------------------------------------------------
# Generate .github/agents/ from claude/agents/ (strips MCP-only sub-sections)
# ---------------------------------------------------------------------------
# Produces copies of claude/agents/*.md into WORKSPACE_ROOT/.github/agents/
# with the "### 1. Serena MCP" sub-section removed (MCP is unavailable in
# Copilot CLI). Sections 2 (serena-query bridge) and 3 (Navigation Strategy)
# are kept verbatim — they work with Bash access.
generate_github_agents() {
  local src_dir="$SCRIPT_DIR/claude/agents"
  local dest_dir="$WORKSPACE_ROOT/.github/agents"

  [ -d "$src_dir" ] || return 0
  mkdir -p "$dest_dir"

  for src_file in "$src_dir"/*.md; do
    [ -f "$src_file" ] || continue
    local agent_name
    agent_name="$(basename "$src_file")"
    local dest_file="$dest_dir/$agent_name"

    # Strip "### 1. Serena MCP ..." sub-section (everything from that heading
    # through the blank line before the next ### or ## heading, or end of block).
    local stripped
    stripped="$(awk '
      /^### 1\. Serena MCP/ { skip=1; next }
      skip && /^###/ { skip=0 }
      skip && /^## / { skip=0 }
      skip { next }
      /^### [0-9]+\./ { sub(/^### [0-9]+\./, "### " ++n "."); print; next }
      { print }
    ' "$src_file")"

    # Skip write if content is identical (idempotent).
    if [ -f "$dest_file" ] && [ "$stripped" = "$(cat "$dest_file")" ]; then
      continue
    fi

    if [ -f "$dest_file" ]; then
      echo "WARNING: Overwriting $dest_file (content differs from generated version)." >&2
      echo "         Any customizations in this file will be lost." >&2
    fi
    printf '%s\n' "$stripped" > "$dest_file"
  done
}

if [ -d "$WORKSPACE_ROOT/.git" ]; then
  generate_github_agents
fi

# ---------------------------------------------------------------------------
# Auto-install Go locally if not present
# ---------------------------------------------------------------------------
# Usage: ensure_go [install_dir]
# Sets PATH and GOROOT if Go is installed locally
ensure_go() {
  local install_dir="${1:-$SCRIPT_DIR/.go}"

  # Already available system-wide?
  if command -v go &>/dev/null; then
    return 0
  fi

  # Already installed locally?
  if [ -x "$install_dir/bin/go" ]; then
    export PATH="$install_dir/bin:$PATH"
    export GOROOT="$install_dir"
    return 0
  fi

  echo "→ Go not found. Installing locally (no sudo required)..."

  local os arch version tarball url tmp_dir

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64)    arch="amd64" ;;
    aarch64)   arch="arm64" ;;
    armv*)     arch="armv6l" ;;
    i386|i686) arch="386" ;;
  esac

  # Fetch latest stable version
  version=""
  if command -v curl &>/dev/null; then
    version=$(curl -sL "https://go.dev/VERSION?m=text" 2>/dev/null | head -1)
  elif command -v wget &>/dev/null; then
    version=$(wget -qO- "https://go.dev/VERSION?m=text" 2>/dev/null | head -1)
  fi

  # Validate or use fallback (update this version periodically as Go releases new versions)
  if ! [[ "$version" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
    version="go1.22.5"
  fi

  tarball="${version}.${os}-${arch}.tar.gz"
  url="https://go.dev/dl/${tarball}"
  tmp_dir=$(mktemp -d)

  echo "  Version: ${version}"
  echo "  Platform: ${os}-${arch}"
  echo "  Downloading from: ${url}"

  local download_ok=0
  if command -v curl &>/dev/null; then
    curl -sL "$url" -o "$tmp_dir/go.tar.gz" && download_ok=1
  elif command -v wget &>/dev/null; then
    wget -q "$url" -O "$tmp_dir/go.tar.gz" && download_ok=1
  else
    echo "ERROR: Neither curl nor wget found." >&2
    rm -rf "$tmp_dir"
    return 1
  fi

  if [ "$download_ok" -ne 1 ] || ! tar tf "$tmp_dir/go.tar.gz" &>/dev/null; then
    rm -rf "$tmp_dir"
    echo "ERROR: Failed to download/validate Go tarball." >&2
    echo "       Install Go manually: https://go.dev/dl/" >&2
    return 1
  fi

  echo "  Extracting to: ${install_dir}"
  # Extract to a sibling temp dir first, then atomically swap so that a failed
  # extraction never leaves the user without a working Go toolchain.
  extract_tmp="${install_dir}.tmp.$$"
  mkdir -p "$extract_tmp"
  tar -C "$extract_tmp" --strip-components=1 -xzf "$tmp_dir/go.tar.gz"
  rm -rf "$tmp_dir"

  if [ ! -x "$extract_tmp/bin/go" ]; then
    rm -rf "$extract_tmp"
    echo "ERROR: Go binary not found after extraction." >&2
    return 1
  fi

  rm -rf "$install_dir"
  mv "$extract_tmp" "$install_dir"

  export PATH="$install_dir/bin:$PATH"
  export GOROOT="$install_dir"

  echo "  Go ${version} installed: $("$install_dir/bin/go" version)"
  return 0
}

# Build the Go binary if needed (required by merge-* commands below).
if [ ! -x "$SCRIPT_DIR/bin/aidw" ] || ! "$SCRIPT_DIR/bin/aidw" --version &>/dev/null 2>&1; then
  echo "→ Building aidw Go binary..."
  ensure_go "$SCRIPT_DIR/.go" || exit 1
  (cd "$SCRIPT_DIR" && go build -o "bin/aidw-$(go env GOOS)-$(go env GOARCH)" ./cmd/aidw) || {
    echo "ERROR: 'go build' failed." >&2
    exit 1
  }
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
# Serena LSP setup — detect workspace language and install required LSP binary
# ---------------------------------------------------------------------------
# Only Go requires an explicit binary install; all other languages Serena
# supports are either bundled (Python/pyright), auto-downloaded (TS/Bash via
# npm, Java via GitHub release), or handled by the user's toolchain (Rust via
# rustup, which Serena triggers automatically at runtime).
install_workspace_lsp() {
  [ -d "$WORKSPACE_ROOT" ] || return 0

  # --- Go ---
  if [ -f "$WORKSPACE_ROOT/go.mod" ]; then
    if ! command -v go &>/dev/null; then
      echo "NOTE: Go project detected (go.mod found) but 'go' is not on PATH." >&2
      echo "      Serena's Go LSP will be unavailable until Go is installed." >&2
      return 0
    fi
    # Ensure ~/go/bin (where 'go install' places binaries) is on PATH.
    export PATH="$HOME/go/bin:$PATH"
    if command -v gopls &>/dev/null; then
      echo "→ gopls already installed: $(command -v gopls)"
    else
      echo "→ Go project detected: installing gopls (required for Serena Go LSP)..."
      # Pin to a specific version to avoid supply-chain risk from @latest.
      # Update this version after vetting each new gopls release.
      go install golang.org/x/tools/gopls@v0.15.3 && \
        echo "  gopls installed: $(command -v gopls 2>/dev/null || echo '~/go/bin/gopls')" || \
        echo "WARNING: gopls install failed. Serena Go LSP will be unavailable." >&2
    fi
  fi

  # --- Rust ---
  # Serena calls 'rustup component add rust-analyzer' automatically at runtime
  # if rustup is present. We just need ~/.cargo/bin on PATH for the project server.
  if [ -f "$WORKSPACE_ROOT/Cargo.toml" ] && [ -d "$HOME/.cargo/bin" ]; then
    export PATH="$HOME/.cargo/bin:$PATH"
    echo "→ Rust project detected: ~/.cargo/bin added to PATH (rust-analyzer provisioned by Serena at runtime)"
  fi

  # --- C/C++ ---
  if [ -f "$WORKSPACE_ROOT/CMakeLists.txt" ] || \
     compgen -G "$WORKSPACE_ROOT/*.c" > /dev/null 2>&1 || \
     compgen -G "$WORKSPACE_ROOT/*.cpp" > /dev/null 2>&1; then
    if ! command -v clangd &>/dev/null; then
      echo "NOTE: C/C++ project detected but 'clangd' is not installed." >&2
      echo "      Install it for Serena LSP support: brew install llvm  (macOS)" >&2
      echo "                                          apt install clangd  (Debian/Ubuntu)" >&2
    fi
  fi
}

if [ -n "$WORKSPACE_ROOT" ]; then
  install_workspace_lsp
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

# Add ai-dev-workflow bin/ to PATH (provides serena-query and other helpers).
[[ ":$PATH:" != *":$HOME/.claude/ai-dev-workflow/bin:"* ]] && export PATH="$HOME/.claude/ai-dev-workflow/bin:$PATH"

# Add Go bin/ to PATH (provides gopls for Serena Go LSP support).
[[ ":$PATH:" != *":$HOME/go/bin:"* ]] && export PATH="$HOME/go/bin:$PATH"

# Adversarial review (optional) — choose a provider: gemini, copilot, codex
# Set AIDW_ADVERSARIAL_REVIEW=1 to enable
export AIDW_ADVERSARIAL_REVIEW="0"
export AIDW_ADVERSARIAL_PROVIDER="gemini"
export AIDW_ADVERSARIAL_MODEL="gemini-2.5-pro"
export AIDW_ADVERSARIAL_TIMEOUT="120"
# Legacy aliases (deprecated — kept for backward compatibility):
# export AIDW_GEMINI_REVIEW="0"
# export AIDW_GEMINI_MODEL="gemini-2.5-pro"
# export AIDW_GEMINI_TIMEOUT="120"
ENVEOF
    echo "Created env file:  $env_file"
  else
    echo "Env file already exists: $env_file (appending missing vars if needed)"
    local _added=0
    # Ensure PATH includes ai-dev-workflow/bin (idempotent guard).
    if ! grep -q "ai-dev-workflow/bin" "$env_file" 2>/dev/null; then
      echo '[[ ":$PATH:" != *":$HOME/.claude/ai-dev-workflow/bin:"* ]] && export PATH="$HOME/.claude/ai-dev-workflow/bin:$PATH"' >> "$env_file"
      _added=$((_added + 1))
    fi
    # Ensure PATH includes ~/go/bin for gopls.
    if ! grep -q 'go/bin' "$env_file" 2>/dev/null; then
      echo '[[ ":$PATH:" != *":$HOME/go/bin:"* ]] && export PATH="$HOME/go/bin:$PATH"' >> "$env_file"
      _added=$((_added + 1))
    fi
    # Migrate AIDW_ADVERSARIAL_REVIEW: preserve legacy enablement if AIDW_GEMINI_REVIEW="1".
    if ! grep -q '^export AIDW_ADVERSARIAL_REVIEW=' "$env_file" 2>/dev/null; then
      if grep -q '^export AIDW_GEMINI_REVIEW="1"' "$env_file" 2>/dev/null; then
        echo 'export AIDW_ADVERSARIAL_REVIEW="1"' >> "$env_file"
      else
        echo 'export AIDW_ADVERSARIAL_REVIEW="0"' >> "$env_file"
      fi
      _added=$((_added + 1))
    fi
    for _var_line in \
      'export AIDW_ADVERSARIAL_PROVIDER="gemini"' \
      'export AIDW_ADVERSARIAL_MODEL="gemini-2.5-pro"' \
      'export AIDW_ADVERSARIAL_TIMEOUT="120"'
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

configure_adversarial_review() {
  local env_file="$CLAUDE_HOME/ai-dev-workflow/aidw.env.sh"

  # Skip if env file missing or not interactive
  [ -f "$env_file" ] || return 0
  [ -t 0 ] || return 0

  # Skip if already explicitly enabled (new or legacy var)
  if grep -q '^export AIDW_ADVERSARIAL_REVIEW="1"' "$env_file" 2>/dev/null || \
     grep -q '^export AIDW_GEMINI_REVIEW="1"' "$env_file" 2>/dev/null; then
    echo "Adversarial review: already enabled in $env_file"
    return 0
  fi

  echo ""

  # Detect available providers
  local _available=()
  if command -v gemini &>/dev/null; then
    _available+=("gemini")
  fi
  if command -v copilot &>/dev/null; then
    _available+=("copilot")
  fi
  if command -v codex &>/dev/null; then
    _available+=("codex")
  fi

  if [ ${#_available[@]} -eq 0 ]; then
    echo "Adversarial review: no supported providers detected (gemini, copilot, codex)."
    echo "  To enable later, install a provider and set AIDW_ADVERSARIAL_REVIEW=1 in $env_file"
    return 0
  fi

  echo "Adversarial review providers detected:"
  local _i=1
  for _p in "${_available[@]}"; do
    echo "  $_i) $_p"
    _i=$((_i + 1))
  done
  echo "  $_i) none (skip)"
  printf "Choose adversarial review provider [1-%s]: " "$_i"
  read -r _choice </dev/tty || _choice=""
  echo ""

  local _chosen=""
  if [[ "$_choice" =~ ^[0-9]+$ ]] && [ "$_choice" -ge 1 ] && [ "$_choice" -lt "$_i" ]; then
    _chosen="${_available[$((_choice - 1))]}"
  fi

  if [ -n "$_chosen" ]; then
    # Write or update AIDW_ADVERSARIAL_REVIEW
    if grep -qE '^#*[[:space:]]*export AIDW_ADVERSARIAL_REVIEW=' "$env_file" 2>/dev/null; then
      awk '{if (/^#*[[:space:]]*export AIDW_ADVERSARIAL_REVIEW=/) {print "export AIDW_ADVERSARIAL_REVIEW=\"1\""} else {print}}' \
        "$env_file" > "$env_file.tmp" && mv "$env_file.tmp" "$env_file"
    else
      printf '\nexport AIDW_ADVERSARIAL_REVIEW="1"\n' >> "$env_file"
    fi
    # Write or update AIDW_ADVERSARIAL_PROVIDER
    if grep -qE '^#*[[:space:]]*export AIDW_ADVERSARIAL_PROVIDER=' "$env_file" 2>/dev/null; then
      awk -v p="$_chosen" '{if (/^#*[[:space:]]*export AIDW_ADVERSARIAL_PROVIDER=/) {print "export AIDW_ADVERSARIAL_PROVIDER=\""p"\""} else {print}}' \
        "$env_file" > "$env_file.tmp" && mv "$env_file.tmp" "$env_file"
    else
      printf 'export AIDW_ADVERSARIAL_PROVIDER="%s"\n' "$_chosen" >> "$env_file"
    fi
    # For non-gemini providers, clear AIDW_ADVERSARIAL_MODEL so the provider uses its own default.
    if [ "$_chosen" != "gemini" ]; then
      if grep -qE '^#*[[:space:]]*export AIDW_ADVERSARIAL_MODEL=' "$env_file" 2>/dev/null; then
        awk '{if (/^#*[[:space:]]*export AIDW_ADVERSARIAL_MODEL=/) {print "export AIDW_ADVERSARIAL_MODEL=\"\""} else {print}}' \
          "$env_file" > "$env_file.tmp" && mv "$env_file.tmp" "$env_file"
      fi
    fi
    echo "Adversarial review enabled with provider: $_chosen"
    case "$_chosen" in
      gemini)
        if ! command -v gemini &>/dev/null; then
          echo "  gemini CLI not found — see https://github.com/google-gemini/gemini-cli"
        fi
        ;;
      copilot)
        if ! command -v copilot &>/dev/null; then
          echo "  copilot CLI not found — see https://github.com/github/copilot-cli"
        fi
        ;;
      codex)
        if ! command -v codex &>/dev/null; then
          echo "  codex CLI not found — see https://github.com/openai/codex"
        fi
        ;;
    esac
  else
    echo "Adversarial review skipped. To enable later:"
    echo "  Set AIDW_ADVERSARIAL_REVIEW=1 and AIDW_ADVERSARIAL_PROVIDER=<gemini|copilot|codex> in $env_file"
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
configure_adversarial_review
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
echo "             To enable fast symbol lookup via serena-query, register this repo:"
echo "             uvx --from git+https://github.com/oraios/serena@v0.1.4 serena project create"
echo "             (Run from the repo root. One-time per repo.)"
echo "             Language servers are installed automatically when detected."
echo "             Serena calls will be logged to ~/.claude/serena.log"
echo "  Context7 → up-to-date library documentation (API usage, framework examples)"
