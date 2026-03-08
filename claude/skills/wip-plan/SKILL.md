---
name: wip-plan
description: Create or refresh the implementation plan for the current branch.
---

When this skill is used:

1. Ensure `.wip/<branch>/` exists; if not, run `/wip-start`.
2. Use the `wip-planner` subagent to inspect the repo, current branch, repo docs, and current `.wip` state.
3. Update `plan.md` with a practical, stepwise plan.
4. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . planned
```

5. Summarize the plan.
