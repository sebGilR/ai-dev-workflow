"""
Tests for the configure_repo_gitignore feature.

Covers add_entries() from scripts/update_global_gitignore.py:
  - New entries are appended to an existing file
  - New entries are written when the file does not exist (file + parents created)
  - Duplicate entries are never added (idempotency)
  - Entries already present are not duplicated on re-run
  - The --add CLI flag passes extra entries through main()
  - Existing MANAGED_LINES behaviour is unaffected
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from unittest.mock import patch

import pytest

# ---------------------------------------------------------------------------
# Load update_global_gitignore module
# ---------------------------------------------------------------------------
_ROOT = Path(__file__).parent.parent
_UGG_PATH = _ROOT / "scripts" / "update_global_gitignore.py"
_spec = importlib.util.spec_from_file_location("update_global_gitignore", _UGG_PATH)
_ugg = importlib.util.module_from_spec(_spec)  # type: ignore[arg-type]
_spec.loader.exec_module(_ugg)  # type: ignore[union-attr]

REPO_ENTRIES = [
    ".github/copilot-instructions.md",
    ".github/skills/",
    ".github/agents/",
    ".claude/repo-docs/",
    ".claude/settings.local.json",
]


# ===========================================================================
# add_entries
# ===========================================================================


class TestAddEntries:
    def test_appends_new_entries_to_existing_file(self, tmp_path):
        gi = tmp_path / ".gitignore_global"
        gi.write_text("*.pyc\n", encoding="utf-8")

        added = _ugg.add_entries(REPO_ENTRIES, gi)

        assert added == REPO_ENTRIES
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in REPO_ENTRIES:
            assert entry in lines
        assert "*.pyc" in lines

    def test_creates_file_when_missing(self, tmp_path):
        gi = tmp_path / "sub" / ".gitignore_global"
        assert not gi.exists()

        added = _ugg.add_entries(REPO_ENTRIES, gi)

        assert gi.exists()
        assert added == REPO_ENTRIES
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in REPO_ENTRIES:
            assert entry in lines

    def test_idempotent_no_duplicates(self, tmp_path):
        gi = tmp_path / ".gitignore_global"

        _ugg.add_entries(REPO_ENTRIES, gi)
        added_second = _ugg.add_entries(REPO_ENTRIES, gi)

        assert added_second == []
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in REPO_ENTRIES:
            assert lines.count(entry) == 1

    def test_partial_overlap_only_adds_missing(self, tmp_path):
        gi = tmp_path / ".gitignore_global"
        gi.write_text(".github/skills/\n", encoding="utf-8")

        added = _ugg.add_entries(REPO_ENTRIES, gi)

        assert ".github/skills/" not in added
        assert ".github/copilot-instructions.md" in added
        assert ".github/agents/" in added

    def test_returns_empty_list_when_nothing_to_add(self, tmp_path):
        gi = tmp_path / ".gitignore_global"
        gi.write_text("\n".join(REPO_ENTRIES) + "\n", encoding="utf-8")

        added = _ugg.add_entries(REPO_ENTRIES, gi)

        assert added == []

    def test_file_ends_with_newline(self, tmp_path):
        gi = tmp_path / ".gitignore_global"

        _ugg.add_entries(REPO_ENTRIES, gi)

        assert gi.read_text(encoding="utf-8").endswith("\n")

    def test_existing_content_preserved(self, tmp_path):
        gi = tmp_path / ".gitignore_global"
        existing = "*.pyc\n*.DS_Store\n.env\n"
        gi.write_text(existing, encoding="utf-8")

        _ugg.add_entries(REPO_ENTRIES, gi)

        content = gi.read_text(encoding="utf-8")
        assert "*.pyc" in content
        assert "*.DS_Store" in content
        assert ".env" in content


# ===========================================================================
# main() with --add flag
# ===========================================================================


class TestMainAddFlag:
    def _run_main(self, tmp_path, extra_args=None):
        """Run main() with core.excludesfile pointing to a temp file."""
        gi = tmp_path / ".gitignore_global"

        def fake_get_global_excludes():
            return gi

        with patch.object(_ugg, "get_global_excludes", fake_get_global_excludes):
            argv = ["update_global_gitignore.py"] + (extra_args or [])
            with patch.object(sys, "argv", argv):
                rc = _ugg.main()
        return rc, gi

    def test_add_flag_appends_extra_entries(self, tmp_path):
        rc, gi = self._run_main(tmp_path, ["--add"] + REPO_ENTRIES)

        assert rc == 0
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in REPO_ENTRIES:
            assert entry in lines

    def test_add_flag_idempotent(self, tmp_path):
        args = ["--add"] + REPO_ENTRIES
        self._run_main(tmp_path, args)
        rc, gi = self._run_main(tmp_path, args)

        assert rc == 0
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in REPO_ENTRIES:
            assert lines.count(entry) == 1

    def test_managed_lines_always_included(self, tmp_path):
        rc, gi = self._run_main(tmp_path)

        assert rc == 0
        lines = gi.read_text(encoding="utf-8").splitlines()
        for entry in _ugg.MANAGED_LINES:
            assert entry in lines

    def test_claude_entries_not_in_managed_lines(self):
        """
        .claude/ entries were moved out of MANAGED_LINES and are now handled
        by configure_repo_gitignore() so users can opt out (skip/commit them).
        """
        assert ".claude/repo-docs/" not in _ugg.MANAGED_LINES
        assert ".claude/settings.local.json" not in _ugg.MANAGED_LINES

    def test_managed_lines_plus_add_combined(self, tmp_path):
        rc, gi = self._run_main(
            tmp_path, ["--add", ".github/skills/"]
        )
        assert rc == 0
        lines = gi.read_text(encoding="utf-8").splitlines()
        assert ".github/skills/" in lines
        assert ".wip/" in lines
