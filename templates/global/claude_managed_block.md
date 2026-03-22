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

## END AI-DEV-WORKFLOW MANAGED BLOCK
