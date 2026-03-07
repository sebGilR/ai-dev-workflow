#!/usr/bin/env python3
from __future__ import annotations
import subprocess
from pathlib import Path

MANAGED_LINES = [
    ".wip/",
    ".claude/repo-docs/",
    ".claude/settings.local.json",
    "CLAUDE.local.md",
]


def get_global_excludes() -> Path:
    result = subprocess.run(
        ["git", "config", "--global", "core.excludesfile"],
        capture_output=True,
        text=True,
        check=False,
    )
    path = result.stdout.strip()
    if not path:
        path = str(Path.home() / ".gitignore_global")
        subprocess.run(
            ["git", "config", "--global", "core.excludesfile", path],
            check=True,
        )
    return Path(path).expanduser()


def main() -> int:
    gitignore = get_global_excludes()
    if gitignore.exists():
        lines = gitignore.read_text(encoding="utf-8").splitlines()
    else:
        lines = []

    existing = set(lines)
    changed = False
    for line in MANAGED_LINES:
        if line not in existing:
            lines.append(line)
            changed = True

    if changed or not gitignore.exists():
        gitignore.parent.mkdir(parents=True, exist_ok=True)
        gitignore.write_text("\n".join(lines).strip() + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
