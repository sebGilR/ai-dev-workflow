---
name: wip-resume
description: Resume work from the current branch state without redoing the full workflow.
---

When this skill is used:

1. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw status .
```

2. Check whether `context-summary.md` exists in the current branch `.wip` directory:

```bash
~/.claude/ai-dev-workflow/bin/aidw context-summary .
```

   - If it exists, read `context-summary.md` instead of the individual files — it contains a compact summary of all relevant workflow state.
   - If it does not exist (command exits non-zero), fall back to reading `status.json`, `context.md`, `plan.md`, and `execution.md` individually.

3. Summarize the current stage, what is done, and the next recommended action.
4. Continue from the right stage instead of restarting the workflow.
