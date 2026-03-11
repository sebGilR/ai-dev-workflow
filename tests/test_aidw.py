"""
Tests for ai-dev-workflow correctness and safety fixes.

Covers:
  - Branch slug uniqueness  (safe_slug)
  - Diff truncation         (_truncate_diff)
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
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
        # Create minimal plan.md to satisfy stage verification
        (wip_dir / "plan.md").write_text("# Plan\n\nMinimal plan.\n", encoding="utf-8")
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


class TestWipFileVerification:
    def test_verify_plan_success_when_file_exists(self, tmp_path, capsys):
        """Test verify-plan command succeeds when plan.md exists and has content."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        (wip_dir / "plan.md").write_text("# Plan\n\nThis is a valid plan.\n", encoding="utf-8")
        
        args = _aidw.build_parser().parse_args(["verify-plan", str(repo)])
        rc = _aidw.cmd_verify_plan(args)
        
        out = json.loads(capsys.readouterr().out)
        assert rc == 0
        assert out["verified"] is True
        assert "plan.md" in out["file"]
    
    def test_verify_plan_failure_when_file_missing(self, tmp_path, capsys):
        """Test verify-plan command fails when plan.md has too small/placeholder content.

        Note: ensure_branch_state always seeds plan.md with a minimal header, so the
        failure path here is "content too small" rather than "file does not exist".
        """
        repo = _make_git_repo(tmp_path)
        _aidw.ensure_branch_state(repo)  # Seeds plan.md with a minimal placeholder
        
        args = _aidw.build_parser().parse_args(["verify-plan", str(repo)])
        rc = _aidw.cmd_verify_plan(args)
        
        out = json.loads(capsys.readouterr().out)
        assert rc == 1
        assert out["verified"] is False
        assert "error" in out
    
    def test_verify_plan_failure_when_file_empty(self, tmp_path, capsys):
        """Test verify-plan command fails when plan.md is too small."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        (wip_dir / "plan.md").write_text("", encoding="utf-8")  # Empty file
        
        args = _aidw.build_parser().parse_args(["verify-plan", str(repo)])
        rc = _aidw.cmd_verify_plan(args)
        
        out = json.loads(capsys.readouterr().out)
        assert rc == 1
        assert out["verified"] is False
        assert "error" in out
    
    def test_set_stage_verification_blocks_planned_without_plan(self, tmp_path):
        """Test set_stage fails to transition to 'planned' stage when plan.md is missing."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        # status.json created by ensure_branch_state, but no plan.md
        
        with pytest.raises(SystemExit) as exc_info:
            _aidw.set_stage(repo, "planned")
        assert exc_info.value.code != 0
    
    def test_set_stage_verification_succeeds_with_plan(self, tmp_path):
        """Test set_stage succeeds when plan.md exists."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        (wip_dir / "plan.md").write_text("# Plan\n\nValid plan content.\n", encoding="utf-8")
        
        _aidw.set_stage(repo, "planned")
        
        # Verify status was updated
        updated_status = json.loads((wip_dir / "status.json").read_text(encoding="utf-8"))
        assert updated_status["stage"] == "planned"
    
    def test_set_stage_skip_verification_flag(self, tmp_path):
        """Test set_stage --skip-verification flag bypasses file checks."""
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        # No plan.md created
        
        # This should succeed despite missing plan.md
        _aidw.set_stage(repo, "planned", skip_verification=True)
        
        updated_status = json.loads((wip_dir / "status.json").read_text(encoding="utf-8"))
        assert updated_status["stage"] == "planned"


class TestReviewTargetedAndAtomicWrite:
    def test_synthesize_review_atomic_write_no_tmp_left(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        _aidw.synthesize_review(repo)

        assert (wip_dir / "review.md").exists()
        assert not (wip_dir / "review.md.tmp").exists()


class TestGeminiReview:
    def test_gemini_review_registered(self):
        args = _aidw.build_parser().parse_args(["gemini-review", "."])
        assert args.func == _aidw.cmd_gemini_review

    def test_synthesize_review_includes_adversarial_section(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        (wip_dir / "adversarial-review.md").write_text(
            "### Critical\n- Found a bug in auth flow\n", encoding="utf-8"
        )
        _aidw.synthesize_review(repo)
        content = (wip_dir / "review.md").read_text(encoding="utf-8")
        assert "## Adversarial Review" in content
        assert "Found a bug in auth flow" in content

    def test_synthesize_review_claude_section_before_adversarial(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        (wip_dir / "adversarial-review.md").write_text(
            "### Critical\n- Security issue found\n", encoding="utf-8"
        )
        _aidw.synthesize_review(repo)
        content = (wip_dir / "review.md").read_text(encoding="utf-8")
        claude_pos = content.index("## Claude Review")
        adversarial_pos = content.index("## Adversarial Review")
        assert claude_pos < adversarial_pos, "## Claude Review must appear before ## Adversarial Review"

    def test_synthesize_review_preserves_claude_content_on_second_call(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        # First call: scaffold written with placeholder
        _aidw.synthesize_review(repo)
        review_path = wip_dir / "review.md"
        first_content = review_path.read_text(encoding="utf-8")
        # Simulate Claude filling in its review
        filled_content = first_content.replace(
            "<!-- Claude should add its own review findings here -->",
            "Real Claude analysis here.\n\n### Verdict\nApprove.",
        )
        review_path.write_text(filled_content, encoding="utf-8")

        # Second call: Gemini adversarial review is now present
        (wip_dir / "adversarial-review.md").write_text(
            "### Critical\n- Adversarial finding\n", encoding="utf-8"
        )
        _aidw.synthesize_review(repo)
        second_content = review_path.read_text(encoding="utf-8")

        assert "Real Claude analysis here." in second_content, "Claude content must be preserved"
        assert "Adversarial finding" in second_content, "Adversarial content must be present"
        assert _aidw.CLAUDE_REVIEW_PLACEHOLDER not in second_content, "Placeholder must not replace real content"
        claude_pos = second_content.index("## Claude Review")
        adversarial_pos = second_content.index("## Adversarial Review")
        assert claude_pos < adversarial_pos, "## Claude Review must still appear before ## Adversarial Review"

    def test_synthesize_review_omits_adversarial_when_absent(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        _aidw.synthesize_review(repo)
        content = (wip_dir / "review.md").read_text(encoding="utf-8")
        assert "## Adversarial Review" not in content

    def test_gemini_review_skipped_clears_stale_output(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])

        # Create a stale adversarial-review.md
        adv_path = wip_dir / "adversarial-review.md"
        adv_path.write_text("stale content\n", encoding="utf-8")

        # Create an empty review bundle so gemini_review skips
        bundle = {"branch_diff": "", "diff": "", "staged_diff": "", "changed_files": []}
        _aidw.write_json(wip_dir / "review-bundle.json", bundle)

        result = _aidw.gemini_review(repo)
        assert result["status"] == "skipped"
        assert not adv_path.exists()

    def test_cmd_gemini_review_skipped_when_disabled(self, monkeypatch):
        monkeypatch.setenv("AIDW_GEMINI_REVIEW", "0")
        args = _aidw.build_parser().parse_args(["gemini-review", "."])
        assert _aidw.cmd_gemini_review(args) == 0

    def test_cmd_gemini_review_timeout_clamped(self):
        args = _aidw.build_parser().parse_args(["gemini-review", ".", "--timeout", "0"])
        # timeout=0 should be clamped to 10 by cmd_gemini_review, not crash
        with patch.dict(os.environ, {"AIDW_GEMINI_REVIEW": "1"}, clear=False):
            with patch.object(_aidw, "gemini_is_installed", return_value=True):
                with patch.object(_aidw, "gemini_review", return_value={"status": "ok"}) as mock_review:
                    rc = _aidw.cmd_gemini_review(args)

        assert rc == 0
        _, kwargs = mock_review.call_args
        assert kwargs["timeout"] == 10


class TestCopilotBootstrap:
    def test_ensure_repo_seeds_copilot_assets(self, tmp_path):
        repo = _make_git_repo(tmp_path)

        info = _aidw.ensure_repo(repo)

        assert Path(info["github_dir"]).exists()
        assert (repo / ".github" / "copilot-instructions.md").exists()
        assert (repo / ".github" / "skills" / "wip-start" / "SKILL.md").exists()
        assert (repo / ".github" / "agents" / "wip-planner.md").exists()

    def test_ensure_repo_does_not_overwrite_existing_copilot_instructions(self, tmp_path):
        repo = _make_git_repo(tmp_path)
        custom = "# custom instructions\n"
        target = repo / ".github" / "copilot-instructions.md"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(custom, encoding="utf-8")

        _aidw.ensure_repo(repo)

        assert target.read_text(encoding="utf-8") == custom

    def test_seeded_agent_removes_permission_mode(self, tmp_path):
        repo = _make_git_repo(tmp_path)

        _aidw.ensure_repo(repo)

        agent = (repo / ".github" / "agents" / "wip-planner.md").read_text(encoding="utf-8")
        assert "permissionMode:" not in agent

    def test_strip_permission_mode_handles_indentation(self):
        """Test that _strip_permission_mode_frontmatter handles indented YAML keys."""
        content_with_indented = """---
