#!/usr/bin/env python3
from __future__ import annotations
import argparse
import concurrent.futures
import hashlib
import itertools
import re
import json
import os
import shutil
import subprocess
import sys
import textwrap
import threading
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

REPO_DOCS = ["architecture.md", "patterns.md", "commands.md", "testing.md", "gotchas.md"]
WIP_FILES = ["plan.md", "review.md", "research.md", "context.md", "execution.md", "pr.md"]
STAGES = {"started", "planned", "researched", "implementing", "reviewed", "review-fixed", "pr-prep"}
REVIEW_KINDS = ["bug-risk", "missing-tests", "regression-risk", "docs-needed"]

INDEX_MAX_FILES = 400
INDEX_MAX_BYTES_PER_FILE = 60_000
INDEX_MAX_POINTERS_PER_FILE = 20
INDEX_MAX_SECTION_POINTERS = 5

_status_lock = threading.Lock()

PARALLEL_HARD_CAP = 2
DEFAULT_OLLAMA_MAX_PARALLEL = 2
DEFAULT_RESEARCH_PARALLEL = 1
DEFAULT_REVIEW_PARALLEL = 1

# Per-task model defaults (tuned for 16 GB M1 Mac)
_DEFAULT_MODEL_FAST = "phi3:mini"
_DEFAULT_MODEL_REVIEW = "qwen2.5-coder:7b"
_DEFAULT_MODEL_GENERATE = "deepseek-coder:6.7b"

OLLAMA_MODEL_FAST = os.environ.get("AIDW_OLLAMA_MODEL_FAST", _DEFAULT_MODEL_FAST)
OLLAMA_MODEL_REVIEW = os.environ.get("AIDW_OLLAMA_MODEL_REVIEW", _DEFAULT_MODEL_REVIEW)
OLLAMA_MODEL_GENERATE = os.environ.get("AIDW_OLLAMA_MODEL_GENERATE", _DEFAULT_MODEL_GENERATE)

DEFAULT_OLLAMA_MODEL = os.environ.get("AIDW_OLLAMA_MODEL", OLLAMA_MODEL_REVIEW)
DEFAULT_OLLAMA_ENDPOINT = os.environ.get("AIDW_OLLAMA_ENDPOINT", "http://localhost:11434")

# Task kind → model bucket mapping
_REVIEW_TASK_KINDS = frozenset({"bug-risk", "missing-tests", "regression-risk"})
_FAST_TASK_KINDS = frozenset({"docs-needed", "summaries", "synthesis"})
_GENERATE_TASK_KINDS = frozenset({"generate-code", "debug-patch", "patch-draft"})
ALL_TASK_KINDS = sorted(_REVIEW_TASK_KINDS | _FAST_TASK_KINDS | _GENERATE_TASK_KINDS)


def resolve_model_for_kind(kind: str) -> str:
    """Return the appropriate Ollama model name for a given task kind."""
    if kind in _REVIEW_TASK_KINDS:
        return OLLAMA_MODEL_REVIEW
    if kind in _FAST_TASK_KINDS:
        return OLLAMA_MODEL_FAST
    if kind in _GENERATE_TASK_KINDS:
        return OLLAMA_MODEL_GENERATE
    return DEFAULT_OLLAMA_MODEL


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


def _parse_int_env(name: str, default: int, minimum: int, maximum: int) -> int:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return default
    try:
        parsed = int(raw.strip())
    except ValueError:
        return default
    return max(minimum, min(parsed, maximum))


def _effective_parallel(requested: int, global_max: int) -> int:
    return max(1, min(requested, global_max, PARALLEL_HARD_CAP))


def _repo_index_path(repo: Path) -> Path:
    return repo / ".wip" / "repo-index.json"


def _is_ignored_path(path: Path) -> bool:
    ignored = {".git", ".wip", "node_modules", ".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache"}
    return any(part in ignored for part in path.parts)


def _file_kind(path: Path) -> str:
    suffix = path.suffix.lower()
    if suffix == ".py":
        return "python"
    if suffix in {".js", ".jsx", ".mjs", ".cjs"}:
        return "javascript"
    if suffix in {".ts", ".tsx"}:
        return "typescript"
    if suffix == ".md":
        return "markdown"
    if suffix in {".sh", ".zsh", ".bash"}:
        return "shell"
    if suffix == ".json":
        return "json"
    if suffix in {".yml", ".yaml"}:
        return "yaml"
    if suffix in {".toml", ".ini", ".cfg"}:
        return "config"
    if suffix in {".env"} or path.name.endswith(".env"):
        return "env"
    return "other"


def _extract_top_level_keys(text: str) -> list[str]:
    try:
        data = json.loads(text)
        if isinstance(data, dict):
            return [str(k) for k in list(data.keys())[:INDEX_MAX_POINTERS_PER_FILE]]
    except Exception:
        pass
    return []


