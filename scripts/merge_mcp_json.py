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
        "command": "uvx",
        "args": [
            "--from",
            "git+https://github.com/oraios/serena@v0.1.4",
            "serena",
            "start-mcp-server",
        ],
    },
    "context7": {
        "command": "npx",
        "args": ["-y", "@upstash/context7-mcp@2.1.3"],
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

    if not isinstance(existing, dict):
        print(f"WARNING: {mcp_path} has unexpected JSON structure (expected object). Skipping MCP merge.", file=sys.stderr)
        return 1

    servers = existing.setdefault("mcpServers", {})
    if not isinstance(servers, dict):
        print(f"WARNING: {mcp_path} has unexpected mcpServers structure (expected object). Skipping MCP merge.", file=sys.stderr)
        return 1

    added = []
    updated = []
    for name, config in MCP_SERVERS.items():
        if name not in servers:
            servers[name] = config
            added.append(name)
        elif servers[name].get("command") != config["command"] or servers[name].get("args") != config["args"]:
            # Overwrite stale/broken entries (e.g. wrong command or args)
            servers[name] = config
            updated.append(name)

    if not added and not updated:
        print("MCP servers already configured (no changes made).")
        return 0

    mcp_path.parent.mkdir(parents=True, exist_ok=True)
    content = json.dumps(existing, indent=2) + "\n"
    tmp = mcp_path.with_name(mcp_path.name + ".tmp")
    tmp.write_text(content, encoding="utf-8")
    tmp.replace(mcp_path)

    if updated:
        print(f"MCP servers updated (config corrected): {', '.join(updated)}")
    if added:
        print(f"MCP servers added: {', '.join(added)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
