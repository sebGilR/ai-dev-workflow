---
name: wip-plan
description: 'Create or refresh the implementation specification for the current branch using a deterministic 3-step sequence: Clarify, Draft, and Skeptic Review.'
context: fork
agent: analyst
---

# Workflow: Spec-Driven Planning

This workflow enforces a disciplined "Chain-of-Command" to ensure the project is **Ready for Development**.

## STEP 1: Clarify & Distill (Analyst)

1. Use the `wip-analyst` subagent to clarify your intent and gather relevant project context.
2. The agent will read repo docs and inspect code to produce a `task-context.md` in the `.wip/<branch>/` directory.
3. **Verify** the artifact:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw verify-wip-file . task-context.md
   ```

## STEP 2: Draft Specification (Planner)

1. Switch to the `wip-planner` subagent.
2. Read `task-context.md` and draft a hardened `spec.md`.
3. **Spec Standard**:
   - **Actionable**: Every task has a file path and a specific literal action.
   - **Logical**: Tasks are ordered by dependency.
   - **Testable**: Acceptance Criteria (AC) use Given/When/Then.
4. **Verify** the write:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw verify-wip-file . spec.md
   ```

## STEP 3: Skeptic Review (Skeptic)

1. Switch to the `wip-skeptic` subagent.
2. Review the `spec.md` and `task-context.md` for blind spots and logic flaws.
3. Provide adversarial feedback and suggest mitigations.
4. **Finalize**: Set the stage and summarize.
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw set-stage . spec-reviewed
   ```

**HALT and Ask User**: "Please review the distilled context (`task-context.md`), the implementation specification (`spec.md`), and the skeptic's feedback. If you approve, we can proceed to implementation (`/wip-implement`), or let me know what needs to change."
