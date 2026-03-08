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
