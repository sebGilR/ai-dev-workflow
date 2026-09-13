---
name: wip-plan
description: 'Create or refresh the implementation specification for the current branch using a deterministic 3-step sequence: Clarify, Draft, and Skeptic Review.'
context: fork
agent: analyst
effort: high
---

# Workflow: Spec-Driven Planning

This workflow enforces a disciplined "Chain-of-Command" to ensure the project is **Ready for Development**.

## Model guidance

Planning and spec review benefit most from the frontier tier — mistakes
here are expensive to unwind later. `effort: high` above requests that tier
where the host honors SKILL.md frontmatter (Claude Code >= 2.1.259); on
older hosts it is silently ignored, so escalate manually if the plan is
unusually large or the spec review keeps missing things. `aidw model route
<tier>` prints the configured model name for the frontier/efficient tiers
(env `AIDW_FRONTIER_MODEL` / `AIDW_EFFICIENT_MODEL`). Respect an explicit
user model choice without re-prompting, and never claim a model switch
happened unless the host actually performed it.

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

**Approval gate — size/risk heuristic**: Decide whether to halt based on the spec just produced.

- **Skip the halt and proceed automatically to `/wip-implement`** when the plan is small/low-risk:
  the spec touches a single component or file cluster, the skeptic raised no High-severity
  concerns, and there is no irreversible or outward-facing action involved (no schema/data
  migration, no public API change, no infra/deploy step, no credential or destructive-operation
  handling). Note in your summary that you auto-proceeded and why (small/low-risk plan, no
  High-severity skeptic findings).
- **HALT and Ask User** otherwise — i.e. when the spec is large (spans many files/modules or
  multiple subsystems), the skeptic flagged a High-severity concern, or the plan involves an
  irreversible/outward-facing action: "Please review the distilled context (`task-context.md`),
  the implementation specification (`spec.md`), and the skeptic's feedback. If you approve, we
  can proceed to implementation (`/wip-implement`), or let me know what needs to change."
