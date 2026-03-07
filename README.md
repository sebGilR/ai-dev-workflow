# ai-dev-workflow

A portable Claude Code workflow kit for VS Code workspaces.

This repo installs a native Claude Code workflow into `~/.claude`, keeps your personal working state in a gitignored `.wip/<branch>/` folder inside each repo, and bootstraps every repo it finds in the current VS Code workspace.

It preserves your conventions:

- `.wip/<branch>/...`
- `/wip-*` skill names
- `plan.md`, `review.md`, `research.md`, `context.md`
- `execution.md`, `pr.md`, `status.json`
- global `CLAUDE.md` workflow guidance
- global gitignore for personal workflow files

It also adds:

- reusable Claude subagents (`wip-planner`, `wip-researcher`, `wip-reviewer`, `wip-tester`)
- a portable installer that symlinks skills and agents into `~/.claude`
- workspace scanning and per-repo bootstrapping
- starter permission rules in `~/.claude/settings.json`
- optional local Ollama review passes with structured JSON output
- a CLI (`aidw`) for automation and verification

---

## What gets installed

### In `~/.claude`

The installer creates or updates:

| Path | Purpose |
|------|---------|
| `~/.claude/skills/wip-*` | Skill directories (symlinked from this repo) |
| `~/.claude/agents/wip-*` | Subagent definitions (symlinked from this repo) |
| `~/.claude/settings.json` | Merged permission rules |
| `~/.claude/CLAUDE.md` | Managed workflow block (non-destructive merge) |
| `~/.claude/ai-dev-workflow` | Symlink pointing to this repo |

Claude Code picks up skills and agents natively because they live in the standard user-level directories.

### In each repo detected in the workspace

| Path | Purpose |
|------|---------|
| `.wip/` | Branch-scoped workflow state (gitignored globally) |
| `.claude/repo-docs/` | Lightweight project knowledge files (gitignored globally) |
| `.vscode/tasks.json` | Convenience tasks merged in |

The repo docs are seeded from templates when missing. Existing files are never overwritten.

### In global gitignore

The installer ensures your global gitignore contains:

```gitignore
.wip/
.claude/repo-docs/
.claude/settings.local.json
CLAUDE.local.md
```

This keeps the workflow personal and portable without editing tracked repo `.gitignore` files.

---

## Quick start

### 1. Clone the repo

```bash
git clone <your-fork-or-local-path> ai-dev-workflow
```

### 2. Run the installer

From a VS Code workspace root (single repo or parent of multiple repos):

```bash
./ai-dev-workflow/install.sh
```

Or with an explicit workspace path:

```bash
./ai-dev-workflow/install.sh /path/to/workspace
```

### 3. Verify the installation

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py verify
```

Or with workspace checking:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py verify --workspace /path/to/workspace
```

### 4. Start using the workflow

In Claude Code inside any bootstrapped repo:

```
/wip-start
/wip-plan
/wip-research
/wip-implement
/wip-review
/wip-fix-review
/wip-pr
```

To continue after a break:

```
/wip-resume
```

---

## Recommended usage flow

### Happy path

1. Checkout or create your ticket branch.
2. Open Claude Code in the repo.
3. `/wip-start` - Initialize `.wip/<branch>/`.
4. `/wip-plan` - Create an implementation plan.
5. `/wip-research` - Gather codebase findings.
6. `/wip-implement` - Build the next chunk of work.
7. `/wip-review` - Run review passes and consolidate findings.
8. `/wip-fix-review` - Address review issues.
9. `/wip-pr` - Draft PR content.

### Resume flow

1. Open the repo.
2. `/wip-resume`
3. Claude reads `.wip/<branch>/status.json` and the relevant notes.
4. Continue from the correct stage.

---

## What each skill does

| Skill | Purpose |
|-------|---------|
| `/wip-install` | Verify installation and explain how to bootstrap |
| `/wip-start` | Create `.wip/<branch>/`, seed files, initialize `status.json` |
| `/wip-plan` | Use `wip-planner` subagent to create/refresh `plan.md` |
| `/wip-research` | Use `wip-researcher` subagent to populate `research.md` and `context.md` |
| `/wip-implement` | Implement next chunk, update `execution.md` |
| `/wip-review` | Build review bundle, run Ollama passes if available, consolidate into `review.md` |
| `/wip-fix-review` | Fix review issues, update `execution.md` |
| `/wip-resume` | Summarize current state and continue from the right stage |
| `/wip-pr` | Draft PR content into `pr.md` |

