#!/usr/bin/env python3
from __future__ import annotations
import argparse
import hashlib
import logging
import re
import json
import os
import shutil
import subprocess
import sys
import textwrap
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


logger = logging.getLogger(__name__)

REPO_DOCS = ["architecture.md", "patterns.md", "commands.md", "testing.md", "gotchas.md"]
WIP_FILES = ["plan.md", "review.md", "research.md", "context.md", "execution.md", "pr.md"]
STAGES = {"started", "planned", "researched", "implementing", "reviewed", "review-fixed", "pr-prep"}


def _parse_int_env(name: str, default: int, minimum: int, maximum: int) -> int:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return default
    try:
        parsed = int(raw.strip())
    except ValueError:
        return default
    return max(minimum, min(parsed, maximum))


# Gemini adversarial review configuration
_DEFAULT_GEMINI_MODEL = "gemini-2.5-pro"
_DEFAULT_GEMINI_TIMEOUT = 120
GEMINI_MODEL = os.environ.get("AIDW_GEMINI_MODEL", _DEFAULT_GEMINI_MODEL)
GEMINI_TIMEOUT = _parse_int_env("AIDW_GEMINI_TIMEOUT", _DEFAULT_GEMINI_TIMEOUT, 10, 600)


def now_iso() -> str:
    return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def script_root() -> Path:
    return Path(__file__).resolve().parents[1]


def template_root() -> Path:
    return script_root() / "templates"


def run(cmd: list[str], cwd: str | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, cwd=cwd, text=True, capture_output=True, check=check)


def is_git_repo(path: Path) -> bool:
    result = subprocess.run(["git", "-C", str(path), "rev-parse", "--show-toplevel"], capture_output=True, text=True)
    return result.returncode == 0


def git_toplevel(path: Path) -> Path:
    result = run(["git", "-C", str(path), "rev-parse", "--show-toplevel"])
    return Path(result.stdout.strip())


def current_branch(repo: Path) -> str:
    result = run(["git", "-C", str(repo), "branch", "--show-current"])
    branch = result.stdout.strip()
    return branch or "detached-head"


def current_head_sha(repo: Path) -> str:
    result = run(["git", "-C", str(repo), "rev-parse", "HEAD"], check=False)
    return result.stdout.strip() if result.returncode == 0 else ""


def safe_slug(value: str) -> str:
    allowed = []
    for ch in value:
        if ch.isalnum() or ch in {"-", "_", "."}:
            allowed.append(ch)
        else:
            allowed.append("-")
    slug = "".join(allowed).strip("-") or "unknown-branch"
    # Add a short hash when the slug differs from the original to prevent
    # collisions (e.g. "feature/foo" and "feature-foo" producing the same slug).
    if slug != value:
        short_hash = hashlib.sha256(value.encode()).hexdigest()[:8]
        slug = f"{slug}-{short_hash}"
    return slug


def parse_files_arg(repo: Path, files_str: str) -> list[Path]:
    """Parse comma-separated file list and validate files exist in repo.

    Args:
        repo: Repository root path
        files_str: Comma-separated file paths (relative to repo root)

    Returns:
        List of validated Path objects

    Raises:
        SystemExit: If any file is invalid, outside the repo, or the list is empty
    """
    repo_root = git_toplevel(repo).resolve()
    files = [f.strip() for f in files_str.split(",") if f.strip()]
    if not files:
        raise SystemExit("[aidw] No files specified.")
    validated: list[Path] = []

    for file_str in files:
        raw_path = Path(file_str)
        if raw_path.is_absolute():
            raise SystemExit(f"[aidw] Absolute paths are not allowed: {file_str}")
        file_path = (repo_root / raw_path).resolve()
        try:
            file_path.relative_to(repo_root)
        except ValueError:
            raise SystemExit(f"[aidw] File is outside the repository: {file_str}")
        if not file_path.exists():
            raise SystemExit(f"[aidw] File not found: {file_str}")
        if not file_path.is_file():
            raise SystemExit(f"[aidw] Not a file: {file_str}")
        validated.append(file_path)

    return validated


def detect_repos(workspace_root: Path) -> list[Path]:
    repos: list[Path] = []
    seen: set[str] = set()
    candidates = [workspace_root] + [p for p in workspace_root.iterdir() if p.is_dir()]
    for candidate in candidates:
        if is_git_repo(candidate):
            top = git_toplevel(candidate).resolve()
            key = str(top)
            if key not in seen:
                seen.add(key)
                repos.append(top)
    return repos