name: test-agent
  permissionMode: plan
description: A test
---
Body content
"""
        result = _aidw._strip_permission_mode_frontmatter(content_with_indented)
        assert "permissionMode" not in result
        assert "test-agent" in result
        assert "Body content" in result

    def test_strip_permission_mode_handles_no_indentation(self):
        """Test that _strip_permission_mode_frontmatter handles non-indented keys."""
        content_no_indent = """---
name: test-agent
permissionMode: plan
description: A test
---
Body content
"""
        result = _aidw._strip_permission_mode_frontmatter(content_no_indent)
        assert "permissionMode" not in result
        assert "test-agent" in result
        assert "Body content" in result

    def test_verify_reports_missing_workspace_copilot_assets(self, tmp_path):
        workspace = tmp_path / "ws"
        workspace.mkdir()
        repo = _make_git_repo(workspace / "sample")

        results = _aidw.verify(workspace)
        checks = {c["name"]: c["status"] for c in results["checks"]}

        assert checks[f"workspace: {repo.name} .github/copilot-instructions.md"] == "warn"
        assert checks[f"workspace: {repo.name} .github/skills/wip-start/SKILL.md"] == "warn"
        assert checks[f"workspace: {repo.name} .github/agents/wip-planner.md"] == "warn"


# ===========================================================================
# TestEnsureBranchStateDatedDir — YYYYMMDD- prefix for new WIP directories
# ===========================================================================


class TestEnsureBranchStateDatedDir:
    def test_new_dir_has_date_prefix(self, tmp_path):
        """ensure_branch_state creates a YYYYMMDD-<slug> directory on first call."""
        from datetime import datetime
        repo = _make_git_repo(tmp_path)
        state = _aidw.ensure_branch_state(repo)
        wip_dir = Path(state["wip_dir"])
        # Validate the prefix is a real calendar date, not just any 8 digits
        datetime.strptime(wip_dir.name[:8], "%Y%m%d")
        assert wip_dir.name[8] == "-", f"Expected YYYYMMDD- prefix, got: {wip_dir.name!r}"

    def test_second_call_reuses_same_dir(self, tmp_path):
        """A second call returns the same prefixed dir, not a new one."""
        repo = _make_git_repo(tmp_path)
        state1 = _aidw.ensure_branch_state(repo)
        state2 = _aidw.ensure_branch_state(repo)
        assert state1["wip_dir"] == state2["wip_dir"]
        wip_base = tmp_path / ".wip"
        dated_dirs = [p for p in wip_base.iterdir() if p.is_dir()]
        assert len(dated_dirs) == 1, f"Expected one WIP dir, found: {dated_dirs}"

    def test_legacy_unprefixed_dir_is_reused(self, tmp_path):
        """An existing unprefixed dir is reused without creating a new dated dir."""
        repo = _make_git_repo(tmp_path)
        branch_name = _aidw.safe_slug(_aidw.current_branch(repo))
        legacy = tmp_path / ".wip" / branch_name
        legacy.mkdir(parents=True)

        state = _aidw.ensure_branch_state(repo)
        assert Path(state["wip_dir"]) == legacy

        wip_base = tmp_path / ".wip"
        dirs = [p for p in wip_base.iterdir() if p.is_dir()]
        assert len(dirs) == 1, f"Expected only legacy dir, found: {dirs}"

    def test_multiple_dated_dirs_picks_newest(self, tmp_path):
        """When multiple dated dirs exist for the same branch, the newest is used."""
        repo = _make_git_repo(tmp_path)
        branch_name = _aidw.safe_slug(_aidw.current_branch(repo))
        wip_base = tmp_path / ".wip"
        wip_base.mkdir(parents=True)
        old_dir = wip_base / f"20260101-{branch_name}"
        new_dir = wip_base / f"20260311-{branch_name}"
        old_dir.mkdir()
        new_dir.mkdir()

        state = _aidw.ensure_branch_state(repo)
        assert Path(state["wip_dir"]) == new_dir, (
            f"Expected newest dir {new_dir}, got {state['wip_dir']}"
        )

    def test_branch_wip_dir_returns_dated_path(self, tmp_path):
        """_branch_wip_dir with explicit prefix returns the correct path."""
        repo = tmp_path
        path = _aidw._branch_wip_dir(repo, "feat-foo-abc12345", "20260311")
        assert path == repo / ".wip" / "20260311-feat-foo-abc12345"
