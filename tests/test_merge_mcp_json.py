"""
Tests for scripts/merge_mcp_json.py

Covers:
  - Additive merge: existing mcpServers keys preserved, missing keys added
  - Idempotent re-run: second run makes no changes
  - Corrupt JSON: exits 1, warning to stderr, mcp.json unchanged
  - Missing ~/.claude/ directory created automatically
  - Atomic write: no .tmp file left behind after successful run
"""
from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Load merge_mcp_json module
# ---------------------------------------------------------------------------
_ROOT = Path(__file__).parent.parent
_MMJ_PATH = _ROOT / "scripts" / "merge_mcp_json.py"
_spec = importlib.util.spec_from_file_location("merge_mcp_json", _MMJ_PATH)
_mmj = importlib.util.module_from_spec(_spec)  # type: ignore[arg-type]
_spec.loader.exec_module(_mmj)  # type: ignore[union-attr]


# ===========================================================================
# TestMergeMcpJson — main() integration tests
# ===========================================================================


class TestMergeMcpJson:
    def _mcp_path(self, tmp_path: Path) -> Path:
        """Return a mcp.json path inside a fake ~/.claude dir."""
        p = tmp_path / ".claude" / "mcp.json"
        return p

    def _patch(self, monkeypatch, tmp_path: Path):
        """Redirect HOME and MCP_SERVERS to a temp directory."""
        monkeypatch.setattr(Path, "home", staticmethod(lambda: tmp_path))

    def test_missing_mcp_json_created_with_default_servers(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        ret = _mmj.main()
        assert ret == 0
        mcp_path = self._mcp_path(tmp_path)
        assert mcp_path.exists()
        data = json.loads(mcp_path.read_text())
        assert "serena" in data["mcpServers"]
        assert "context7" in data["mcpServers"]

    def test_additive_merge_preserves_existing_keys(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        mcp_path = self._mcp_path(tmp_path)
        mcp_path.parent.mkdir(parents=True)
        existing = {
            "mcpServers": {
                "my-custom-server": {"command": "my-cmd", "args": []},
                "serena": {"command": "custom-serena", "args": ["--custom"]},
            }
        }
        mcp_path.write_text(json.dumps(existing), encoding="utf-8")

        ret = _mmj.main()
        assert ret == 0

        data = json.loads(mcp_path.read_text())
        # Existing keys not overwritten
        assert data["mcpServers"]["serena"] == {"command": "custom-serena", "args": ["--custom"]}
        assert data["mcpServers"]["my-custom-server"] == {"command": "my-cmd", "args": []}
        # Missing key added
        assert "context7" in data["mcpServers"]

    def test_idempotent_rerun_makes_no_changes(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        _mmj.main()
        mcp_path = self._mcp_path(tmp_path)
        after_first = mcp_path.read_text()
        _mmj.main()
        after_second = mcp_path.read_text()
        assert after_first == after_second

    def test_corrupt_json_returns_exit_1(self, tmp_path, monkeypatch, capsys):
        self._patch(monkeypatch, tmp_path)
        mcp_path = self._mcp_path(tmp_path)
        mcp_path.parent.mkdir(parents=True)
        mcp_path.write_text("{ not valid JSON !!!", encoding="utf-8")
        original_content = mcp_path.read_text()

        ret = _mmj.main()
        assert ret == 1

        # mcp.json left unchanged
        assert mcp_path.read_text() == original_content

        # Warning written to stderr
        captured = capsys.readouterr()
        assert "WARNING" in captured.err

    def test_missing_parent_directory_created(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        # Do not pre-create ~/.claude/
        assert not (tmp_path / ".claude").exists()
        ret = _mmj.main()
        assert ret == 0
        assert self._mcp_path(tmp_path).exists()

    def test_no_tmp_file_left_after_successful_write(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        _mmj.main()
        tmp_file = self._mcp_path(tmp_path).with_name("mcp.json.tmp")
        assert not tmp_file.exists()

    def test_output_is_valid_json(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        _mmj.main()
        content = self._mcp_path(tmp_path).read_text()
        data = json.loads(content)
        assert isinstance(data, dict)

    def test_mcp_servers_have_correct_structure(self, tmp_path, monkeypatch):
        self._patch(monkeypatch, tmp_path)
        _mmj.main()
        data = json.loads(self._mcp_path(tmp_path).read_text())
        for name, cfg in data["mcpServers"].items():
            assert "command" in cfg
            assert "args" in cfg
            assert isinstance(cfg["args"], list)