def seed_file_if_missing(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists():
        path.write_text(content, encoding="utf-8")


def infer_repo_notes(repo: Path) -> dict[str, str]:
    notes: dict[str, str] = {}
    package_json = repo / "package.json"
    gemfile = repo / "Gemfile"
    pyproject = repo / "pyproject.toml"
    requirements = repo / "requirements.txt"

    commands = []
    testing = []
    patterns = []

    if package_json.exists():
        commands.extend([
            "- Install: `npm install` or the repo's preferred package-manager equivalent",
            "- Dev: inspect package.json scripts and fill in the preferred dev command",
            "- Lint: `npm run lint` if available",
            "- Test: `npm run test` if available",
            "- Build: `npm run build` if available",
        ])
        testing.append("- JavaScript/TypeScript repo detected via package.json")
        patterns.append("- Keep changes aligned with the existing JS/TS framework conventions")

    if gemfile.exists():
        commands.extend([
            "- Ruby install flow likely uses Bundler: `bundle install`",
            "- Rails tests may use `bundle exec rspec` or `bin/rails test`",
        ])
        testing.append("- Ruby repo detected via Gemfile")
        patterns.append("- Keep changes aligned with the existing Ruby/Rails conventions")

    if pyproject.exists() or requirements.exists():
        commands.extend([
            "- Python install flow likely uses `pip`, `uv`, or Poetry depending on the repo",
            "- Python tests may use `pytest`",
        ])
        testing.append("- Python repo detected via pyproject.toml or requirements.txt")
        patterns.append("- Keep changes aligned with the existing Python conventions")

    if commands:
        notes["commands.md"] = "# Commands\n\n" + "\n".join(commands) + "\n"
    if testing:
        notes["testing.md"] = "# Testing\n\n" + "\n".join(testing) + "\n"
    if patterns:
        notes["patterns.md"] = "# Patterns\n\n## Preferred patterns\n\n" + "\n".join(patterns) + "\n"

    arch = [f"# Architecture\n\n## Repo purpose\n\n- Repo name: `{repo.name}`\n"]
    notes["architecture.md"] = "\n".join(arch)
    return notes


def ensure_repo(repo: Path) -> dict[str, Any]:
    repo = repo.resolve()
    if not is_git_repo(repo):
        raise SystemExit(f"Not a git repo: {repo}")

    docs_dir = repo / ".claude" / "repo-docs"
    docs_dir.mkdir(parents=True, exist_ok=True)

    inferred = infer_repo_notes(repo)
    for filename in REPO_DOCS:
        default_path = template_root() / "repo-docs" / filename
        content = inferred.get(filename) or default_path.read_text(encoding="utf-8")
        seed_file_if_missing(docs_dir / filename, content)

    (repo / ".wip").mkdir(parents=True, exist_ok=True)
    merge_vscode_tasks(repo)

    return {
        "repo": str(repo),
        "docs_dir": str(docs_dir),
        "wip_dir": str(repo / ".wip"),
    }


def branch_wip_dir(repo: Path, branch: str | None = None) -> Path:
    branch_name = safe_slug(branch or current_branch(repo))
    return repo / ".wip" / branch_name


def initial_status(repo: Path, branch: str) -> dict[str, Any]:
    return {
        "repo": repo.name,
        "repo_path": str(repo),
        "branch": branch,
        "stage": "started",
        "created_at": now_iso(),
        "updated_at": now_iso(),
        "last_completed_step": None,
        "review_passes": [],
    }


def write_json(path: Path, data: dict[str, Any]) -> None:
    atomic_write(path, json.dumps(data, indent=2) + "\n")


def read_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def atomic_write(path: Path, content: str) -> None:
    """Write content to path atomically using a temp file + replace."""
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_text(content, encoding="utf-8")
    tmp.replace(path)


def verify_wip_file(wip_dir: Path, filename: str) -> tuple[bool, str]:
    """Verify a WIP file exists, is readable, and is non-empty.
    
    Returns (success, error_message). Error message is empty string on success.
    """
    file_path = wip_dir / filename
    if not file_path.exists():
        return False, f"{filename} does not exist"
    if not file_path.is_file():
        return False, f"{filename} is not a regular file"
    try:
        content = file_path.read_text(encoding="utf-8")
    except OSError as e:
        return False, f"{filename} is not readable: {e}"
    if len(content.strip()) < 10:
        return False, f"{filename} is too small or empty (< 10 characters of content)"
    return True, ""


def ensure_branch_state(repo: Path, branch: str | None = None) -> dict[str, Any]:
    repo = git_toplevel(repo)
    ensure_repo(repo)
    branch_name = safe_slug(branch or current_branch(repo))
    wip_dir = branch_wip_dir(repo, branch_name)
    wip_dir.mkdir(parents=True, exist_ok=True)

    for filename in WIP_FILES:
        seed_file_if_missing(wip_dir / filename, f"# {filename.replace('.md', '').replace('-', ' ').title()}\n\n")

    status_path = wip_dir / "status.json"
    if not status_path.exists():
        write_json(status_path, initial_status(repo, branch_name))
    else:
        status = read_json(status_path)
        changed = False
        if status.get("repo") != repo.name:
            status["repo"] = repo.name
            changed = True
        if status.get("repo_path") != str(repo):
            status["repo_path"] = str(repo)
            changed = True
        if status.get("branch") != branch_name:
            status["branch"] = branch_name
            changed = True
        if changed:
            status["updated_at"] = now_iso()
            write_json(status_path, status)

    context_path = wip_dir / "context.md"
    existing_context = context_path.read_text(encoding="utf-8").strip()
    if existing_context in {"# Context", ""} or "- Repo: ``" in existing_context:
        atomic_write(context_path, textwrap.dedent(f"""\
        # Context

        - Repo: `{repo.name}`
        - Repo path: `{repo}`
        - Branch: `{branch_name}`
        - Initialized at: `{now_iso()}`
        - Current status file: `status.json`
        """))

    return {
        "repo": str(repo),
        "branch": branch_name,
        "wip_dir": str(wip_dir),
        "status": read_json(status_path),
    }


def set_stage(repo: Path, stage: str, skip_verification: bool = False) -> dict[str, Any]:
    if stage not in STAGES:
        raise SystemExit(f"Unsupported stage: {stage}. Allowed: {sorted(STAGES)}")
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])
    
    # Verify required files exist for specific stages
    if not skip_verification:
        required_file: str | None = None
        if stage == "planned":
            required_file = "plan.md"
        elif stage == "researched":
            required_file = "research.md"
        elif stage == "reviewed":
            required_file = "review.md"
        
        if required_file:
            success, error = verify_wip_file(wip_dir, required_file)
            if not success:
                raise SystemExit(
                    f"[aidw] Cannot transition to stage '{stage}': {error}\n"
                    f"Hint: Ensure {required_file} exists and has content before setting this stage.\n"
                    f"      Use --skip-verification to bypass this check (not recommended)."
                )
    
    status_path = wip_dir / "status.json"
    status = read_json(status_path)
    status["stage"] = stage
    status["updated_at"] = now_iso()
    status["last_completed_step"] = stage
    write_json(status_path, status)
    
    if (wip_dir / "context-summary.md").exists():
        try:
            write_context_summary(repo)
        except Exception as exc:
            logger.warning("[aidw] failed to auto-regenerate context-summary.md: %s", exc)
    return status


