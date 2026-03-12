#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE="${1:-}"

if [ -n "$WORKSPACE" ]; then
  exec "$SCRIPT_DIR/../bin/aidw" verify --workspace "$WORKSPACE"
else
  exec "$SCRIPT_DIR/../bin/aidw" verify
fi
