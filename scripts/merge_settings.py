#!/usr/bin/env python3
from __future__ import annotations
import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def merge_lists(existing: list[Any], incoming: list[Any]) -> list[Any]:
    seen = set()
    merged = []
    for item in existing + incoming:
        key = json.dumps(item, sort_keys=True)
        if key not in seen:
            seen.add(key)
            merged.append(item)
    return merged


def merge_dict(existing: dict[str, Any], incoming: dict[str, Any]) -> dict[str, Any]:
    out = dict(existing)
    for key, value in incoming.items():
        if key in out:
            if isinstance(out[key], dict) and isinstance(value, dict):
                out[key] = merge_dict(out[key], value)
            elif isinstance(out[key], list) and isinstance(value, list):
                out[key] = merge_lists(out[key], value)
            # else: user scalar wins — do not overwrite
        else:
            out[key] = value
    return out


def backup_path_for(settings_path: Path) -> Path:
    backup = settings_path.with_suffix(".json.bak")
    if not backup.exists():
        return backup
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    return settings_path.with_suffix(f".json.{timestamp}.bak")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--settings", required=True)
    parser.add_argument("--template", required=True)
    args = parser.parse_args()

    settings_path = Path(args.settings)
    template_path = Path(args.template)

    template = json.loads(template_path.read_text(encoding="utf-8"))
    if settings_path.exists():
        raw = settings_path.read_text(encoding="utf-8")
        try:
            existing = json.loads(raw)
        except json.JSONDecodeError as exc:
            backup = backup_path_for(settings_path)
            settings_path.replace(backup)
            print(
                f"WARNING: {settings_path} contains invalid JSON ({exc}). "
                f"Backed up to {backup} and starting fresh.",
                file=sys.stderr,
            )
            existing = {}
    else:
        existing = {}

    merged = merge_dict(existing, template)
    settings_path.parent.mkdir(parents=True, exist_ok=True)
    settings_path.write_text(json.dumps(merged, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