def collect_context_files(wip_dir: Path) -> dict[str, str]:
    """Read all WIP files and return a filename-keyed dict of their contents."""
    filenames = ["plan.md", "research.md", "execution.md", "review.md", "pr.md", "context.md"]
    result: dict[str, str] = {}
    for name in filenames:
        p = wip_dir / name
        result[name] = p.read_text(encoding="utf-8").strip() if p.exists() else ""
    json_path = wip_dir / "status.json"
    try:
        result["status.json"] = json_path.read_text(encoding="utf-8").strip() if json_path.exists() else ""
    except OSError:
        result["status.json"] = ""
    return result


def _trim(text: str, limit: int) -> str:
    """Trim text to limit chars, appending an ellipsis marker if truncated."""
    text = text.strip()
    if not text:
        return "_none_"
    if len(text) <= limit:
        return text
    return text[:limit].rstrip() + " …"


def generate_summary_text(files: dict[str, str], status: dict) -> str:
    """Produce a compact structured markdown summary from WIP file contents."""
    branch = status.get("branch", "unknown")
    stage = status.get("stage", "unknown")
    updated = status.get("updated_at", "")

    lines = [
        "# Workflow Summary",
        "",
        f"## Branch\n{branch}",
        "",
        f"## Current Stage\n{stage}  (updated: {updated})",
        "",
        f"## Goal\n{_trim(files.get('context.md', ''), 300)}",
        "",
        f"## Implementation Plan\n{_trim(files.get('plan.md', ''), 400)}",
        "",
        f"## Key Research Findings\n{_trim(files.get('research.md', ''), 300)}",
        "",
        f"## Implementation Progress\n{_trim(files.get('execution.md', ''), 300)}",
        "",
        f"## Review Findings\n{_trim(files.get('review.md', ''), 200)}",
        "",
        f"## PR Preparation\n{_trim(files.get('pr.md', ''), 150)}",
        "",
    ]
    return "\n".join(lines)


def write_context_summary(repo: Path, branch: str | None = None) -> dict[str, Any]:
    """Generate context-summary.md for the current branch."""
    repo = git_toplevel(repo)
    state = ensure_branch_state(repo, branch)
    wip_dir = Path(state["wip_dir"])
    status = state["status"]

    files = collect_context_files(wip_dir)
    summary = generate_summary_text(files, status)

    summary_path = wip_dir / "context-summary.md"
    atomic_write(summary_path, summary)

    return {
        "summary_path": str(summary_path),
        "size_bytes": len(summary.encode("utf-8")),
        "branch": status.get("branch", "unknown"),
    }