All action skills have `disable-model-invocation: true`, meaning Claude will not auto-invoke them. You invoke them explicitly with `/wip-*`.

---

## Subagents

These are defined in `claude/agents/` and installed as user-level subagents in `~/.claude/agents/`.

| Agent | Model | Permission Mode | Purpose |
|-------|-------|-----------------|---------|
| `wip-planner` | inherit | plan (read-only) | Inspect repo and propose implementation sequence |
| `wip-researcher` | inherit | plan (read-only) | Find relevant files, patterns, and evidence |
| `wip-reviewer` | inherit | plan (read-only) | Review diff, consolidate findings, prioritize issues |
| `wip-tester` | inherit | default | Run tests, identify gaps, interpret failures |

---

## Single-repo VS Code workspace

If your workspace is a single repo:

```bash
cd my-repo
../ai-dev-workflow/install.sh .
```

The installer detects the repo at the workspace root and bootstraps it.

---

## Multi-repo VS Code workspace

If your workspace is a parent directory containing multiple repos:

```
workspace/
  ai-dev-workflow/
  my-app/
  my-api/
  my-lib/
```

From `workspace/`:

```bash
./ai-dev-workflow/install.sh .
```

The installer scans immediate children and bootstraps every directory that is a git repo. Non-repo directories are skipped.

---

## Workspace detection rules

The installer scans:

