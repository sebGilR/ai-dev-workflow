#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
WORKSPACE="${1:-}"

if [ -n "$WORKSPACE" ]; then
  exec "$PYTHON_BIN" "$SCRIPT_DIR/aidw.py" verify --workspace "$WORKSPACE"
else
  exec "$PYTHON_BIN" "$SCRIPT_DIR/aidw.py" verify
fi
