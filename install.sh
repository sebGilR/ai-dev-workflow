#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="${1:-$PWD}"
CLAUDE_HOME="${CLAUDE_HOME:-$HOME/.claude}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

mkdir -p "$CLAUDE_HOME"

# Keep a stable link back to this repo so skills can reference scripts reliably.
ln -sfn "$SCRIPT_DIR" "$CLAUDE_HOME/ai-dev-workflow"

mkdir -p "$CLAUDE_HOME/skills" "$CLAUDE_HOME/agents"

link_dir() {
  local src="$1"
  local dest="$2"
  mkdir -p "$(dirname "$dest")"
  rm -rf "$dest"
  ln -s "$src" "$dest"
}

for skill_dir in "$SCRIPT_DIR"/claude/skills/*; do
  [ -d "$skill_dir" ] || continue
  skill_name="$(basename "$skill_dir")"
  link_dir "$skill_dir" "$CLAUDE_HOME/skills/$skill_name"
done

for agent_file in "$SCRIPT_DIR"/claude/agents/*; do
  [ -f "$agent_file" ] || continue
  agent_name="$(basename "$agent_file")"
  link_dir "$agent_file" "$CLAUDE_HOME/agents/$agent_name"
done

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/merge_claude_md.py" \
  --claude-md "$CLAUDE_HOME/CLAUDE.md" \
  --snippet "$SCRIPT_DIR/templates/global/claude_managed_block.md"

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/merge_settings.py" \
  --settings "$CLAUDE_HOME/settings.json" \
  --template "$SCRIPT_DIR/templates/global/settings.template.json"

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/update_global_gitignore.py"

"$PYTHON_BIN" "$SCRIPT_DIR/scripts/aidw.py" bootstrap-workspace "$WORKSPACE_ROOT"

echo
echo "ai-dev-workflow installed."
echo "Workspace bootstrapped: $WORKSPACE_ROOT"
echo "Claude home: $CLAUDE_HOME"
echo
echo "Suggested next step inside a repo: /wip-start"
