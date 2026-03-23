## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK

Use the installed `/wip-*` skills as the default workflow for code tasks when appropriate.

Workflow conventions:

- Every repo uses a gitignored `.wip/` directory for branch-scoped workflow state.
- Branch directories use a **date-prefixed format**: `.wip/YYYYMMDD-<branch-slug>/` (e.g., `.wip/20260313-main/`). The branch name is slugified by the CLI, so special characters are replaced.
  - **Never create or reference `.wip/<branch>/` paths directly.** Always use `aidw start .` to initialize a branch session. The CLI resolves the correct directory; use `aidw upgrade .` or `aidw migrate-wip .` to rename legacy un-prefixed dirs.
- The canonical branch folder files are:
  - `plan.md`
  - `review.md`
  - `research.md`
  - `context.md`
  - `execution.md`
  - `pr.md`
  - `status.json`
- Prefer the `/wip-start`, `/wip-plan`, `/wip-research`, `/wip-implement`, `/wip-review`, `/wip-fix-review`, `/wip-resume`, and `/wip-pr` skills.
- Keep `.wip` files updated as work progresses.
- Use the specialized `wip-planner`, `wip-researcher`, `wip-reviewer`, and `wip-tester` subagents when the task fits.
- Treat Claude as the final decision maker and final editor.

Default expectations:

- Start a task with `/wip-start` when the repo or branch has not been initialized yet.
- Use `/wip-resume` to continue after a context reset or a new session.
- Use `/wip-review` before `/wip-pr` for any non-trivial change.
- Keep updates concise and useful; do not spam `.wip` files with noise.
- Use `/wip-upgrade` when `.wip` files appear in the wrong directory, or after pulling updates to `ai-dev-workflow`.

## Repository Intelligence Tools

**IMPORTANT: Do NOT use the `Explore` subagent for code navigation. Use Serena MCP tools directly.**

### When to use Serena (always try first)

Use `mcp__serena__*` tools directly — not through a subagent — for any of these:

| Task | Tool |
|------|------|
| Find a class/function/method definition | `mcp__serena__find_symbol` with `name_path_pattern` |
| Find all usages/callers of a symbol | `mcp__serena__find_referencing_symbols` (needs `name_path` + `relative_path` from find_symbol first) |
| Understand a file's structure without reading it | `mcp__serena__get_symbols_overview` |
| Search for a pattern across the codebase | `mcp__serena__search_for_pattern` |

### When NOT to use Serena (fall back to Grep/Read/Glob)

- Searching for **text content** in non-code files (JSON, YAML, config, markdown)
- The target is a text string, not a symbol name (e.g. a log message, a URL, a string literal)
- The file is very small (< 80 lines) and a direct Read is simpler
- Serena returns an error or no results — fall back to Grep then ranged Read

### Prohibited fallback

**Never use `Agent(subagent_type="Explore")` for code navigation.** The built-in Explore subagent reads entire files and consumes excessive tokens. Serena + Grep cover all legitimate navigation needs. (This does not apply to named workflow subagents like `wip-researcher`.)

### External libraries and APIs

When working with external libraries, frameworks, or APIs, use Context7 to retrieve
the latest documentation before generating code. This reduces outdated API usage.

## Token Compression (RTK)

RTK is an optional CLI proxy that compresses Bash tool call output before Claude processes it (60-90% token reduction). When installed (`rtk init -g`), it transparently rewrites Bash commands. Claude Code's built-in Read, Grep, and Glob tools are **not** affected.

### When to use RTK (high value)

| Situation | Commands |
|-----------|----------|
| Codebase exploration | `rtk ls`, `rtk grep`, `rtk find`, `rtk git log` |
| Build and test output | `rtk cargo build`, `rtk test`, `rtk lint`, `rtk tsc` |
| Review artifacts | `rtk git diff`, `rtk lint`, `rtk tsc` |
| PR workflow | `gh pr list`, `rtk git log` |

### When to bypass RTK (full output needed)

Use `rtk proxy <command>` for full raw output when:
- Debugging a failure that requires complete stack traces or sequential log context
- Reading `docker logs` or `kubectl logs` during a live debug session
- A command failed and the compressed output is insufficient to diagnose the cause

### Failure log convention

When a command fails during implementation and the agent needs to preserve the raw output for reference, dump it to the branch logs directory. Look up the actual wip path from `status.json` (`wip_dir` field — e.g., `.wip/20260322-my-feature/`), then:
```bash
mkdir -p <wip_dir>/logs
rtk proxy <failing-command> 2>&1 | tee <wip_dir>/logs/<timestamp>-<cmd>.log
```
**Never use `.wip/<branch-name>/logs/`** — the CLI enforces date-prefixed slugs. Always derive the path from `status.json`. The `logs/` directory is cleaned up automatically by `/wip-cleanup`.

## END AI-DEV-WORKFLOW MANAGED BLOCK
