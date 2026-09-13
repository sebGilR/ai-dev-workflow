## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK

Use the installed `/wip-*` skills as the default workflow for code tasks. The only exception is a repo that ships its own workflow (see below) — task size is not an exception: small, low-risk tasks use `/wip-auto`, not no tooling.

If a repository ships its own multi-step workflow (for example, a `.bmad/` setup or another project-managed orchestration), that repo's workflow owns the multi-step flow — planning, implementation, review, and PR — and `/wip-*` is reserved for ad-hoc, non-managed fixes only. On plain repositories with no such workflow installed, `/wip-*` remains the default for code tasks. When unsure, check whether the repo declares its own workflow before reaching for `/wip-*`.

Workflow conventions:

- Every repo uses a gitignored `.wip/` directory for branch-scoped workflow state.
- Branch directories use a **date-prefixed format**: `.wip/YYYYMMDDHHMMSS-<branch-slug>/` (e.g., `.wip/20260313150405-main/`). The branch name is slugified by the CLI, so special characters are replaced.
  - **Never create or reference `.wip/<branch>/` paths directly.** Always use `aidw start .` to initialize a branch session. The CLI resolves the correct directory; use `aidw upgrade .` or `aidw migrate-wip .` to rename legacy un-prefixed dirs.
- The canonical branch folder files are:
  - `plan.md`
  - `review.md`
  - `research.md`
  - `context.md`
  - `execution.md`
  - `pr.md`
  - `status.json`
- Prefer the `/wip-start`, `/wip-auto`, `/wip-plan`, `/wip-research`, `/wip-implement`, `/wip-fanout`, `/wip-review`, `/wip-fix-review`, `/wip-resume`, and `/wip-pr` skills.
- Use `/wip-fanout` (coordinator-only) to dispatch 2-3 independent, non-file-overlapping work items as parallel background agents instead of running them serially.
- Use `/wip-auto` for small, low-risk tasks (docs, boilerplate, isolated one-line fixes) — it runs Start → Plan → Implement in a single turn, so trivial tasks still get `.wip` tooling instead of being skipped.
- Keep `.wip` files updated as work progresses.
- Use the specialized `wip-planner`, `wip-researcher`, `wip-reviewer`, and `wip-tester` subagents when the task fits.
- Treat Claude as the final decision maker and final editor.

Default expectations:

- Start a task with `/wip-start` when the repo or branch has not been initialized yet. For a task small enough that a full plan/review/PR cycle is overkill, use `/wip-auto` instead of bypassing `.wip` tooling altogether.
- Use `/wip-resume` to continue after a context reset or a new session.
- Use `/wip-review` before `/wip-pr` for any non-trivial change.
- Keep updates concise and useful; do not spam `.wip` files with noise.
- Use `/wip-upgrade` when `.wip` files appear in the wrong directory, or after pulling updates to `ai-dev-workflow`.
- Proceed on a stated default rather than pausing to confirm it; list the assumptions you made at the end of your response instead of asking upfront.
- Only block and ask the user for irreversible or outward-facing actions (e.g. force-push, deleting data, sending a message, publishing something public).
- Try your own tools/credentials first — a local command, an available MCP tool, a config you can read yourself — before asking the user for information you could get directly.

## Repository Intelligence Tools

If `mcp__serena__*` tools respond, use them for code navigation instead of the `Explore` subagent. On any error, or if the tools are unavailable, fall back to Grep/Read/Glob immediately — don't retry Serena.

### When Serena is available, use it for

Use `mcp__serena__*` tools directly — not through a subagent — for any of these:

| Task | Tool |
|------|------|
| Find a class/function/method definition | `mcp__serena__find_symbol` with `name_path_pattern` |
| Find all usages/callers of a symbol | `mcp__serena__find_referencing_symbols` (needs `name_path` + `relative_path` from find_symbol first) |
| Understand a file's structure without reading it | `mcp__serena__get_symbols_overview` |
| Search for a symbol pattern across the codebase | `mcp__serena__search_for_pattern` |

### When NOT to use Serena (fall back to Grep/Read/Glob)

- Searching for **text content** in non-code files (JSON, YAML, config, markdown)
- The target is a text string, not a symbol name (e.g. a log message, a URL, a string literal)
- The file is very small (< 80 lines) and a direct Read is simpler
- Serena returns an error or no results — fall back to Grep then ranged Read

### Prohibited fallback

**Avoid `Agent(subagent_type="Explore")` for code navigation when Serena or Grep can answer instead.** The built-in Explore subagent reads entire files and consumes excessive tokens. Serena (when available) + Grep cover all legitimate navigation needs. (This does not apply to named workflow subagents like `wip-researcher`.)

### External libraries and APIs

When working with external libraries, frameworks, or APIs, use Context7 to retrieve
the latest documentation before generating code. This reduces outdated API usage.

## Token Compression (RTK)

RTK is an optional CLI proxy that compresses Bash tool call output (60-90% token reduction). When installed (`rtk init -g`), it transparently rewrites commands. Claude Code's built-in Read, Grep, and Glob tools are **not** affected.

### Selective RTK usage (high value)

RTK is most valuable when commands produce large outputs that would otherwise consume your context window:

| Situation | Recommended Commands |
|-----------|----------------------|
| **Deep Research** | `rtk ls -R`, `rtk grep`, `rtk find` |
| **History Review** | `rtk git log`, `rtk git show` |
| **Noisy Builds** | `rtk cargo build`, `rtk npm install`, `rtk go test ./...` |
| **Large Diffs** | `rtk git diff HEAD`, `rtk git diff main...` |

### When to bypass RTK (full output needed)

**In the main/coordinating session** (the one talking to the user): ask before bypassing RTK. If you need full uncompressed output to see complete stack traces or sequential log context, explain why and get confirmation:

> "I need full uncompressed output from `<cmd>` to [specific reason]. Run without RTK compression? [y/N]"

Only proceed with `rtk proxy <command>` after the user confirms.

**In a subagent** (no interactive user to ask): run `rtk proxy <cmd>` directly when uncompressed output is needed, and note in your final report that you bypassed RTK and why. Do not block waiting on a confirmation that can't arrive.

### Failure log convention

When a command fails during implementation and the agent needs to preserve the raw output for reference, dump it to the branch logs directory. Look up the actual wip path from `status.json` (`wip_dir` field — e.g., `.wip/20260322150405-my-feature/`), then:
```bash
mkdir -p <wip_dir>/logs
rtk proxy <failing-command> 2>&1 | tee <wip_dir>/logs/$(date -u +"%Y%m%d%H%M%S")-<cmd>.log
```
**Never use `.wip/<branch-name>/logs/`** — the CLI enforces date-prefixed slugs. Always derive the path from `status.json`. The `logs/` directory is cleaned up automatically by `/wip-cleanup`.

## END AI-DEV-WORKFLOW MANAGED BLOCK
