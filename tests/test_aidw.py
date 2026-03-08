"""
Tests for ai-dev-workflow correctness and safety fixes.

Covers:
  - Branch slug uniqueness  (safe_slug)
  - Diff truncation         (_truncate_diff)
  - Ollama endpoint safety  (validate_ollama_endpoint)
  - Model routing by kind   (resolve_model_for_kind)
  - Model defaults          (OLLAMA_MODEL_* constants)
  - Stop model helper       (stop_ollama_model)
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import subprocess
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

# ---------------------------------------------------------------------------
# Load the aidw module without running __main__
# ---------------------------------------------------------------------------
_ROOT = Path(__file__).parent.parent
_AIDW_PATH = _ROOT / "scripts" / "aidw.py"
_spec = importlib.util.spec_from_file_location("aidw", _AIDW_PATH)
_aidw = importlib.util.module_from_spec(_spec)  # type: ignore[arg-type]
_spec.loader.exec_module(_aidw)  # type: ignore[union-attr]


# ===========================================================================
# safe_slug — branch-to-directory slug
# ===========================================================================


class TestSafeSlug:
    def test_simple_name_unchanged(self):
        assert _aidw.safe_slug("main") == "main"

    def test_alphanumeric_with_dash_unchanged(self):
        assert _aidw.safe_slug("feature-foo") == "feature-foo"

    def test_slash_replaced_and_hash_appended(self):
        slug = _aidw.safe_slug("feature/foo")
        expected_hash = hashlib.sha256(b"feature/foo").hexdigest()[:8]
        assert slug == f"feature-foo-{expected_hash}"

    def test_collision_resistance(self):
        """'feature/foo' and 'feature-foo' must not produce the same slug."""
        assert _aidw.safe_slug("feature/foo") != _aidw.safe_slug("feature-foo")

    def test_hash_is_stable(self):
        assert _aidw.safe_slug("fix/abc-123") == _aidw.safe_slug("fix/abc-123")

    def test_empty_string_returns_unknown_branch_with_hash(self):
        slug = _aidw.safe_slug("")
        # empty → "unknown-branch" (differs from ""), hash appended
        expected_hash = hashlib.sha256(b"").hexdigest()[:8]
        assert slug == f"unknown-branch-{expected_hash}"

    def test_special_chars_only_returns_unknown_branch_with_hash(self):
        slug = _aidw.safe_slug("///")
        assert slug.startswith("unknown-branch-")
        assert len(slug) == len("unknown-branch-") + 8

    def test_two_different_branches_with_slash(self):
        a = _aidw.safe_slug("feat/login")
        b = _aidw.safe_slug("feat/logout")
        assert a != b

    def test_periods_and_underscores_kept(self):
        # Allowed chars: a-z, A-Z, 0-9, -, _, .
        result = _aidw.safe_slug("v1.2_patch")
        assert result == "v1.2_patch"

    def test_uppercase_kept(self):
        result = _aidw.safe_slug("HotFix")
        assert result == "HotFix"


# ===========================================================================
# _truncate_diff
# ===========================================================================


class TestTruncateDiff:
    def test_short_text_not_truncated(self):
        text, truncated = _aidw._truncate_diff("hello", limit=100)
        assert text == "hello"
        assert truncated is False

    def test_exactly_at_limit_not_truncated(self):
        text = "a" * 100
        result, truncated = _aidw._truncate_diff(text, limit=100)
        assert result == text
        assert truncated is False

    def test_one_over_limit_truncated(self):
        text = "a" * 101
        result, truncated = _aidw._truncate_diff(text, limit=100)
        assert result == "a" * 100
        assert truncated is True

    def test_far_over_limit_truncated(self):
        text = "x" * 200_000
        result, truncated = _aidw._truncate_diff(text, limit=50_000)
        assert len(result.encode("utf-8")) == 50_000
        assert truncated is True

    def test_empty_string(self):
        result, truncated = _aidw._truncate_diff("", limit=100)
        assert result == ""
        assert truncated is False

    def test_truncates_on_utf8_bytes_without_invalid_codepoint(self):
        text = "é" * 60
        result, truncated = _aidw._truncate_diff(text, limit=101)
        assert truncated is True
        assert len(result.encode("utf-8")) == 100
        assert result == ("é" * 50)

    def test_default_limit_is_50kb(self):
        # Verify that the module-level constant matches expectations
        assert _aidw.MAX_DIFF_BYTES == 50_000


# ===========================================================================
# validate_ollama_endpoint — endpoint safety
# ===========================================================================


class TestValidateOllamaEndpoint:
    def test_localhost_allowed(self):
        _aidw.validate_ollama_endpoint("http://localhost:11434")

    def test_127_0_0_1_allowed(self):
        _aidw.validate_ollama_endpoint("http://127.0.0.1:11434")

    def test_ipv6_loopback_allowed(self):
        _aidw.validate_ollama_endpoint("http://[::1]:11434")

    def test_remote_host_rejected(self):
        with pytest.raises(SystemExit) as exc_info:
            _aidw.validate_ollama_endpoint("http://remote-server:11434")
        assert "not a local address" in str(exc_info.value)
        assert "AIDW_OLLAMA_ALLOW_REMOTE" in str(exc_info.value)

    def test_remote_ip_rejected(self):
        with pytest.raises(SystemExit):
            _aidw.validate_ollama_endpoint("http://10.0.0.5:11434")

    def test_remote_allowed_with_env_1(self, monkeypatch):
        monkeypatch.setenv("AIDW_OLLAMA_ALLOW_REMOTE", "1")
        _aidw.validate_ollama_endpoint("http://remote-server:11434")

    def test_remote_allowed_with_env_true(self, monkeypatch):
        monkeypatch.setenv("AIDW_OLLAMA_ALLOW_REMOTE", "true")
        _aidw.validate_ollama_endpoint("http://remote-server:11434")

    def test_remote_allowed_with_env_yes(self, monkeypatch):
        monkeypatch.setenv("AIDW_OLLAMA_ALLOW_REMOTE", "yes")
        _aidw.validate_ollama_endpoint("http://remote-server:11434")

    def test_remote_rejected_when_env_unset(self, monkeypatch):
        monkeypatch.delenv("AIDW_OLLAMA_ALLOW_REMOTE", raising=False)
        with pytest.raises(SystemExit):
            _aidw.validate_ollama_endpoint("http://remote-server:11434")

    def test_remote_rejected_when_env_is_zero(self, monkeypatch):
        monkeypatch.setenv("AIDW_OLLAMA_ALLOW_REMOTE", "0")
        with pytest.raises(SystemExit):
            _aidw.validate_ollama_endpoint("http://remote-server:11434")

    def test_missing_scheme_rejected(self):
        with pytest.raises(SystemExit) as exc_info:
            _aidw.validate_ollama_endpoint("localhost:11434")
        assert "include a scheme" in str(exc_info.value)

    def test_non_http_scheme_rejected(self):
        with pytest.raises(SystemExit) as exc_info:
            _aidw.validate_ollama_endpoint("ftp://localhost:11434")
        assert "Only http and https" in str(exc_info.value)

    def test_path_component_rejected(self):
        with pytest.raises(SystemExit) as exc_info:
            _aidw.validate_ollama_endpoint("http://localhost:11434/api")
        assert "without a path" in str(exc_info.value)

    def test_allowed_hosts_set(self):
        assert "localhost" in _aidw.ALLOWED_OLLAMA_HOSTS
        assert "127.0.0.1" in _aidw.ALLOWED_OLLAMA_HOSTS
        assert "::1" in _aidw.ALLOWED_OLLAMA_HOSTS


# ===========================================================================
# resolve_model_for_kind — per-task model routing
# ===========================================================================


class TestResolveModelForKind:
    def test_bug_risk_uses_review_model(self):
        assert _aidw.resolve_model_for_kind("bug-risk") == _aidw.OLLAMA_MODEL_REVIEW

    def test_missing_tests_uses_review_model(self):
        assert _aidw.resolve_model_for_kind("missing-tests") == _aidw.OLLAMA_MODEL_REVIEW

    def test_regression_risk_uses_review_model(self):
        assert _aidw.resolve_model_for_kind("regression-risk") == _aidw.OLLAMA_MODEL_REVIEW

    def test_docs_needed_uses_fast_model(self):
        assert _aidw.resolve_model_for_kind("docs-needed") == _aidw.OLLAMA_MODEL_FAST

    def test_summaries_uses_fast_model(self):
        assert _aidw.resolve_model_for_kind("summaries") == _aidw.OLLAMA_MODEL_FAST

    def test_synthesis_uses_fast_model(self):
        assert _aidw.resolve_model_for_kind("synthesis") == _aidw.OLLAMA_MODEL_FAST

    def test_generate_code_uses_generate_model(self):
        assert _aidw.resolve_model_for_kind("generate-code") == _aidw.OLLAMA_MODEL_GENERATE

    def test_debug_patch_uses_generate_model(self):
        assert _aidw.resolve_model_for_kind("debug-patch") == _aidw.OLLAMA_MODEL_GENERATE

    def test_patch_draft_uses_generate_model(self):
        assert _aidw.resolve_model_for_kind("patch-draft") == _aidw.OLLAMA_MODEL_GENERATE

    def test_unknown_kind_falls_back_to_default_model(self):
        assert _aidw.resolve_model_for_kind("unknown-kind") == _aidw.DEFAULT_OLLAMA_MODEL

    def test_all_task_kinds_covered(self):
        """Every kind in ALL_TASK_KINDS resolves to a non-empty string."""
        for kind in _aidw.ALL_TASK_KINDS:
            model = _aidw.resolve_model_for_kind(kind)
            assert isinstance(model, str) and model

    def test_env_override_changes_review_model(self, monkeypatch):
        monkeypatch.setenv("AIDW_OLLAMA_MODEL_REVIEW", "custom-review:latest")
        # Reload to pick up the env change
        import importlib
        import importlib.util as _util
        spec = _util.spec_from_file_location("aidw_fresh", _AIDW_PATH)
        fresh = _util.module_from_spec(spec)
        spec.loader.exec_module(fresh)
        assert fresh.OLLAMA_MODEL_REVIEW == "custom-review:latest"
        assert fresh.resolve_model_for_kind("bug-risk") == "custom-review:latest"


# ===========================================================================
# Model defaults — constants when no env vars are set
# ===========================================================================


class TestModelDefaults:
    def test_default_fast_model(self, monkeypatch):
        monkeypatch.delenv("AIDW_OLLAMA_MODEL_FAST", raising=False)
        import importlib.util as _util
        spec = _util.spec_from_file_location("aidw_defaults", _AIDW_PATH)
        m = _util.module_from_spec(spec)
        spec.loader.exec_module(m)
        assert m.OLLAMA_MODEL_FAST == "phi3:mini"

    def test_default_review_model(self, monkeypatch):
        monkeypatch.delenv("AIDW_OLLAMA_MODEL_REVIEW", raising=False)
        import importlib.util as _util
        spec = _util.spec_from_file_location("aidw_defaults2", _AIDW_PATH)
        m = _util.module_from_spec(spec)
        spec.loader.exec_module(m)
        assert m.OLLAMA_MODEL_REVIEW == "qwen2.5-coder:7b"

    def test_default_generate_model(self, monkeypatch):
        monkeypatch.delenv("AIDW_OLLAMA_MODEL_GENERATE", raising=False)
        import importlib.util as _util
        spec = _util.spec_from_file_location("aidw_defaults3", _AIDW_PATH)
        m = _util.module_from_spec(spec)
        spec.loader.exec_module(m)
        assert m.OLLAMA_MODEL_GENERATE == "deepseek-coder:6.7b"

    def test_default_endpoint(self, monkeypatch):
        monkeypatch.delenv("AIDW_OLLAMA_ENDPOINT", raising=False)
        import importlib.util as _util
        spec = _util.spec_from_file_location("aidw_defaults4", _AIDW_PATH)
        m = _util.module_from_spec(spec)
        spec.loader.exec_module(m)
        assert m.DEFAULT_OLLAMA_ENDPOINT == "http://localhost:11434"

    def test_default_fallback_model_is_review_model(self):
        """AIDW_OLLAMA_MODEL should default to the review model value."""
        assert _aidw.DEFAULT_OLLAMA_MODEL == _aidw.OLLAMA_MODEL_REVIEW

    def test_all_task_kinds_list_is_sorted(self):
        assert _aidw.ALL_TASK_KINDS == sorted(_aidw.ALL_TASK_KINDS)

    def test_all_task_kinds_contains_expected(self):
        expected = {
            "bug-risk", "missing-tests", "regression-risk",
            "docs-needed", "summaries", "synthesis",
            "generate-code", "debug-patch", "patch-draft",
        }
        assert set(_aidw.ALL_TASK_KINDS) == expected


# ===========================================================================
# stop_ollama_model — model unload helper
# ===========================================================================


class TestStopOllamaModel:
    def test_returns_true_on_200(self):
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.__enter__ = lambda s: s
        mock_resp.__exit__ = MagicMock(return_value=False)

        with patch("urllib.request.urlopen", return_value=mock_resp):
            result = _aidw.stop_ollama_model("phi3:mini", "http://localhost:11434")
        assert result is True

    def test_returns_false_on_connection_error(self):
        import urllib.error
        with patch("urllib.request.urlopen", side_effect=urllib.error.URLError("refused")):
            result = _aidw.stop_ollama_model("phi3:mini", "http://localhost:11434")
        assert result is False

    def test_returns_false_on_timeout(self):
        import socket
        with patch("urllib.request.urlopen", side_effect=socket.timeout("timed out")):
            result = _aidw.stop_ollama_model("phi3:mini", "http://localhost:11434")
        assert result is False

    def test_warning_printed_on_failure(self, capsys):
        with patch("urllib.request.urlopen", side_effect=Exception("boom")):
            _aidw.stop_ollama_model("phi3:mini", "http://localhost:11434")
        captured = capsys.readouterr()
        assert "Warning" in captured.err
        assert "phi3:mini" in captured.err

    def test_no_exception_raised_on_failure(self):
        """stop_ollama_model must never raise — it only logs warnings."""
        with patch("urllib.request.urlopen", side_effect=Exception("unexpected")):
            # Should not raise
            _aidw.stop_ollama_model("any-model", "http://localhost:11434")


# ===========================================================================
# ollama-config output — cmd_ollama_config
# ===========================================================================


class TestOllamaConfig:
    def _run_config(self, args_extra=None):
        import argparse
        parser = _aidw.build_parser()
        argv = ["ollama-config"]
        if args_extra:
            argv.extend(args_extra)
        args = parser.parse_args(argv)
        return args

    def test_subcommand_exists(self):
        """ollama-config sub-command parses without error."""
        args = self._run_config()
        assert args.command == "ollama-config"

    def test_cmd_prints_json(self, capsys):
        import argparse
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-config"])
        rc = args.func(args)
        out = capsys.readouterr().out
        import json
        data = json.loads(out)
        assert "models" in data
        assert "fast" in data["models"]
        assert "review" in data["models"]
        assert "generate" in data["models"]
        assert rc == 0

    def test_effective_fast_model_in_output(self, capsys):
        import json
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-config"])
        args.func(args)
        out = json.loads(capsys.readouterr().out)
        assert out["models"]["fast"]["effective"] == _aidw.OLLAMA_MODEL_FAST

    def test_effective_review_model_in_output(self, capsys):
        import json
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-config"])
        args.func(args)
        out = json.loads(capsys.readouterr().out)
        assert out["models"]["review"]["effective"] == _aidw.OLLAMA_MODEL_REVIEW

    def test_effective_generate_model_in_output(self, capsys):
        import json
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-config"])
        args.func(args)
        out = json.loads(capsys.readouterr().out)
        assert out["models"]["generate"]["effective"] == _aidw.OLLAMA_MODEL_GENERATE


# ===========================================================================
# install.sh env file and shell profile idempotency (shell-level, via Python)
# ===========================================================================


class TestEnvFileContent:
    """Verify the aidw.env.sh content written by install.sh contains correct exports."""

    # The expected content is embedded in install.sh as a heredoc.
    # We test it by reading install.sh and extracting the block.
    _INSTALL_SH = _ROOT / "install.sh"

    def test_install_sh_contains_fast_model_export(self):
        content = self._INSTALL_SH.read_text()
        assert 'AIDW_OLLAMA_MODEL_FAST="phi3:mini"' in content

    def test_install_sh_contains_review_model_export(self):
        content = self._INSTALL_SH.read_text()
        assert 'AIDW_OLLAMA_MODEL_REVIEW="qwen2.5-coder:7b"' in content

    def test_install_sh_contains_generate_model_export(self):
        content = self._INSTALL_SH.read_text()
        assert 'AIDW_OLLAMA_MODEL_GENERATE="deepseek-coder:6.7b"' in content

    def test_install_sh_contains_endpoint_export(self):
        content = self._INSTALL_SH.read_text()
        assert 'AIDW_OLLAMA_ENDPOINT="http://localhost:11434"' in content

    def test_install_sh_contains_source_line_guard(self):
        """The source line must be guarded so it only fires when the file exists."""
        content = self._INSTALL_SH.read_text()
        assert "aidw.env.sh" in content
        assert "[ -f" in content

    def test_install_sh_pull_flag_supported(self):
        """install.sh must handle --pull-ollama-models flag."""
        content = self._INSTALL_SH.read_text()
        assert "--pull-ollama-models" in content

    def test_install_sh_idempotent_env_file(self):
        """write_ollama_env skips creation if the file already exists."""
        content = self._INSTALL_SH.read_text()
        # The bash function should check for existence before writing
        assert '[ -f "$env_file" ]' in content

    def test_install_sh_idempotent_source_line(self):
        """patch_shell_profile skips adding the line if already present."""
        content = self._INSTALL_SH.read_text()
        assert 'grep -qF "aidw.env.sh"' in content


# ===========================================================================
# Parser — new commands registered correctly
# ===========================================================================


class TestParserNewCommands:
    def test_ollama_config_registered(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-config"])
        assert args.func == _aidw.cmd_ollama_config

    def test_ollama_stop_registered(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-stop", "--model", "phi3:mini"])
        assert args.func == _aidw.cmd_ollama_stop
        assert args.model == "phi3:mini"

    def test_ollama_stop_all_registered(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-stop-all"])
        assert args.func == _aidw.cmd_ollama_stop_all

    def test_ollama_review_no_stop_flag(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-review", ".", "--kind", "bug-risk", "--no-stop"])
        assert args.no_stop is True

    def test_ollama_review_no_stop_default_false(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-review", ".", "--kind", "bug-risk"])
        assert args.no_stop is False

    def test_review_all_no_stop_flag(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["review-all", ".", "--no-stop"])
        assert args.no_stop is True

    def test_ollama_review_no_auto_start_flag(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-review", ".", "--kind", "bug-risk", "--no-auto-start"])
        assert args.no_auto_start is True

    def test_ollama_review_no_auto_start_default_false(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["ollama-review", ".", "--kind", "bug-risk"])
        assert args.no_auto_start is False

    def test_review_all_no_auto_start_flag(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["review-all", ".", "--no-auto-start"])
        assert args.no_auto_start is True

    def test_review_all_no_auto_start_default_false(self):
        parser = _aidw.build_parser()
        args = parser.parse_args(["review-all", "."])
        assert args.no_auto_start is False

    def test_ollama_review_all_task_kinds_accepted(self):
        """Every kind in ALL_TASK_KINDS must be a valid choice for --kind."""
        parser = _aidw.build_parser()
        for kind in _aidw.ALL_TASK_KINDS:
            args = parser.parse_args(["ollama-review", ".", "--kind", kind])
            assert args.kind == kind


# ===========================================================================
# ollama_start_server / ollama_stop_server — lifecycle helpers
# ===========================================================================


class TestOllamaStartServer:
    """Unit tests for ollama_start_server (no real processes spawned)."""

    def test_returns_none_when_already_running(self):
        with patch.object(_aidw, "ollama_is_running", return_value=True):
            result = _aidw.ollama_start_server("http://localhost:11434")
        assert result is None

    def test_returns_none_for_remote_endpoint(self):
        """Remote endpoints must never be auto-started regardless of state."""
        with patch.object(_aidw, "ollama_is_running", return_value=False):
            result = _aidw.ollama_start_server("http://remote-server:11434")
        assert result is None

    def test_raises_when_not_installed(self):
        with patch.object(_aidw, "ollama_is_running", return_value=False):
            with patch.object(_aidw, "ollama_is_installed", return_value=False):
                with pytest.raises(SystemExit) as exc_info:
                    _aidw.ollama_start_server("http://localhost:11434")
                assert "not installed" in str(exc_info.value).lower()

    def test_returns_proc_when_server_starts_successfully(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = None  # still running

        # First call: not running; second call: running (server came up)
        running_sequence = [False, True]

        def _is_running(_endpoint):
            return running_sequence.pop(0)

        with patch.object(_aidw, "ollama_is_running", side_effect=_is_running):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch("subprocess.Popen", return_value=mock_proc):
                    with patch("time.sleep"):
                        result = _aidw.ollama_start_server("http://localhost:11434")
        assert result is mock_proc

    def test_passes_endpoint_host_to_ollama_serve(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = None

        running_sequence = [False, True]

        def _is_running(_endpoint):
            return running_sequence.pop(0)

        with patch.object(_aidw, "ollama_is_running", side_effect=_is_running):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch("subprocess.Popen", return_value=mock_proc) as popen_mock:
                    with patch("time.sleep"):
                        _aidw.ollama_start_server("http://127.0.0.1:23456")
        assert popen_mock.call_args.kwargs["env"]["OLLAMA_HOST"] == "127.0.0.1:23456"

    def test_raises_if_process_exits_unexpectedly(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = 1  # exited with error

        with patch.object(_aidw, "ollama_is_running", return_value=False):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch("subprocess.Popen", return_value=mock_proc):
                    with patch("time.sleep"):
                        with patch("time.monotonic", side_effect=[0.0, 1.0]):
                            with pytest.raises(SystemExit) as exc_info:
                                _aidw.ollama_start_server("http://localhost:11434")
                            assert "exited unexpectedly" in str(exc_info.value)

    def test_raises_on_timeout(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = None  # still running but never ready

        # Simulate time advancing past the deadline immediately
        time_values = iter([0.0, 35.0])  # start=0, next check=35 > timeout=30

        with patch.object(_aidw, "ollama_is_running", return_value=False):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch("subprocess.Popen", return_value=mock_proc):
                    with patch("time.sleep"):
                        with patch("time.monotonic", side_effect=time_values):
                            with pytest.raises(SystemExit) as exc_info:
                                _aidw.ollama_start_server("http://localhost:11434")
                            assert "did not become ready" in str(exc_info.value)


class TestOllamaStopServer:
    def test_terminates_running_process(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = None  # still running

        _aidw.ollama_stop_server(mock_proc)

        mock_proc.terminate.assert_called_once()
        mock_proc.wait.assert_called_once()

    def test_skips_already_exited_process(self):
        mock_proc = MagicMock()
        mock_proc.poll.return_value = 0  # already exited

        _aidw.ollama_stop_server(mock_proc)

        mock_proc.terminate.assert_not_called()

    def test_kills_if_terminate_times_out(self):
        mock_proc = MagicMock()
        mock_proc.pid = 12345
        mock_proc.poll.return_value = None
        mock_proc.wait.side_effect = [subprocess.TimeoutExpired(cmd="ollama", timeout=10), None]

        _aidw.ollama_stop_server(mock_proc)

        mock_proc.kill.assert_called_once()


class TestOllamaStopAll:
    def test_returns_nonzero_when_any_stop_fails(self, capsys):
        args = _aidw.build_parser().parse_args(["ollama-stop-all"])
        with patch.object(_aidw, "stop_ollama_model", side_effect=[True, False, True]):
            rc = _aidw.cmd_ollama_stop_all(args)
        out = json.loads(capsys.readouterr().out)
        assert rc == 1
        assert any(not item["stopped"] for item in out)


# ===========================================================================
# Helper: minimal git repo fixture
# ===========================================================================

def _make_git_repo(tmp_path: Path) -> Path:
    """Create a minimal git repository in tmp_path and return its root."""
    subprocess.run(["git", "init", str(tmp_path)], check=True, capture_output=True)
    subprocess.run(
        ["git", "config", "user.email", "test@example.com"],
        cwd=str(tmp_path), check=True, capture_output=True,
    )
    subprocess.run(
        ["git", "config", "user.name", "Test"],
        cwd=str(tmp_path), check=True, capture_output=True,
    )
    # Create an initial commit so the branch name is defined
    (tmp_path / "README.md").write_text("init\n", encoding="utf-8")
    subprocess.run(["git", "add", "README.md"], cwd=str(tmp_path), check=True, capture_output=True)
    subprocess.run(
        ["git", "commit", "-m", "init"],
        cwd=str(tmp_path), check=True, capture_output=True,
    )
    return tmp_path


# ===========================================================================
# TestSummarizeContext — write_context_summary / collect_context_files / generate_summary_text
# ===========================================================================


class TestSummarizeContext:
    def test_generates_summary_file(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)
        result = _aidw.write_context_summary(repo)
        summary_path = Path(result["summary_path"])
        assert summary_path.exists()
        assert summary_path.name == "context-summary.md"

    def test_summary_in_correct_location(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        _aidw.write_context_summary(repo)
        expected = Path(state["wip_dir"]) / "context-summary.md"
        assert expected.exists()

    def test_summary_updates_on_reinvoke(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)
        r1 = _aidw.write_context_summary(repo)
        # Write something to a WIP file and regenerate
        wip_dir = Path(Path(r1["summary_path"]).parent)
        (wip_dir / "plan.md").write_text("# Plan\n\nNew plan content.\n", encoding="utf-8")
        r2 = _aidw.write_context_summary(repo)
        content = Path(r2["summary_path"]).read_text(encoding="utf-8")
        assert "New plan content." in content

    def test_atomic_write(self, tmp_path):
        """tmp file should not linger after a successful write."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        _aidw.write_context_summary(repo)
        wip_dir = Path(state["wip_dir"])
        assert not (wip_dir / "context-summary.md.tmp").exists()

    def test_size_bytes_in_result(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)
        result = _aidw.write_context_summary(repo)
        assert isinstance(result["size_bytes"], int)
        assert result["size_bytes"] > 0

    def test_collect_missing_files_returns_empty_strings(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        # Remove a WIP file to simulate missing content
        (wip_dir / "pr.md").unlink()
        files = _aidw.collect_context_files(wip_dir)
        assert files["pr.md"] == ""

    def test_generate_summary_text_under_2kb(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        # Seed all WIP files with large content
        for name in ["plan.md", "research.md", "execution.md", "review.md", "pr.md", "context.md"]:
            (wip_dir / name).write_text("x" * 5000, encoding="utf-8")
        files = _aidw.collect_context_files(wip_dir)
        status = _aidw.read_json(wip_dir / "status.json")
        summary = _aidw.generate_summary_text(files, status)
        assert len(summary.encode("utf-8")) < 2048

    def test_parser_registration(self):
        args = _aidw.build_parser().parse_args(["summarize-context", "."])
        assert args.func == _aidw.cmd_summarize_context


# ===========================================================================
# TestContextSummaryCommand — cmd_context_summary
# ===========================================================================


class TestContextSummaryCommand:
    def test_prints_summary_to_stdout(self, tmp_path, capsys):
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)
        _aidw.write_context_summary(repo)
        args = _aidw.build_parser().parse_args(["context-summary", str(repo)])
        rc = _aidw.cmd_context_summary(args)
        assert rc == 0
        out = capsys.readouterr().out
        assert "Workflow Summary" in out

    def test_exits_nonzero_when_missing(self, tmp_path, capsys):
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)
        args = _aidw.build_parser().parse_args(["context-summary", str(repo)])
        rc = _aidw.cmd_context_summary(args)
        assert rc == 1

    def test_parser_registration(self):
        args = _aidw.build_parser().parse_args(["context-summary", "."])
        assert args.func == _aidw.cmd_context_summary


