#!/usr/bin/env python3
from __future__ import annotations
import argparse
import json
import os
import shutil
import subprocess
import sys
import textwrap
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

REPO_DOCS = ["architecture.md", "patterns.md", "commands.md", "testing.md", "gotchas.md"]
WIP_FILES = ["plan.md", "review.md", "research.md", "context.md", "execution.md", "pr.md"]
STAGES = {"started", "planned", "researched", "implementing", "reviewed", "review-fixed", "pr-prep"}
REVIEW_KINDS = ["bug-risk", "missing-tests", "regression-risk", "docs-needed"]

DEFAULT_OLLAMA_MODEL = os.environ.get("AIDW_OLLAMA_MODEL", "qwen2.5-coder:14b")
DEFAULT_OLLAMA_ENDPOINT = os.environ.get("AIDW_OLLAMA_ENDPOINT", "http://localhost:11434")


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
    return "".join(allowed).strip("-") or "unknown-branch"


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
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def read_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


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
    return status


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


def review_bundle(repo: Path) -> dict[str, Any]:
    repo = git_toplevel(repo)
    diff = run(["git", "-C", str(repo), "diff", "--", "."], check=False).stdout
    staged_diff = run(["git", "-C", str(repo), "diff", "--cached", "--", "."], check=False).stdout
    status = run(["git", "-C", str(repo), "status", "--short"], check=False).stdout
    changed_files = [line[3:] for line in status.splitlines() if len(line) > 3]

    bundle = {
        "repo": repo.name,
        "repo_path": str(repo),
        "branch": current_branch(repo),
        "generated_at": now_iso(),
        "changed_files": changed_files,
        "status": status,
        "diff": diff[:50000],
        "staged_diff": staged_diff[:50000],
    }

    state = ensure_branch_state(repo)
    out_path = Path(state["wip_dir"]) / "review-bundle.json"
    write_json(out_path, bundle)
    return bundle


# ---------------------------------------------------------------------------
# Ollama helpers
# ---------------------------------------------------------------------------

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


def ollama_review(repo: Path, kind: str, model: str, endpoint: str) -> dict[str, Any]:
    repo = git_toplevel(repo)
    bundle = review_bundle(repo)
    prompt = {
        "bug-risk": "Review this diff for likely correctness issues, hidden bugs, and risky assumptions.",
        "missing-tests": "Review this diff and identify missing tests or validation coverage gaps.",
        "regression-risk": "Review this diff for likely regressions, affected nearby code paths, and brittle changes.",
        "docs-needed": "Review this diff and determine what documentation, runbooks, or notes should be updated.",
    }.get(kind)
    if not prompt:
        raise SystemExit(f"Unsupported review kind: {kind}")

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

    sections.append("## Claude Review\n")
    sections.append("<!-- Claude should add its own review findings here -->\n")

    review_path = wip_dir / "review.md"
    review_path.write_text("\n".join(sections) + "\n", encoding="utf-8")

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
        running = ollama_is_running(DEFAULT_OLLAMA_ENDPOINT)
        check("ollama: service running", running, warn=not running)
        if running:
            has_model = ollama_has_model(DEFAULT_OLLAMA_MODEL, DEFAULT_OLLAMA_ENDPOINT)
            check(f"ollama: model {DEFAULT_OLLAMA_MODEL}", has_model, warn=not has_model)

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


def cmd_review_bundle(args: argparse.Namespace) -> int:
    bundle = review_bundle(Path(args.path))
    print(json.dumps(bundle, indent=2))
    return 0


def cmd_ollama_review(args: argparse.Namespace) -> int:
    parsed = ollama_review(Path(args.path), args.kind, args.model, args.endpoint)
    print(json.dumps(parsed, indent=2))
    return 0


def cmd_ollama_check(args: argparse.Namespace) -> int:
    endpoint = args.endpoint
    model = args.model
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
    results["requested_model"] = model
    results["model_available"] = ollama_has_model(model, endpoint)

    if not results["model_available"]:
        results["ok"] = False
        results["message"] = f"Model '{model}' is not pulled locally."
        results["pull_help"] = f"Run: ollama pull {model}"
        if results["available_models"]:
            results["hint"] = f"Available models: {', '.join(results['available_models'][:10])}"
        print(json.dumps(results, indent=2))
        return 1

    results["ok"] = True
    results["message"] = f"Ollama is ready with model '{model}' at {endpoint}."
    print(json.dumps(results, indent=2))
    return 0


def cmd_review_all(args: argparse.Namespace) -> int:
    repo = Path(args.path)
    model = args.model
    endpoint = args.endpoint

    # Check Ollama readiness first
    if not ollama_is_installed():
        raise SystemExit("Ollama is not installed. Run: aidw ollama-check")
    if not ollama_is_running(endpoint):
        raise SystemExit(f"Ollama is not running at {endpoint}. Run: ollama serve")
    if not ollama_has_model(model, endpoint):
        raise SystemExit(f"Model '{model}' not available. Run: ollama pull {model}")

    results: list[dict[str, Any]] = []
    for kind in REVIEW_KINDS:
        print(f"Running {kind} review pass...", file=sys.stderr)
        try:
            parsed = ollama_review(repo, kind, model, endpoint)
            findings_count = len(parsed.get("findings", []))
            print(f"  {kind}: {findings_count} finding(s)", file=sys.stderr)
            results.append(parsed)
        except SystemExit as exc:
            print(f"  {kind}: failed ({exc})", file=sys.stderr)
            results.append({"kind": kind, "summary": f"Failed: {exc}", "findings": []})

    print(json.dumps(results, indent=2))
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
    p.set_defaults(func=cmd_set_stage)

    p = sub.add_parser("review-bundle", help="Build a review bundle from the current diff")
    p.add_argument("path")
    p.set_defaults(func=cmd_review_bundle)

    p = sub.add_parser("ollama-review", help="Run a single Ollama review pass")
    p.add_argument("path")
    p.add_argument("--kind", required=True, choices=REVIEW_KINDS)
    p.add_argument("--model", default=DEFAULT_OLLAMA_MODEL)
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_review)

    p = sub.add_parser("ollama-check", help="Check Ollama installation and model availability")
    p.add_argument("--model", default=DEFAULT_OLLAMA_MODEL)
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_ollama_check)

    p = sub.add_parser("review-all", help="Run all Ollama review passes in sequence")
    p.add_argument("path")
    p.add_argument("--model", default=DEFAULT_OLLAMA_MODEL)
    p.add_argument("--endpoint", default=DEFAULT_OLLAMA_ENDPOINT)
    p.set_defaults(func=cmd_review_all)

    p = sub.add_parser("synthesize-review", help="Merge review sources into review.md")
    p.add_argument("path")
    p.set_defaults(func=cmd_synthesize_review)

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
