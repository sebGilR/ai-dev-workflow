#!/usr/bin/env python3
"""Additive MCP config merger.

Merges MCP_SERVERS entries into ~/.claude/mcp.json without overwriting
any existing server entries the user has already configured.
"""
from __future__ import annotations
import json
import sys
from pathlib import Path

MCP_SERVERS = {
    "serena": {
        "command": "npx",
        "args": ["-y", "serena-mcp"],
    },
    "context7": {
        "command": "npx",
        "args": ["-y", "@upstash/context7-mcp"],
    },
}


def main() -> int:
    mcp_path = Path.home() / ".claude" / "mcp.json"

    if mcp_path.exists():
        try:
            existing = json.loads(mcp_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            print(f"WARNING: {mcp_path} contains invalid JSON ({exc}). Skipping MCP merge.", file=sys.stderr)
            return 1
    else:
        existing = {}

    servers = existing.setdefault("mcpServers", {})
    added = []
    for name, config in MCP_SERVERS.items():
        if name not in servers:
            servers[name] = config
            added.append(name)

    mcp_path.parent.mkdir(parents=True, exist_ok=True)
    content = json.dumps(existing, indent=2) + "\n"
    tmp = mcp_path.with_name(mcp_path.name + ".tmp")
    tmp.write_text(content, encoding="utf-8")
    tmp.replace(mcp_path)

    if added:
        print(f"MCP servers added: {', '.join(added)}")
    else:
        print("MCP servers already configured (no changes made).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
