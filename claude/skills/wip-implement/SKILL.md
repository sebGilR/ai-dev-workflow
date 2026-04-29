---
name: wip-implement
description: 'Executes the implementation tasks defined in the spec.md using a deterministic, artifact-driven sequence.'
context: fork
agent: implementer
---

# Workflow: Spec-Driven Implementation

**Goal:** Turn the agreed-upon `spec.md` into hardened, working code.

## STEP 1: Load Specification

1. Strictly load only `spec.md` and `task-context.md` from the `.wip/<branch>/` directory.
2. DO NOT rely on previous chat history for the implementation details — the `spec.md` is the final contract.

## STEP 2: Task-by-Task Implementation

1. Execute the implementation tasks **exactly** in the order specified in `spec.md`.
2. Do not "optimize" the order or combine unrelated tasks.
3. For every change, append a concise update to `execution.md` describing what was changed and why.

## STEP 3: Self-Review & Verification (Tiered Autonomy)

1. After completing all tasks, run a self-verification pass.
2. Check your work against the **Acceptance Criteria (AC)** defined in `spec.md`.
3. **Safety First**: Before running any Bash command (tests, builds, etc.), check its safety:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw policy check . "<your command>"
   ```
4. **Autonomous Execution**:
   - If the verdict is **"allow"**, you may proceed with the command autonomously.
   - If the verdict is **"prompt"**, you MUST ask the user for a decision:
     - **Allow Once**: Run the command this time only.
     - **Deny**: Do not run the command.
     - **Different Action**: Ask the user what they would like to do instead (e.g. "I won't run rm, should I rename it instead?").
   - If the verdict is **"deny"** (hard block), inform the user why it was blocked.
5. If errors are found, fix them immediately and update `execution.md`.

## STEP 4: Finalize

1. Set the stage:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw set-stage . implementing
   ```
2. Summarize the completed work and any remaining tasks (if scope was split).

## RTK Usage (Token Compression)

When RTK is installed (`rtk init -g`), Bash commands are automatically compressed. This is essential for build/test output:

- `rtk cargo build` / `rtk cargo clippy`
- `rtk cargo test` — failures only
- `rtk npm test` / `rtk tsc`
- `rtk git diff` — condensed diff

If a command fails and the compressed output is insufficient, ask the user before running `rtk proxy <cmd>`.
