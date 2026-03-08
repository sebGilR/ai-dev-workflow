"""
Tests for scripts/merge_settings.py

Covers:
  - User scalar values preserved on merge
  - Missing scalars added from template
  - Nested dicts merged recursively
  - Lists deduplicated without ordering loss
  - Corrupt JSON: backed-up, warning emitted, rebuilt from template
"""
from __future__ import annotations

import importlib.util
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

# ---------------------------------------------------------------------------
# Load merge_settings module
# ---------------------------------------------------------------------------
_ROOT = Path(__file__).parent.parent
_MS_PATH = _ROOT / "scripts" / "merge_settings.py"
_spec = importlib.util.spec_from_file_location("merge_settings", _MS_PATH)
_ms = importlib.util.module_from_spec(_spec)  # type: ignore[arg-type]
_spec.loader.exec_module(_ms)  # type: ignore[union-attr]


# ===========================================================================
# merge_dict
# ===========================================================================


class TestMergeDict:
    def test_missing_key_added_from_incoming(self):
        result = _ms.merge_dict({"a": 1}, {"a": 99, "b": 2})
        assert result["b"] == 2

    def test_existing_scalar_not_overwritten(self):
        result = _ms.merge_dict({"key": "user-value"}, {"key": "template-value"})
        assert result["key"] == "user-value"

    def test_empty_existing_takes_all_from_incoming(self):
        template = {"a": 1, "b": "hello", "c": [1, 2]}
        result = _ms.merge_dict({}, template)
        assert result == template

    def test_nested_dict_merged_recursively(self):
        existing = {"nested": {"a": 1, "x": "keep"}}
        incoming = {"nested": {"a": 99, "b": 2}}
        result = _ms.merge_dict(existing, incoming)
        assert result["nested"]["a"] == 1   # user wins
        assert result["nested"]["b"] == 2   # new key added
        assert result["nested"]["x"] == "keep"  # untouched user key

    def test_list_values_deduplicated(self):
        result = _ms.merge_dict({"items": [1, 2, 3]}, {"items": [2, 3, 4]})
        assert result["items"] == [1, 2, 3, 4]

    def test_list_order_preserved(self):
        result = _ms.merge_dict({"perms": ["read"]}, {"perms": ["write", "read"]})
        assert result["perms"] == ["read", "write"]

    def test_deeply_nested_scalar_preserved(self):
        existing = {"a": {"b": {"c": "user"}}}
        incoming = {"a": {"b": {"c": "template", "d": "new"}}}
        result = _ms.merge_dict(existing, incoming)
        assert result["a"]["b"]["c"] == "user"
        assert result["a"]["b"]["d"] == "new"

    def test_none_value_not_overwritten(self):
        result = _ms.merge_dict({"k": None}, {"k": "template"})
        assert result["k"] is None

    def test_false_value_not_overwritten(self):
        result = _ms.merge_dict({"enabled": False}, {"enabled": True})
        assert result["enabled"] is False


# ===========================================================================
# merge_lists
# ===========================================================================


class TestMergeLists:
    def test_no_duplicates_simple(self):
        assert _ms.merge_lists([1, 2], [2, 3]) == [1, 2, 3]

    def test_order_preserved(self):
        assert _ms.merge_lists(["a", "b"], ["c", "a"]) == ["a", "b", "c"]

    def test_both_empty(self):
        assert _ms.merge_lists([], []) == []

    def test_existing_empty(self):
        assert _ms.merge_lists([], [1, 2]) == [1, 2]

    def test_incoming_empty(self):
        assert _ms.merge_lists([1, 2], []) == [1, 2]

    def test_deduplicates_dicts(self):
        result = _ms.merge_lists([{"a": 1}], [{"a": 1}, {"b": 2}])
        assert result == [{"a": 1}, {"b": 2}]

    def test_deduplicates_strings(self):
        result = _ms.merge_lists(["Bash(git status)", "Read(.wip/**)"], ["Read(.wip/**)"])
        assert result == ["Bash(git status)", "Read(.wip/**)"]


# ===========================================================================
# main() — CLI integration (corrupt JSON + normal merge)
# ===========================================================================


class TestMain:
    def test_normal_merge(self, tmp_path, monkeypatch):
        settings = tmp_path / "settings.json"
        template = tmp_path / "template.json"

        settings.write_text(json.dumps({"existing": "keep", "allow": ["a"]}), encoding="utf-8")
        template.write_text(json.dumps({"existing": "overridden", "allow": ["a", "b"], "newkey": 42}), encoding="utf-8")

        monkeypatch.setattr(
            sys, "argv",
            ["merge_settings.py", "--settings", str(settings), "--template", str(template)],
        )
        ret = _ms.main()
        assert ret == 0

        result = json.loads(settings.read_text())
        assert result["existing"] == "keep"         # user scalar preserved
        assert result["newkey"] == 42               # new key from template
        assert result["allow"] == ["a", "b"]        # list merged

    def test_corrupt_json_backed_up_and_rebuilt(self, tmp_path, monkeypatch, capsys):
        settings = tmp_path / "settings.json"
        template = tmp_path / "template.json"

        settings.write_text("{ not valid JSON !!!", encoding="utf-8")
        template.write_text(json.dumps({"key": "from-template"}), encoding="utf-8")

        monkeypatch.setattr(
            sys, "argv",
            ["merge_settings.py", "--settings", str(settings), "--template", str(template)],
        )
        ret = _ms.main()
        assert ret == 0

        # Backup created
        backup = settings.with_suffix(".json.bak")
        assert backup.exists()
        assert backup.read_text() == "{ not valid JSON !!!"

        # Settings rebuilt from template
        result = json.loads(settings.read_text())
        assert result["key"] == "from-template"

        # Warning written to stderr
        captured = capsys.readouterr()
        assert "WARNING" in captured.err
        assert "invalid JSON" in captured.err

    def test_corrupt_json_uses_unique_backup_when_default_exists(self, tmp_path, monkeypatch):
        settings = tmp_path / "settings.json"
        template = tmp_path / "template.json"
        existing_backup = settings.with_suffix(".json.bak")

        settings.write_text("{ not valid JSON !!!", encoding="utf-8")
        existing_backup.write_text("older backup", encoding="utf-8")
        template.write_text(json.dumps({"key": "from-template"}), encoding="utf-8")

        class FrozenDateTime:
            @staticmethod
            def now(tz=None):
                return datetime(2026, 3, 8, 3, 4, 5, tzinfo=tz or timezone.utc)

        monkeypatch.setattr(_ms, "datetime", FrozenDateTime)
        monkeypatch.setattr(
            sys, "argv",
            ["merge_settings.py", "--settings", str(settings), "--template", str(template)],
        )
        ret = _ms.main()
        assert ret == 0

        timestamped_backup = settings.with_suffix(".json.20260308030405.bak")
        assert existing_backup.read_text() == "older backup"
        assert timestamped_backup.exists()
        assert timestamped_backup.read_text() == "{ not valid JSON !!!"

    def test_missing_settings_file_created(self, tmp_path, monkeypatch):
        settings = tmp_path / "new" / "settings.json"
        template = tmp_path / "template.json"
        template.write_text(json.dumps({"key": "value"}), encoding="utf-8")

        monkeypatch.setattr(
            sys, "argv",
            ["merge_settings.py", "--settings", str(settings), "--template", str(template)],
        )
        ret = _ms.main()
        assert ret == 0
        assert settings.exists()
        result = json.loads(settings.read_text())
        assert result["key"] == "value"
