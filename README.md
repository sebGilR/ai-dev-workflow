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
| `~/.claude/statusline.sh` | Claude statusline renderer (usage summary + session token summary) |
| `~/.claude/claude-watch.sh` | Daily token watcher (20s loop, threshold warnings at 75/85/90%) |
| `~/.claude/start-claude-watch.sh` | SessionStart hook — launches watcher and tracks per-session reference count |
| `~/.claude/claude-fetch-usage.sh` | OAuth usage poller — fetches real utilization % from Anthropic API (macOS) |
| `~/.claude/save-wip-snapshot.sh` | Snapshot helper used by watcher and Claude hooks |

Claude Code picks up skills and agents natively because they live in the standard user-level directories.

Because skills and agents are symlinks (not copies), any edit you make to files in this repo is picked up immediately — no need to re-run the installer. Re-running install is only needed when you add a new skill or agent directory (to create its symlink), or if a symlink becomes stale.

Managed scripts such as `~/.claude/statusline.sh` and `~/.claude/claude-watch.sh` follow the same rule only when they are installed as symlinks. If one of those files already exists as an unmanaged local file, the installer will not overwrite it; move or rename it and re-run `install.sh` if you want the repo version to take over.

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

### 5. Optional: token monitor + autosave

Run this in a second terminal while working:

```bash
~/.claude/claude-watch.sh
```

On macOS with an active Claude Code session, `claude-watch.sh` automatically delegates to `claude-fetch-usage.sh` to fetch real utilization % from the Anthropic OAuth API. No extra setup needed — it happens transparently as long as you are logged in to Claude Code. On non-macOS systems or if the token is unavailable, the watcher falls back to a transcript-based estimate.

Defaults:

- Daily limit: `200000` tokens
- Warn at `75%`
- Autosave snapshot at `85%` (`warn`)
- Final autosave snapshot at `90%` (`critical`)

What this currently powers:

- `~/.claude/claude-fetch-usage.sh` (macOS) calls the Anthropic OAuth API and writes `~/.claude/usage-status.json` with real utilization %
- `~/.claude/claude-watch.sh` delegates to the fetcher, falls back to transcript-based estimate on non-macOS or auth failure, and fires threshold warnings at 75/85/90%
- `~/.claude/statusline.sh` reads the cache and shows Claude account usage plus session token percentage

Statusline shape when OAuth data is available (macOS):

```text
Claude usage: 17% | 7d 72% | resets 2026-03-09T02:00:00+00:00
Session: 38%
```

Statusline shape when falling back to transcript estimate:

```text
Claude usage: 142k / 200k today | 71% | source transcript-estimate
Session: 38%
```

If the watcher is not running or the cache file is missing, the statusline falls back to:

```text
Claude usage: unavailable
Session: 38%
```

Note: the OAuth endpoint used by `claude-fetch-usage.sh` is unofficial and undocumented. It relies on the macOS Keychain for the OAuth token and may change without notice.

Snapshots are written in the active repo under `.wip/<branch>/` and include:

- `research.md`
- `plan.md`
- `handoff.md`
- `progress.log`

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

#### 3. Pull recommended models

Default routing uses three models (16 GB-safe baseline):

```bash
ollama pull phi3:mini
ollama pull qwen2.5-coder:7b
ollama pull deepseek-coder:6.7b
```

Alternative models that work well for code review:

| Model | Size | Notes |
|-------|------|-------|
| `qwen2.5-coder:14b` | ~9 GB | Optional heavier review model on larger-memory machines. |
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
| `AIDW_OLLAMA_MAX_PARALLEL` | `2` | Global hard limit for parallel Ollama work |
| `AIDW_RESEARCH_PARALLEL` | `1` | Research lane concurrency request (kept sequential by default) |
| `AIDW_REVIEW_PARALLEL` | `1` | Review pass concurrency request (sequential default, max 2) |
| `AIDW_OLLAMA_ALLOW_REMOTE` | unset | Set to `1` to allow non-localhost endpoints |
| `AIDW_OLLAMA_TIMEOUT` | `180` | Generic HTTP timeout fallback (seconds) |
| `AIDW_OLLAMA_TIMEOUT_FAST` | `120` | Timeout for fast tasks (docs-needed, summaries, synthesis) |
| `AIDW_OLLAMA_TIMEOUT_REVIEW` | `300` | Timeout for review passes (bug-risk, missing-tests, regression-risk) |
| `AIDW_OLLAMA_TIMEOUT_GENERATE` | `240` | Timeout for code-generation tasks |

