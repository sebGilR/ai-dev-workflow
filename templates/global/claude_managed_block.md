## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK

Use the installed `/wip-*` skills as the default workflow for code tasks when appropriate.

Workflow conventions:

- Every repo uses a gitignored `.wip/<branch>/` directory for branch-scoped workflow state.
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
- Treat local Ollama review helpers as secondary review passes, not the primary coder.

Default expectations:

- Start a task with `/wip-start` when the repo or branch has not been initialized yet.
- Use `/wip-resume` to continue after a context reset or a new session.
- Use `/wip-review` before `/wip-pr` for any non-trivial change.
- Keep updates concise and useful; do not spam `.wip` files with noise.

## Repository Intelligence Tools

Always prefer Serena for repository exploration and code navigation:
- symbol lookup, dependency tracing, call chain analysis
- file discovery, semantic code search, class/module relationships

When working with external libraries, frameworks, or APIs, use Context7 to retrieve
the latest documentation before generating code. This reduces outdated API usage.

## END AI-DEV-WORKFLOW MANAGED BLOCK
