#!/usr/bin/env python3
from __future__ import annotations
import argparse
from pathlib import Path

START = "## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK"
END = "## END AI-DEV-WORKFLOW MANAGED BLOCK"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--claude-md", required=True)
    parser.add_argument("--snippet", required=True)
    args = parser.parse_args()

    claude_md = Path(args.claude_md)
    snippet = Path(args.snippet).read_text(encoding="utf-8").strip() + "\n"

    if claude_md.exists():
        content = claude_md.read_text(encoding="utf-8")
    else:
        content = "# Global Claude Code Instructions\n\n"

    if START in content and END in content:
        pre = content.split(START, 1)[0].rstrip()
        post = content.split(END, 1)[1].lstrip()
        merged = pre + "\n\n" + snippet + "\n" + post
    else:
        merged = content.rstrip() + "\n\n" + snippet

    claude_md.parent.mkdir(parents=True, exist_ok=True)
    claude_md.write_text(merged.strip() + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