- the workspace root itself (if it's a git repo)
- each immediate child directory (if it's a git repo)

It deduplicates by resolved git toplevel, so nested repos or symlinks won't cause double-bootstrapping.

For unusual workspace layouts, use the CLI directly:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py bootstrap-workspace /path/to/workspace
python ~/.claude/ai-dev-workflow/scripts/aidw.py ensure-repo /path/to/specific-repo
```

---

## Permissions model

The installer merges starter permission rules into `~/.claude/settings.json`.

### Allowed by default

Safe, repetitive operations:

- `git status`, `git diff *`, `git log *`
- `ls *`, `find *`, `rg *`, `fd *`
- Common test/lint commands (`npm run test *`, `pytest *`, etc.)
- Reading and editing `.wip/**` files

### Ask first

State-changing operations:

- `git commit *`, `git checkout *`, `git switch *`
- Package installs (`npm install *`, `yarn add *`, `bundle add *`)
- Database migrations

### Denied

Sensitive reads and network operations:

- `.env` files, `secrets/`, `~/.aws/credentials`
- `curl *`, `wget *`

Tune these for your stack and risk tolerance. The merge is additive; existing rules are preserved.

---

## Ollama integration

Ollama provides optional local review passes. When available, `/wip-review` automatically runs structured review passes against your current diff using a local model.

### Setup

#### 1. Install Ollama

| Platform | Command |
|----------|---------|
| macOS | `brew install ollama` or download from [ollama.com/download](https://ollama.com/download) |
| Linux | `curl -fsSL https://ollama.com/install.sh \| sh` |
| Windows | Download from [ollama.com/download](https://ollama.com/download) |

#### 2. Start the service

```bash
ollama serve
```

On macOS, the Ollama app starts the service automatically.

#### 3. Pull a model

The default model is `qwen2.5-coder:14b`. Pull it:

```bash
ollama pull qwen2.5-coder:14b
```

Alternative models that work well for code review:

| Model | Size | Notes |
|-------|------|-------|
| `qwen2.5-coder:14b` | ~9 GB | Default. Strong code understanding. |
| `qwen2.5-coder:7b` | ~4.7 GB | Lighter, faster, still good for review. |
| `codellama:13b` | ~7.4 GB | Meta's code model. |
| `deepseek-coder-v2:16b` | ~8.9 GB | Strong at code analysis. |

#### 4. Verify readiness

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py ollama-check
```

This checks: Ollama installed, service running, model pulled. Returns JSON with actionable guidance if anything is missing.

### Configuration

| Environment variable | Default | Purpose |
|---------------------|---------|---------|
| `AIDW_OLLAMA_MODEL` | `qwen2.5-coder:14b` | Model to use for review passes |
| `AIDW_OLLAMA_ENDPOINT` | `http://localhost:11434` | Ollama API endpoint |

Override per-command:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py ollama-review . --kind bug-risk --model qwen2.5-coder:7b
```

### Review passes

Four structured review passes are available:

| Kind | What it checks |
|------|---------------|
| `bug-risk` | Correctness issues, hidden bugs, risky assumptions |
| `missing-tests` | Missing tests, validation coverage gaps |
| `regression-risk` | Regressions, affected nearby code paths, brittle changes |
| `docs-needed` | Documentation, runbooks, or notes that should be updated |

### CLI commands

```bash
# Check Ollama readiness
python ~/.claude/ai-dev-workflow/scripts/aidw.py ollama-check

# Run a single review pass
python ~/.claude/ai-dev-workflow/scripts/aidw.py ollama-review . --kind bug-risk

# Run all four review passes in sequence
python ~/.claude/ai-dev-workflow/scripts/aidw.py review-all .

# Merge all review sources into review.md
python ~/.claude/ai-dev-workflow/scripts/aidw.py synthesize-review .
```

Each review pass outputs structured JSON and saves results to `.wip/<branch>/ollama-review-<kind>.json`.

### How /wip-review uses Ollama

When you run `/wip-review`:

1. It builds a review bundle (diff + changed files).
2. It checks if Ollama is available locally.
3. If available, it runs all four review passes.
4. The `wip-reviewer` subagent reads both the diff and the Ollama results.
5. It consolidates everything into a prioritized `review.md`.

If Ollama is not available, the review still works. Claude does the review entirely on its own.

---

## VS Code integration

The installer merges convenience tasks into `.vscode/tasks.json`:

- `AI Dev Workflow: Start`
- `AI Dev Workflow: Resume`
- `AI Dev Workflow: Review`
- `AI Dev Workflow: PR Draft`

These are convenience shortcuts. The real workflow lives in skills and scripts.

---

## CLI reference

The `aidw` CLI is available at `~/.claude/ai-dev-workflow/scripts/aidw.py` or via the wrapper at `~/.claude/ai-dev-workflow/bin/aidw`.

| Command | Purpose |
|---------|---------|
| `bootstrap-workspace <path>` | Bootstrap all repos in a workspace |
| `ensure-repo <path>` | Bootstrap a single repo |
| `start <path> [--branch NAME]` | Initialize `.wip/<branch>/` |
| `status <path>` | Show current branch status |
| `set-stage <path> <stage>` | Update the workflow stage |
| `review-bundle <path>` | Build a review bundle from the current diff |
| `ollama-check [--model M] [--endpoint E]` | Check Ollama readiness |
| `ollama-review <path> --kind KIND [--model M] [--endpoint E]` | Run a single Ollama review pass |
| `review-all <path> [--model M] [--endpoint E]` | Run all Ollama review passes |
| `synthesize-review <path>` | Merge review sources into `review.md` |
| `verify [--workspace PATH]` | Verify installation and configuration |

---

## Manual things you may still want to do

- Tune `~/.claude/settings.json` permission rules for your environment
- Fill in repo docs with real project knowledge
- Decide whether review findings should be fixed or deferred
- Install and choose your preferred Ollama model
- Adjust VS Code tasks if you have a complex existing `.vscode/tasks.json`

---

## Gotchas

### Existing `~/.claude/CLAUDE.md`
The installer updates a managed block inside your global `CLAUDE.md`. It does not wipe the rest of the file.

### Existing `~/.claude/settings.json`
The installer merges permission arrays. Existing rules are preserved.

### Existing `.vscode/tasks.json`
Tasks are merged by label. Existing tasks with the same label are not overwritten.

### Existing `.claude/repo-docs/`
Missing files are created. Existing files are left alone.

### Existing `.wip/<branch>/`
Existing files are not overwritten by `/wip-start`. Only missing files are seeded.

### Skills not showing in Claude Code
Skills are loaded at session start. If you install while a session is running, restart the session or run `/agents` to refresh.

### Global gitignore not working
The installer sets `core.excludesfile` in your global git config. If you use a non-standard gitignore path, verify with `git config --global core.excludesfile`.

### Ollama review timeout
Review passes have a 120-second timeout. Large diffs may exceed this. The diff is truncated to 50KB to mitigate this.

---

## Uninstall

To remove the global install:

1. Delete the managed block from `~/.claude/CLAUDE.md`
2. Remove linked skills: `rm -rf ~/.claude/skills/wip-*`
3. Remove linked agents: `rm -f ~/.claude/agents/wip-*`
4. Remove the symlink: `rm ~/.claude/ai-dev-workflow`
5. Optionally remove permission entries from `~/.claude/settings.json`

The repo-local `.wip/` and `.claude/repo-docs/` folders can be removed manually if no longer wanted.
