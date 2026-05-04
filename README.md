# ai-dev-workflow

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/sebGilR/ai-dev-workflow)](https://goreportcard.com/report/github.com/sebGilR/ai-dev-workflow)
[![Homebrew](https://img.shields.io/badge/homebrew-tap-orange.svg)](https://github.com/sebGilR/homebrew-tap)

A spec-driven, file-backed development workflow that wraps **Claude Code**, **GitHub Copilot**, **Gemini**, and **Codex** in a deterministic state machine. The `aidw` Go CLI manages branch-scoped artifacts on disk; a set of Markdown "skills" and subagents drive the model through `Plan → Spec → Skeptic → Implement → Review → PR` against those artifacts.

The core idea: the LLM is treated as an unreliable executor. Workflow integrity is enforced by the CLI (file existence, stage transitions, policy checks), not by trusting the model to follow instructions.

---

## Install

```bash
brew tap sebGilR/homebrew-tap
brew install aidw

aidw bootstrap --setup-shell --interactive .
exec $SHELL
```

`bootstrap` is idempotent: it creates `~/.claude/`, extracts the embedded skills/agents, downloads `sqlite-vec`, deep-merges JSON into `~/.claude/{settings,mcp}.json`, patches `CLAUDE.md` / `GEMINI.md` / your shell profile via managed sentinels, and seeds `.wip/` + `.github/{skills,agents,copilot-instructions.md}` in the repo. See [Bootstrap mechanics](#bootstrap-mechanics) for the full breakdown.

### Hacking on skills and agents

Clone the repo and re-bootstrap with `--source-path` to symlink `claude/skills/*` and `claude/agents/*.md` from the checkout — edits go live without a rebuild:

```bash
git clone https://github.com/sebGilR/ai-dev-workflow.git
cd ai-dev-workflow
make install              # builds bin/aidw-<os>-<arch> and copies to ~/.claude/ai-dev-workflow/bin/
./bin/aidw-darwin-arm64 bootstrap --setup-shell --source-path . ~/some/repo
```

---

## Quick start

In Claude Code (or Copilot CLI, Gemini CLI, Codex — same skill files):

```
/wip-start         # seed .wip/<dated-slug>/, refresh repo-docs index
/wip-plan          # Analyst → Planner → Skeptic; produces task-context.md + spec.md
/wip-implement     # iterate spec.md tasks with policy-gated Bash
/wip-review        # diff bundle → Claude review → optional adversarial pass (gemini/copilot/codex)
/wip-pr            # draft pr.md, open the PR
```

After a `/clear` or compaction:

```
/wip-resume        # reads context-summary.md + handoff.md, picks up at the current stage
```

For low-risk work (docs, boilerplate, isolated refactors), one shot:

```
/wip-auto          # autonomous Start → Plan → Implement loop, gated by .aidw/policy.json
```

Useful CLI invocations from your shell:

```bash
aidw status .                              # current stage + recommended next action
aidw next .                                # JSON: {stage, action, command}
aidw memory search . "auth middleware"     # semantic recall against indexed repo-docs
aidw policy check . "go test ./..."        # allow | prompt | deny
aidw review-bundle .                       # rebuild diff bundle (sha256-cached)
```

The full skill catalogue is in [Skill catalogue](#skill-catalogue); the full CLI surface is in [CLI reference](#cli-reference-sketched).

---

## What this actually is

Three things, glued together:

1. **`aidw`** — a single static Go binary (`cmd/aidw`, ~5 MB, no CGO). Manages `.wip/`, runs an embedded SQLite + `sqlite-vec` memory store, evaluates a per-repo command policy, builds review bundles from git diffs, and shells out to external review CLIs.
2. **`claude/skills/` and `claude/agents/`** — Markdown skill files (`/wip-start`, `/wip-plan`, `/wip-implement`, …) and subagent definitions, embedded into the binary via `//go:embed` (`embed.go`) and extracted to `~/.claude/skills/`, `~/.copilot/skills/`, `~/.claude/agents/`. Skills are scripts; the model executes the steps verbatim.
3. **A bootstrap layer** that idempotently merges templates into `~/.claude/CLAUDE.md`, `~/.claude/settings.json`, `~/.claude/mcp.json`, `~/.gemini/GEMINI.md`, your shell profile, and the repo's `.github/copilot-instructions.md`.

---

## Why a Go binary instead of bash/Python

The original installer was a 22 KB bash script (`install.sh`, still in the tree as a fallback). It was rewritten in Go because:

- **One artifact, no runtime deps.** GoReleaser cross-compiles for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`. Skills, agents, templates, and the `serena-query` helper are all embedded — see `embed.go`.
- **CGO disabled.** Uses `modernc.org/sqlite` (pure-Go SQLite driver) so the binary stays portable. The `sqlite-vec` extension is loaded at runtime as a `.dylib`/`.so` from `~/.claude/lib/` (downloaded by `bootstrap`, see `cmd/aidw/internal/install/sqlite_vec.go`).
- **Determinism.** Stage transitions, file verification, JSON merging, and atomic writes need to be reliable across `bash`, `zsh`, macOS, and Linux. Go gives test coverage (`*_test.go`) and structured error handling that bash didn't.

---

## The `.wip/` state machine

Every branch you touch gets a directory:

```
.wip/<YYYYMMDDHHMMSS>-<branch-slug>/
├── status.json          # Stage, timestamps, completed steps
├── context.md           # Repo + branch metadata (seeded by start)
├── task-context.md      # Distilled intent (Analyst output)
├── plan.md              # High-level plan (Planner output)
├── spec.md              # Hardened, task-numbered spec (final contract)
├── research.md          # Findings (Serena/Context7/grep)
├── execution.md         # Append-only log written by the Implementer
├── review.md            # Synthesized review (Claude + Adversarial)
├── pr.md                # PR draft
├── review-bundle.json   # Fingerprinted diff bundle (cached by sha256)
└── handoff.md           # Latest snapshot from Stop/PreCompact hooks
```

The directory name is **content-addressable by date+branch**, computed by `cmd/aidw/internal/wip/wip.go` and `slug.SafeSlug`. Slug collisions across `feature/foo` and `feature-foo` are disambiguated with an 8-char SHA256 suffix. Resolution order:

1. Newest dated dir matching the current branch slug
2. Legacy un-prefixed dir (kept for back-compat; migrated by `aidw migrate-wip`)
3. Create a new `YYYYMMDDHHMMSS-<slug>` dir

Stage transitions are gated. Calling `aidw set-stage . spec-reviewed` fails unless `spec.md`, `task-context.md`, and `review.md` all exist with non-trivial content (`wip.VerifyWipFile` checks ≥10 chars). `--skip-verification` exists, but skills don't use it — that's the point.

Valid stages: `started → specified → spec-reviewed → implementing → reviewed → review-fixed → pr-prep` (plus legacy `planned`, `researched`).

---

## How skills drive the model

A skill is a Markdown file with a YAML frontmatter and a numbered procedure. Example, `claude/skills/wip-plan/SKILL.md`:

```yaml
---
name: wip-plan
description: 'Create or refresh the implementation specification for the current branch using a deterministic 3-step sequence: Clarify, Draft, and Skeptic Review.'
context: fork
agent: analyst
---
```

The body is a literal procedure: "Use the `wip-analyst` subagent → produce `task-context.md` → run `aidw verify-wip-file . task-context.md` → switch to `wip-planner` → write `spec.md` → verify → switch to `wip-skeptic` → review → `aidw set-stage . spec-reviewed` → HALT."

Each step is either a Bash invocation of `aidw` (verifiable) or a subagent switch (Claude's built-in mechanism). The CLI is the source of truth for "did this step actually happen". When the model lies about completing a step, the next `aidw verify-*` call fails and the skill aborts.

---

## Subagents

Defined in `claude/agents/*.md`, each with constrained tool access (`mcp__serena__*` only, `permissionMode: plan`, etc.).

| Agent           | Role                                                           | Writes code? |
| --------------- | -------------------------------------------------------------- | ------------ |
| `wip-analyst`   | Distills user intent + repo docs into `task-context.md`        | No           |
| `wip-planner`   | Drafts `spec.md` with file paths and Given/When/Then ACs       | No           |
| `wip-skeptic`   | Adversarial review of the spec — risks, blast radius, history  | No           |
| `wip-researcher`| Targeted code discovery, populates `research.md`               | No           |
| `wip-reviewer`  | Independent diff review (uses Serena to trace callers)         | No           |
| `wip-tester`    | Test coverage and regression analysis                          | No           |

Implementation is intentionally not delegated to a "wip-implementer" agent — it runs in the main session so the user sees every Edit/Write call.

---

## Persistent memory (sqlite-vec)

`~/.claude/memory.db` is a SQLite database with two surfaces:

- **Facts**: keyed `(repo_path, branch, key) → value` for branch-scoped notes. Set with `aidw memory store . <key> <value>`. The `wip-planner` records architectural decisions here so they survive `/clear`.
- **Documents**: a flat `items(repo_path, file_path, content)` table indexed for semantic search.

When `sqlite-vec v0.1.9` is loadable (`vec_version()` succeeds), two virtual tables are created:

```sql
CREATE VIRTUAL TABLE vec_facts USING vec0(id INTEGER PRIMARY KEY, embedding FLOAT[768]);
CREATE VIRTUAL TABLE vec_items USING vec0(id INTEGER PRIMARY KEY, embedding FLOAT[768]);
```

Embeddings are 768-dim, matching Google's `text-embedding-004`/`text-embedding-006`. Generation is delegated to `~/.claude/get-embeddings.sh`, a bridge script that reads text on stdin and emits `[float, float, …]` on stdout. The default template hits Vertex AI via `gcloud auth print-access-token`; replace it with OpenAI, Cohere, or any local model that returns 768-dim floats.

`aidw memory search . "<query>" [--global]` returns top-5 nearest neighbours by L2 distance. `wip-planner` and `wip-skeptic` call this before drafting/reviewing specs.

If `vec0.{dylib,so}` is missing or the load fails, fact storage still works — only semantic search is disabled (`db.VectorEnabled() == false`).

---

## The policy engine

`.aidw/policy.json` lets you precompute "should the agent run this command without asking?". Rules are regexes evaluated in order:

```json
{
  "rules": [
    {"pattern": "^git (status|branch|diff|log|rev-parse|show)", "verdict": "allow"},
    {"pattern": "^(go|npm|cargo|pip) (test|build|check|lint)",  "verdict": "allow"},
    {"pattern": "^(rm|sudo|curl|wget|gcloud|aws|ssh|scp)",       "verdict": "prompt"}
  ]
}
```

Verdicts: `allow`, `prompt`, `audit`, `deny`. Default-fallback is `prompt`.

`/wip-implement` and `/wip-auto` invoke `aidw policy check . "<cmd>"` before every Bash call. On `prompt`, the user is offered `Allow Once / Allow Always / Deny`. `Allow Always` calls `aidw policy allow . "<cmd>" --reason "..."`, which prepends a quoted-regex `^<cmd>` rule to `policy.json`.

This is a soft layer — Claude Code's own permission system still governs execution. Policy provides a second, repo-local check that makes autonomous loops (`/wip-auto`) safer without manually pre-approving every test command.

---

## Adversarial review

`aidw adversarial-review .` (and the deprecated `aidw gemini-review`) builds the diff bundle, writes it to a temp file, and execs the configured CLI. Provider is selectable:

```bash
export AIDW_ADVERSARIAL_REVIEW=1
export AIDW_ADVERSARIAL_PROVIDER=gemini   # gemini | copilot | codex
export AIDW_ADVERSARIAL_MODEL=gemini-2.5-pro
export AIDW_ADVERSARIAL_TIMEOUT=120
```

If the provider's CLI isn't installed, the command exits with `status: not_installed` and the skill skips the section instead of failing. Findings are merged back into `review.md` under `## Adversarial Review` by `aidw synthesize-review`.

The review bundle (`review-bundle.json`) caps total diff at 50 KB and is fingerprinted with `sha256(branch_diff || working_diff || staged_diff || git_status)` so re-running `aidw review-bundle` is a no-op when nothing has changed.

---

## Bootstrap mechanics

`aidw bootstrap [--setup-shell] [--interactive] [--source-path PATH] [REPO_PATH]` is idempotent. It:

1. Creates `~/.claude/`, `~/.copilot/`, `~/.gemini/`.
2. **Skills/agents**:
   - Without `--source-path`: extracts the embedded `claude/skills` and `claude/agents` from the binary into `~/.claude/skills/`, `~/.copilot/skills/`, and `~/.claude/agents/`.
   - With `--source-path`: symlinks them from your local checkout for live editing (use this when developing the project).
3. Downloads `sqlite-vec` `v0.1.9` for the host triple (`darwin-aarch64`, `linux-x86_64`, …) and unpacks `vec0.{dylib,so}` to `~/.claude/lib/`.
4. **Merges JSON**: `templates/global/settings.template.json` is deep-merged into `~/.claude/settings.json` with these rules (`cmd/aidw/internal/install/merge_settings.go`):
   - Objects: recursive merge.
   - Arrays: union, deduped by JSON serialisation of each element.
   - Scalars: **user value wins** — the template never overwrites your existing scalars.
   - Invalid existing JSON is backed up to `settings.json.bak` and the template alone is written.
5. **Merges MCP servers** into `~/.claude/mcp.json`. Adds (or refreshes if `command`/`args` drift): `serena`, `context7`, `sequential-thinking`. User-added fields on existing entries are preserved.
6. **Patches `CLAUDE.md` and `GEMINI.md`** with `BEGIN/END … MANAGED BLOCK` sentinels, so re-runs replace only the managed region without disturbing your additions.
7. **Patches your shell profile** (`.zshrc` / `.bashrc` / `.bash_profile`) with `source ~/.claude/ai-dev-workflow/aidw.env.sh`, also wrapped in a managed block.
8. **Repo bootstrap** (when a `[REPO_PATH]` arg is given): creates `.wip/`, `.claude/repo-docs/`, seeds `.github/copilot-instructions.md`, `.github/skills/`, `.github/agents/` (with MCP-only sections stripped — see `cmd/aidw/internal/install/generate_github_agents.go`), and adds `.wip/`, `.claude/repo-docs/`, `.claude/settings.local.json` to `~/.config/git/ignore`.

Re-run `aidw upgrade .` after pulling new versions; it's the same path with extra `migrate-wip` and skill-refresh logic.

---

## Hooks (Claude Code)

Three hook entries land in `~/.claude/settings.json`:

```json
"hooks": {
  "Stop":        [{"matcher": "*", "hooks": [{"command": "~/.claude/save-wip-snapshot.sh stop"}]}],
  "PreCompact":  [{"matcher": "*", "hooks": [{"command": "~/.claude/save-wip-snapshot.sh precompact"}]}],
  "SessionEnd":  [{"matcher": "*", "hooks": [{"command": "~/.claude/save-wip-snapshot.sh sessionend"}]}]
}
```

`save-wip-snapshot.sh` resolves the current `.wip/<dated-slug>/` (mirroring the Go resolution logic so they can't drift) and writes `handoff.md` containing `git status --short --branch`, the last 8 commits, changed files, and `git diff --stat`. A line is appended to `progress.log` for every fire.

The point: when context is compacted or a session ends, the next `/wip-resume` reads `handoff.md` and `context-summary.md` instead of replaying the entire chat.

---

## Skill catalogue

| Skill                  | Effect                                                                  |
| ---------------------- | ----------------------------------------------------------------------- |
| `/wip-start`           | `aidw start .` → seed branch dir; trigger `/wip-document-project` if `.claude/repo-docs/` is empty; refresh the semantic index. |
| `/wip-plan`            | Analyst → Planner → Skeptic. Produces `task-context.md`, `spec.md`. Stage → `spec-reviewed`. |
| `/wip-research`        | Targeted research pass into `research.md` (Serena + Context7 + grep).   |
| `/wip-implement`       | Loads `spec.md` only; iterates tasks; checks each Bash command via `aidw policy check`. Writes `execution.md`. |
| `/wip-auto`            | Autonomous Start → Plan → Implement loop for low-risk tasks (`aidw task next/done` drives the loop). |
| `/wip-review`          | `aidw review-bundle` → `aidw synthesize-review` → `wip-reviewer` agent → optional adversarial pass. |
| `/wip-fix-review`      | Walks blockers from `review.md`, applies fixes, re-verifies.            |
| `/wip-pr`              | Drafts `pr.md` from the WIP history; runs `gh pr create` against it.    |
| `/wip-resume`          | Reads `context-summary.md` (or falls back to individual files) to continue without rewinding. |
| `/wip-sync`            | Manual snapshot — same writer as the hooks.                              |
| `/wip-document-project`| Deep-research pass that populates `.claude/repo-docs/` for first-time use. |
| `/wip-cleanup`         | Delete all-but-canonical files in the current branch dir.                |
| `/wip-clear`           | Drop all `.wip/<…>` dirs except the most-recently-dated one.             |
| `/wip-upgrade`         | Re-apply bootstrap; migrate legacy `.wip/<branch>` dirs to the dated format. |
| `/wip-install` / `/wip-setup-brew` | Verify install / migrate from `install.sh` to the Homebrew binary. |

---

## CLI reference (sketched)

```
aidw bootstrap [path]                    # global install + optional repo seed
aidw bootstrap-workspace <path>          # bootstrap every git repo under a workspace
aidw upgrade [path]                      # re-apply, migrate

aidw start <path>                        # ensure .wip/<dated-slug>/ for current branch
aidw status <path>                       # human summary + recommended next step
aidw next <path>                         # JSON: stage → action → command
aidw set-stage <path> <stage>            # gated transition
aidw verify-{plan,spec,research,review,task-context} <path>
aidw verify-wip-file <path> <filename>

aidw task next <path>                    # next un-done task from spec.md
aidw task done <path> <id> <description>

aidw review-bundle <path>                # build diff bundle (cached by fingerprint)
aidw synthesize-review <path>            # merge sources into review.md
aidw adversarial-review <path>           # exec gemini/copilot/codex CLI

aidw policy {init,check,allow} <path> [args]
aidw memory  {status,store,list,index,search} [args]
aidw model   route {frontier|efficient}

aidw migrate-wip <path>                  # YYYYMMDDHHMMSS-rename legacy dirs
aidw cleanup-branch <path>               # keep canonical files in current branch
aidw clear-wip <path>                    # keep only newest dated dir
aidw summarize-context <path>            # write context-summary.md
aidw context-summary <path>              # print to stdout
```

All commands print structured JSON to stdout and human messages to stderr, so they compose with `jq`.

---

## Configuration

Edit `~/.claude/ai-dev-workflow/aidw.env.sh` (sourced by your shell profile):

```bash
# Adversarial review
export AIDW_ADVERSARIAL_REVIEW=1
export AIDW_ADVERSARIAL_PROVIDER=gemini   # gemini | copilot | codex
export AIDW_ADVERSARIAL_MODEL=gemini-2.5-pro
export AIDW_ADVERSARIAL_TIMEOUT=120

# Model routing tiers (used by /wip-review escalation prompts, etc.)
export AIDW_FRONTIER_MODEL=claude-opus-4-7
export AIDW_EFFICIENT_MODEL=claude-haiku-4-5

# Embedding override (skips ~/.claude/get-embeddings.sh discovery)
export AIDW_EMBEDDING_CMD='openai-embed-stdin --model text-embedding-3-small'
export AIDW_EMBEDDING_MODEL=text-embedding-006
```

The repo-local policy file lives at `<repo>/.aidw/policy.json`. The MCP config and skills/agents are global (`~/.claude/`).

---

## Repo layout

```
cmd/aidw/                     # Go CLI entrypoint
  cmd/                        # Cobra commands (bootstrap, start, memory, policy, review, …)
  internal/
    wip/                      # .wip/<dated-slug>/ resolution, stage machine, verification
    install/                  # bootstrap, JSON merge, sqlite-vec download, shell patch, gh agent gen
    memory/                   # SQLite + sqlite-vec, embeddings bridge
    policy/                   # Regex policy engine
    review/                   # Diff bundle, synthesize, adversarial
    git/                      # Toplevel, current branch, merge-base
    slug/                     # Branch-name → safe slug + collision suffix
    util/                     # Atomic writes, JSON I/O, CopyFS, SafeLink

claude/
  skills/                     # /wip-* skill markdown — embedded into the binary
  agents/                     # Subagent definitions — embedded into the binary

templates/
  global/                     # CLAUDE.md, GEMINI.md, settings.template.json, hooks, scripts
  github/                     # copilot-instructions.md template
  vscode/                     # Workspace-level recommendations
  repo-docs/                  # repo-docs scaffolding

bin/                          # Pre-built per-arch binaries (used by make install)
embed.go                      # //go:embed templates claude/{skills,agents} bin/serena-query
.goreleaser.yaml              # Cross-compile + Homebrew tap + checksum
install.sh                    # Legacy bash installer (still works as a fallback)
```

---

## How it composes with the wider toolchain

- **Serena** (LSP-backed semantic search) is the preferred navigation tool. The agent definitions hard-code `mcp__serena__*` permissions; `claude/agents/wip-planner.md` enforces `find_symbol → find_referencing_symbols` order. A `bin/serena-query` shim is embedded for hosts without MCP.
- **Context7** is auto-registered to fetch versioned library docs at generation time, reducing stale-API hallucinations.
- **RTK** (optional, [Rust Token Killer](https://github.com/.../rtk)) compresses Bash output 60–90%. When installed (`brew install rtk && rtk init -g`), Claude Code's hook transparently rewrites `git log` → `rtk git log`, etc. Skills know to `rtk proxy <cmd>` only after asking the user, when full output is required.
- **GitHub Copilot** uses the same skill files via `~/.copilot/skills/`, plus the seeded `.github/copilot-instructions.md`. Subagent files are re-emitted into `.github/agents/` with MCP-only sections stripped (Copilot can't talk to Serena).
- **Gemini / Codex** use `GEMINI.md` (global + per-repo) and can be the adversarial-review provider.

---

## Design notes

- **Idempotency over diff-tracking.** Every install/upgrade step is safe to re-run. JSON merges preserve user data; managed Markdown blocks are sentinel-replaced; symlinks are SafeLinked (existing non-symlink files cause an error rather than silent overwrite).
- **Atomic writes.** `util.AtomicWrite` writes via tmpfile + rename. `status.json` updates can't tear.
- **Fail closed on stage transitions.** A skill that lies about completing a step gets caught at the next `aidw verify-*`. The model can't bluff its way past gating.
- **Context decay.** Each stage-transition optionally triggers `aidw summarize-context`, which produces a compact `context-summary.md` (< 2 KB typical) used by `/wip-resume` after compaction.
- **Cross-host portability.** Skills are plain Markdown invoking Bash. Anything with `bash` and `aidw` on PATH can drive them — Claude Code, Copilot CLI, Codex CLI, even a shell loop.

---

## Contributing

```bash
git clone https://github.com/sebGilR/ai-dev-workflow.git
cd ai-dev-workflow
make build && make test
```

Tests live alongside the packages they exercise (`*_test.go` in `cmd/aidw/internal/install/`, `cmd/aidw/internal/wip/`, `cmd/aidw/internal/review/`). Use `/wip-start` against this repo itself when working on a feature — the workflow dogfoods.

---

## License

MIT. See [LICENSE](LICENSE).
