# ai-dev-workflow

A portable Claude + GitHub Copilot workflow kit for VS Code workspaces.

This repo installs a native Claude Code workflow into `~/.claude`, links shared skills into `~/.copilot/skills`, keeps your personal working state in a gitignored `.wip/<branch>/` folder inside each repo, and bootstraps every repo it finds in the current VS Code workspace.

It preserves your conventions across Claude and Copilot:

- `.wip/<branch>/...`
- `/wip-*` skill names
- `plan.md`, `review.md`, `research.md`, `context.md`
- `execution.md`, `pr.md`, `status.json`
- global `CLAUDE.md` workflow guidance
- repo-level `.github/copilot-instructions.md`, `.github/skills/`, `.github/agents/`
- global gitignore for personal workflow files

It also adds:

- reusable Claude subagents (`wip-planner`, `wip-researcher`, `wip-reviewer`, `wip-tester`)
- a portable installer that symlinks skills and agents into `~/.claude`
- workspace scanning and per-repo bootstrapping
- starter permission rules in `~/.claude/settings.json`
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
| `~/.claude/statusline.sh` | Claude statusline renderer (session token summary) |
| `~/.claude/save-wip-snapshot.sh` | Snapshot helper used by Claude hooks |

Claude Code picks up skills and agents natively because they live in the standard user-level directories.

Because skills and agents are symlinks (not copies), any edit you make to files in this repo is picked up immediately — no need to re-run the installer. Re-running install is only needed when you add a new skill or agent directory (to create its symlink), or if a symlink becomes stale.

Managed scripts such as `~/.claude/statusline.sh` follow the same rule only when they are installed as symlinks. If one of those files already exists as an unmanaged local file, the installer will not overwrite it; move or rename it and re-run `install.sh` if you want the repo version to take over.

### In `~/.copilot`

The installer creates or updates:

| Path | Purpose |
|------|---------|
| `~/.copilot/skills/wip-*` | Personal Copilot skill links (shared from this repo) |

Repo-level Copilot agents and instructions are managed in each workspace repo under `.github/` (see below).

### In each repo detected in the workspace

| Path | Purpose |
|------|---------|
| `.wip/` | Branch-scoped workflow state (gitignored globally) |
| `.claude/repo-docs/` | Lightweight project knowledge files (gitignored globally) |
| `.github/copilot-instructions.md` | Repo-level Copilot coding instructions |
| `.github/skills/` | Repo-level Copilot skill definitions |
| `.github/agents/` | Repo-level Copilot custom agents |
| `.vscode/tasks.json` | Convenience tasks merged in |

Repo docs and Copilot assets are seeded from templates/sources when missing. Existing files are never overwritten.

### In global gitignore

The installer ensures your global gitignore contains:

```gitignore
.wip/
.claude/repo-docs/
.claude/settings.local.json
CLAUDE.local.md
```

This keeps the workflow personal and portable without editing tracked repo `.gitignore` files.

During installation you'll also be prompted how to handle the files seeded per-repo — `.github/copilot-instructions.md`, `.github/skills/`, `.github/agents/`, `.claude/repo-docs/`, and `.claude/settings.local.json` — add them to the local `.git/info/exclude` (default), to your global gitignore, or skip (if you want to commit them for GitHub Copilot or shared team context).

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

### 4. Vendor-Agnostic Memory Layer

The workflow includes a persistent memory layer (`aidw memory`) that stores facts and provides semantic search over project documentation. This layer is designed to be **vendor-agnostic**, working identically across Claude, Gemini, Copilot, and Codex.

To support any LLM provider and authentication method (including OAuth), semantic search delegates embedding generation to a customizable bridge script:

- `~/.claude/get-embeddings.sh`

By default, this script is configured for **Gemini (Vertex AI via gcloud OAuth)**. You can easily modify it to use OpenAI, Anthropic, or any local embedding model. The script simply needs to read text from STDIN and output a JSON array of floats to STDOUT.

### 5. Recommended usage flow

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
| `/wip-review` | Build review bundle and consolidate findings into `review.md` |
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

## WIP file persistence and verification

WIP markdown files are the source of truth for stage transitions. The CLI verifies required files before advancing workflow stages and uses atomic writes for generated artifacts.

### Verification commands

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-plan .
~/.claude/ai-dev-workflow/bin/aidw verify-research .
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
| Go 1.21+ | Required to build the `aidw` binary on Linux or non-arm64 macOS. Pre-built `darwin-arm64` binary included. Install from https://go.dev/dl/ |

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
references in `cmd/aidw/internal/install/merge_mcp_json.go` and re-run the installer.

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

The `aidw` CLI is available via `~/.claude/ai-dev-workflow/bin/aidw`. The source is in `cmd/aidw/`.

| Command | Purpose |
|---------|---------|
| `bootstrap-workspace <path>` | Bootstrap all repos in a workspace |
| `ensure-repo <path>` | Bootstrap a single repo |
| `start <path> [--branch NAME]` | Initialize `.wip/<branch>/` |
| `status <path>` | Show current branch status |
| `set-stage <path> <stage>` | Update the workflow stage |
| `review-bundle <path>` | Build a review bundle from the current diff |
| `gemini-review <path>` | Run adversarial Gemini review pass (requires `AIDW_GEMINI_REVIEW=1`) |
| `synthesize-review <path>` | Merge review sources into `review.md` |
| `verify-plan <path>` | Verify `plan.md` exists and has content |
| `verify-research <path>` | Verify `research.md` exists and has content |
| `verify-review <path>` | Verify `review.md` exists and has content |
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
- Edit `~/.claude/ai-dev-workflow/aidw.env.sh` to override settings defaults (e.g. Gemini settings)
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

### Branch directory naming
`.wip/` subdirectory names follow the format `YYYYMMDD-<slug>`, where the date is the day the directory was first created and the slug is derived from the branch name. Alphanumeric characters (including uppercase), `-`, `_`, and `.` are preserved; all other characters are replaced with `-`. A short SHA-256 hash suffix is appended when the slugified name would differ from the original (e.g. `feature/foo` → `20260311-feature-foo-<hash>`). This prevents collisions between branches that would otherwise resolve to the same slug, and allows `ls .wip/` to show branches in chronological creation order.

When resolving the WIP directory for a branch, `aidw` uses a three-phase lookup: (1) find an existing `YYYYMMDD-<slug>` directory, (2) fall back to a legacy unprefixed `<slug>` directory, (3) create a new `YYYYMMDD-<slug>` directory using today's date. This means existing unprefixed directories continue to work without any migration.

---

## Requirements

| Requirement | Notes |
|-------------|-------|
| uv / uvx | Required for Serena. Install: `curl -LsSf https://astral.sh/uv/install.sh \| sh` |
| Node.js / npx | Required for Context7. Install from https://nodejs.org |
| Claude Code MCP support | Both servers run as MCP processes |
| Go 1.21+ | Required to build the `aidw` binary. |

---

## Uninstall

To remove the global install:

1. Delete the managed block from `~/.claude/CLAUDE.md`
2. Remove linked skills: `rm -rf ~/.claude/skills/wip-*`
3. Remove linked agents: `rm -f ~/.claude/agents/wip-*`
4. Remove the symlink: `rm ~/.claude/ai-dev-workflow`
5. Optionally remove permission entries from `~/.claude/settings.json`

The repo-local `.wip/` and `.claude/repo-docs/` folders can be removed manually if no longer wanted.