def summarize_status(repo: Path) -> str:
    repo = git_toplevel(repo)
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])
    status = state["status"]

    def first_nonempty(path: Path, fallback: str) -> str:
        txt = path.read_text(encoding="utf-8").strip()
        return txt if txt else fallback

    plan = first_nonempty(wip_dir / "plan.md", "")
    execution = first_nonempty(wip_dir / "execution.md", "")
    context = first_nonempty(wip_dir / "context.md", "")

    return textwrap.dedent(f"""\
    Repo: {repo}
    Branch: {status['branch']}
    Stage: {status['stage']}
    Updated: {status['updated_at']}
    WIP directory: {wip_dir}

    Context preview:
    {context[:600]}

    Plan preview:
    {plan[:600]}

    Execution preview:
    {execution[:600]}
    """)


def merge_vscode_tasks(repo: Path) -> None:
    vscode_dir = repo / ".vscode"
    vscode_dir.mkdir(parents=True, exist_ok=True)
    tasks_path = vscode_dir / "tasks.json"
    template = json.loads((template_root() / "vscode" / "tasks.template.json").read_text(encoding="utf-8"))

    if tasks_path.exists():
        try:
            existing = json.loads(tasks_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            existing = {"version": "2.0.0", "tasks": []}
    else:
        existing = {"version": "2.0.0", "tasks": []}

    existing_tasks = {task.get("label"): task for task in existing.get("tasks", []) if isinstance(task, dict)}
    for task in template.get("tasks", []):
        existing_tasks.setdefault(task.get("label"), task)

    merged = {"version": existing.get("version", "2.0.0"), "tasks": list(existing_tasks.values())}
    tasks_path.write_text(json.dumps(merged, indent=2) + "\n", encoding="utf-8")


MAX_DIFF_BYTES = 50_000


def _find_merge_base(repo: Path, branch: str) -> str | None:
    """Find the merge-base of the current branch against main or master."""
    for base_branch in ("main", "master"):
        result = run(
            ["git", "-C", str(repo), "merge-base", base_branch, "HEAD"],
            check=False,
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    return None


def _truncate_diff(text: str, limit: int = MAX_DIFF_BYTES) -> tuple[str, bool]:
    encoded = text.encode("utf-8")
    if len(encoded) <= limit:
        return text, False
    truncated = encoded[:limit].decode("utf-8", errors="ignore")
    return truncated, True


def review_bundle(repo: Path) -> dict[str, Any]:
    repo = git_toplevel(repo)
    branch = current_branch(repo)
    merge_base = _find_merge_base(repo, branch)

    # Full branch diff (all committed changes since divergence from base)
    branch_diff = ""
    branch_diff_truncated = False
    if merge_base:
        raw = run(
            ["git", "-C", str(repo), "diff", merge_base, "HEAD", "--"],
            check=False,
        ).stdout
        branch_diff, branch_diff_truncated = _truncate_diff(raw)

    # Working-tree (unstaged) changes
    raw_diff = run(["git", "-C", str(repo), "diff", "--", "."], check=False).stdout
    diff, diff_truncated = _truncate_diff(raw_diff)

    # Staged changes
    raw_staged = run(["git", "-C", str(repo), "diff", "--cached", "--", "."], check=False).stdout
    staged_diff, staged_diff_truncated = _truncate_diff(raw_staged)

    status = run(["git", "-C", str(repo), "status", "--short"], check=False).stdout
    changed_files = [line[3:] for line in status.splitlines() if len(line) > 3]

    bundle = {
        "repo": repo.name,
        "repo_path": str(repo),
        "branch": branch,
        "generated_at": now_iso(),
        "diff_sources": {
            "branch_diff": {
                "base": merge_base,
                "description": f"git diff {merge_base[:10]}..HEAD" if merge_base else "unavailable (no merge base found)",
                "truncated": branch_diff_truncated,
                "original_bytes": len(raw) if merge_base else 0,
            },
            "working_tree": {
                "description": "git diff -- .",
                "truncated": diff_truncated,
                "original_bytes": len(raw_diff),
            },
            "staged": {
                "description": "git diff --cached -- .",
                "truncated": staged_diff_truncated,
                "original_bytes": len(raw_staged),
            },
        },
        "changed_files": changed_files,
        "status": status,
        "branch_diff": branch_diff,
        "diff": diff,
        "staged_diff": staged_diff,
    }

    state = ensure_branch_state(repo)
    out_path = Path(state["wip_dir"]) / "review-bundle.json"
    write_json(out_path, bundle)
    return bundle


def gemini_is_installed() -> bool:
    return shutil.which("gemini") is not None


def gemini_review(repo: Path, model: str = GEMINI_MODEL, timeout: int = GEMINI_TIMEOUT) -> dict[str, Any]:
    """Run an adversarial Gemini review pass and write adversarial-review.md."""
    repo = git_toplevel(repo)
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])

    bundle_path = wip_dir / "review-bundle.json"
    if not bundle_path.exists():
        raise SystemExit("[aidw] review-bundle.json not found. Run `aidw review-bundle .` first.")
    bundle = read_json(bundle_path)
    diff_text: str = bundle.get("branch_diff", "") or bundle.get("diff", "") or bundle.get("staged_diff", "")
    changed_files: list[str] = bundle.get("changed_files", [])

    adversarial_path = wip_dir / "adversarial-review.md"

    if not diff_text.strip():
        print("[aidw] No diff available for adversarial review.", file=sys.stderr)
        # Remove stale output so synthesize_review doesn't include old findings
        if adversarial_path.exists():
            adversarial_path.unlink()
        return {"status": "skipped", "reason": "empty diff"}

    file_list_str = "\n".join(f"  - {f}" for f in changed_files) if changed_files else "  (unknown)"
    prompt = (
        "You are an adversarial code reviewer. Your job is to find bugs, security issues, "
        "logic errors, edge cases, and design weaknesses that a friendly reviewer might miss.\n\n"
        "Be critical and direct. Focus on HIGH and CRITICAL issues only — skip nitpicks.\n\n"
        "Changed files:\n"
        f"{file_list_str}\n\n"
        "The full diff is provided via stdin. Review it thoroughly and report your findings."
    )

    try:
        result = subprocess.run(
            ["gemini", "--prompt", prompt, "--model", model, "--output-format", "text"],
            input=diff_text,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        print(
            f"[aidw] Gemini adversarial review timed out after {timeout}s.",
            file=sys.stderr,
        )
        return {"status": "timeout", "model": model}
    except FileNotFoundError:
        print(
            "[aidw] `gemini` binary not found. Install: npm install -g @google/gemini-cli",
            file=sys.stderr,
        )
        return {"status": "not_installed"}

    if result.returncode != 0:
        print(
            f"[aidw] Gemini adversarial review failed (exit {result.returncode}):\n{result.stderr}",
            file=sys.stderr,
        )
        return {"status": "error", "returncode": result.returncode, "stderr": result.stderr}

    output = result.stdout.strip()
    if not output:
        print("[aidw] Gemini returned empty output.", file=sys.stderr)
        # Remove stale output so synthesize_review doesn't include old findings
        if adversarial_path.exists():
            adversarial_path.unlink()
        return {"status": "empty", "model": model}
    atomic_write(adversarial_path, output + "\n")

    status_path = wip_dir / "status.json"
    if status_path.exists():
        st = read_json(status_path)
        rp = st.get("review_passes", [])
        if "gemini-adversarial" not in rp:
            rp.append("gemini-adversarial")
        st["review_passes"] = rp
        st["updated_at"] = now_iso()
        write_json(status_path, st)

    return {"status": "ok", "model": model}


def cmd_gemini_review(args: argparse.Namespace) -> int:
    """CLI wrapper for running an adversarial Gemini review pass."""
    if os.environ.get("AIDW_GEMINI_REVIEW", "0") != "1":
        print("[aidw] Gemini adversarial review disabled (AIDW_GEMINI_REVIEW != 1).", file=sys.stderr)
        return 0
    if not gemini_is_installed():
        raise SystemExit(
            "[aidw] `gemini` binary not found.\n"
            "Install: npm install -g @google/gemini-cli\n"
            "Auth:    gemini auth login  (or set GEMINI_API_KEY)"
        )
    model = getattr(args, "model", GEMINI_MODEL) or GEMINI_MODEL
    timeout = getattr(args, "timeout", GEMINI_TIMEOUT) or GEMINI_TIMEOUT
    timeout = max(10, min(timeout, 600))
    result = gemini_review(Path(args.path), model=model, timeout=timeout)
    status = result.get("status")
    if status == "ok":
        print("Gemini adversarial review complete.", file=sys.stderr)
        return 0
    if status in ("skipped", "empty"):
        print(f"[aidw] Gemini adversarial review {status}: {result.get('reason', '')}", file=sys.stderr)
        return 0
    print(f"[aidw] Gemini adversarial review failed: {result}", file=sys.stderr)
    return 1

def synthesize_review(repo: Path) -> dict[str, Any]:
    """Merge all review sources into review.md."""
    repo = git_toplevel(repo)
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])

    sections: list[str] = ["# Review\n"]

    # Read review bundle summary
    bundle_path = wip_dir / "review-bundle.json"
    if bundle_path.exists():
        try:
            bundle = read_json(bundle_path)
            changed = bundle.get("changed_files", [])
            if changed:
                sections.append("## Changed Files\n")
                for f in changed:
                    sections.append(f"- {f}")
                sections.append("")
        except (json.JSONDecodeError, OSError):
            pass

    # Preserve any existing Claude review content
    review_path = wip_dir / "review.md"
    existing_claude_content: str = ""
    existing_adversarial_review: str = ""
    placeholder = "<!-- Claude should add its own review findings here -->"
    
    if review_path.exists():
        existing_text = review_path.read_text(encoding="utf-8")
        
        # Extract Claude Review section
        heading_match = re.search(r"(?m)^## Claude Review\s*$", existing_text)
        if heading_match is not None:
            after_heading = existing_text[heading_match.end():].lstrip("\n")
            # Find next ## heading or end of file
            next_heading = re.search(r"(?m)^## ", after_heading)
            if next_heading:
                claude_section = after_heading[:next_heading.start()].rstrip("\n")
            else:
                claude_section = after_heading.rstrip("\n")
            if claude_section and claude_section.strip() != placeholder.strip():
                existing_claude_content = claude_section
        
        # Extract Adversarial Review section
        adversarial_match = re.search(r"(?m)^## Adversarial Review\s*$", existing_text)
        if adversarial_match is not None:
            after_heading = existing_text[adversarial_match.end():].lstrip("\n")
            next_heading = re.search(r"(?m)^## ", after_heading)
            if next_heading:
                adversarial_section = after_heading[:next_heading.start()].rstrip("\n")
            else:
                adversarial_section = after_heading.rstrip("\n")
            if adversarial_section and adversarial_section.strip():
                existing_adversarial_review = adversarial_section

    adversarial_path = wip_dir / "adversarial-review.md"
    if adversarial_path.exists():
        sections.append("## Adversarial Review\n")
        adv_content = adversarial_path.read_text(encoding="utf-8").strip()
        sections.append(adv_content + "\n")
    elif existing_adversarial_review:
        sections.append("## Adversarial Review\n")
        sections.append(existing_adversarial_review + "\n")

    sections.append("## Claude Review\n")
    if existing_claude_content:
        sections.append(existing_claude_content + "\n")
    else:
        sections.append(placeholder + "\n")

    atomic_write(review_path, "\n".join(sections) + "\n")

    if (wip_dir / "context-summary.md").exists():
        try:
            write_context_summary(repo)
        except Exception as exc:
            logger.warning("[aidw] failed to auto-regenerate context-summary.md: %s", exc)

    return {
        "review_path": str(review_path),
        "synthesized": True,
    }


