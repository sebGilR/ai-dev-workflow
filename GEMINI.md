# Global Gemini CLI Instructions

## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK

Use the workflow skills in `.github/skills/` as the default process for code tasks.

Workflow conventions:

- Every repo uses a gitignored `.wip/` directory for branch-scoped workflow state.
- Branch directories use a **date-prefixed format**: `.wip/YYYYMMDDHHMMSS-<branch-slug>/`.
  - **Never create or reference `.wip/<branch>/` paths directly.** Always use `aidw start .` to initialize a branch session. The CLI resolves the correct directory.
- The canonical branch folder files are: `plan.md`, `review.md`, `research.md`, `context.md`, `execution.md`, `pr.md`, `status.json`.
- When a task fits, refer to the skills in `.github/skills/` and execute the described `bash` commands.
- Use the specialized subagents in `.github/agents/` when appropriate.

## Repository Intelligence Tools

**IMPORTANT: Use Serena MCP tools directly for code navigation.**

| Task | Tool |
|------|------|
| Find a class/function/method definition | `mcp_serena_find_symbol` |
| Find all usages/callers of a symbol | `mcp_serena_find_referencing_symbols` |
| Understand a file's structure | `mcp_serena_get_symbols_overview` |
| Search for a symbol pattern | `mcp_serena_search_for_pattern` |

### External libraries and APIs

Use Context7 to retrieve the latest documentation before generating code.

## Token Compression (RTK)

RTK is an optional CLI proxy that compresses Bash tool call output. Use it for high-volume commands like `rtk grep`, `rtk git diff`, or noisy builds/tests.

## END AI-DEV-WORKFLOW MANAGED BLOCK