# ===========================================================================
# TestAutoRegen — auto-regeneration hooks in set_stage and synthesize_review
# ===========================================================================


class TestAutoRegen:
    def test_set_stage_regenerates_when_summary_exists(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        # Create a summary file first
        _aidw.write_context_summary(repo)
        original = (wip_dir / "context-summary.md").read_text(encoding="utf-8")
        # Change plan content and advance stage
        (wip_dir / "plan.md").write_text("# Plan\n\nUpdated plan.\n", encoding="utf-8")
        _aidw.set_stage(repo, "planned")
        updated = (wip_dir / "context-summary.md").read_text(encoding="utf-8")
        assert updated != original
        assert "Updated plan." in updated

    def test_set_stage_skips_regen_when_summary_absent(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        assert not (wip_dir / "context-summary.md").exists()
        # Should not raise and should not create the file
        _aidw.set_stage(repo, "planned")
        assert not (wip_dir / "context-summary.md").exists()

    def test_synthesize_review_regenerates_when_summary_exists(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        _aidw.write_context_summary(repo)
        (wip_dir / "review.md").write_text("# Review\n\nCritical issue found.\n", encoding="utf-8")
        _aidw.synthesize_review(repo)
        content = (wip_dir / "context-summary.md").read_text(encoding="utf-8")
        # The summary should have been regenerated (stage/updated_at will differ at minimum)
        assert "Workflow Summary" in content


class TestVerify:
    def test_warns_and_skips_network_when_default_endpoint_invalid(self, monkeypatch):
        monkeypatch.setattr(_aidw, "DEFAULT_OLLAMA_ENDPOINT", "http://remote-server:11434")
        with patch.object(_aidw, "ollama_is_installed", return_value=True):
            with patch.object(_aidw, "ollama_is_running") as running_mock:
                with patch.object(_aidw, "ollama_has_model") as has_model_mock:
                    results = _aidw.verify()
        assert any(
            check["name"] == "ollama: endpoint configuration" and check["status"] == "warn"
            for check in results["checks"]
        )
        running_mock.assert_not_called()
        has_model_mock.assert_not_called()


class TestIndexAndResearchScan:
    def test_build_index_writes_repo_index_file(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        (repo / "scripts").mkdir(exist_ok=True)
        (repo / "scripts" / "demo.py").write_text("def alpha():\n    return 1\n", encoding="utf-8")
        (repo / "README.md").write_text("# Demo\n\n## Usage\n", encoding="utf-8")

        index = _aidw.build_repo_index(repo)
        index_path = repo / ".wip" / "repo-index.json"

        assert index_path.exists()
        assert index["repo"] == repo.name
        assert any(item["path"] == "scripts/demo.py" for item in index["files"])

    def test_research_scan_writes_branch_artifact(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        (repo / "scripts").mkdir(exist_ok=True)
        (repo / "scripts" / "aidw_feature.py").write_text(
            "def build_index():\n    return True\n",
            encoding="utf-8",
        )
        _aidw.ensure_branch_state(repo)

        result = _aidw.research_scan(repo, "build index command")
        state = _aidw.ensure_branch_state(repo)
        scan_path = Path(state["wip_dir"]) / "research-scan.json"

        assert scan_path.exists()
        assert result["goal"] == "build index command"
        assert isinstance(result["results"], list)

    def test_research_scan_parallel_uses_executor(self, tmp_path, monkeypatch):
        repo = _make_git_repo(tmp_path)
        (repo / "scripts").mkdir(exist_ok=True)
        (repo / "scripts" / "aidw_feature.py").write_text(
            "def build_index():\n    return True\n",
            encoding="utf-8",
        )

        monkeypatch.setenv("AIDW_RESEARCH_PARALLEL", "2")
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "2")

        with patch.object(_aidw.concurrent.futures, "ThreadPoolExecutor") as executor_cls:
            result = _aidw.research_scan(repo, "build index command")

        executor_cls.assert_called_once_with(max_workers=2)
        assert result["parallel"]["effective"] == 2

    def test_research_scan_parallel_respects_global_limit(self, tmp_path, monkeypatch):
        repo = _make_git_repo(tmp_path)
        (repo / "scripts").mkdir(exist_ok=True)
        (repo / "scripts" / "aidw_feature.py").write_text(
            "def build_index():\n    return True\n",
            encoding="utf-8",
        )

        monkeypatch.setenv("AIDW_RESEARCH_PARALLEL", "2")
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "1")

        with patch.object(_aidw.concurrent.futures, "ThreadPoolExecutor") as executor_cls:
            result = _aidw.research_scan(repo, "build index command")

        executor_cls.assert_not_called()
        assert result["parallel"]["requested"] == 2
        assert result["parallel"]["effective"] == 1
        assert result["parallel"]["ollama_max_parallel"] == 1

    def test_build_index_skips_unreadable_file(self, tmp_path, monkeypatch):
        repo = _make_git_repo(tmp_path)
        (repo / "scripts").mkdir(exist_ok=True)
        bad_file = repo / "scripts" / "bad.py"
        good_file = repo / "scripts" / "ok.py"
        bad_file.write_text("def bad():\n    return 0\n", encoding="utf-8")
        good_file.write_text("def ok():\n    return 1\n", encoding="utf-8")

        original_read_text = Path.read_text

        def flaky_read_text(self: Path, *args, **kwargs):
            if self == bad_file:
                raise OSError("permission denied")
            return original_read_text(self, *args, **kwargs)

        monkeypatch.setattr(Path, "read_text", flaky_read_text)
        index = _aidw.build_repo_index(repo)

        paths = {entry["path"] for entry in index["files"]}
        assert "scripts/ok.py" in paths
        assert "scripts/bad.py" not in paths
        assert index["truncated"]["skipped_files"] >= 1
        assert any(item["path"] == "scripts/bad.py" for item in index["skipped"])


class TestParserV2Commands:
    def test_build_index_registered(self):
        args = _aidw.build_parser().parse_args(["build-index", "."])
        assert args.func == _aidw.cmd_build_index

    def test_research_scan_registered(self):
        args = _aidw.build_parser().parse_args(["research-scan", ".", "--goal", "find review code"])
        assert args.func == _aidw.cmd_research_scan
        assert args.goal == "find review code"

    def test_review_all_parallel_flag(self):
        args = _aidw.build_parser().parse_args(["review-all", ".", "--parallel", "2"])
        assert args.parallel == 2


class TestReviewAllParallelFallback:
    def test_parallel_failure_falls_back_to_remaining_sequential(self, tmp_path, monkeypatch, capsys):
        repo = _make_git_repo(tmp_path)
        args = _aidw.build_parser().parse_args(["review-all", str(repo), "--no-auto-start", "--parallel", "2"])

        monkeypatch.setenv("AIDW_REVIEW_PARALLEL", "2")
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "2")

        call_order: list[str] = []

        def fake_ollama_review(_repo, kind, _model, _endpoint):
            call_order.append(kind)
            if kind == "missing-tests":
                raise SystemExit("simulated parallel failure")
            return {"kind": kind, "summary": "ok", "findings": []}

        with patch.object(_aidw, "validate_ollama_endpoint", return_value=None):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch.object(_aidw, "ollama_is_running", return_value=True):
                    with patch.object(_aidw, "ollama_has_model", return_value=True):
                        with patch.object(_aidw, "stop_ollama_model", return_value=True):
                            with patch.object(_aidw, "ollama_review", side_effect=fake_ollama_review):
                                rc = _aidw.cmd_review_all(args)

        out = json.loads(capsys.readouterr().out)
        assert rc == 0
        assert len(out) == 4
        assert any(item["kind"] == "missing-tests" and "Failed:" in item["summary"] for item in out)
        assert "regression-risk" in call_order
        assert "docs-needed" in call_order

    def test_parallel_runtime_error_falls_back_to_remaining_sequential(self, tmp_path, monkeypatch, capsys):
        repo = _make_git_repo(tmp_path)
        args = _aidw.build_parser().parse_args(["review-all", str(repo), "--no-auto-start", "--parallel", "2"])

        monkeypatch.setenv("AIDW_REVIEW_PARALLEL", "2")
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "2")

        call_order: list[str] = []

        def fake_ollama_review(_repo, kind, _model, _endpoint):
            call_order.append(kind)
            if kind == "missing-tests":
                raise RuntimeError("unexpected worker crash")
            return {"kind": kind, "summary": "ok", "findings": []}

        with patch.object(_aidw, "validate_ollama_endpoint", return_value=None):
            with patch.object(_aidw, "ollama_is_installed", return_value=True):
                with patch.object(_aidw, "ollama_is_running", return_value=True):
                    with patch.object(_aidw, "ollama_has_model", return_value=True):
                        with patch.object(_aidw, "stop_ollama_model", return_value=True):
                            with patch.object(_aidw, "ollama_review", side_effect=fake_ollama_review):
                                rc = _aidw.cmd_review_all(args)

        out = json.loads(capsys.readouterr().out)
        assert rc == 0
        assert len(out) == 4
        assert any(item["kind"] == "missing-tests" and "Failed:" in item["summary"] for item in out)
        assert "regression-risk" in call_order
        assert "docs-needed" in call_order


class TestParallelConfigClamp:
    def test_ollama_config_clamps_invalid_parallel_env_values(self, monkeypatch, capsys):
        args = _aidw.build_parser().parse_args(["ollama-config"])
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "not-a-number")
        monkeypatch.setenv("AIDW_RESEARCH_PARALLEL", "-99")
        monkeypatch.setenv("AIDW_REVIEW_PARALLEL", "999")

        rc = _aidw.cmd_ollama_config(args)
        out = json.loads(capsys.readouterr().out)

        assert rc == 0
        assert out["parallel"]["ollama_max_parallel"]["effective"] == 2
        assert out["parallel"]["research_parallel"]["effective"] == 1
        assert out["parallel"]["review_parallel"]["effective"] == 2

    def test_ollama_config_respects_global_parallel_limit(self, monkeypatch, capsys):
        args = _aidw.build_parser().parse_args(["ollama-config"])
        monkeypatch.setenv("AIDW_OLLAMA_MAX_PARALLEL", "1")
        monkeypatch.setenv("AIDW_RESEARCH_PARALLEL", "2")
        monkeypatch.setenv("AIDW_REVIEW_PARALLEL", "2")

        rc = _aidw.cmd_ollama_config(args)
        out = json.loads(capsys.readouterr().out)

        assert rc == 0
        assert out["parallel"]["research_parallel"]["effective_with_max"] == 1
        assert out["parallel"]["review_parallel"]["effective_with_max"] == 1