# ---------------------------------------------------------------------------
# Verification
# ---------------------------------------------------------------------------

def verify(check_workspace: Path | None = None) -> dict[str, Any]:
    """Verify the installation is correct and functional."""
    results: dict[str, Any] = {"checks": [], "passed": 0, "failed": 0, "warnings": 0}

    def check(name: str, passed: bool, detail: str = "", warn: bool = False) -> None:
        status = "pass" if passed else ("warn" if warn else "FAIL")
        results["checks"].append({"name": name, "status": status, "detail": detail})
        if passed:
            results["passed"] += 1
        elif warn:
            results["warnings"] += 1
        else:
            results["failed"] += 1

    root = script_root()
    claude_home = Path.home() / ".claude"

    # Source repo checks
    check("source: scripts/aidw.py", (root / "scripts" / "aidw.py").exists())
    check("source: install.sh", (root / "install.sh").exists())
    check("source: templates/global/settings.template.json", (root / "templates" / "global" / "settings.template.json").exists())
    check("source: templates/global/claude_managed_block.md", (root / "templates" / "global" / "claude_managed_block.md").exists())
    check("source: templates/vscode/tasks.template.json", (root / "templates" / "vscode" / "tasks.template.json").exists())

    for doc in REPO_DOCS:
        check(f"source: templates/repo-docs/{doc}", (root / "templates" / "repo-docs" / doc).exists())

    skill_names = ["wip-start", "wip-plan", "wip-research", "wip-implement", "wip-review",
                    "wip-fix-review", "wip-resume", "wip-pr", "wip-install"]
    for skill in skill_names:
        skill_path = root / "claude" / "skills" / skill / "SKILL.md"
        check(f"source: skill {skill}", skill_path.exists())

    agent_names = ["wip-planner", "wip-researcher", "wip-reviewer", "wip-tester"]
    for agent in agent_names:
        agent_path = root / "claude" / "agents" / f"{agent}.md"
        check(f"source: agent {agent}", agent_path.exists())

    # Install checks
    symlink = claude_home / "ai-dev-workflow"
    check("install: ~/.claude/ai-dev-workflow symlink", symlink.is_symlink() or symlink.is_dir(),
          str(symlink.resolve()) if symlink.exists() else "not found")

    for skill in skill_names:
        dest = claude_home / "skills" / skill
        is_ok = dest.exists() and (dest / "SKILL.md").exists()
        check(f"install: skill {skill}", is_ok,
              f"symlink -> {dest.resolve()}" if dest.is_symlink() else "not installed", warn=not is_ok)

    for agent in agent_names:
        dest = claude_home / "agents" / f"{agent}.md"
        is_ok = dest.exists()
        check(f"install: agent {agent}", is_ok, warn=not is_ok)

    # Settings check
    settings_path = claude_home / "settings.json"
    if settings_path.exists():
        try:
            settings = json.loads(settings_path.read_text(encoding="utf-8"))
            has_perms = "permissions" in settings
            check("install: settings.json has permissions", has_perms)
        except json.JSONDecodeError:
            check("install: settings.json valid JSON", False, "parse error")
    else:
        check("install: settings.json exists", False, warn=True)

    # CLAUDE.md managed block
    claude_md = claude_home / "CLAUDE.md"
    if claude_md.exists():
        content = claude_md.read_text(encoding="utf-8")
        has_block = "BEGIN AI-DEV-WORKFLOW MANAGED BLOCK" in content
        check("install: CLAUDE.md managed block", has_block)
    else:
        check("install: CLAUDE.md exists", False, warn=True)

    # Global gitignore
    git_config = run(["git", "config", "--global", "core.excludesfile"], check=False)
    if git_config.returncode == 0 and git_config.stdout.strip():
        gi_path = Path(git_config.stdout.strip()).expanduser()
        if gi_path.exists():
            gi_content = gi_path.read_text(encoding="utf-8")
            check("install: global gitignore has .wip/", ".wip/" in gi_content)
            check("install: global gitignore has .claude/repo-docs/", ".claude/repo-docs/" in gi_content)
        else:
            check("install: global gitignore file exists", False, str(gi_path), warn=True)
    else:
        check("install: global gitignore configured", False, warn=True)

    # Workspace / repo check
    if check_workspace:
        ws = check_workspace.resolve()
        if ws.exists():
            repos = detect_repos(ws)
            check(f"workspace: found {len(repos)} repo(s)", len(repos) > 0)
            for repo in repos:
                has_wip = (repo / ".wip").exists()
                has_docs = (repo / ".claude" / "repo-docs").exists()
                check(f"workspace: {repo.name} .wip/", has_wip, warn=not has_wip)
                check(f"workspace: {repo.name} .claude/repo-docs/", has_docs, warn=not has_docs)
        else:
            check("workspace: path exists", False, str(ws))

    # MCP / intelligence tools check (informational)
    uvx_ok = shutil.which("uvx") is not None
    npx_ok = shutil.which("npx") is not None
    check("mcp: uvx installed (for Serena)", uvx_ok, warn=not uvx_ok)
    check("mcp: npx installed (for Context7)", npx_ok, warn=not npx_ok)

    mcp_config = claude_home / "mcp.json"
    if mcp_config.exists():
        try:
            mcp_data = json.loads(mcp_config.read_text(encoding="utf-8"))
            if not isinstance(mcp_data, dict):
                check("mcp: mcp.json has expected object structure", False,
                      "top-level JSON value must be an object", warn=True)
            else:
                servers = mcp_data.get("mcpServers")
                if not isinstance(servers, dict):
                    check("mcp: mcpServers has expected object structure", False,
                          "mcpServers must be a JSON object", warn=True)
                    servers = {}
                check("mcp: serena configured", "serena" in servers, warn="serena" not in servers)
                check("mcp: context7 configured", "context7" in servers, warn="context7" not in servers)
        except json.JSONDecodeError:
            check("mcp: mcp.json valid JSON", False, "parse error")
    else:
        check("mcp: mcp.json exists", False,
              "run installer or create ~/.claude/mcp.json manually", warn=True)

    results["ok"] = results["failed"] == 0
    return results


