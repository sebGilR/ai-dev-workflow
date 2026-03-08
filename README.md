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

Because skills and agents are symlinks (not copies), any edit you make to files in this repo is picked up immediately — no need to re-run the installer. Re-running install is only needed when you add a new skill or agent directory (to create its symlink), or if a symlink becomes stale.

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
~/.claude/ai-dev-workflow/bin/aidw verify
```

Or with workspace checking:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify --workspace /path/to/workspace
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

Action skills can be invoked explicitly with `/wip-*` slash commands, or Claude will infer the right skill to invoke based on your conversation context (e.g. saying "plan this out" or "let's implement").

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
~/.claude/ai-dev-workflow/bin/aidw bootstrap-workspace /path/to/workspace
~/.claude/ai-dev-workflow/bin/aidw ensure-repo /path/to/specific-repo
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
~/.claude/ai-dev-workflow/bin/aidw ollama-check
```

This checks: Ollama installed, service running, model pulled. Returns JSON with actionable guidance if anything is missing.

### Configuration

| Environment variable | Default | Purpose |
|---------------------|---------|---------|
| `AIDW_OLLAMA_MODEL` | `qwen2.5-coder:7b` | Fallback model when a task kind does not map to a per-role model |
| `AIDW_OLLAMA_MODEL_FAST` | `phi3:mini` | Model used for docs/summaries/synthesis tasks |
| `AIDW_OLLAMA_MODEL_REVIEW` | `qwen2.5-coder:7b` | Model used for review-oriented tasks |
| `AIDW_OLLAMA_MODEL_GENERATE` | `deepseek-coder:6.7b` | Model used for code-generation tasks |
| `AIDW_OLLAMA_ENDPOINT` | `http://localhost:11434` | Base Ollama API endpoint |
| `AIDW_OLLAMA_ALLOW_REMOTE` | unset | Set to `1` to allow non-localhost endpoints |

### Endpoint safety

By default, only local endpoints (`localhost`, `127.0.0.1`, `::1`) are accepted. Attempts to use a remote endpoint will fail with a clear error:

```
Ollama endpoint 'http://remote-host:11434' is not a local address.
Only localhost, 127.0.0.1, and ::1 are allowed by default.
Set AIDW_OLLAMA_ALLOW_REMOTE=1 to allow remote endpoints.
```

To allow a remote endpoint, set the env var explicitly:

```bash
AIDW_OLLAMA_ALLOW_REMOTE=1 ~/.claude/ai-dev-workflow/bin/aidw ollama-check --endpoint http://remote-host:11434
```
Override per-command:

```bash
~/.claude/ai-dev-workflow/bin/aidw ollama-review . --kind bug-risk --model qwen2.5-coder:7b
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
~/.claude/ai-dev-workflow/bin/aidw ollama-check

# Run a single review pass
~/.claude/ai-dev-workflow/bin/aidw ollama-review . --kind bug-risk

# Run all four review passes in sequence
~/.claude/ai-dev-workflow/bin/aidw review-all .

# Merge all review sources into review.md
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
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

## Review bundle behavior

Running `review-bundle` builds a JSON snapshot of the current branch state. It captures three diff sources:

| Key | Source | Purpose |
|-----|--------|---------|
| `branch_diff` | `git diff <merge-base>..HEAD` | Full set of committed changes on this branch vs `main`/`master` |
| `diff` | `git diff -- .` | Unstaged working-tree changes |
| `staged_diff` | `git diff --cached -- .` | Staged but uncommitted changes |

Each source includes metadata in `diff_sources`:

- `description` — the exact git command used
- `base` — the merge-base commit SHA (for `branch_diff`)
- `truncated` — `true` if the content was cut off
- `original_bytes` — original size before truncation

All diff sources are truncated at 50 KB to stay within model context limits. If truncated, `truncated: true` appears in the metadata so downstream tools know the content is incomplete.

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

The `aidw` CLI is available via `~/.claude/ai-dev-workflow/bin/aidw`. The underlying script is `~/.claude/ai-dev-workflow/scripts/aidw.py`.

| Command | Purpose |
|---------|---------|
| `bootstrap-workspace <path>` | Bootstrap all repos in a workspace |
| `ensure-repo <path>` | Bootstrap a single repo |
| `start <path> [--branch NAME]` | Initialize `.wip/<branch>/` |
| `status <path>` | Show current branch status |
| `set-stage <path> <stage>` | Update the workflow stage |
| `review-bundle <path>` | Build a review bundle from the current diff |
| `ollama-config [--endpoint E]` | Print resolved Ollama endpoint and model configuration |
| `ollama-check [--endpoint E]` | Check Ollama installation and all configured models |
| `ollama-review <path> --kind KIND [--model M] [--endpoint E] [--no-stop]` | Run a single Ollama pass (model auto-routed by kind) |
| `review-all <path> [--endpoint E] [--no-stop]` | Run all four review passes with per-kind model routing |
| `ollama-stop --model M [--endpoint E]` | Unload a specific model to free RAM |
| `ollama-stop-all [--endpoint E]` | Unload all configured models to free RAM |
| `synthesize-review <path>` | Merge review sources into `review.md` |
| `verify [--workspace PATH]` | Verify installation and configuration |

---

## Manual things you may still want to do

- Tune `~/.claude/settings.json` permission rules for your environment
- Fill in repo docs with real project knowledge
- Decide whether review findings should be fixed or deferred
- Pull Ollama models if not done during install: `ollama pull phi3:mini && ollama pull qwen2.5-coder:7b && ollama pull deepseek-coder:6.7b`
- Edit `~/.claude/ai-dev-workflow/aidw.env.sh` to override model defaults
- Adjust VS Code tasks if you have a complex existing `.vscode/tasks.json`

---

## Gotchas

### Existing `~/.claude/CLAUDE.md`
The installer updates a managed block inside your global `CLAUDE.md`. It does not wipe the rest of the file.

### Existing `~/.claude/settings.json`
The installer merges permission arrays and nested objects. Existing rules are preserved. Scalar values you have already set are **not overwritten** — only missing keys are added. If the file contains invalid JSON it is backed up (`.json.bak`) and rebuilt from the template.

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
Review passes have a 120-second timeout. Large diffs may exceed this. All diff sources are truncated to 50 KB each to mitigate this. The `branch_diff` field in the bundle always covers the full committed branch diff since the merge-base, not just working-tree changes.

### Branch directory naming
`.wip/` subdirectory names are derived from the branch name. Characters outside `a-z0-9-` are replaced with `-`, and a short SHA-256 hash suffix is appended when the slugified name would differ from the original (e.g. `feature/foo` → `feature-foo-<hash>`). This prevents collisions between branches that would otherwise resolve to the same slug.

---

## Uninstall

To remove the global install:

1. Delete the managed block from `~/.claude/CLAUDE.md`
2. Remove linked skills: `rm -rf ~/.claude/skills/wip-*`
3. Remove linked agents: `rm -f ~/.claude/agents/wip-*`
4. Remove the symlink: `rm ~/.claude/ai-dev-workflow`
5. Optionally remove permission entries from `~/.claude/settings.json`

The repo-local `.wip/` and `.claude/repo-docs/` folders can be removed manually if no longer wanted.
