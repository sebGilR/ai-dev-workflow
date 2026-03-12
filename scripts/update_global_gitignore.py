#!/usr/bin/env python3
from __future__ import annotations
import subprocess
from pathlib import Path

MANAGED_LINES = [
    ".wip/",
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


def add_entries(entries: list[str], gitignore: Path) -> list[str]:
    """Append *entries* to *gitignore* that are not already present.

    Returns the list of entries that were actually added.
    Creates the file (and parent dirs) if it does not exist.
    """
    lines = gitignore.read_text(encoding="utf-8").splitlines() if gitignore.exists() else []
    existing = set(lines)
    added = [e for e in entries if e not in existing]
    if added:
        lines.extend(added)
        gitignore.parent.mkdir(parents=True, exist_ok=True)
        gitignore.write_text("\n".join(lines).strip() + "\n", encoding="utf-8")
    return added


def main() -> int:
    import argparse

    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--add",
        nargs="+",
        metavar="ENTRY",
        help="Extra entries to add to the global gitignore (in addition to managed lines)",
    )
    args = parser.parse_args()

    gitignore = get_global_excludes()
    entries = MANAGED_LINES + (args.add or [])
    added = add_entries(entries, gitignore)
    if added:
        print(f"Added to global gitignore ({gitignore}):")
        for e in added:
            print(f"  {e}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