### 16 GB tuning guidance

Use these defaults first and only raise to `2` after confirming stable local runs:

```bash
export AIDW_OLLAMA_MAX_PARALLEL=2
export AIDW_RESEARCH_PARALLEL=1
export AIDW_REVIEW_PARALLEL=1
```

If you increase research/review parallelism to `2`, keep `AIDW_OLLAMA_MAX_PARALLEL=2` to avoid overcommitting memory.

### Staged v2 research flow

v2 adds an index-first narrowing workflow to reduce prompt size and improve file targeting:

```bash
# Build/rebuild repo structure index
~/.claude/ai-dev-workflow/bin/aidw build-index .

# Produce branch-local narrowed research map
~/.claude/ai-dev-workflow/bin/aidw research-scan . --goal "your implementation goal"
```

Artifacts:
- `.wip/repo-index.json` (repo-level index)
- `.wip/<branch>/research-scan.json` (branch-local narrowed relevance map)

### WIP file persistence and verification

WIP markdown files are treated as the source of truth for stage transitions. The CLI now verifies required files before advancing workflow stages and uses atomic writes for generated artifacts.

Verification commands:

```bash
# Verify branch plan exists and has content
~/.claude/ai-dev-workflow/bin/aidw verify-plan .

# Verify branch research exists and has content
~/.claude/ai-dev-workflow/bin/aidw verify-research .

# Verify branch review exists and has content
~/.claude/ai-dev-workflow/bin/aidw verify-review .
```

Stage gating behavior:
- `set-stage planned` requires `plan.md`
- `set-stage researched` requires `research.md`
- `set-stage reviewed` requires `review.md`
- emergency bypass is available with `--skip-verification`

Atomic write guarantees:
- generated markdown/json artifacts are written via temp file + replace
- partial writes are avoided on interruptions
- `.tmp` files are replaced in-place and should not remain after successful command completion

### Multi-pass review pipeline

The review pipeline is now split into four passes so Claude performs an independent code review instead of only summarizing model output.

Passes:
- Pass A: Ollama broad review (`bug-risk`, `missing-tests`, `regression-risk`, `docs-needed`)
- Pass B: Claude gap analysis via `review-gaps`
- Pass C: Optional targeted Ollama follow-up via `review-targeted`
- Pass D: Claude independent final review over the actual diff

Artifacts:
- `review-bundle.json`
- `ollama-review-<kind>.json`
- `review-gaps.json`
- `ollama-review-targeted.json` (when targeted pass runs)

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

# Run all four review passes (sequential by default)
~/.claude/ai-dev-workflow/bin/aidw review-all .

# Optional bounded parallel review (max 2)
~/.claude/ai-dev-workflow/bin/aidw review-all . --parallel 2

# Analyze review coverage gaps to guide Claude follow-up
~/.claude/ai-dev-workflow/bin/aidw review-gaps .

# Run targeted follow-up review on specific files/focus area
~/.claude/ai-dev-workflow/bin/aidw review-targeted . --files scripts/aidw.py,tests/test_aidw.py --focus error-handling

# Verify branch WIP files before stage transitions
~/.claude/ai-dev-workflow/bin/aidw verify-plan .
~/.claude/ai-dev-workflow/bin/aidw verify-research .
~/.claude/ai-dev-workflow/bin/aidw verify-review .

# Merge all review sources into review.md
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