def _extract_pointers(kind: str, text: str) -> dict[str, list[str]]:
    pointers: dict[str, list[str]] = {"symbols": [], "headings": [], "keys": []}
    lines = text.splitlines()

    if kind == "python":
        rx = re.compile(r"^\s*(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)")
        pointers["symbols"] = [m.group(1) for line in lines for m in [rx.match(line)] if m][:INDEX_MAX_POINTERS_PER_FILE]
    elif kind in {"javascript", "typescript"}:
        rx = re.compile(r"^\s*(?:export\s+)?(?:async\s+)?(?:function|class|const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)")
        pointers["symbols"] = [m.group(1) for line in lines for m in [rx.match(line)] if m][:INDEX_MAX_POINTERS_PER_FILE]
    elif kind == "markdown":
        headings = [line.strip() for line in lines if line.lstrip().startswith("#")]
        pointers["headings"] = headings[:INDEX_MAX_POINTERS_PER_FILE]
    elif kind == "shell":
        fn_rx = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{")
        section_rx = re.compile(r"^\s*##+\s+(.+)$")
        fns = [m.group(1) for line in lines for m in [fn_rx.match(line)] if m]
        sections = [m.group(1).strip() for line in lines for m in [section_rx.match(line)] if m]
        pointers["symbols"] = fns[:INDEX_MAX_POINTERS_PER_FILE]
        pointers["headings"] = sections[:INDEX_MAX_POINTERS_PER_FILE]
    elif kind == "json":
        pointers["keys"] = _extract_top_level_keys(text)
    elif kind in {"yaml", "config", "env"}:
        key_rx = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*[:=]")
        keys = [m.group(1) for line in lines for m in [key_rx.match(line)] if m]
        pointers["keys"] = keys[:INDEX_MAX_POINTERS_PER_FILE]

    return pointers


def build_repo_index(repo: Path) -> dict[str, Any]:
    repo = git_toplevel(repo)
    entries: list[dict[str, Any]] = []
    truncated_files = False
    skipped_files: list[dict[str, str]] = []

    _gen = (p for p in repo.rglob("*") if p.is_file() and not _is_ignored_path(p))
    _sampled = list(itertools.islice(_gen, INDEX_MAX_FILES + 1))
    truncated_files = len(_sampled) > INDEX_MAX_FILES
    all_files = sorted(_sampled[:INDEX_MAX_FILES])

    for path in all_files:
        rel = path.relative_to(repo)
        kind = _file_kind(path)
        try:
            size = path.stat().st_size
        except OSError as exc:
            skipped_files.append({"path": str(rel), "reason": f"stat failed: {exc}"})
            continue

        try:
            if size > INDEX_MAX_BYTES_PER_FILE:
                with path.open(encoding="utf-8", errors="ignore") as _fh:
                    content = _fh.read(INDEX_MAX_BYTES_PER_FILE)
                content_truncated = True
            else:
                content = path.read_text(encoding="utf-8", errors="ignore")
                content_truncated = False
        except OSError as exc:
            skipped_files.append({"path": str(rel), "reason": f"read failed: {exc}"})
            continue

        pointers = _extract_pointers(kind, content)
        line_count = content.count("\n") + (1 if content else 0)

        entries.append(
            {
                "path": str(rel),
                "kind": kind,
                "line_count": line_count,
                "size_bytes": size,
                "content_truncated": content_truncated,
                "pointers": pointers,
            }
        )

    index = {
        "repo": repo.name,
        "repo_path": str(repo),
        "generated_at": now_iso(),
        "limits": {
            "max_files": INDEX_MAX_FILES,
            "max_bytes_per_file": INDEX_MAX_BYTES_PER_FILE,
            "max_pointers_per_file": INDEX_MAX_POINTERS_PER_FILE,
        },
        "truncated": {"files": truncated_files, "skipped_files": len(skipped_files)},
        "skipped": skipped_files[:20],
        "files": entries,
    }

    index_path = _repo_index_path(repo)
    index_path.parent.mkdir(parents=True, exist_ok=True)
    write_json(index_path, index)
    return index


def _tokenize_goal(goal: str) -> list[str]:
    return [t for t in re.findall(r"[a-zA-Z0-9_-]+", goal.lower()) if len(t) >= 3]


def _entry_text_for_scoring(entry: dict[str, Any]) -> str:
    pointers = entry.get("pointers", {})
    tokens = [entry.get("path", ""), entry.get("kind", "")]
    for key in ("symbols", "headings", "keys"):
        tokens.extend(pointers.get(key, []))
    return " ".join(tokens).lower()


def _score_entry(entry: dict[str, Any], terms: list[str]) -> int:
    if not terms:
        return 1
    haystack = _entry_text_for_scoring(entry)
    score = 0
    for term in terms:
        if term in haystack:
            score += 2
        if term in entry.get("path", "").lower():
            score += 1
    return score


def _select_lane(index: dict[str, Any], terms: list[str], lane: str, limit: int) -> list[dict[str, Any]]:
    files = index.get("files", [])
    if lane == "code":
        allowed = {"python", "javascript", "typescript", "shell"}
    else:
        allowed = {"markdown", "yaml", "json", "config", "env", "other"}
    ranked = []
    for entry in files:
        if entry.get("kind") not in allowed:
            continue
        score = _score_entry(entry, terms)
        if score > 0:
            ranked.append((score, entry))
    ranked.sort(key=lambda it: (-it[0], it[1].get("path", "")))
    return [entry for _, entry in ranked[:limit]]


def research_scan(repo: Path, goal: str) -> dict[str, Any]:
    repo = git_toplevel(repo)
    index_path = _repo_index_path(repo)
    index = read_json(index_path) if index_path.exists() else build_repo_index(repo)

    requested_research_parallel = _parse_int_env(
        "AIDW_RESEARCH_PARALLEL", DEFAULT_RESEARCH_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    ollama_max_parallel = _parse_int_env(
        "AIDW_OLLAMA_MAX_PARALLEL", DEFAULT_OLLAMA_MAX_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    effective_research_parallel = _effective_parallel(requested_research_parallel, ollama_max_parallel)

    terms = _tokenize_goal(goal)
    if effective_research_parallel <= 1:
        code_lane = _select_lane(index, terms, lane="code", limit=5)
        docs_lane = _select_lane(index, terms, lane="docs", limit=5)
    else:
        with concurrent.futures.ThreadPoolExecutor(max_workers=effective_research_parallel) as executor:
            code_future = executor.submit(_select_lane, index, terms, "code", 5)
            docs_future = executor.submit(_select_lane, index, terms, "docs", 5)
            # Resolve in lane order for deterministic output while still parallelizing work.
            code_lane = code_future.result()
            docs_lane = docs_future.result()

    merged: dict[str, dict[str, Any]] = {}
    for lane_name, entries in (("code", code_lane), ("docs", docs_lane)):
        for entry in entries:
            path = entry.get("path", "")
            pointers = entry.get("pointers", {})
            sections = (pointers.get("symbols", []) + pointers.get("headings", []) + pointers.get("keys", []))
            sections = sections[:INDEX_MAX_SECTION_POINTERS]
            reason = f"{lane_name} lane score={_score_entry(entry, terms)}"
            if path in merged:
                merged[path]["lanes"].append(lane_name)
                merged[path]["why"].append(reason)
            else:
                merged[path] = {
                    "path": path,
                    "kind": entry.get("kind", "other"),
                    "lanes": [lane_name],
                    "sections": sections,
                    "why": [reason],
                }

    selected = sorted(merged.values(), key=lambda item: item["path"])[:10]
    state = ensure_branch_state(repo)
    out_path = Path(state["wip_dir"]) / "research-scan.json"
    result = {
        "repo": repo.name,
        "branch": state["status"]["branch"],
        "goal": goal,
        "generated_at": now_iso(),
        "terms": terms,
        "limits": {
            "max_results": 10,
            "max_sections_per_file": INDEX_MAX_SECTION_POINTERS,
        },
        "lanes": {
            "code_count": len(code_lane),
            "docs_count": len(docs_lane),
        },
        "parallel": {
            "requested": requested_research_parallel,
            "effective": effective_research_parallel,
            "ollama_max_parallel": ollama_max_parallel,
        },
        "results": selected,
        "truncated": {
            "results": len(merged) > len(selected),
        },
    }
    write_json(out_path, result)
    return result


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
        context_path.write_text(textwrap.dedent(f"""\
        # Context

        - Repo: `{repo.name}`
        - Repo path: `{repo}`
        - Branch: `{branch_name}`
        - Initialized at: `{now_iso()}`
        - Current status file: `status.json`
        """), encoding="utf-8")

    return {
        "repo": str(repo),
        "branch": branch_name,
        "wip_dir": str(wip_dir),
        "status": read_json(status_path),
    }


def set_stage(repo: Path, stage: str) -> dict[str, Any]:
    if stage not in STAGES:
        raise SystemExit(f"Unsupported stage: {stage}. Allowed: {sorted(STAGES)}")
    state = ensure_branch_state(repo)
    status_path = Path(state["wip_dir"]) / "status.json"
    status = read_json(status_path)
    status["stage"] = stage
    status["updated_at"] = now_iso()
    status["last_completed_step"] = stage
    write_json(status_path, status)
    wip_dir = Path(state["wip_dir"])
    if (wip_dir / "context-summary.md").exists():
        try:
            write_context_summary(repo)
        except Exception as exc:
            print(f"[aidw] Warning: failed to auto-regenerate context-summary.md: {exc}", file=sys.stderr)
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


# ---------------------------------------------------------------------------
# Ollama helpers
# ---------------------------------------------------------------------------

ALLOWED_OLLAMA_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})


def validate_ollama_endpoint(endpoint: str) -> None:
    """Reject non-local Ollama endpoints unless explicitly opted in."""
    if "://" not in endpoint:
        raise SystemExit(
            f"Invalid Ollama endpoint '{endpoint}'.\n"
            f"Please include a scheme, e.g. 'http://localhost:11434' or 'https://localhost:11434'."
        )
    parsed = urlparse(endpoint)
    scheme = (parsed.scheme or "").lower()
    if scheme not in ("http", "https"):
        raise SystemExit(
            f"Invalid Ollama endpoint scheme '{parsed.scheme}' in '{endpoint}'.\n"
            f"Only http and https schemes are supported."
        )
    if parsed.path not in ("", "/"):
        raise SystemExit(
            f"Invalid Ollama endpoint '{endpoint}'.\n"
            f"Please provide the base Ollama URL without a path, e.g. 'http://localhost:11434'."
        )
    hostname = parsed.hostname or ""
    if hostname not in ALLOWED_OLLAMA_HOSTS:
        if os.environ.get("AIDW_OLLAMA_ALLOW_REMOTE", "").strip() in ("1", "true", "yes"):
            return
        raise SystemExit(
            f"Ollama endpoint '{endpoint}' is not a local address.\n"
            f"Only localhost, 127.0.0.1, and ::1 are allowed by default.\n"
            f"Set AIDW_OLLAMA_ALLOW_REMOTE=1 to allow remote endpoints."
        )


def ollama_is_installed() -> bool:
    return shutil.which("ollama") is not None


def ollama_is_running(endpoint: str) -> bool:
    try:
        req = urllib.request.Request(endpoint.rstrip("/") + "/api/tags", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status == 200
    except Exception:
        return False


def ollama_has_model(model: str, endpoint: str) -> bool:
    try:
        req = urllib.request.Request(endpoint.rstrip("/") + "/api/tags", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            models = [m.get("name", "") for m in data.get("models", [])]
            return any(model == m or model == m.split(":")[0] for m in models)
    except Exception:
        return False


def ollama_list_models(endpoint: str) -> list[str]:
    try:
        req = urllib.request.Request(endpoint.rstrip("/") + "/api/tags", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return [m.get("name", "") for m in data.get("models", [])]
    except Exception:
        return []


def stop_ollama_model(model: str, endpoint: str) -> bool:
    """Request Ollama to unload a model from memory (sets keep_alive=0).

    Returns True on success. Failures are logged as warnings, never fatal.
    Important on RAM-constrained machines (e.g. 16 GB M1 Mac).
    """
    try:
        body = json.dumps({"model": model, "keep_alive": 0, "stream": False}).encode("utf-8")
        req = urllib.request.Request(
            endpoint.rstrip("/") + "/api/generate",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.status == 200
    except Exception as exc:
        print(f"Warning: could not stop model '{model}': {exc}", file=sys.stderr)
        return False


def ollama_start_server(endpoint: str, timeout_sec: int = 30) -> "subprocess.Popen[str] | None":
    """Start the Ollama server if it is not already running.

    Only starts a server for local endpoints (localhost / 127.0.0.1 / ::1).
    Returns the ``Popen`` object if *we* started the server so the caller can
    stop it later, or ``None`` if the server was already running (or the
    endpoint is remote and we should not touch it).

    Raises ``SystemExit`` when Ollama is not installed or fails to start.
    """
    parsed = urlparse(endpoint)
    hostname = parsed.hostname or ""
    if hostname not in ALLOWED_OLLAMA_HOSTS:
        # Remote endpoint — never auto-start.
        return None

    if ollama_is_running(endpoint):
        return None  # Already up; nothing to do.

    if not ollama_is_installed():
        raise SystemExit(
            "Ollama is not installed. Cannot auto-start.\n"
            "  macOS : brew install ollama\n"
            "  Linux : curl -fsSL https://ollama.com/install.sh | sh\n"
            "  Windows: https://ollama.com/download"
        )

    print("Auto-starting Ollama server...", file=sys.stderr)
    parsed = urlparse(endpoint)
    host = parsed.hostname or "localhost"
    port = parsed.port or (443 if parsed.scheme == "https" else 11434)
    env = os.environ.copy()
    env["OLLAMA_HOST"] = f"{host}:{port}"
    proc: subprocess.Popen[str] = subprocess.Popen(
        ["ollama", "serve"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
    )

    deadline = time.monotonic() + timeout_sec
    while time.monotonic() < deadline:
        time.sleep(1)
        if proc.poll() is not None:
            raise SystemExit(
                f"Ollama server process exited unexpectedly (code {proc.returncode})."
            )
        if ollama_is_running(endpoint):
            print(f"Ollama server started (PID {proc.pid}).", file=sys.stderr)
            return proc

    proc.terminate()
    raise SystemExit(f"Ollama server did not become ready within {timeout_sec}s.")


def ollama_stop_server(proc: "subprocess.Popen[str]") -> None:
    """Terminate an Ollama server process started by :func:`ollama_start_server`."""
    if proc.poll() is not None:
        return  # Already exited.
    print(f"Stopping Ollama server (PID {proc.pid})...", file=sys.stderr)
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    print("Ollama server stopped.", file=sys.stderr)


def ollama_review(repo: Path, kind: str, model: str, endpoint: str) -> dict[str, Any]:
    repo = git_toplevel(repo)
    bundle = review_bundle(repo)
    prompt = {
        "bug-risk": "Review this diff for likely correctness issues, hidden bugs, and risky assumptions.",
        "missing-tests": "Review this diff and identify missing tests or validation coverage gaps.",
        "regression-risk": "Review this diff for likely regressions, affected nearby code paths, and brittle changes.",
        "docs-needed": "Review this diff and determine what documentation, runbooks, or notes should be updated.",
        "summaries": "Produce a concise, structured summary of what this diff changes and why it matters.",
        "synthesis": "Synthesize the key themes and risk areas across this diff into a brief executive review summary.",
        "generate-code": "Analyze this diff and generate any missing implementation, stubs, or helpers that would complete the feature.",
        "debug-patch": "Analyze this diff for bugs and produce a concrete minimal patch that fixes the most critical issue found.",
        "patch-draft": "Draft a minimal, focused patch that addresses the most important correctness or reliability issue in this diff.",
    }.get(kind)
    if not prompt:
        raise SystemExit(f"Unsupported kind: {kind}. Allowed: {ALL_TASK_KINDS}")

    schema_hint = {
        "type": "object",
        "properties": {
            "kind": {"type": "string"},
            "summary": {"type": "string"},
            "findings": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "severity": {"type": "string"},
                        "file": {"type": "string"},
                        "issue": {"type": "string"},
                        "recommendation": {"type": "string"}
                    },
                    "required": ["severity", "issue", "recommendation"]
                }
            }
        },
        "required": ["kind", "summary", "findings"]
    }

    body = {
        "model": model,
        "stream": False,
        "format": schema_hint,
        "messages": [
            {
                "role": "system",
                "content": (
                    "You are a code review assistant. Return strict JSON that matches the requested schema. "
                    "Be concrete, terse, and practical."
                ),
            },
            {
                "role": "user",
                "content": json.dumps({
                    "instruction": prompt,
                    "kind": kind,
                    "bundle": bundle,
                }),
            },
        ],
    }

    req = urllib.request.Request(
        endpoint.rstrip("/") + "/api/chat",
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=120) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.URLError as exc:
        raise SystemExit(f"Failed to reach Ollama at {endpoint}: {exc}")

    raw_message = payload.get("message", {}).get("content", "{}")
    try:
        parsed = json.loads(raw_message)
    except json.JSONDecodeError:
        parsed = {"kind": kind, "summary": raw_message, "findings": []}

    state = ensure_branch_state(repo)
    out_path = Path(state["wip_dir"]) / f"ollama-review-{kind}.json"
    write_json(out_path, parsed)

    status_path = Path(state["wip_dir"]) / "status.json"
    with _status_lock:
        status = read_json(status_path)
        review_passes = status.get("review_passes", [])
        if kind not in review_passes:
            review_passes.append(kind)
        status["review_passes"] = review_passes
        status["updated_at"] = now_iso()
        write_json(status_path, status)

    return parsed


def synthesize_review(repo: Path) -> dict[str, Any]:
    """Merge all review sources into review.md."""
    repo = git_toplevel(repo)
    state = ensure_branch_state(repo)
    wip_dir = Path(state["wip_dir"])

    sections: list[str] = ["# Review\n"]

    # Collect Ollama review results
    ollama_results: list[dict[str, Any]] = []
    for kind in REVIEW_KINDS:
        result_path = wip_dir / f"ollama-review-{kind}.json"
        if result_path.exists():
            try:
                ollama_results.append(read_json(result_path))
            except (json.JSONDecodeError, OSError):
                pass

    if ollama_results:
        sections.append("## Ollama Review Passes\n")
        for result in ollama_results:
            kind = result.get("kind", "unknown")
            summary = result.get("summary", "No summary.")
            findings = result.get("findings", [])
            sections.append(f"### {kind}\n")
            sections.append(f"{summary}\n")
            if findings:
                for f in findings:
                    sev = f.get("severity", "?")
                    issue = f.get("issue", "")
                    rec = f.get("recommendation", "")
                    file_ = f.get("file", "")
                    loc = f" (`{file_}`)" if file_ else ""
                    sections.append(f"- **{sev}**{loc}: {issue}")
                    if rec:
                        sections.append(f"  - Recommendation: {rec}")
                sections.append("")

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
    placeholder = "<!-- Claude should add its own review findings here -->"
    if review_path.exists():
        existing_text = review_path.read_text(encoding="utf-8")
        heading_match = re.search(r"(?m)^## Claude Review\s*$", existing_text)
        if heading_match is not None:
            after_heading = existing_text[heading_match.end():].lstrip("\n")
            if after_heading and after_heading.strip() != placeholder.strip():
                existing_claude_content = after_heading.rstrip("\n")

    sections.append("## Claude Review\n")
    if existing_claude_content:
        sections.append(existing_claude_content + "\n")
    else:
        sections.append(placeholder + "\n")

    review_path.write_text("\n".join(sections) + "\n", encoding="utf-8")

    if (wip_dir / "context-summary.md").exists():
        try:
            write_context_summary(repo)
        except Exception as exc:
            print(f"[aidw] Warning: failed to auto-regenerate context-summary.md: {exc}", file=sys.stderr)

    return {
        "review_path": str(review_path),
        "ollama_passes": len(ollama_results),
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

    # Ollama check (informational)
    check("ollama: binary installed", ollama_is_installed(), warn=not ollama_is_installed())
    if ollama_is_installed():
        try:
            validate_ollama_endpoint(DEFAULT_OLLAMA_ENDPOINT)
        except SystemExit as exc:
            check("ollama: endpoint configuration", False, str(exc), warn=True)
        else:
            running = ollama_is_running(DEFAULT_OLLAMA_ENDPOINT)
            check("ollama: service running", running, warn=not running)
            if running:
                for role, model_name in [
                    ("fast", OLLAMA_MODEL_FAST),
                    ("review", OLLAMA_MODEL_REVIEW),
                    ("generate", OLLAMA_MODEL_GENERATE),
                ]:
                    has_m = ollama_has_model(model_name, DEFAULT_OLLAMA_ENDPOINT)
                    check(f"ollama: {role} model {model_name}", has_m, warn=not has_m)

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
    status = set_stage(Path(args.path), args.stage)
    print(json.dumps(status, indent=2))
    return 0


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


def cmd_ollama_review(args: argparse.Namespace) -> int:
    endpoint = args.endpoint
    validate_ollama_endpoint(endpoint)
    model = args.model if args.model else resolve_model_for_kind(args.kind)

    server_proc: subprocess.Popen[str] | None = None
    if not getattr(args, "no_auto_start", False):
        server_proc = ollama_start_server(endpoint)
    elif not ollama_is_running(endpoint):
        raise SystemExit(f"Ollama is not running at {endpoint}. Run: ollama serve")

    print(f"[ollama-review] kind={args.kind} model={model} endpoint={endpoint}", file=sys.stderr)
    try:
        parsed = ollama_review(Path(args.path), args.kind, model, endpoint)
        if not getattr(args, "no_stop", False):
            stop_ollama_model(model, endpoint)
        print(json.dumps(parsed, indent=2))
    finally:
        if server_proc is not None:
            ollama_stop_server(server_proc)
    return 0


def cmd_ollama_check(args: argparse.Namespace) -> int:
    endpoint = args.endpoint
    validate_ollama_endpoint(endpoint)
    results: dict[str, Any] = {}

    results["ollama_installed"] = ollama_is_installed()
    if not results["ollama_installed"]:
        results["ok"] = False
        results["message"] = "Ollama is not installed."
        results["install_help"] = {
            "macOS": "brew install ollama  # or download from https://ollama.com/download",
            "Linux": "curl -fsSL https://ollama.com/install.sh | sh",
            "Windows": "Download from https://ollama.com/download",
        }
        print(json.dumps(results, indent=2))
        return 1

    results["ollama_running"] = ollama_is_running(endpoint)
    if not results["ollama_running"]:
        results["ok"] = False
        results["message"] = f"Ollama is installed but not running at {endpoint}."
        results["start_help"] = "Run: ollama serve"
        print(json.dumps(results, indent=2))
        return 1

    results["available_models"] = ollama_list_models(endpoint)
    models_to_check = {
        "fast": OLLAMA_MODEL_FAST,
        "review": OLLAMA_MODEL_REVIEW,
        "generate": OLLAMA_MODEL_GENERATE,
    }
    model_results: dict[str, Any] = {}
    missing_models: list[str] = []
    for role, model_name in models_to_check.items():
        available = ollama_has_model(model_name, endpoint)
        model_results[role] = {"model": model_name, "available": available}
        if not available:
            missing_models.append(model_name)

    results["models"] = model_results

    if missing_models:
        results["ok"] = False
        results["message"] = f"{len(missing_models)} model(s) not pulled locally."
        results["pull_help"] = [f"ollama pull {m}" for m in missing_models]
        if results["available_models"]:
            results["hint"] = f"Available: {', '.join(results['available_models'][:10])}"
        print(json.dumps(results, indent=2))
        return 1

    results["ok"] = True
    results["message"] = "Ollama is ready. All configured models are available."
    print(json.dumps(results, indent=2))
    return 0


def cmd_review_all(args: argparse.Namespace) -> int:
    repo = Path(args.path)
    endpoint = args.endpoint
    validate_ollama_endpoint(endpoint)

    if not ollama_is_installed():
        raise SystemExit("Ollama is not installed. Run: aidw ollama-check")

    # Auto-start the server if it is not running (unless opted out).
    server_proc: subprocess.Popen[str] | None = None
    if not getattr(args, "no_auto_start", False):
        server_proc = ollama_start_server(endpoint)
    elif not ollama_is_running(endpoint):
        raise SystemExit(f"Ollama is not running at {endpoint}. Run: ollama serve")

    # Resolve per-kind models; verify all required ones are available.
    kind_models: dict[str, str] = {kind: resolve_model_for_kind(kind) for kind in REVIEW_KINDS}
    required_models = set(kind_models.values())
    for model_name in sorted(required_models):
        if not ollama_has_model(model_name, endpoint):
            if server_proc is not None:
                ollama_stop_server(server_proc)
            raise SystemExit(f"Model '{model_name}' not available. Run: ollama pull {model_name}")

    review_parallel_requested = _parse_int_env(
        "AIDW_REVIEW_PARALLEL", DEFAULT_REVIEW_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    ollama_max_parallel = _parse_int_env(
        "AIDW_OLLAMA_MAX_PARALLEL", DEFAULT_OLLAMA_MAX_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    cli_parallel = getattr(args, "parallel", None)
    if cli_parallel is not None:
        requested = max(1, min(int(cli_parallel), PARALLEL_HARD_CAP))
    else:
        requested = review_parallel_requested
    effective_parallel = _effective_parallel(requested, ollama_max_parallel)

    if effective_parallel > 1 and len(required_models) == 1:
        # A single shared model often causes local contention; keep stable default.
        effective_parallel = 1

    results_by_kind: dict[str, dict[str, Any]] = {}
    used_models: set[str] = set()

    def _run_one(kind: str) -> tuple[str, dict[str, Any]]:
        model = kind_models[kind]
        used_models.add(model)
        parsed = ollama_review(repo, kind, model, endpoint)
        return kind, parsed

    def _run_one_safe(kind: str) -> tuple[str, dict[str, Any], bool]:
        model = kind_models[kind]
        print(f"Running {kind} review pass (model: {model})...", file=sys.stderr)
        try:
            _, parsed = _run_one(kind)
            print(f"  {kind}: {len(parsed.get('findings', []))} finding(s)", file=sys.stderr)
            return kind, parsed, True
        except SystemExit as exc:
            print(f"  {kind}: failed ({exc})", file=sys.stderr)
            return kind, {"kind": kind, "summary": f"Failed: {exc}", "findings": []}, False
        except Exception as exc:
            print(f"  {kind}: failed ({exc})", file=sys.stderr)
            return kind, {"kind": kind, "summary": f"Failed: {exc}", "findings": []}, False

    try:
        if effective_parallel <= 1:
            for kind in REVIEW_KINDS:
                _, parsed, _ = _run_one_safe(kind)
                results_by_kind[kind] = parsed
        else:
            batches = [REVIEW_KINDS[:2], REVIEW_KINDS[2:]]
            pending_sequential: list[str] = []
            for batch in batches:
                if not batch:
                    continue
                print(f"Running parallel review batch: {', '.join(batch)}", file=sys.stderr)
                batch_failed = False
                with concurrent.futures.ThreadPoolExecutor(max_workers=effective_parallel) as executor:
                    futures = {executor.submit(_run_one_safe, kind): kind for kind in batch}
                    for future in concurrent.futures.as_completed(futures):
                        kind, parsed, success = future.result()
                        results_by_kind[kind] = parsed
                        if not success:
                            batch_failed = True
                if batch_failed:
                    index = batches.index(batch)
                    for remainder in batches[index + 1:]:
                        pending_sequential.extend(remainder)
                    break

            if pending_sequential:
                print(
                    "Parallel instability detected; falling back to sequential for remaining passes.",
                    file=sys.stderr,
                )
                for kind in pending_sequential:
                    _, parsed, _ = _run_one_safe(kind)
                    results_by_kind[kind] = parsed

        if not getattr(args, "no_stop", False):
            for model_name in sorted(used_models):
                print(f"Stopping model {model_name}...", file=sys.stderr)
                stop_ollama_model(model_name, endpoint)
    finally:
        if server_proc is not None:
            ollama_stop_server(server_proc)

    results = [results_by_kind.get(kind, {"kind": kind, "summary": "Not run", "findings": []}) for kind in REVIEW_KINDS]
    print(json.dumps(results, indent=2))
    return 0


def cmd_build_index(args: argparse.Namespace) -> int:
    result = build_repo_index(Path(args.path))
    print(json.dumps(result, indent=2))
    return 0


def cmd_research_scan(args: argparse.Namespace) -> int:
    result = research_scan(Path(args.path), args.goal)
    print(json.dumps(result, indent=2))
    return 0


def cmd_synthesize_review(args: argparse.Namespace) -> int:
    result = synthesize_review(Path(args.path))
    print(json.dumps(result, indent=2))
    return 0


def cmd_ollama_config(args: argparse.Namespace) -> int:
    endpoint = getattr(args, "endpoint", DEFAULT_OLLAMA_ENDPOINT)
    requested_review_parallel = _parse_int_env(
        "AIDW_REVIEW_PARALLEL", DEFAULT_REVIEW_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    requested_research_parallel = _parse_int_env(
        "AIDW_RESEARCH_PARALLEL", DEFAULT_RESEARCH_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    ollama_max_parallel = _parse_int_env(
        "AIDW_OLLAMA_MAX_PARALLEL", DEFAULT_OLLAMA_MAX_PARALLEL, 1, PARALLEL_HARD_CAP
    )
    config = {
        "endpoint": endpoint,
        "models": {
            "fast": {
                "env": "AIDW_OLLAMA_MODEL_FAST",
                "effective": OLLAMA_MODEL_FAST,
                "default": _DEFAULT_MODEL_FAST,
                "uses": sorted(_FAST_TASK_KINDS),
            },
            "review": {
                "env": "AIDW_OLLAMA_MODEL_REVIEW",
                "effective": OLLAMA_MODEL_REVIEW,
                "default": _DEFAULT_MODEL_REVIEW,
                "uses": sorted(_REVIEW_TASK_KINDS),
            },
            "generate": {
                "env": "AIDW_OLLAMA_MODEL_GENERATE",
                "effective": OLLAMA_MODEL_GENERATE,
                "default": _DEFAULT_MODEL_GENERATE,
                "uses": sorted(_GENERATE_TASK_KINDS),
            },
        },
        "fallback_model": {
            "env": "AIDW_OLLAMA_MODEL",
            "effective": DEFAULT_OLLAMA_MODEL,
        },
        "parallel": {
            "hard_cap": PARALLEL_HARD_CAP,
            "ollama_max_parallel": {
                "env": "AIDW_OLLAMA_MAX_PARALLEL",
                "effective": ollama_max_parallel,
                "default": DEFAULT_OLLAMA_MAX_PARALLEL,
            },
            "research_parallel": {
                "env": "AIDW_RESEARCH_PARALLEL",
                "effective": requested_research_parallel,
                "default": DEFAULT_RESEARCH_PARALLEL,
                "effective_with_max": _effective_parallel(requested_research_parallel, ollama_max_parallel),
            },
            "review_parallel": {
                "env": "AIDW_REVIEW_PARALLEL",
                "effective": requested_review_parallel,
                "default": DEFAULT_REVIEW_PARALLEL,
                "effective_with_max": _effective_parallel(requested_review_parallel, ollama_max_parallel),
            },
        },
        "env_vars": {
            "AIDW_OLLAMA_ENDPOINT": os.environ.get("AIDW_OLLAMA_ENDPOINT", "(not set — using default)"),
            "AIDW_OLLAMA_MODEL": os.environ.get("AIDW_OLLAMA_MODEL", "(not set — using review model)"),
            "AIDW_OLLAMA_MODEL_FAST": os.environ.get("AIDW_OLLAMA_MODEL_FAST", "(not set — using default)"),
            "AIDW_OLLAMA_MODEL_REVIEW": os.environ.get("AIDW_OLLAMA_MODEL_REVIEW", "(not set — using default)"),
            "AIDW_OLLAMA_MODEL_GENERATE": os.environ.get("AIDW_OLLAMA_MODEL_GENERATE", "(not set — using default)"),
            "AIDW_OLLAMA_MAX_PARALLEL": os.environ.get("AIDW_OLLAMA_MAX_PARALLEL", "(not set — using default)"),
            "AIDW_RESEARCH_PARALLEL": os.environ.get("AIDW_RESEARCH_PARALLEL", "(not set — using default)"),
            "AIDW_REVIEW_PARALLEL": os.environ.get("AIDW_REVIEW_PARALLEL", "(not set — using default)"),
            "AIDW_OLLAMA_ALLOW_REMOTE": os.environ.get("AIDW_OLLAMA_ALLOW_REMOTE", "(not set — remote blocked)"),
        },
    }
    print(json.dumps(config, indent=2))
    return 0


def cmd_ollama_stop(args: argparse.Namespace) -> int:
    endpoint = args.endpoint
    validate_ollama_endpoint(endpoint)
    success = stop_ollama_model(args.model, endpoint)
    print(json.dumps({"model": args.model, "stopped": success}, indent=2))
    return 0 if success else 1


def cmd_ollama_stop_all(args: argparse.Namespace) -> int:
    endpoint = args.endpoint
    validate_ollama_endpoint(endpoint)
    models_to_stop = [OLLAMA_MODEL_FAST, OLLAMA_MODEL_REVIEW, OLLAMA_MODEL_GENERATE]
    # Deduplicate while preserving order
    seen: set[str] = set()
    unique: list[str] = []
    for m in models_to_stop:
        if m not in seen:
            seen.add(m)
            unique.append(m)
    results: list[dict[str, Any]] = []
    all_stopped = True
    for model_name in unique:
        success = stop_ollama_model(model_name, endpoint)
        if not success:
            all_stopped = False
        results.append({"model": model_name, "stopped": success})
    print(json.dumps(results, indent=2))
    return 0 if all_stopped else 1


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
    p.set_defaults(func=cmd_set_stage)

    p = sub.add_parser("review-bundle", help="Build a review bundle from the current diff")
    p.add_argument("path")
    p.set_defaults(func=cmd_review_bundle)

    p = sub.add_parser("build-index", help="Build a lightweight repository structure index")
    p.add_argument("path")
    p.set_defaults(func=cmd_build_index)

    p = sub.add_parser("research-scan", help="Run index-first staged research scan")
    p.add_argument("path")
    p.add_argument("--goal", required=True, help="Research goal statement used for relevance ranking")
    p.set_defaults(func=cmd_research_scan)

    p = sub.add_parser("ollama-config", help="Show resolved Ollama model configuration")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_config)

    p = sub.add_parser("ollama-review", help="Run a single Ollama review/generate pass")
    p.add_argument("path")
    p.add_argument("--kind", required=True, choices=ALL_TASK_KINDS)
    p.add_argument("--model", default=None, help="Override model (default: auto-selected by kind)")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.add_argument("--no-stop", dest="no_stop", action="store_true",
                   help="Do not unload the model after the run (keeps it loaded in RAM)")
    p.add_argument("--no-auto-start", dest="no_auto_start", action="store_true",
                   help="Do not auto-start the Ollama server; fail if it is not already running")
    p.set_defaults(func=cmd_ollama_review)

    p = sub.add_parser("ollama-check", help="Check Ollama installation and all configured models")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_check)

    p = sub.add_parser("review-all", help="Run all Ollama review passes (sequential by default)")
    p.add_argument("path")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.add_argument("--parallel", type=int, choices=[1, 2], default=None,
                   help="Override review parallel workers (1=sequential, 2=bounded parallel)")
    p.add_argument("--no-stop", dest="no_stop", action="store_true",
                   help="Do not unload models after all passes complete")
    p.add_argument("--no-auto-start", dest="no_auto_start", action="store_true",
                   help="Do not auto-start the Ollama server; fail if it is not already running")
    p.set_defaults(func=cmd_review_all)

    p = sub.add_parser("ollama-stop", help="Stop a specific Ollama model to free RAM")
    p.add_argument("--model", required=True, help="Model name to unload")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_stop)

    p = sub.add_parser("ollama-stop-all", help="Stop all configured Ollama models to free RAM")
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_stop_all)

    p = sub.add_parser("synthesize-review", help="Merge review sources into review.md")
    p.add_argument("path")
    p.set_defaults(func=cmd_synthesize_review)

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
