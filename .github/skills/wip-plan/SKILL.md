---
name: wip-plan
description: Create or refresh the implementation plan for the current branch.
context: fork
agent: plan
---

When this skill is used:

1. Ensure `.wip/<branch>/` exists; if not, run `/wip-start`.
2. Use the `wip-planner` subagent to **ultrathink** and inspect the repo, current branch, repo docs, and current `.wip` state.
3. Update `plan.md` with a practical, stepwise plan.
4. Verify the write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-plan .
```

5. If verification passes, set the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . planned
```

6. Summarize the plan.