# ---------------------------------------------------------------------------
# CLI commands
# ---------------------------------------------------------------------------

def cmd_bootstrap_workspace(args: argparse.Namespace) -> int:
    workspace = Path(args.path).resolve()
    if not workspace.exists():
        raise SystemExit(f"Workspace does not exist: {workspace}")
    repos = detect_repos(workspace)
    if not repos:
        print(f"No git repos found under {workspace}")
        return 0
    for repo in repos:
        info = ensure_repo(repo)
        print(f"Bootstrapped repo: {info['repo']}")
    return 0


def cmd_ensure_repo(args: argparse.Namespace) -> int:
    info = ensure_repo(Path(args.path))
    print(json.dumps(info, indent=2))
    return 0


def cmd_start(args: argparse.Namespace) -> int:
    state = ensure_branch_state(Path(args.path), args.branch)
    print(json.dumps(state, indent=2))
    return 0


def cmd_status(args: argparse.Namespace) -> int:
    print(summarize_status(Path(args.path)))
    return 0


def cmd_set_stage(args: argparse.Namespace) -> int:
    skip_verification = getattr(args, 'skip_verification', False)
    status = set_stage(Path(args.path), args.stage, skip_verification=skip_verification)
    print(json.dumps(status, indent=2))
    return 0


def cmd_verify_plan(args: argparse.Namespace) -> int:
    """Verify that plan.md exists and has content."""
    repo = git_toplevel(Path(args.path))
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])
    success, error = verify_wip_file(wip_dir, "plan.md")
    result = {
        "file": "plan.md",
        "wip_dir": str(wip_dir),
        "verified": success,
    }
    if not success:
        result["error"] = error
    print(json.dumps(result, indent=2))
    return 0 if success else 1


