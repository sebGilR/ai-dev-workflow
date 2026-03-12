# GitHub Copilot Instructions

This repository uses the `ai-dev-workflow` model with branch-scoped WIP files.

## Workflow Defaults

- Use `.wip/<branch>/` for planning, research, execution notes, and reviews.
- Use the committed skills in `.github/skills/` and agents in `.github/agents/` for Copilot lifecycle actions.
  - These are seeded from shared source files in `claude/skills/` and `claude/agents/` during repo bootstrap.
  - The `claude/` source is canonical; `.github/` copies are committed for Copilot's use.
- Keep edits minimal, non-destructive, and aligned with existing repository conventions.
- Keep bootstrap surfaces synchronized (`README.md`, `install.sh`, `cmd/aidw/`).

## WIP Rules

- Keep WIP files uncommitted unless explicitly requested.
- Treat `plan.md` as source of truth for implementation order.
- Append updates to `execution.md` after each implementation chunk.
- Run verification commands before concluding implementation.
