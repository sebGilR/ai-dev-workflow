#!/usr/bin/env python3
from __future__ import annotations
import argparse
import json
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
        if key in out and isinstance(out[key], dict) and isinstance(value, dict):
            out[key] = merge_dict(out[key], value)
        elif key in out and isinstance(out[key], list) and isinstance(value, list):
            out[key] = merge_lists(out[key], value)
        else:
            out[key] = value
    return out


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--settings", required=True)
    parser.add_argument("--template", required=True)
    args = parser.parse_args()

    settings_path = Path(args.settings)
    template_path = Path(args.template)

    template = json.loads(template_path.read_text(encoding="utf-8"))
    if settings_path.exists():
        try:
            existing = json.loads(settings_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            backup = settings_path.with_suffix(".json.bak")
            settings_path.replace(backup)
            existing = {}
    else:
        existing = {}

    merged = merge_dict(existing, template)
    settings_path.parent.mkdir(parents=True, exist_ok=True)
    settings_path.write_text(json.dumps(merged, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