def cmd_verify_research(args: argparse.Namespace) -> int:
    """Verify that research.md exists and has content."""
    repo = git_toplevel(Path(args.path))
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])
    success, error = verify_wip_file(wip_dir, "research.md")
    result = {
        "file": "research.md",
        "wip_dir": str(wip_dir),
        "verified": success,
    }
    if not success:
        result["error"] = error
    print(json.dumps(result, indent=2))
    return 0 if success else 1


def cmd_verify_review(args: argparse.Namespace) -> int:
    """Verify that review.md exists and has content."""
    repo = git_toplevel(Path(args.path))
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])
    success, error = verify_wip_file(wip_dir, "review.md")
    result = {
        "file": "review.md",
        "wip_dir": str(wip_dir),
        "verified": success,
    }
    if not success:
        result["error"] = error
    print(json.dumps(result, indent=2))
    return 0 if success else 1


def cmd_summarize_context(args: argparse.Namespace) -> int:
    result = write_context_summary(Path(args.path))
    print(json.dumps(result, indent=2))
    return 0


def cmd_context_summary(args: argparse.Namespace) -> int:
    repo = git_toplevel(Path(args.path))
    state = ensure_branch_state(repo)
    summary_path = Path(state["wip_dir"]) / "context-summary.md"
    if not summary_path.exists():
        print("No context-summary.md found. Run: aidw summarize-context <path>", file=sys.stderr)
        return 1
    print(summary_path.read_text(encoding="utf-8"), end="")
    return 0