Each review pass outputs structured JSON and saves results to `.wip/<branch>/ollama-review-<kind>.json`. In parallel mode, failures in a batch trigger automatic fallback to sequential execution for the remaining passes.

If a review pass times out, a structured result is written to the artifact file instead of crashing the run:

```json
{
  "kind": "bug-risk",
  "status": "timeout",
  "timeout_seconds": 300,
  "summary": "The bug-risk review pass timed out after 300s.",
  "findings": []
}
```

On the next run, if a completed (non-timeout) artifact already exists for a pass, that artifact is reused and the HTTP call is skipped. This prevents re-running work that Ollama already completed. A `status: "timeout"` artifact is never reused — the pass is always retried.

### How /wip-review uses Ollama

When you run `/wip-review`:

1. It builds a review bundle (diff + changed files).
2. It checks if Ollama is available locally.
3. If available, it runs all four review passes.
4. The `wip-reviewer` subagent reads both the diff and the Ollama results.
5. It consolidates everything into a prioritized `review.md`.

If Ollama is not available, the review still works. Claude does the review entirely on its own.

---

## Repository intelligence tools

Two MCP servers extend Claude's ability to understand your code and external libraries.

### Serena — semantic code navigation

Serena gives Claude semantic understanding of your repository. Instead of text search,
Claude can look up symbol definitions, trace call chains, and discover relationships
between files and modules.

What it improves:
- finding relevant files during research (faster, more precise than grep)
- understanding class hierarchies and module dependencies
- tracing which code calls a given function
- discovering existing patterns before writing new code

### Context7 — up-to-date library documentation

Context7 retrieves current documentation for external libraries directly from source.
This reduces hallucinated API usage — especially for fast-moving libraries.

What it improves:
- writing integration code against third-party APIs
- configuring frameworks with current syntax
- avoiding usage of deprecated methods

### Requirements

| Requirement | Notes |
|-------------|-------|
| uv / uvx | Required for Serena. Install: `curl -LsSf https://astral.sh/uv/install.sh \| sh` |
| Node.js / npx | Required for Context7. Install from https://nodejs.org |
| Claude Code MCP support | Both servers run as MCP processes |

### Configuration

The installer configures both servers in `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "serena": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/oraios/serena@v0.1.4", "serena", "start-mcp-server"]
    },
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp@2.1.3"]
    }
  }
}
```

Both servers are pinned to specific audited versions. To upgrade, update the version
references in `scripts/merge_mcp_json.py` and re-run the installer.

If `uvx` or `npx` is not installed, the installer prints instructions instead of
writing the config. Re-run `install.sh` after installing the missing tools.

### Verify

```bash
~/.claude/ai-dev-workflow/bin/aidw verify
```

Look for `mcp:` checks in the output. Warnings indicate missing `uvx`/`npx` or an
unconfigured server.

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
| `build-index <path>` | Build/update `.wip/repo-index.json` with lightweight file structure pointers |
| `research-scan <path> --goal TEXT` | Generate `.wip/<branch>/research-scan.json` from index-first relevance scoring |
| `ollama-config [--endpoint E]` | Print resolved Ollama endpoint and model configuration |
| `ollama-check [--endpoint E]` | Check Ollama installation and all configured models |
| `ollama-review <path> --kind KIND [--model M] [--endpoint E] [--no-stop]` | Run a single Ollama pass (model auto-routed by kind) |
| `review-all <path> [--endpoint E] [--parallel {1,2}] [--no-stop]` | Run all review passes with bounded optional parallelism and fallback safeguards |
| `ollama-stop --model M [--endpoint E]` | Unload a specific model to free RAM |
| `ollama-stop-all [--endpoint E]` | Unload all configured models to free RAM |
| `synthesize-review <path>` | Merge review sources into `review.md` |
| `summarize-context <path>` | Generate `context-summary.md` from all WIP files |
| `context-summary <path>` | Print `context-summary.md` to stdout |
| `verify [--workspace PATH]` | Verify installation and configuration |

---

## Context distillation

As a branch progresses, `.wip/<branch>/` accumulates multiple files that Claude must read to get oriented. `context-summary.md` reduces this cost to a single compact file.

### Generating the summary

```bash
~/.claude/ai-dev-workflow/bin/aidw summarize-context .
```

This reads `plan.md`, `research.md`, `execution.md`, `review.md`, `pr.md`, `context.md`, and `status.json`, then writes a structured `context-summary.md` (target size: < 2 KB) to the branch WIP directory.

### Viewing the summary

```bash
~/.claude/ai-dev-workflow/bin/aidw context-summary .
```

Prints the summary to stdout. Exits non-zero if no summary exists yet.

### Automatic regeneration

`context-summary.md` is automatically regenerated when:

- `aidw set-stage` advances the workflow stage
- `aidw synthesize-review` merges review sources

The auto-regen only fires if `context-summary.md` already exists — fresh branches are unaffected.

### How Claude uses it

When `/wip-resume` is invoked, Claude checks for `context-summary.md` first. If it exists, Claude reads only that file instead of the four individual WIP files. The full documents remain available as a fallback when deeper context is needed.

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
Review passes use per-task-kind timeouts (default: 300s for review passes, 120s for fast tasks). Large diffs may take longer — all diff sources are truncated to 50 KB each to mitigate this. The `branch_diff` field in the bundle always covers the full committed branch diff since the merge-base, not just working-tree changes.

If a pass times out, the run continues with the remaining passes. A structured `status: "timeout"` artifact is written so the result is recorded cleanly. You can increase timeouts via environment variables:

```bash
export AIDW_OLLAMA_TIMEOUT_REVIEW=600   # 10 minutes for review passes on large diffs
export AIDW_OLLAMA_TIMEOUT_FAST=180     # 3 minutes for fast tasks
```

### Branch directory naming
`.wip/` subdirectory names are derived from the branch name. Characters outside `a-z0-9-` are replaced with `-`, and a short SHA-256 hash suffix is appended when the slugified name would differ from the original (e.g. `feature/foo` → `feature-foo-<hash>`). This prevents collisions between branches that would otherwise resolve to the same slug.

---

## Known limitations and future improvements

### Concurrent multi-repo sessions and `.active-repo`

`save-wip-snapshot.sh` uses `~/.claude/.active-repo` as a fallback when `git rev-parse` fails to detect the current repo from the hook's working directory. This file is a single global pointer written on every session start and updated on every successful git resolution.

With two concurrent Claude sessions open in **different repositories**, the pointer races continuously — whichever session most recently triggered a hook owns the file. An emergency autosave fired from session A may read session B's repo path and append the warning to the wrong `context.md`. A stderr warning is emitted whenever the fallback fires.

**For single-session use (the common case) this works correctly.** If you routinely work with multiple concurrent Claude sessions in different repos, avoid relying on the emergency autosave and prefer explicit `/wip-sync` before context resets.

### Streaming support
Review passes currently wait for Ollama to return the full response before writing results. Adding `stream: true` to the `/api/chat` request would let passes succeed incrementally, reducing sensitivity to timeouts on slow models or large diffs. This requires parsing the NDJSON response stream and reassembling the structured output.

### Review chunking for large diffs
All diff sources are already truncated to 50 KB each, but very large changes may still stress a single review pass. A future improvement would split the diff into segments and run passes on each chunk, then merge and deduplicate findings. This is complementary to streaming and would further reduce timeout exposure on large PRs.

---

## Uninstall

To remove the global install:

1. Delete the managed block from `~/.claude/CLAUDE.md`
2. Remove linked skills: `rm -rf ~/.claude/skills/wip-*`
3. Remove linked agents: `rm -f ~/.claude/agents/wip-*`
4. Remove the symlink: `rm ~/.claude/ai-dev-workflow`
5. Optionally remove permission entries from `~/.claude/settings.json`

The repo-local `.wip/` and `.claude/repo-docs/` folders can be removed manually if no longer wanted.