def cmd_review_bundle(args: argparse.Namespace) -> int:
    bundle = review_bundle(Path(args.path))
    print(json.dumps(bundle, indent=2))
    return 0


def cmd_synthesize_review(args: argparse.Namespace) -> int:
    result = synthesize_review(Path(args.path))
    print(json.dumps(result, indent=2))
    return 0



def cmd_verify(args: argparse.Namespace) -> int:
    workspace = Path(args.workspace) if args.workspace else None
    results = verify(workspace)

    for c in results["checks"]:
        icon = {"pass": "+", "FAIL": "!", "warn": "~"}[c["status"]]
        detail = f"  ({c['detail']})" if c.get("detail") else ""
        print(f"[{icon}] {c['name']}{detail}")

    print()
    print(f"Passed: {results['passed']}  Failed: {results['failed']}  Warnings: {results['warnings']}")
    return 0 if results["ok"] else 1


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="aidw", description="AI Dev Workflow CLI")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("bootstrap-workspace", help="Bootstrap all repos in a workspace")
    p.add_argument("path")
    p.set_defaults(func=cmd_bootstrap_workspace)

    p = sub.add_parser("ensure-repo", help="Bootstrap a single repo")
    p.add_argument("path")
    p.set_defaults(func=cmd_ensure_repo)

    p = sub.add_parser("start", help="Initialize branch state in .wip/<branch>/")
    p.add_argument("path")
    p.add_argument("--branch")
    p.set_defaults(func=cmd_start)

    p = sub.add_parser("status", help="Show current branch workflow status")
    p.add_argument("path")
    p.set_defaults(func=cmd_status)

    p = sub.add_parser("set-stage", help="Update the workflow stage")
    p.add_argument("path")
    p.add_argument("stage")
    p.add_argument("--skip-verification", action="store_true",
                   help="Skip file verification checks (emergency bypass)")
    p.set_defaults(func=cmd_set_stage)

    p = sub.add_parser("verify-plan", help="Verify plan.md exists and has content")
    p.add_argument("path")
    p.set_defaults(func=cmd_verify_plan)

    p = sub.add_parser("verify-research", help="Verify research.md exists and has content")
    p.add_argument("path")
    p.set_defaults(func=cmd_verify_research)

    p = sub.add_parser("verify-review", help="Verify review.md exists and has content")
    p.add_argument("path")
    p.set_defaults(func=cmd_verify_review)

    p = sub.add_parser("review-bundle", help="Build a review bundle from the current diff")
    p.add_argument("path")
    p.set_defaults(func=cmd_review_bundle)

    p = sub.add_parser("synthesize-review", help="Merge review sources into review.md")
    p.add_argument("path")
    p.set_defaults(func=cmd_synthesize_review)

    p = sub.add_parser("gemini-review", help="Run adversarial Gemini review pass")
    p.add_argument("path")
    p.add_argument("--model", default=GEMINI_MODEL, help="Gemini model to use")
    p.add_argument("--timeout", type=int, default=GEMINI_TIMEOUT, help="Timeout in seconds")
    p.set_defaults(func=cmd_gemini_review)

    p = sub.add_parser("summarize-context", help="Generate context-summary.md from all WIP files")
    p.add_argument("path")
    p.set_defaults(func=cmd_summarize_context)

    p = sub.add_parser("context-summary", help="Print context-summary.md to stdout")
    p.add_argument("path")
    p.set_defaults(func=cmd_context_summary)

    p = sub.add_parser("verify", help="Verify installation and configuration")
    p.add_argument("--workspace", help="Optional workspace path to also check repos")
    p.set_defaults(func=cmd_verify)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
